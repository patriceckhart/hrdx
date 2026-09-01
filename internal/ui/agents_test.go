package ui

import (
	"strings"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestAgentKinds(t *testing.T) {
	for _, kind := range []string{"zot", "pi", "claude", "codex", "copilot"} {
		if !isAgentKind(kind) {
			t.Fatalf("%s should be an agent kind", kind)
		}
	}
	if isAgentKind("shell") || isAgentKind("nope") {
		t.Fatal("shell/nope must not be agent kinds")
	}
}

func TestBinaryForHonorsOverride(t *testing.T) {
	config := Config{AgentBins: map[string]string{"claude": "/opt/claude"}}
	if got := config.binaryFor("claude"); got != "/opt/claude" {
		t.Fatalf("binaryFor = %q, want /opt/claude", got)
	}
	if got := config.binaryFor("codex"); got != "codex" {
		t.Fatalf("binaryFor = %q, want codex", got)
	}
	if got := (Config{AgentBins: map[string]string{"copilot": "/opt/copilot"}}).binaryFor("copilot"); got != "/opt/copilot" {
		t.Fatalf("binaryFor = %q, want /opt/copilot", got)
	}
}

func TestDefaultAgentUsedForNewPanes(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "claude"}, []string{"/tmp/api"}, "", state.State{})
	first := model.spaces[0].tab()
	if first.panes[0].kind != "claude" {
		t.Fatalf("pane kind = %q, want claude", first.panes[0].kind)
	}
	if first.panes[0].name != "claude 1" {
		t.Fatalf("pane name = %q, want claude 1", first.panes[0].name)
	}
}

func TestContinueMarksNewAgentPanesForResume(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "copilot", Resume: true}, []string{"/tmp/api"}, "", state.State{})
	if target := model.spaces[0].tab().panes[0]; target.kind != "copilot" || !target.resume {
		t.Fatalf("pane = %+v, want resumable copilot", target)
	}
	if target := model.addPane(model.spaces[0], "shell", true); target.resume {
		t.Fatal("shell pane must not be marked resumable")
	}
}

func TestInvalidDefaultAgentFallsBack(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "gemini"}, []string{"/tmp/api"}, "", state.State{})
	if model.config.DefaultAgent != "zot" {
		t.Fatalf("default agent = %q, want zot", model.config.DefaultAgent)
	}
}

func TestToggleAgentDisablesAndPersistsInSnapshot(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})

	model.toggleAgent("claude")
	if !model.disabled["claude"] {
		t.Fatal("claude should be disabled after toggle")
	}
	for _, kind := range model.availableAgents() {
		if kind == "claude" {
			t.Fatal("disabled agent must not be available")
		}
	}

	saved := model.snapshot()
	if len(saved.DisabledAgents) != 1 || saved.DisabledAgents[0] != "claude" {
		t.Fatalf("snapshot disabled = %v, want [claude]", saved.DisabledAgents)
	}

	model.toggleAgent("claude")
	if model.disabled["claude"] {
		t.Fatal("claude should be enabled after the second toggle")
	}
}

func TestToggleAgentKeepsLastEnabled(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})
	model.toggleAgent("pi")
	model.toggleAgent("claude")
	model.toggleAgent("codex")
	model.toggleAgent("copilot")
	if cmd := model.toggleAgent("zot"); cmd == nil {
		t.Fatal("disabling the last agent should flash a status")
	}
	if model.disabled["zot"] {
		t.Fatal("the last enabled agent must stay enabled")
	}
}

func TestDisablingDefaultAgentMovesDefault(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", DefaultAgent: "zot"}, []string{"/tmp/api"}, "", state.State{})
	model.toggleAgent("zot")
	if model.config.DefaultAgent == "zot" {
		t.Fatalf("default agent = zot, want it moved to an enabled agent")
	}
}

func TestRestoreDisabledAgents(t *testing.T) {
	saved := state.State{DisabledAgents: []string{"pi", "nope"}}
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", saved)
	if !model.disabled["pi"] {
		t.Fatal("pi should be restored as disabled")
	}
	if model.disabled["nope"] {
		t.Fatal("unknown kinds must not restore as disabled")
	}
}

func TestSettingsRowsShowCheckboxes(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})
	model.toggleAgent("codex")
	rows := model.settingsRows()
	if len(rows) != len(agentSpecs) {
		t.Fatalf("rows = %d, want %d", len(rows), len(agentSpecs))
	}
	for _, row := range rows {
		switch row.action {
		case "toggle:codex":
			if !strings.HasPrefix(row.label, "[ ] ") {
				t.Fatalf("codex label = %q, want unchecked", row.label)
			}
		case "toggle:zot":
			if !strings.HasPrefix(row.label, "[x] ") {
				t.Fatalf("zot label = %q, want checked", row.label)
			}
		}
	}
}

func TestSoundToggleRoundTrip(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})
	if model.soundOn {
		t.Fatal("sound should default to off")
	}
	model.settingsTab = 1
	rows := model.settingsRows()
	// checkbox + one radio per sound + the system notification toggle.
	if len(rows) != 2+len(soundKindList()) || rows[0].action != "sound" {
		t.Fatalf("notification rows = %+v", rows)
	}
	if rows[len(rows)-1].action != "notify" {
		t.Fatalf("last row = %+v, want the notify toggle", rows[len(rows)-1])
	}
	model.toggleSettingsRow(rows[0])
	if !model.soundOn {
		t.Fatal("sound should be on after toggle")
	}
	if !model.snapshot().Sound {
		t.Fatal("snapshot should carry the sound setting")
	}

	// The system notification toggle persists too.
	model.toggleSettingsRow(settingsRow{action: "notify"})
	if !model.notifyOn || !model.snapshot().Notify {
		t.Fatal("notify should be on and persisted")
	}

	restored := New(Config{Shell: "/bin/sh"}, nil, "", model.snapshot())
	if !restored.soundOn || !restored.notifyOn || restored.soundKind != defaultSoundKind {
		t.Fatalf("restore = %v/%v/%q", restored.soundOn, restored.notifyOn, restored.soundKind)
	}

	// Unknown kinds fall back to the default.
	bad := New(Config{Shell: "/bin/sh"}, nil, "", state.State{SoundKind: "airhorn"})
	if bad.soundKind != defaultSoundKind {
		t.Fatalf("soundKind = %q, want default", bad.soundKind)
	}
}

func TestPaneDisplayName(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})
	target := model.spaces[0].tab().panes[0]

	// Agent panes show their own name.
	if got := model.paneDisplayName(target); got != target.name {
		t.Fatalf("agent pane display = %q, want %q", got, target.name)
	}

	// Shell panes without a running agent show their own name too. The
	// pane has no terminal yet, so no foreground detection happens.
	shellPane := model.addPane(model.spaces[0], "shell", true)
	if got := model.paneDisplayName(shellPane); got != shellPane.name {
		t.Fatalf("shell pane display = %q, want %q", got, shellPane.name)
	}
}

func TestRestoreKeepsAgentKinds(t *testing.T) {
	saved := state.State{Workspaces: []state.Workspace{{
		Name: "api", CWD: "/tmp/api",
		Tabs: []state.Tab{{Panes: []state.Pane{
			{Kind: "codex", Name: "codex 1"},
			{Kind: "copilot", Name: "copilot 1"},
			{Kind: "pi", Name: "pi 1"},
			{Kind: "mystery", Name: "x"},
		}}},
	}}}
	model := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	panes := model.spaces[0].tab().panes
	if panes[0].kind != "codex" || !panes[0].resume {
		t.Fatalf("pane 0 = %+v, want resumable codex", panes[0])
	}
	if panes[1].kind != "copilot" || !panes[1].resume {
		t.Fatalf("pane 1 = %+v, want resumable copilot", panes[1])
	}
	if spec := agentByKind("copilot"); len(spec.resume) != 1 || spec.resume[0] != "--continue" {
		t.Fatalf("copilot resume = %v, want [--continue]", spec.resume)
	}
	if panes[2].kind != "pi" || !panes[2].resume {
		t.Fatalf("pane 2 = %+v, want resumable pi", panes[2])
	}
	if panes[3].kind != "shell" || panes[3].resume {
		t.Fatalf("pane 3 = %+v, want non-resuming shell fallback", panes[3])
	}
}
