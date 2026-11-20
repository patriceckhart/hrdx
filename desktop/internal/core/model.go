// Package core owns all live state of hrdx Desktop: workspaces, tabs,
// panes, split layouts, settings, and their persistence. One goroutine runs
// the model loop; every mutation arrives as a message, mirroring the
// contract Bubble Tea gives the TUI.
package core

import (
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
)

// Terminal is the pane surface the core drives. *term.Pane implements it;
// tests substitute a fake so no PTY is needed.
type Terminal interface {
	Write(data []byte)
	Resize(cols, rows int)
	Close()
	Detach()
	Running() bool
	Title() string
	ProgramStatus() (state, app string)
	HasScreenText(needle string) bool
	HasSpinner() bool
	ForegroundCommand() string
	PlainScreen() string
	KittyKeys() bool
	BracketedPaste() bool
	ModePreamble() string
	HolderSession() int64
	Updates() <-chan struct{}
}

var _ Terminal = (*term.Pane)(nil)

// floatPlacement positions an ephemeral floating pane over the split view.
type floatPlacement struct {
	anchor    string
	widthPct  int
	heightPct int
}

type pane struct {
	id      int
	name    string
	kind    string
	cwd     string   // start directory when it differs from the workspace
	args    []string // extra agent arguments entered at workspace creation
	session int64    // holder session id, 0 when not holder-backed
	resume  bool     // relaunch with the agent's resume arguments

	term     Terminal
	running  bool
	starting bool
	failure  string
	floating *floatPlacement

	// durable carries a zot continuous binding the desktop app does not
	// implement yet. The pane is shown as a placeholder, never started, and
	// written back exactly as it was read.
	durable *state.DurableRef

	cols, rows int // last size requested by the frontend
	visible    bool
}

type tab struct {
	name     string
	panes    []*pane
	layout   *layout.Node[*pane]
	selected int
}

type space struct {
	groupPath []string
	name      string
	cwd       string
	tabs      []*tab
	active    int
	folded    bool

	// Nested repositories hidden from the sidebar, as clean relative paths.
	hiddenSubrepos []string

	// Fields the desktop app does not implement in v1, preserved for the TUI.
	host       string
	continuous []state.DurableRef
}

func (s *space) tab() *tab {
	if len(s.tabs) == 0 {
		s.tabs = []*tab{{}}
	}
	s.active = clamp(s.active, 0, len(s.tabs)-1)
	return s.tabs[s.active]
}

func clamp(value, low, high int) int {
	if high < low {
		return low
	}
	return min(max(value, low), high)
}

func clampFloat(value, low, high float64) float64 {
	return min(max(value, low), high)
}
