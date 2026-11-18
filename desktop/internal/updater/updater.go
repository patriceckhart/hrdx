// Package updater checks GitHub releases for a newer hrdx Desktop build,
// installs it in place, and relaunches the app. Release lookup, downloads,
// checksum verification, and Windows executable replacement are shared with
// the CLI through internal/update.
package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/update"
)

// Info is the update state shown to the webview.
type Info struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`
}

// Asset names the release files for one platform, matching release.yml.
type Asset struct {
	Archive   string
	Checksums string
	// Binary is the executable inside zip and tar.gz archives. The macOS
	// dmg holds hrdx.app instead.
	Binary string
}

// AssetFor returns the release asset for version on goos.
func AssetFor(version, goos string) (Asset, error) {
	switch goos {
	case "darwin":
		return Asset{Archive: "hrdx-desktop_" + version + "_macos_universal.dmg", Checksums: "desktop-checksums-macos.txt"}, nil
	case "windows":
		return Asset{Archive: "hrdx-desktop_" + version + "_windows_amd64.zip", Checksums: "desktop-checksums-windows.txt", Binary: "hrdx-desktop.exe"}, nil
	case "linux":
		return Asset{Archive: "hrdx-desktop_" + version + "_linux_amd64.tar.gz", Checksums: "desktop-checksums-linux.txt", Binary: "hrdx-desktop"}, nil
	}
	return Asset{}, fmt.Errorf("no desktop build for %s", goos)
}

// Supported reports whether this platform has a release build to install.
func Supported() bool {
	if runtime.GOOS == "darwin" {
		return true
	}
	return runtime.GOARCH == "amd64" && (runtime.GOOS == "windows" || runtime.GOOS == "linux")
}

// Check asks GitHub for the latest release. Dev builds and unsupported
// platforms never report an update.
func Check(ctx context.Context, cacheDir, version string) Info {
	if !Supported() {
		return Info{}
	}
	got := update.Check(ctx, cacheDir, version)
	info := Info{Current: got.Current, Latest: got.Latest, Available: got.Available, URL: got.URL}
	// A release is published as soon as the CLI builds are up, while the
	// desktop builds may still be running. Offer it only once this
	// platform's files exist, so "Update now" cannot hit a 404.
	if info.Available && !assetsReady(ctx, info, runtime.GOOS) {
		info.Available = false
	}
	return info
}

// assetHead checks that a release file exists. Tests replace it.
var assetHead = func(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// assetsReady reports whether the archive and checksums for goos are
// uploaded to info's release.
func assetsReady(ctx context.Context, info Info, goos string) bool {
	asset, err := AssetFor(info.Latest, goos)
	if err != nil || info.URL == "" {
		return false
	}
	base := update.DownloadBase(info.URL)
	return assetHead(ctx, base+"/"+asset.Checksums) && assetHead(ctx, base+"/"+asset.Archive)
}

// Install downloads, verifies, and installs info.Latest over the running
// app. It returns the path to relaunch: the .app bundle on macOS, the
// executable elsewhere.
func Install(ctx context.Context, info Info) (string, error) {
	if !info.Available || info.Latest == "" || info.URL == "" {
		return "", errors.New("no update available")
	}
	asset, err := AssetFor(info.Latest, runtime.GOOS)
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve app path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	tmp, err := os.MkdirTemp("", "hrdx-desktop-update-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	base := update.DownloadBase(info.URL)
	sums := filepath.Join(tmp, asset.Checksums)
	if err := update.Download(ctx, base+"/"+asset.Checksums, sums); err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	archive := filepath.Join(tmp, asset.Archive)
	if err := update.Download(ctx, base+"/"+asset.Archive, archive); err != nil {
		return "", fmt.Errorf("download update: %w", err)
	}
	if err := update.VerifyChecksum(sums, asset.Archive, archive); err != nil {
		return "", err
	}

	if runtime.GOOS == "darwin" {
		bundle, err := BundlePath(exe)
		if err != nil {
			return "", err
		}
		return bundle, installDMG(ctx, archive, tmp, bundle)
	}
	newBin := filepath.Join(tmp, asset.Binary)
	if err := ExtractFile(archive, asset.Binary, newBin); err != nil {
		return "", fmt.Errorf("extract update: %w", err)
	}
	if err := update.ReplaceExecutable(exe, newBin); err != nil {
		return "", fmt.Errorf("replace app: %w", err)
	}
	return exe, nil
}

// BundlePath returns the .app directory that contains exe.
func BundlePath(exe string) (string, error) {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(bundle, ".app") {
		return "", fmt.Errorf("not running from an app bundle: %s", exe)
	}
	return bundle, nil
}

// installDMG mounts the disk image and swaps the bundle beside the old one,
// so a failed copy leaves the installed app untouched.
func installDMG(ctx context.Context, dmg, tmp, bundle string) error {
	mount := filepath.Join(tmp, "mnt")
	if err := os.Mkdir(mount, 0o755); err != nil {
		return err
	}
	if out, err := exec.CommandContext(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mount, dmg).CombinedOutput(); err != nil {
		return fmt.Errorf("mount update: %v: %s", err, strings.TrimSpace(string(out)))
	}
	defer func() { _ = exec.Command("hdiutil", "detach", "-force", mount).Run() }()

	source := filepath.Join(mount, "hrdx.app")
	if st, err := os.Stat(source); err != nil || !st.IsDir() {
		return errors.New("update image does not contain hrdx.app")
	}
	return SwapBundle(bundle, func(staged string) error {
		if out, err := exec.CommandContext(ctx, "ditto", source, staged).CombinedOutput(); err != nil {
			return fmt.Errorf("copy app: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	})
}

// SwapBundle stages a new bundle with fill, then replaces bundle with it.
// The old bundle is restored when the final rename fails.
func SwapBundle(bundle string, fill func(staged string) error) error {
	staged := bundle + ".new"
	backup := bundle + ".old"
	_ = os.RemoveAll(staged)
	_ = os.RemoveAll(backup)
	if err := fill(staged); err != nil {
		_ = os.RemoveAll(staged)
		return err
	}
	if err := os.Rename(bundle, backup); err != nil {
		_ = os.RemoveAll(staged)
		return fmt.Errorf("move old app aside (is the app in a writable folder such as Applications?): %w", err)
	}
	if err := os.Rename(staged, bundle); err != nil {
		if rollback := os.Rename(backup, bundle); rollback != nil {
			return fmt.Errorf("install app: %v, restore also failed: %v", err, rollback)
		}
		return fmt.Errorf("install app (old version restored): %w", err)
	}
	_ = os.RemoveAll(backup)
	return nil
}

// ExtractFile copies the file named name from a .zip or .tar.gz archive to
// dst, ignoring directories in the archive path.
func ExtractFile(archive, name, dst string) error {
	if strings.EqualFold(filepath.Ext(archive), ".zip") {
		r, err := zip.OpenReader(archive)
		if err != nil {
			return err
		}
		defer r.Close()
		for _, f := range r.File {
			if f.FileInfo().IsDir() || filepath.Base(filepath.FromSlash(f.Name)) != name {
				continue
			}
			in, err := f.Open()
			if err != nil {
				return err
			}
			defer in.Close()
			return writeExecutable(dst, in)
		}
		return fmt.Errorf("archive does not contain %s", name)
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("archive does not contain %s", name)
		}
		if err != nil {
			return err
		}
		if header.Typeflag == tar.TypeReg && filepath.Base(header.Name) == name {
			return writeExecutable(dst, tr)
		}
	}
}

func writeExecutable(dst string, r io.Reader) error {
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// Relaunch starts a detached helper that waits for this process to exit and
// then opens target. The caller quits afterwards, which detaches panes from
// the session holder so the new version reattaches them.
func Relaunch(target string) error {
	cmd := RelaunchCommand(runtime.GOOS, os.Getpid(), target)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start relauncher: %w", err)
	}
	return cmd.Process.Release()
}

// RelaunchCommand builds the wait-then-open helper. The target is passed as
// an argument, never interpolated into the script.
func RelaunchCommand(goos string, pid int, target string) *exec.Cmd {
	id := strconv.Itoa(pid)
	switch goos {
	case "windows":
		script := "Wait-Process -Id $args[0] -ErrorAction SilentlyContinue; Start-Process -FilePath $args[1]"
		return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script, id, target)
	case "darwin":
		return exec.Command("/bin/sh", "-c", `while kill -0 "$0" 2>/dev/null; do sleep 0.2; done; exec open "$1"`, id, target)
	}
	return exec.Command("/bin/sh", "-c", `while kill -0 "$0" 2>/dev/null; do sleep 0.2; done; exec "$1" >/dev/null 2>&1`, id, target)
}
