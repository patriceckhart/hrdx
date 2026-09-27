package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/state"
)

// stopTimeout bounds every management call. It is short so a holder that is
// busy serving an attached terminal fails fast instead of hanging.
const stopTimeout = 2 * time.Second

// runStop stops the background session holder without starting the TUI. It
// takes no options: it targets the default state location and, when sessions
// are still held, asks before killing them.
func runStop(args []string, in io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: hrdx stop")
		fmt.Fprintln(stderr, "hrdx stop takes no arguments; it prompts before killing held sessions.")
		return 2
	}
	return stopHolder(state.DefaultPath(), in, stdout, stderr)
}

// stopHolder stops the session holder for one state location. The prompt
// defaults to no, so a stray Enter or an end of input never discards running
// shells and agents. Exit 0 means no holder is left running: it was already
// stopped, or it was stopped just now.
func stopHolder(statePath string, in io.Reader, stdout, stderr io.Writer) int {
	if statePath == "" {
		fmt.Fprintln(stderr, "hrdx stop: no state location is configured")
		return 1
	}

	manager, err := dialHolderManager(statePath)
	if err != nil {
		if errors.Is(err, holder.ErrNoHolder) {
			fmt.Fprintln(stdout, "No session holder is running.")
			return 0
		}
		fmt.Fprintln(stderr, "hrdx stop: "+err.Error())
		if errors.Is(err, holder.ErrHolderBusy) {
			fmt.Fprintln(stderr, "Quit the attached hrdx terminal (ctrl+b q), then retry.")
		}
		return 1
	}
	defer manager.Close()

	sessions, err := manager.Sessions()
	if err != nil {
		fmt.Fprintln(stderr, "hrdx stop: the holder is running but its sessions could not be listed:", err)
		return 1
	}
	if len(sessions) > 0 && !confirmStop(in, stderr, sessions) {
		fmt.Fprintln(stderr, "Aborted. The session holder is still running.")
		return 1
	}

	if err := manager.Stop(); err != nil {
		fmt.Fprintln(stderr, "hrdx stop: "+err.Error())
		return 1
	}
	if len(sessions) > 0 {
		fmt.Fprintf(stdout, "Session holder stopped; killed %d held session(s).\n", len(sessions))
	} else {
		fmt.Fprintln(stdout, "Session holder stopped.")
	}
	return 0
}

// confirmStop lists the held sessions and asks whether to kill them. Only a
// lone y/Y proceeds; every other answer, including end of input, declines.
func confirmStop(in io.Reader, stderr io.Writer, sessions []holder.SessionInfo) bool {
	fmt.Fprintf(stderr, "The session holder is running %d held session(s):\n", len(sessions))
	for _, session := range sessions {
		status := "exited"
		if session.Running {
			status = "running"
		}
		fmt.Fprintf(stderr, "  %d  %s  %s  %s\n", session.ID, status, session.Command, session.CWD)
	}
	fmt.Fprint(stderr, "Stop them too? [y/N] ")
	answer, _ := bufio.NewReader(in).ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

// dialHolderManager mirrors the running instance's socket selection: the
// legacy holder socket beside the state file first, then the runtime
// fallback. A busy holder is reported as-is rather than retried elsewhere.
func dialHolderManager(statePath string) (*holder.Manager, error) {
	manager, err := holder.DialManager(legacySocketPaths(statePath).holder, stopTimeout)
	if err == nil || errors.Is(err, holder.ErrHolderBusy) {
		return manager, err
	}
	sockets, pathErr := runtimeSocketPaths(statePath, runtime.GOOS, os.Getenv("XDG_RUNTIME_DIR"))
	if pathErr != nil {
		return nil, err
	}
	return holder.DialManager(sockets.holder, stopTimeout)
}
