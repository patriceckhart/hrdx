package continuous

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// pidFile records the managed host process so hrdx can stop the service it
// started. It lives in the store directory and never names another store.
const pidFile = "hrdx-host.pid"

// stoppedFile marks an intentional stop: automatic startup stays off until
// the user starts the host again or explicitly asks for new durable work.
const stoppedFile = "hrdx-host.stopped"

// Probe reports whether a host answers at the profile's address.
func Probe(ctx context.Context, profile Profile) error {
	token, err := profile.Token()
	if err != nil {
		return err
	}
	dial, err := DialerFor(profile)
	if err != nil {
		return err
	}
	client, err := DialWith(ctx, dial, profile.Address, token)
	if err != nil {
		return err
	}
	defer client.Close()
	_, err = Host{client}.Status(ctx)
	return err
}

// StartManaged starts the profile's managed host unless one already answers.
// zot's store writer lock is the authority against duplicate writers: a
// second concurrent start exits on the lock, and the probe then finds the
// winner. The host is detached so it outlives the TUI.
func StartManaged(ctx context.Context, profile Profile, zotBin string) error {
	if !profile.Managed {
		return errors.New("host is not managed by hrdx")
	}
	if !LocalSupported {
		return fmt.Errorf("managed continuous hosts: %w on this platform", ErrUnsupported)
	}
	if err := CheckAddress(profile.Address); err != nil {
		return err
	}
	if Probe(ctx, profile) == nil {
		_ = os.Remove(filepath.Join(profile.Store, stoppedFile))
		return nil
	}
	if zotBin == "" {
		zotBin = "zot"
	}
	path, err := exec.LookPath(zotBin)
	if err != nil {
		return fmt.Errorf("zot binary not found: %w", err)
	}
	if err := os.MkdirAll(profile.Store, 0o700); err != nil {
		return fmt.Errorf("create host store: %w", err)
	}
	logPath := filepath.Join(profile.Store, "hrdx-host.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open host log: %w", err)
	}
	defer logFile.Close()
	cmd := exec.Command(path, "continuous", "serve", "--store", profile.Store, "--socket", profile.Address, "--cwd", profile.CWD)
	cmd.Dir = profile.CWD
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start continuous host: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	_ = os.WriteFile(filepath.Join(profile.Store, pidFile), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	_ = os.Remove(filepath.Join(profile.Store, stoppedFile))

	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			// Lost a start race: another process owns the store and answers.
			if Probe(ctx, profile) == nil {
				return nil
			}
			return fmt.Errorf("continuous host exited during startup: %s", lastLogLine(logPath))
		case <-tick.C:
			probeCtx, cancel := context.WithTimeout(ctx, time.Second)
			err := Probe(probeCtx, profile)
			cancel()
			if err == nil {
				return nil
			}
		case <-deadline.C:
			return fmt.Errorf("continuous host did not answer within 20s: %s", lastLogLine(logPath))
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// StopManaged interrupts the managed host hrdx started and records the
// intentional stop. Interrupted work stays recoverable in the store.
func StopManaged(profile Profile) error {
	if !profile.Managed {
		return errors.New("host is not managed by hrdx; stop it where it was started")
	}
	data, err := os.ReadFile(filepath.Join(profile.Store, pidFile))
	if err != nil {
		return errors.New("no managed host process recorded")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return errors.New("managed host pid file is invalid")
	}
	// Only signal a recorded pid while the host still answers, so a stale
	// pid reused by an unrelated process is never interrupted.
	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Probe(probeCtx, profile); err != nil {
		_ = os.Remove(filepath.Join(profile.Store, pidFile))
		return errors.New("managed host is not running")
	}
	_ = os.WriteFile(filepath.Join(profile.Store, stoppedFile), nil, 0o600)
	if err := interrupt(pid); err != nil {
		return fmt.Errorf("stop continuous host: %w", err)
	}
	_ = os.Remove(filepath.Join(profile.Store, pidFile))
	return nil
}

// IntentionallyStopped reports whether the user stopped this managed host.
func IntentionallyStopped(profile Profile) bool {
	if !profile.Managed {
		return false
	}
	_, err := os.Stat(filepath.Join(profile.Store, stoppedFile))
	return err == nil
}

func lastLogLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "no host log"
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := strings.TrimSpace(lines[index])
		if line != "" {
			if len(line) > 200 {
				line = line[:200]
			}
			return line
		}
	}
	return "host log is empty"
}
