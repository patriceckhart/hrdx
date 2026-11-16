package paths

import (
	"os"
	"os/exec"
	"runtime"
)

// DefaultShell picks the shell for shell panes when --shell is unset.
func DefaultShell() string {
	return DefaultShellFor(runtime.GOOS, os.Getenv, exec.LookPath)
}

// DefaultShellFor takes its platform dependencies as arguments so the Windows
// selection rules can be tested on every host.
func DefaultShellFor(goos string, getenv func(string) string, lookPath func(string) (string, error)) string {
	shell := getenv("SHELL")
	if goos != "windows" {
		if shell != "" {
			return shell
		}
		return "/bin/sh"
	}

	// Git Bash exports SHELL=/usr/bin/bash, but that MSYS path cannot be
	// launched by a native Windows hrdx process. Keep SHELL when Windows can
	// actually resolve it (including an absolute Git for Windows path).
	if shell != "" {
		if _, err := lookPath(shell); err == nil {
			return shell
		}
	}
	if comspec := getenv("COMSPEC"); comspec != "" {
		return comspec
	}
	return "powershell.exe"
}
