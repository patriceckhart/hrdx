package core

import (
	"errors"
	"sync"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/state"
)

// LaunchSpec describes one pane process to start or reattach.
type LaunchSpec struct {
	Command  string
	Args     []string
	CWD      string
	Cols     int
	Rows     int
	Reattach int64 // holder session to reattach, 0 starts fresh
}

// Launcher starts pane processes. tee receives every output chunk after the
// pane's emulator consumed it; it is called from I/O goroutines and must not
// block.
type Launcher interface {
	Launch(spec LaunchSpec, tee func([]byte)) (Terminal, error)
	// Persistent reports whether launched panes survive the app (holder).
	Persistent() bool
	// Sessions lists live holder session ids, nil when not holder-backed.
	Sessions() []int64
	// Kill terminates a holder session by id.
	Kill(session int64)
	// Close detaches from the holder, leaving sessions running.
	Close()
}

// Hooks connect the core to the bridge and the platform. Every hook is
// called on the model loop and must not block.
type Hooks struct {
	// Emit receives a fresh view at most once per frame interval.
	Emit func(View)
	// Output receives pane output; called from I/O goroutines.
	Output func(paneID int, data []byte)
	// PaneGone tells the bridge to drop per-pane buffers.
	PaneGone func(paneID int)
	// Notify reports an agent turn that finished.
	Notify func(Notification)
	// Attention requests the platform attention indicator.
	Attention func()
	// PlaySound plays a notification sound by kind.
	PlaySound func(kind string)
	// Badge receives the number of panes waiting for the user whenever it
	// changes.
	Badge func(count int)
}

// Notification is the payload of the notify event.
type Notification struct {
	Kind      string `json:"kind"` // "finished" or "attention"
	PaneID    int    `json:"pane_id"`
	Workspace string `json:"workspace"`
	Pane      string `json:"pane"`   // display name, e.g. "Claude Code"
	Unseen    bool   `json:"unseen"` // the pane was not in front of the user
	Banner    bool   `json:"banner"` // the notification setting is on
}

// Config is the startup configuration assembled from flags.
type Config struct {
	Version      string
	DefaultAgent string
	AgentBins    map[string]string
	ZotArgs      []string
	Shell        string
	ShellArgs    []string
	StatePath    string // empty disables persistence
	Fresh        bool
	// FrameInterval bounds view emission; zero selects 16 ms.
	FrameInterval time.Duration
	// SaveDelay debounces state writes; zero selects 250 ms.
	SaveDelay time.Duration
	// FinishDelay debounces busy to idle before a turn counts as finished;
	// zero selects 1500 ms, matching the TUI.
	FinishDelay time.Duration
}

// ErrStopped reports a call after the loop stopped.
var ErrStopped = errors.New("hrdx desktop is shutting down")

// Core owns the model. Every exported method is safe from any goroutine.
type Core struct {
	msgs    chan func()
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once

	m *model
}

// model is the live state. Only the loop goroutine touches it.
type model struct {
	config   Config
	launcher Launcher
	hooks    Hooks
	events   *api.Broadcaster
	prefs    prefs.Prefs
	post     func(func())

	spaces   []*space
	selected int
	nextID   int

	// Shared settings, persisted in state.json.
	disabled         map[string]bool
	soundOn          bool
	soundKind        string
	notifyOn         bool
	themeName        string
	autoCopy         bool
	sidebarCollapsed bool
	splitDirection   string
	foldEnabled      bool
	keyOverrides     map[string]string
	problems         []string

	// Nested git repositories, shared with the TUI through state.json.
	subrepoDepth     int // search depth, 0 = off
	subrepoLastDepth int // depth restored when the setting is switched back on
	subrepoGen       int // invalidates scans started before a setting change
	subrepos         map[string]subrepoCache

	// Preserved untouched for the TUI.
	tuiSidebarWidth int
	durableMuted    []string
	unknownDisabled []string

	customMenus []api.MenuRegister

	// blocked is set while another frontend owns the holder. It is part of
	// the view so a frontend that mounts late still shows the dialog.
	blocked string

	wasBusy       map[int]bool
	finishSeq     map[int]uint64
	paneAttention map[int]bool
	windowFocused bool

	dirty     bool
	badge     int
	saveDue   bool
	saveTimer *time.Timer
	quitting  bool
}

// New builds a core from saved state. It does not start any process; call
// Run on a dedicated goroutine, then Start.
func New(config Config, saved state.State, launcher Launcher, hooks Hooks, events *api.Broadcaster, desktopPrefs prefs.Prefs, cwds []string) *Core {
	if config.FrameInterval <= 0 {
		config.FrameInterval = 16 * time.Millisecond
	}
	if config.SaveDelay <= 0 {
		config.SaveDelay = 250 * time.Millisecond
	}
	if config.FinishDelay <= 0 {
		config.FinishDelay = 1500 * time.Millisecond
	}
	if events == nil {
		events = api.NewBroadcaster()
	}
	c := &Core{
		msgs:    make(chan func(), 1024),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	m := newModel(config, saved, launcher, hooks, events, desktopPrefs)
	m.post = c.post
	for _, cwd := range cwds {
		m.openWorkspace(cwd)
	}
	c.m = m
	return c
}

// Run executes the model loop until Stop. It returns after the loop exited.
func (c *Core) Run() {
	defer close(c.stopped)
	frame := time.NewTicker(c.m.config.FrameInterval)
	defer frame.Stop()
	busy := time.NewTicker(150 * time.Millisecond)
	defer busy.Stop()
	subrepoTicker := time.NewTicker(gitinfo.SubrepoInterval)
	defer subrepoTicker.Stop()
	for {
		select {
		case <-c.done:
			return
		case fn := <-c.msgs:
			fn()
			c.m.dirty = true
		case <-frame.C:
			if c.m.dirty {
				c.m.dirty = false
				c.m.updateBadge()
				if c.m.hooks.Emit != nil {
					c.m.hooks.Emit(c.m.view())
				}
			}
		case <-busy.C:
			c.m.pollBusy()
		case <-subrepoTicker.C:
			c.m.refreshSubrepos()
		}
	}
}

// post queues fn for the loop. It never blocks the caller beyond the queue
// capacity and drops work after Stop.
func (c *Core) post(fn func()) {
	select {
	case c.msgs <- fn:
	case <-c.done:
	}
}

// do runs fn on the loop and waits for its result.
func (c *Core) do(fn func() error) error {
	reply := make(chan error, 1)
	select {
	case c.msgs <- func() { reply <- fn() }:
	case <-c.done:
		return ErrStopped
	}
	select {
	case err := <-reply:
		return err
	case <-c.done:
		return ErrStopped
	}
}

// Start restores panes and launches every process. Call once after Run.
func (c *Core) Start() error {
	return c.do(func() error {
		c.m.gcHolderSessions()
		c.m.startAll()
		c.m.refreshSubrepos()
		return nil
	})
}

// Send forwards an API request into the loop; the reply channel is
// answered on the loop. It is the send function for api.Server.
func (c *Core) Send(request api.Request) {
	c.post(func() { c.m.handleAPI(request) })
}

// HolderExited marks every pane bound to session as exited.
func (c *Core) HolderExited(session int64) {
	c.post(func() { c.m.holderExited(session) })
}

// Shutdown persists, detaches persistent panes, kills ephemeral ones, and
// stops the loop. Sessions in the holder keep running.
func (c *Core) Shutdown() {
	_ = c.do(func() error {
		c.m.shutdown()
		return nil
	})
	c.once.Do(func() { close(c.done) })
	<-c.stopped
}

// Stop ends the loop without touching panes; used in tests.
func (c *Core) Stop() {
	c.once.Do(func() { close(c.done) })
	<-c.stopped
}
