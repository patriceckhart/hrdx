//go:build !windows

package updater

import (
	"os/exec"
	"syscall"
)

// detach puts the relauncher in its own session so it outlives the app.
func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
