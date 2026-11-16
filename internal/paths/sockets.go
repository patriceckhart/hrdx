// Package paths resolves the socket locations and default shell shared by
// every hrdx frontend. The TUI and the desktop app must agree on these so
// either can attach to the other's session holder.
package paths

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Sockets names the control API socket and the session holder socket of
// one hrdx instance.
type Sockets struct {
	API, Holder string
}

// Legacy returns the sockets beside the state file.
func Legacy(statePath string) Sockets {
	dir := filepath.Dir(statePath)
	return Sockets{filepath.Join(dir, "hrdx.sock"), filepath.Join(dir, "holder.sock")}
}

// Runtime uses the absolute state path as the instance identity so
// custom --state files do not share a control socket or a holder process.
func Runtime(statePath, goos, runtimeDir string) (Sockets, error) {
	if goos == "windows" || runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return Sockets{}, fmt.Errorf("no usable XDG_RUNTIME_DIR")
	}
	runtimeDir = filepath.Clean(runtimeDir)
	info, err := os.Stat(runtimeDir)
	if err != nil || !info.IsDir() {
		return Sockets{}, fmt.Errorf("XDG_RUNTIME_DIR is not a directory")
	}
	dir := filepath.Join(runtimeDir, "hrdx")
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return Sockets{}, err
	}
	info, err = os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return Sockets{}, fmt.Errorf("hrdx runtime socket directory must be private")
	}
	absolute, err := filepath.Abs(statePath)
	if err != nil {
		return Sockets{}, err
	}
	id := sha256.Sum256([]byte(absolute))
	suffix := fmt.Sprintf("%x", id[:8])
	paths := Sockets{
		filepath.Join(dir, "hrdx-"+suffix+".sock"),
		filepath.Join(dir, "holder-"+suffix+".sock"),
	}
	if !socketPathFits(paths.API, goos) || !socketPathFits(paths.Holder, goos) {
		return Sockets{}, fmt.Errorf("XDG_RUNTIME_DIR is too long for Unix sockets")
	}
	return paths, nil
}

// Select retains the existing sockets whenever the state directory can host
// them. On fallback, keep any existing holder socket so its sessions can
// reattach, and do not start a second API beside an existing live one.
func Select(statePath, goos, runtimeDir string, probe func(string) error) Sockets {
	legacy := Legacy(statePath)
	if goos == "windows" || runtimeDir == "" || !filepath.IsAbs(runtimeDir) {
		return legacy
	}
	dir := filepath.Dir(statePath)
	if socketPathFits(legacy.API, goos) && socketPathFits(legacy.Holder, goos) {
		if err := os.MkdirAll(dir, 0755); err == nil {
			if err := probe(dir); err == nil {
				return legacy
			}
		}
	}
	fallback, err := Runtime(statePath, goos, runtimeDir)
	if err != nil {
		return legacy
	}
	// A probe connection would detach the holder's current TUI client.
	// Preserve its socket by pathname and let ConnectOrSpawn handle staleness.
	if info, err := os.Lstat(legacy.Holder); err == nil && info.Mode()&os.ModeSocket != 0 {
		fallback.Holder = legacy.Holder
	}
	if conn, err := net.DialTimeout("unix", legacy.API, 200*time.Millisecond); err == nil {
		conn.Close()
		fallback.API = legacy.API
	}
	return fallback
}

// Darwin's sockaddr_un holds 104 bytes including the terminator. Linux
// accepts 107 pathname bytes. Other Unix platforms use the conservative size.
func socketPathFits(path, goos string) bool {
	limit := 103
	if goos == "linux" {
		limit = 107
	}
	return len(path) <= limit
}

// ProbeUnixSocket tests socket support without touching the live socket names.
func ProbeUnixSocket(dir string) error {
	file, err := os.CreateTemp(dir, ".hrdx-")
	if err != nil {
		return err
	}
	path := file.Name()
	_ = file.Close()
	if err := os.Remove(path); err != nil {
		return err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	return listener.Close()
}
