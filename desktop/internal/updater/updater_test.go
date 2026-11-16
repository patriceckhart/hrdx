package updater

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetForMatchesReleaseNames(t *testing.T) {
	cases := map[string]Asset{
		"darwin":  {Archive: "hrdx-desktop_1.2.3_macos_universal.dmg", Checksums: "desktop-checksums-macos.txt"},
		"windows": {Archive: "hrdx-desktop_1.2.3_windows_amd64.zip", Checksums: "desktop-checksums-windows.txt", Binary: "hrdx-desktop.exe"},
		"linux":   {Archive: "hrdx-desktop_1.2.3_linux_amd64.tar.gz", Checksums: "desktop-checksums-linux.txt", Binary: "hrdx-desktop"},
	}
	for goos, want := range cases {
		got, err := AssetFor("1.2.3", goos)
		if err != nil || got != want {
			t.Fatalf("%s: got %+v, %v", goos, got, err)
		}
	}
	if _, err := AssetFor("1.2.3", "plan9"); err == nil {
		t.Fatal("unsupported OS should fail")
	}
}

func TestBundlePath(t *testing.T) {
	exe := filepath.Join("/Applications", "hrdx.app", "Contents", "MacOS", "hrdx-desktop")
	got, err := BundlePath(exe)
	if err != nil || got != filepath.Join("/Applications", "hrdx.app") {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := BundlePath(filepath.Join("/usr", "local", "bin", "hrdx-desktop")); err == nil {
		t.Fatal("binary outside a bundle should fail")
	}
}

func TestCheckSkipsDevBuild(t *testing.T) {
	if info := Check(t.Context(), t.TempDir(), "0.0.0"); info.Available {
		t.Fatalf("dev build reported update: %+v", info)
	}
}

func makeBundle(t *testing.T, dir, marker string) string {
	t.Helper()
	bundle := filepath.Join(dir, "hrdx.app")
	if err := os.MkdirAll(filepath.Join(bundle, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "Contents", "marker"), []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	return bundle
}

func readMarker(t *testing.T, bundle string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(bundle, "Contents", "marker"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSwapBundleReplacesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	bundle := makeBundle(t, dir, "old")
	err := SwapBundle(bundle, func(staged string) error {
		return os.Rename(makeBundle(t, t.TempDir(), "new"), staged)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := readMarker(t, bundle); got != "new" {
		t.Fatalf("marker = %q", got)
	}
	for _, leftover := range []string{bundle + ".new", bundle + ".old"} {
		if _, err := os.Stat(leftover); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", leftover)
		}
	}
}

func TestSwapBundleKeepsOldOnFillFailure(t *testing.T) {
	dir := t.TempDir()
	bundle := makeBundle(t, dir, "old")
	err := SwapBundle(bundle, func(staged string) error {
		_ = os.MkdirAll(staged, 0o755)
		return errors.New("copy failed")
	})
	if err == nil {
		t.Fatal("want error")
	}
	if got := readMarker(t, bundle); got != "old" {
		t.Fatalf("marker = %q", got)
	}
	if _, err := os.Stat(bundle + ".new"); !os.IsNotExist(err) {
		t.Fatal("staged bundle left behind")
	}
}

func TestExtractFileZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{"README.md": "doc", "bin/hrdx-desktop.exe": "exe"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	_ = f.Close()
	dst := filepath.Join(dir, "out.exe")
	if err := ExtractFile(archive, "hrdx-desktop.exe", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "exe" {
		t.Fatalf("content = %q", b)
	}
	if err := ExtractFile(archive, "missing", dst); err == nil {
		t.Fatal("missing entry should fail")
	}
}

func TestExtractFileTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "a.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	body := "linux binary"
	_ = tw.WriteHeader(&tar.Header{Name: "hrdx-desktop", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	dst := filepath.Join(dir, "out")
	if err := ExtractFile(archive, "hrdx-desktop", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != body {
		t.Fatalf("content = %q", b)
	}
}

func TestRelaunchCommandPassesTargetAsArgument(t *testing.T) {
	target := `/tmp/odd "name"; rm -rf x/hrdx.app`
	for _, goos := range []string{"darwin", "linux", "windows"} {
		cmd := RelaunchCommand(goos, 42, target)
		args := cmd.Args
		if args[len(args)-1] != target || args[len(args)-2] != "42" {
			t.Fatalf("%s: args = %q", goos, args)
		}
		if strings.Contains(strings.Join(args[:len(args)-2], " "), target) {
			t.Fatalf("%s: target interpolated into script", goos)
		}
	}
}
