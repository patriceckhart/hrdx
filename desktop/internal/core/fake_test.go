package core

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/state"
)

// fakeTerm is a Terminal without a process.
type fakeTerm struct {
	mu       sync.Mutex
	session  int64
	written  []string
	size     [2]int
	closed   bool
	detached bool
	running  bool
	title    string
	status   string
	spinner  bool
	updates  chan struct{}
	exitOnce sync.Once
}

func newFakeTerm(session int64) *fakeTerm {
	return &fakeTerm{session: session, running: true, updates: make(chan struct{}, 1)}
}

func (f *fakeTerm) Write(data []byte) {
	f.mu.Lock()
	f.written = append(f.written, string(data))
	f.mu.Unlock()
}
func (f *fakeTerm) Resize(cols, rows int) {
	f.mu.Lock()
	f.size = [2]int{cols, rows}
	f.mu.Unlock()
}
func (f *fakeTerm) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	f.exit()
}
func (f *fakeTerm) Detach() {
	f.mu.Lock()
	f.detached = true
	f.mu.Unlock()
	f.exit()
}
func (f *fakeTerm) exit() {
	f.exitOnce.Do(func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
		close(f.updates)
	})
}
func (f *fakeTerm) Running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running
}
func (f *fakeTerm) Title() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.title
}
func (f *fakeTerm) ProgramStatus() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, ""
}
func (f *fakeTerm) HasScreenText(string) bool { return false }
func (f *fakeTerm) HasSpinner() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spinner
}
func (f *fakeTerm) ForegroundCommand() string { return "" }
func (f *fakeTerm) PlainScreen() string       { return "screen text" }
func (f *fakeTerm) KittyKeys() bool           { return false }
func (f *fakeTerm) ModePreamble() string      { return "\x1b[?2004h" }
func (f *fakeTerm) HolderSession() int64      { return f.session }
func (f *fakeTerm) Updates() <-chan struct{}  { return f.updates }

// fakeLauncher records launches and hands out fake terminals.
type fakeLauncher struct {
	mu         sync.Mutex
	persistent bool
	next       int64
	launched   []LaunchSpec
	terms      map[int64]*fakeTerm
	sessions   []int64
	killed     []int64
	closed     bool
	fail       error
	tees       map[int64]func([]byte)
}

func newFakeLauncher(persistent bool) *fakeLauncher {
	return &fakeLauncher{persistent: persistent, next: 100, terms: map[int64]*fakeTerm{}, tees: map[int64]func([]byte){}}
}

func (l *fakeLauncher) Launch(spec LaunchSpec, tee func([]byte)) (Terminal, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.launched = append(l.launched, spec)
	if l.fail != nil {
		return nil, l.fail
	}
	session := spec.Reattach
	if session == 0 || !l.persistent {
		l.next++
		session = l.next
	}
	if !l.persistent {
		session = 0
	}
	term := newFakeTerm(session)
	l.terms[int64(len(l.launched))] = term
	l.tees[int64(len(l.launched))] = tee
	return term, nil
}
func (l *fakeLauncher) Persistent() bool { return l.persistent }
func (l *fakeLauncher) Sessions() []int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]int64(nil), l.sessions...)
}
func (l *fakeLauncher) Kill(session int64) {
	l.mu.Lock()
	l.killed = append(l.killed, session)
	l.mu.Unlock()
}
func (l *fakeLauncher) Close() {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
}
func (l *fakeLauncher) launches() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.launched)
}
func (l *fakeLauncher) term(n int) *fakeTerm {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.terms[int64(n)]
}

// harness runs a core in the background for one test.
type harness struct {
	t        *testing.T
	core     *Core
	launcher *fakeLauncher
	events   *api.Broadcaster
	state    string
}

func newHarness(t *testing.T, saved state.State, cwds ...string) *harness {
	t.Helper()
	statePath := t.TempDir() + "/state.json"
	launcher := newFakeLauncher(true)
	events := api.NewBroadcaster()
	c := New(Config{Version: "test", DefaultAgent: "zot", Shell: "/bin/sh", StatePath: statePath,
		FrameInterval: time.Millisecond, SaveDelay: time.Millisecond, FinishDelay: 10 * time.Millisecond},
		saved, launcher, Hooks{}, events, prefs.Prefs{}, cwds)
	go c.Run()
	t.Cleanup(c.Stop)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, core: c, launcher: launcher, events: events, state: statePath}
}

// sync waits until the loop processed everything queued before it.
func (h *harness) sync() {
	h.t.Helper()
	if err := h.core.do(func() error { return nil }); err != nil {
		h.t.Fatal(err)
	}
}

// waitFor polls cond on the loop until it holds or the deadline passes.
func (h *harness) waitFor(what string, cond func(m *model) bool) {
	h.t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ok := false
		_ = h.core.do(func() error { ok = cond(h.core.m); return nil })
		if ok {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", what)
}

func (h *harness) request(method string, payload any) api.Reply {
	h.t.Helper()
	reply := make(chan api.Reply, 1)
	h.core.Send(api.Request{Method: method, Payload: payload, Reply: reply})
	select {
	case answer := <-reply:
		return answer
	case <-time.After(2 * time.Second):
		h.t.Fatalf("%s did not answer", method)
		return api.Reply{}
	}
}

func (h *harness) snapshot() View {
	h.t.Helper()
	view, err := h.core.Snapshot()
	if err != nil {
		h.t.Fatal(err)
	}
	return view
}

func prefsZero() prefs.Prefs { return prefs.Prefs{} }

func timeout() <-chan time.Time { return time.After(2 * time.Second) }

func waitTick() { time.Sleep(2 * time.Millisecond) }

func paneIDs(view View, workspace int) []int {
	var ids []int
	for _, tab := range view.Workspaces[workspace].Tabs {
		for _, pane := range tab.Panes {
			ids = append(ids, pane.ID)
		}
	}
	return ids
}

func contains(haystack []string, needle string) bool {
	for _, value := range haystack {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
