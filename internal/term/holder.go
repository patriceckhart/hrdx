package term

import (
	"fmt"
	"path/filepath"

	"github.com/patriceckhart/hrdx/internal/holder"
)

// HolderClient is the subset of holder.Client a holder-backed pane needs to
// start or reattach. holder.Client implements it.
type HolderClient interface {
	SessionHost
	List() ([]holder.SessionInfo, error)
	Start(command string, args []string, cwd string, env []string, cols, rows int) (int64, error)
	Attach(session int64, cols, rows int, sink func([]byte)) (bool, error)
}

// StartHolderPane starts (or reattaches to) a session in the holder and
// wires its output into a fresh pane terminal. tee, when not nil, receives
// every output chunk after the pane's emulator consumed it.
func StartHolderPane(client HolderClient, reattach int64, command string, args []string, cwd string, cols, rows int, tee func([]byte)) (*Pane, error) {
	session := reattach
	if session != 0 {
		sessions, err := client.List()
		if err != nil {
			return nil, fmt.Errorf("inspect holder sessions: %w", err)
		}
		if !HolderSessionMatchesWorkspace(sessions, session, cwd) {
			// State and holder writes are independent. After an interrupted
			// shutdown, a persisted id can refer to a session from another
			// workspace. Never attach that PTY to the wrong workspace.
			session = 0
		}
	}
	if session == 0 {
		started, err := client.Start(command, args, cwd, PaneEnv(), cols, rows)
		if err != nil {
			return nil, err
		}
		session = started
	}
	pane := NewHolderPane(client, session, cols, rows)
	sink := pane.Feed
	if tee != nil {
		sink = func(data []byte) {
			pane.Feed(data)
			tee(data)
		}
	}
	running, err := client.Attach(session, cols, rows, sink)
	if err != nil {
		if reattach != 0 {
			// The session vanished (holder restarted): start fresh.
			return StartHolderPane(client, 0, command, args, cwd, cols, rows, tee)
		}
		return nil, err
	}
	if !running {
		pane.MarkExited()
	}
	return pane, nil
}

// HolderSessionMatchesWorkspace reports whether session exists and runs in
// cwd.
func HolderSessionMatchesWorkspace(sessions []holder.SessionInfo, session int64, cwd string) bool {
	wanted := filepath.Clean(cwd)
	for _, current := range sessions {
		if current.ID == session {
			return filepath.Clean(current.CWD) == wanted
		}
	}
	return false
}
