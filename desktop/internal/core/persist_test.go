package core

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/state"
)

// tuiFixture is a state file as the TUI writes it, including fields the
// desktop app does not implement. Paths are synthetic.
func tuiFixture(dir string) string {
	// Quote the path as JSON so Windows backslashes stay valid escapes.
	quoted, _ := json.Marshal(dir)
	cwd := string(quoted)
	return `{
  "workspaces": [
    {
      "group_path": ["work"],
      "name": "api",
      "cwd": ` + cwd + `,
      "tabs": [
        {
          "name": "main",
          "panes": [
            {"kind": "zot", "name": "zot 1", "session": 41},
            {"kind": "zot", "name": "durable view", "durable": {"profile": "local", "workspace": "ws-1", "conversation": "conv-1", "title": "t"}},
            {"kind": "shell", "name": "shell 1", "session": 42}
          ],
          "layout": {"vertical": true, "ratio": 0.4,
            "a": {"pane": 0},
            "b": {"ratio": 0.6, "a": {"pane": 1}, "b": {"pane": 2}}},
          "selected": 2
        }
      ],
      "active": 0,
      "continuous": [{"profile": "local", "workspace": "ws-1", "conversation": "conv-1"}],
      "continuous_host": "remote",
      "folded": true,
      "hidden_subrepos": ["vendor/x"]
    },
    {
      "name": "legacy",
      "cwd": ` + cwd + `,
      "panes": [{"kind": "shell", "name": "old"}, {"kind": "claude", "name": "c"}],
      "layout": {"vertical": false, "ratio": 0.3, "a": {"pane": 0}, "b": {"pane": 1}},
      "selected": 1
    }
  ],
  "selected": 1,
  "disabled_agents": ["pi", "aider-missing"],
  "sound": true,
  "sound_kind": "chime",
  "theme": "dracula",
  "durable_muted": ["host"],
  "sidebar_width": 33,
  "split_direction": "auto",
  "workspace_folding": true,
  "subrepo_depth": 2
}`
}

func TestRestoreSnapshotPreservesTUIFields(t *testing.T) {
	dir := t.TempDir()
	var saved state.State
	if err := json.Unmarshal([]byte(tuiFixture(dir)), &saved); err != nil {
		t.Fatal(err)
	}
	m := newModel(Config{DefaultAgent: "zot"}, saved, nil, Hooks{}, nil, prefsZero())

	durable := m.spaces[0].tabs[0].panes[1]
	if durable.durable == nil || durable.failure == "" {
		t.Fatal("durable pane must restore as a placeholder")
	}
	if m.spaces[1].tabs[0].layout.Vertical || len(m.spaces[1].tabs) != 1 {
		t.Fatal("legacy layout not upgraded to one tab")
	}

	got := m.snapshot()
	// The legacy workspace is rewritten in the tab form, exactly as the TUI does.
	want := saved
	want.Workspaces[1].Tabs = []state.Tab{{Panes: saved.Workspaces[1].Panes, Layout: saved.Workspaces[1].Layout, Selected: 1}}
	want.Workspaces[1].Panes, want.Workspaces[1].Layout, want.Workspaces[1].Selected = nil, nil, 0
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	var gotAny, wantAny any
	_ = json.Unmarshal(gotJSON, &gotAny)
	_ = json.Unmarshal(wantJSON, &wantAny)
	if !reflect.DeepEqual(gotAny, wantAny) {
		t.Fatalf("round trip changed state\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

func TestDurablePlaceholderIsNeverStarted(t *testing.T) {
	dir := t.TempDir()
	var saved state.State
	if err := json.Unmarshal([]byte(tuiFixture(dir)), &saved); err != nil {
		t.Fatal(err)
	}
	launcher := newFakeLauncher(true)
	m := newModel(Config{DefaultAgent: "zot", Shell: "/bin/sh"}, saved, launcher, Hooks{}, nil, prefsZero())
	m.post = func(func()) {}
	m.startAll()
	// Four ordinary panes start, the durable one does not.
	deadline := 0
	for launcher.launches() < 4 && deadline < 1000 {
		deadline++
		waitTick()
	}
	if launcher.launches() != 4 {
		t.Fatalf("launches = %d, want 4", launcher.launches())
	}
	for _, spec := range launcher.launched {
		if spec.Reattach != 0 && spec.Reattach != 41 && spec.Reattach != 42 {
			t.Fatalf("unexpected reattach %d", spec.Reattach)
		}
	}
}

func TestRestoreRejectsCorruptLayout(t *testing.T) {
	dir := t.TempDir()
	seven := 7
	saved := state.State{Workspaces: []state.Workspace{{Name: "x", CWD: dir, Active: 9, Tabs: []state.Tab{{
		Panes:    []state.Pane{{Kind: "shell", Name: "a"}, {Kind: "bogus", Name: "b"}},
		Layout:   &state.Node{Pane: &seven},
		Selected: 99,
	}}}}, Selected: 5}
	m := newModel(Config{DefaultAgent: "zot"}, saved, nil, Hooks{}, nil, prefsZero())
	current := m.spaces[0].tabs[0]
	if !layout.Complete(current.layout, current.panes) || current.selected != 1 || m.selected != 0 || m.spaces[0].active != 0 {
		t.Fatal("corrupt layout not repaired")
	}
	if current.panes[1].kind != "shell" {
		t.Fatal("unknown kind must fall back to shell")
	}
}

func TestResumeArgsOnRestore(t *testing.T) {
	dir := t.TempDir()
	saved := state.State{Workspaces: []state.Workspace{{Name: "x", CWD: dir, Tabs: []state.Tab{{
		Panes: []state.Pane{{Kind: "codex", Name: "c", Session: 0}},
	}}}}}
	m := newModel(Config{DefaultAgent: "zot", AgentBins: map[string]string{"codex": "/opt/codex"}}, saved, nil, Hooks{}, nil, prefsZero())
	target := m.spaces[0].tabs[0].panes[0]
	spec := m.launchSpec(m.spaces[0], target)
	if spec.Command != "/opt/codex" || len(spec.Args) != 2 || spec.Args[0] != "resume" {
		t.Fatalf("spec = %+v", spec)
	}
}
