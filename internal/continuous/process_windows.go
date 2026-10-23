//go:build windows

package continuous

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts the managed host in its own process group without a
// console, so closing the TUI's console never ends it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
	}
}

// interrupt ends the host. A detached process has no console to deliver
// Ctrl+C to, so it is terminated; zot treats this like a crash and keeps
// interrupted work recoverable in its store.
func interrupt(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}
