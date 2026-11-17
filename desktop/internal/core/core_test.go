package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/state"
)

func TestCreateSplitCloseKeepsTreeComplete(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, state.State{}, dir)
	h.waitFor("first pane running", func(m *model) bool { return m.spaces[0].tab().panes[0].running })

	if err := h.core.PaneCreate("shell", "right"); err != nil {
		t.Fatal(err)
	}
	if err := h.core.PaneCreate("shell", "down"); err != nil {
		t.Fatal(err)
	}
	view := h.snapshot()
	tab := view.Workspaces[0].Tabs[0]
	if len(tab.Panes) != 3 || !tab.Layout.Vertical || tab.Layout.B.Vertical {
		t.Fatalf("layout = %+v", tab.Layout)
	}
	if tab.Focused != tab.Panes[2].ID {
		t.Fatal("new pane must take focus")
	}

	first := tab.Panes[0].ID
	var firstTerm *fakeTerm
	_ = h.core.do(func() error {
		firstTerm = h.core.m.spaces[0].tab().panes[0].term.(*fakeTerm)
		return nil
	})
	if err := h.core.PaneClose(first); err != nil {
		t.Fatal(err)
	}
	h.waitFor("tree complete", func(m *model) bool {
		current := m.spaces[0].tab()
		return len(current.panes) == 2 && layout.Complete(current.layout, current.panes)
	})
	if !firstTerm.closed {
		t.Fatal("closed pane's process must be killed")
	}
	view = h.snapshot()
	if view.Workspaces[0].Tabs[0].Layout.Vertical {
		t.Fatal("sibling split not promoted")
	}
}

func TestPaneMoveKeepsTreeComplete(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	_ = h.core.PaneCreate("shell", "right")
	_ = h.core.PaneCreate("shell", "down")
	ids := paneIDs(h.snapshot(), 0)
	first, third := ids[0], ids[2]
	if err := h.core.PaneMove(first, third, "bottom"); err != nil {
		t.Fatal(err)
	}
	var complete bool
	_ = h.core.do(func() error {
		current := h.core.m.spaces[0].tab()
		complete = layout.Complete(current.layout, current.panes)
		return nil
	})
	view := h.snapshot().Workspaces[0].Tabs[0]
	if !complete || view.Focused != first {
		t.Fatalf("after move: complete=%v focused=%d", complete, view.Focused)
	}
	if err := h.core.PaneMove(ids[1], first, "center"); err != nil {
		t.Fatal(err)
	}
	if err := h.core.PaneMove(first, first, "left"); err != nil {
		t.Fatal("moving onto itself should be a no-op")
	}
	if err := h.core.PaneMove(first, ids[1], "diagonal"); err == nil {
		t.Fatal("unknown zone accepted")
	}
	_ = h.core.PaneCreateFloat("shell", "center", 50, 50)
	float := h.snapshot().Workspaces[0].Tabs[0].Floats[0].ID
	if err := h.core.PaneMove(float, first, "left"); err == nil {
		t.Fatal("floating pane moved into the split layout")
	}
}

func TestTabLifecycle(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	if err := h.core.TabCreate("shell"); err != nil {
		t.Fatal(err)
	}
	view := h.snapshot()
	if len(view.Workspaces[0].Tabs) != 2 || view.Workspaces[0].Active != 1 {
		t.Fatalf("tabs = %+v", view.Workspaces[0])
	}
	if err := h.core.TabRename(0, 1, "  logs  "); err != nil {
		t.Fatal(err)
	}
	if err := h.core.TabRename(0, 1, "bad\x1b"); err == nil {
		t.Fatal("control characters accepted")
	}
	if err := h.core.TabClose(0, 1); err != nil {
		t.Fatal(err)
	}
	if err := h.core.TabClose(0, 0); err == nil {
		t.Fatal("last tab closed")
	}
}

func TestFloatIsEphemeral(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	if err := h.core.PaneCreateFloat("shell", "center", 50, 40); err != nil {
		t.Fatal(err)
	}
	h.waitFor("float started", func(m *model) bool {
		current := m.spaces[0].tab()
		return len(current.panes) == 2 && current.panes[1].running
	})
	view := h.snapshot()
	if len(view.Workspaces[0].Tabs[0].Floats) != 1 {
		t.Fatal("float missing from view")
	}
	var saved state.State
	var split, float *fakeTerm
	_ = h.core.do(func() error {
		saved = h.core.m.snapshot()
		panes := h.core.m.spaces[0].tab().panes
		split, float = panes[0].term.(*fakeTerm), panes[1].term.(*fakeTerm)
		return nil
	})
	if len(saved.Workspaces[0].Tabs[0].Panes) != 1 {
		t.Fatal("float persisted")
	}
	h.core.Shutdown()
	if !float.closed || float.detached {
		t.Fatal("float must be killed, not detached, on quit")
	}
	if !split.detached || split.closed {
		t.Fatal("persistent pane must detach on quit")
	}
}

func TestSplitRatioAndEqualize(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	_ = h.core.PaneCreate("shell", "right")
	if err := h.core.SplitSetRatio(nil, 0.95); err != nil {
		t.Fatal(err)
	}
	if h.snapshot().Workspaces[0].Tabs[0].Layout.Ratio != 0.9 {
		t.Fatal("ratio not clamped")
	}
	if err := h.core.SplitSetRatio([]int{0}, 0.3); err == nil {
		t.Fatal("ratio set on a leaf")
	}
	_ = h.core.SplitEqualize(nil)
	if h.snapshot().Workspaces[0].Tabs[0].Layout.Ratio != 0.5 {
		t.Fatal("equalize ignored")
	}
}

func TestInputAndResizeReachTerminal(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	h.waitFor("pane running", func(m *model) bool { return m.spaces[0].tab().panes[0].running })
	id := h.snapshot().Workspaces[0].Tabs[0].Panes[0].ID
	h.core.PaneInput(id, []byte("ls\r"))
	h.core.PaneResize(id, 120, 40)
	h.core.PaneResize(id, 1, 1)
	h.sync()
	term := h.launcher.term(1)
	if len(term.written) != 1 || term.written[0] != "ls\r" || term.size != [2]int{120, 40} {
		t.Fatalf("term = %+v", term)
	}
	info, err := h.core.PaneAttachInfo(id)
	if err != nil || info.Preamble != "\x1b[?2004h" || info.Cols != 120 {
		t.Fatalf("attach = %+v %v", info, err)
	}
}

// Pastes follow the TUI: agents always get bracketed paste, so a multi-line
// paste is one input instead of one submit per line, even when the agent's
// DECSET 2004 was missed on reattach. Other programs get markers only when
// they enabled the mode.
func TestPanePasteBracketsLikeTheTUI(t *testing.T) {
	first, second := 0, 1
	saved := state.State{Workspaces: []state.Workspace{{Name: "w", CWD: t.TempDir(), Tabs: []state.Tab{{
		Panes:  []state.Pane{{Kind: "zot", Name: "agent"}, {Kind: "shell", Name: "sh"}},
		Layout: &state.Node{Ratio: 0.5, A: &state.Node{Pane: &first}, B: &state.Node{Pane: &second}},
	}}}}}
	h := newHarness(t, saved)
	h.waitFor("panes running", func(m *model) bool {
		panes := m.spaces[0].tab().panes
		return panes[0].running && panes[1].running
	})
	panes := h.snapshot().Workspaces[0].Tabs[0].Panes
	// Launches run concurrently, so take each pane's own terminal.
	var agentTerm, shellTerm *fakeTerm
	h.waitFor("terminals", func(m *model) bool {
		live := m.spaces[0].tab().panes
		agentTerm, shellTerm = live[0].term.(*fakeTerm), live[1].term.(*fakeTerm)
		return true
	})

	h.core.PanePaste(panes[0].ID, "one\rtwo")
	h.core.PanePaste(panes[1].ID, "one\rtwo")
	h.core.PanePaste(panes[1].ID, "")
	h.sync()
	if got := agentTerm.writes(); len(got) != 1 || got[0] != "\x1b[200~one\rtwo\x1b[201~" {
		t.Fatalf("agent paste = %q", got)
	}
	if got := shellTerm.writes(); len(got) != 1 || got[0] != "one\rtwo" {
		t.Fatalf("plain shell paste = %q", got)
	}

	shellTerm.mu.Lock()
	shellTerm.paste = true
	shellTerm.mu.Unlock()
	h.core.PanePaste(panes[1].ID, "x")
	h.sync()
	if got := shellTerm.writes(); len(got) != 2 || got[1] != "\x1b[200~x\x1b[201~" {
		t.Fatalf("bracketed shell paste = %q", got)
	}
}

func TestLaunchFailureShowsOnPane(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	launcher := newFakeLauncher(false)
	launcher.fail = errors.New("no such binary")
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh", StatePath: statePath}, state.State{}, launcher, Hooks{}, nil, prefsZero(), []string{t.TempDir()})
	go c.Run()
	defer c.Stop()
	_ = c.Start()
	h := &harness{t: t, core: c, launcher: launcher}
	h.waitFor("failure", func(m *model) bool { return m.spaces[0].tab().panes[0].failure == "no such binary" })
}

func TestWorkspaceReorderKeepsSelection(t *testing.T) {
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	h := newHarness(t, state.State{}, a, b, c)
	_ = h.core.WorkspaceSelect(2)
	if err := h.core.WorkspaceReorder(2, 0); err != nil {
		t.Fatal(err)
	}
	view := h.snapshot()
	if view.Workspaces[0].Path != c || view.Selected != 0 {
		t.Fatalf("order = %v selected %d", view.Workspaces[0].Path, view.Selected)
	}
	if err := h.core.WorkspaceClose(0); err != nil {
		t.Fatal(err)
	}
	if len(h.snapshot().Workspaces) != 2 {
		t.Fatal("workspace not closed")
	}
}

func TestBusyTransitionNotifies(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	launcher := newFakeLauncher(true)
	notified := make(chan Notification, 1)
	sounds := make(chan string, 1)
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh", StatePath: statePath, FinishDelay: 1},
		state.State{Sound: true, SoundKind: "chime"}, launcher,
		Hooks{Notify: func(n Notification) { notified <- n }, PlaySound: func(kind string) { sounds <- kind }},
		nil, prefsZero(), []string{t.TempDir()})
	go c.Run()
	defer c.Stop()
	_ = c.Start()
	h := &harness{t: t, core: c, launcher: launcher}
	h.waitFor("agent running", func(m *model) bool { return m.spaces[0].tab().panes[0].running })
	term := launcher.term(1)
	term.mu.Lock()
	term.status = "working"
	term.mu.Unlock()
	h.waitFor("busy", func(m *model) bool { return m.wasBusy[m.spaces[0].tab().panes[0].id] })
	term.mu.Lock()
	term.status = "idle"
	term.mu.Unlock()
	select {
	case n := <-notified:
		if n.Kind != "finished" {
			t.Fatalf("notification = %+v", n)
		}
	case <-timeout():
		t.Fatal("finish not notified")
	}
	if kind := <-sounds; kind != "chime" {
		t.Fatalf("sound = %q", kind)
	}
}

func TestAttachedOutputReachesHook(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	launcher := newFakeLauncher(true)
	got := make(chan string, 1)
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh", StatePath: statePath}, state.State{}, launcher,
		Hooks{Output: func(id int, data []byte) { got <- string(data) }}, nil, prefsZero(), []string{t.TempDir()})
	go c.Run()
	defer c.Stop()
	_ = c.Start()
	h := &harness{t: t, core: c, launcher: launcher}
	h.waitFor("started", func(m *model) bool { return m.spaces[0].tab().panes[0].running })
	launcher.mu.Lock()
	tee := launcher.tees[1]
	launcher.mu.Unlock()
	tee([]byte("hello"))
	if <-got != "hello" {
		t.Fatal("output not forwarded")
	}
}

func TestSettingsPatchPersists(t *testing.T) {
	h := newHarness(t, state.State{}, t.TempDir())
	theme, on := "dracula", true
	if err := h.core.SettingsSet(SettingsPatch{Theme: &theme, Sound: &on}); err != nil {
		t.Fatal(err)
	}
	bad := "nope"
	if err := h.core.SettingsSet(SettingsPatch{Theme: &bad}); err == nil {
		t.Fatal("unknown theme accepted")
	}
	if view := h.snapshot(); view.Theme.Name != "dracula" || view.Theme.Colors["accent"] != "#bd93f9" {
		t.Fatalf("theme = %+v", view.Theme)
	}
	h.core.Shutdown()
	data, err := os.ReadFile(h.state)
	if err != nil {
		t.Fatal(err)
	}
	var saved state.State
	if err := json.Unmarshal(data, &saved); err != nil || saved.Theme != "dracula" || !saved.Sound {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(h.state), "desktop.json")); err != nil {
		t.Fatal("desktop.json not written on quit")
	}
}

func TestBlockedRefusesPanesAndShowsInView(t *testing.T) {
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh"}, state.State{}, nil, Hooks{}, nil, prefsZero(), nil)
	go c.Run()
	defer c.Stop()
	h := &harness{t: t, core: c}
	c.SetBlocked("busy")
	if h.snapshot().Blocked != "busy" {
		t.Fatal("blocked state missing from the view")
	}
	if err := c.WorkspaceCreate(t.TempDir(), "shell", nil); err == nil {
		t.Fatal("workspace created while blocked")
	}
	if err := c.Activate("", state.State{}, newFakeLauncher(true), nil); err != nil {
		t.Fatal(err)
	}
	if h.snapshot().Blocked != "" {
		t.Fatal("activation must clear the blocked state")
	}
}

func TestActivateAfterBlockedStart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	dir := t.TempDir()
	saved := state.State{Workspaces: []state.Workspace{{Name: "w", CWD: dir, Tabs: []state.Tab{{
		Panes: []state.Pane{{Kind: "shell", Name: "s", Session: 7}}}}}}}
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh", SaveDelay: 1}, state.State{}, nil, Hooks{}, nil, prefsZero(), nil)
	go c.Run()
	defer c.Stop()
	h := &harness{t: t, core: c}
	if len(h.snapshot().Workspaces) != 0 {
		t.Fatal("blocked start must not restore workspaces")
	}
	if _, err := os.Stat(statePath); err == nil {
		t.Fatal("blocked start wrote state")
	}
	launcher := newFakeLauncher(true)
	if err := c.Activate(statePath, saved, launcher, nil); err != nil {
		t.Fatal(err)
	}
	h.waitFor("reattached", func(m *model) bool { return m.spaces[0].tab().panes[0].running })
	if launcher.launched[0].Reattach != 7 {
		t.Fatalf("reattach = %d", launcher.launched[0].Reattach)
	}
	if err := c.Activate(statePath, saved, launcher, nil); err == nil {
		t.Fatal("second activation accepted")
	}
}
