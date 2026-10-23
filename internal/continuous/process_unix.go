//go:build !windows

package continuous

import (
	"os/exec"
	"syscall"
)

// detach puts the managed host into its own session so closing the TUI's
// terminal never signals it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// interrupt asks the host to shut down gracefully, like Ctrl+C.
func interrupt(pid int) error {
	return syscall.Kill(pid, syscall.SIGINT)
}
