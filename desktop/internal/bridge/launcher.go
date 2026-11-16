package bridge

import (
	"github.com/patriceckhart/hrdx/desktop/internal/core"
	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/term"
)

// HolderLauncher starts panes as holder sessions that survive the app.
type HolderLauncher struct {
	Client *holder.Client
}

// Launch starts or reattaches a holder session.
func (l HolderLauncher) Launch(spec core.LaunchSpec, tee func([]byte)) (core.Terminal, error) {
	return term.StartHolderPane(l.Client, spec.Reattach, spec.Command, spec.Args, spec.CWD, spec.Cols, spec.Rows, tee)
}

// Persistent is true: holder panes detach on quit.
func (HolderLauncher) Persistent() bool { return true }

// Sessions lists the holder's session ids.
func (l HolderLauncher) Sessions() []int64 {
	sessions, err := l.Client.List()
	if err != nil {
		return nil
	}
	ids := make([]int64, 0, len(sessions))
	for _, session := range sessions {
		ids = append(ids, session.ID)
	}
	return ids
}

// Kill terminates a session.
func (l HolderLauncher) Kill(session int64) { l.Client.Kill(session) }

// Close detaches from the holder.
func (l HolderLauncher) Close() { l.Client.Close() }

// LocalLauncher starts panes on PTYs owned by the app; they die with it.
type LocalLauncher struct{}

// Launch starts a local PTY.
func (LocalLauncher) Launch(spec core.LaunchSpec, tee func([]byte)) (core.Terminal, error) {
	return term.StartTee(spec.Command, spec.Args, spec.CWD, spec.Cols, spec.Rows, tee)
}

// Persistent is false.
func (LocalLauncher) Persistent() bool { return false }

// Sessions is nil: there is no holder.
func (LocalLauncher) Sessions() []int64 { return nil }

// Kill is a no-op without a holder.
func (LocalLauncher) Kill(int64) {}

// Close is a no-op.
func (LocalLauncher) Close() {}
