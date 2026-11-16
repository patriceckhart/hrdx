package core

import (
	"path/filepath"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestBannerText(t *testing.T) {
	base := Notification{Kind: "finished", PaneID: 3, Workspace: "website", Pane: "Claude Code", Unseen: true, Banner: true}
	if title, body, show := BannerText(base); !show || title != "website" || body != "Claude Code finished" {
		t.Fatalf("finished = %q %q %v", title, body, show)
	}
	attention := base
	attention.Kind = "attention"
	if _, body, _ := BannerText(attention); body != "Claude Code needs your input" {
		t.Fatalf("attention body = %q", body)
	}
	seen := base
	seen.Unseen = false
	if _, _, show := BannerText(seen); show {
		t.Fatal("banner for the pane the user is looking at")
	}
	off := base
	off.Banner = false
	if _, _, show := BannerText(off); show {
		t.Fatal("banner with notifications off")
	}
}

func TestPaneLabel(t *testing.T) {
	m := newModel(Config{DefaultAgent: "zot"}, state.State{}, nil, Hooks{}, nil, prefsZero())
	if got := m.paneLabel(&pane{kind: "claude", name: "claude 2"}); got != "Claude Code" {
		t.Fatalf("default name = %q", got)
	}
	if got := m.paneLabel(&pane{kind: "claude", name: "review"}); got != "review" {
		t.Fatalf("custom name = %q", got)
	}
}

// A finished turn in an unfocused pane notifies with a banner and raises the
// badge; focusing the pane clears the badge.
func TestFinishNotifiesAndBadges(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	launcher := newFakeLauncher(true)
	notified := make(chan Notification, 1)
	badges := make(chan int, 8)
	c := New(Config{DefaultAgent: "zot", Shell: "/bin/sh", StatePath: statePath, FinishDelay: 1},
		state.State{Notify: true}, launcher,
		Hooks{Notify: func(n Notification) { notified <- n }, Badge: func(n int) { badges <- n }},
		nil, prefsZero(), []string{t.TempDir()})
	go c.Run()
	defer c.Stop()
	_ = c.Start()
	h := &harness{t: t, core: c, launcher: launcher}
	h.waitFor("running", func(m *model) bool { return m.spaces[0].tab().panes[0].running })
	c.SetWindowFocused(false)
	var agentTerm *fakeTerm
	_ = c.do(func() error { agentTerm = c.m.spaces[0].tab().panes[0].term.(*fakeTerm); return nil })
	agentTerm.mu.Lock()
	agentTerm.status = "working"
	agentTerm.mu.Unlock()
	h.waitFor("busy", func(m *model) bool { return m.wasBusy[m.spaces[0].tab().panes[0].id] })
	agentTerm.mu.Lock()
	agentTerm.status = "blocked"
	agentTerm.mu.Unlock()
	select {
	case n := <-notified:
		if n.Kind != "attention" || !n.Unseen || !n.Banner || n.Pane != "zot" {
			t.Fatalf("notification = %+v", n)
		}
	case <-timeout():
		t.Fatal("not notified")
	}
	if got := <-badges; got != 1 {
		t.Fatalf("badge = %d", got)
	}
	c.SetWindowFocused(true)
	select {
	case got := <-badges:
		if got != 0 {
			t.Fatalf("badge after focus = %d", got)
		}
	case <-timeout():
		t.Fatal("badge not cleared")
	}
}
