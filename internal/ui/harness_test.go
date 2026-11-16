package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
)

// resetHarnesses removes all custom entries registered by a test.
func resetHarnesses() {
	kept := agent.Specs[:0]
	for _, spec := range agent.Specs {
		if !spec.Custom {
			kept = append(kept, spec)
		}
	}
	agent.Specs = kept
}

func writeHarnessFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, agent.HarnessFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadHarnessesRegistersCustomKinds(t *testing.T) {
	defer resetHarnesses()
	dir := writeHarnessFile(t, `[
		{"kind": "aider", "binary": "aider", "args": ["--no-auto-commits"],
		 "resume": ["--restore-chat-history"], "busy": "Waiting for the model",
		 "idle_title": "aider idle", "attention_title": "aider waiting"},
		{"kind": "goose"}
	]`)

	if problem := agent.LoadHarnesses(dir); problem != "" {
		t.Fatalf("loadHarnesses = %q, want no problem", problem)
	}
	if !isAgentKind("aider") || !isAgentKind("goose") {
		t.Fatal("custom kinds should register as agents")
	}
	spec := agentByKind("aider")
	if spec.Binary != "aider" || len(spec.Args) != 1 || spec.BusyMatch != "Waiting for the model" ||
		spec.IdleTitle != "aider idle" || spec.AttentionTitle != "aider waiting" {
		t.Fatalf("aider spec = %+v", spec)
	}
	if !spec.Custom {
		t.Fatal("registered harness must be marked custom")
	}
	if agentByKind("goose").Binary != "goose" {
		t.Fatal("binary should default to kind")
	}
}

// titledPane registers a custom harness and returns a model whose first
// pane in the second space runs it, ready to be fed terminal output.
func titledPane(t *testing.T, spec agent.HarnessSpec) (Model, *pane) {
	t.Helper()
	if err := agent.RegisterHarness(spec); err != nil {
		t.Fatal(err)
	}
	model := New(Config{DefaultAgent: spec.Kind, Shell: "/bin/sh"}, []string{"/tmp/api", "/tmp/web"}, "", state.State{})
	target := model.spaces[1].tab().panes[0]
	target.term = term.NewHolderPane(nil, 1, 80, 24)
	target.running = true
	return model, target
}

// A harness that declares no title markers must behave exactly as it did
// before title states existed: screen detection stays in charge. Guards the
// strings.Contains(title, "") trap, where an unset marker would match every
// title and pin the pane to a permanent idle state.
func TestHarnessWithoutTitleStatesFallsBackToScreen(t *testing.T) {
	defer resetHarnesses()
	model, target := titledPane(t, agent.HarnessSpec{Kind: "plain"})

	if spec := agentByKind("plain"); spec.IdleTitle != "" || spec.AttentionTitle != "" {
		t.Fatalf("unset title states = %q/%q, want both empty", spec.IdleTitle, spec.AttentionTitle)
	}
	target.term.Feed([]byte("\x1b]2;plain — some arbitrary title\x07⠋ Working"))
	if state := model.paneAgentTitleState(target); state != "" {
		t.Fatalf("title state = %q, want empty so the screen scrape decides", state)
	}
	if !model.paneBusy(target) {
		t.Fatal("visible spinner must still read as busy when no titles are declared")
	}
	if got, want := model.sidebarPaneIcon(target), paneIconCell(styleDotBusy, spinnerFrames[0]); got != want {
		t.Fatalf("icon = %q, want animated spinner %q", got, want)
	}
}

func TestHarnessAttentionTitleOverridesVisibleSpinner(t *testing.T) {
	defer resetHarnesses()
	model, target := titledPane(t, agent.HarnessSpec{
		Kind: "titled", IdleTitle: "[ready]", AttentionTitle: "[ask]",
	})
	target.term.Feed([]byte("\x1b]2;[ask] waiting for input\x07⠋ Working"))

	if state := model.paneAgentTitleState(target); state != "attention" {
		t.Fatalf("title state = %q, want attention", state)
	}
	if model.paneBusy(target) {
		t.Fatal("attention title must override the stale visible spinner")
	}
	if got, want := model.sidebarPaneIcon(target), paneIconCell(styleDotBusy, "●"); got != want {
		t.Fatalf("unfocused waiting icon = %q, want static orange circle %q", got, want)
	}

	model.selected = 1
	if got, want := model.sidebarPaneIcon(target), paneIconCell(styleDotOn, "●"); got != want {
		t.Fatalf("focused waiting icon = %q, want acknowledged green circle %q", got, want)
	}

	target.term.Feed([]byte("\x1b]2;[ready] waiting for prompt\x07"))
	if state := model.paneAgentTitleState(target); state != "idle" || model.paneBusy(target) {
		t.Fatalf("idle title state/busy = %q/%v, want idle/false", state, model.paneBusy(target))
	}
	target.term.Feed([]byte("\x1b]2;⠙ working\x07"))
	if state := model.paneAgentTitleState(target); state != "" || !model.paneBusy(target) {
		t.Fatalf("working title state/busy = %q/%v, want empty/true", state, model.paneBusy(target))
	}
}

func TestLoadHarnessesRejectsInvalidEntries(t *testing.T) {
	defer resetHarnesses()
	dir := writeHarnessFile(t, `[
		{"kind": "zot", "binary": "evil"},
		{"kind": "shell"},
		{"binary": "nameless"},
		{"kind": "ok"}
	]`)

	problem := agent.LoadHarnesses(dir)
	if problem == "" {
		t.Fatal("expected a problem report for invalid entries")
	}
	if agentByKind("zot").Binary != "zot" {
		t.Fatal("built-in zot must not be overridden")
	}
	if !isAgentKind("ok") {
		t.Fatal("valid entries should still register")
	}
}

func TestLoadHarnessesMissingFileIsFine(t *testing.T) {
	if problem := agent.LoadHarnesses(t.TempDir()); problem != "" {
		t.Fatalf("missing file reported %q", problem)
	}
}

func TestLoadHarnessesBadJSON(t *testing.T) {
	dir := writeHarnessFile(t, "{not json")
	if problem := agent.LoadHarnesses(dir); problem == "" {
		t.Fatal("bad json should be reported")
	}
}

func TestCustomHarnessInSettingsAndToggle(t *testing.T) {
	defer resetHarnesses()
	if err := agent.RegisterHarness(agent.HarnessSpec{Kind: "aider"}); err != nil {
		t.Fatal(err)
	}
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, "", state.State{})

	found := false
	for _, row := range model.settingsRows() {
		if row.action == "toggle:aider" {
			found = true
		}
	}
	if !found {
		t.Fatal("custom harness missing from the settings rows")
	}

	model.toggleAgent("aider")
	if !model.disabled["aider"] {
		t.Fatal("custom harness should toggle off")
	}
	saved := model.snapshot()
	if len(saved.DisabledAgents) != 1 || saved.DisabledAgents[0] != "aider" {
		t.Fatalf("snapshot disabled = %v, want [aider]", saved.DisabledAgents)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if !restored.disabled["aider"] {
		t.Fatal("restore should keep the custom harness disabled")
	}
}

func TestCustomHarnessRestoresPanes(t *testing.T) {
	defer resetHarnesses()
	if err := agent.RegisterHarness(agent.HarnessSpec{Kind: "aider", Resume: []string{"--restore"}}); err != nil {
		t.Fatal(err)
	}
	saved := state.State{Workspaces: []state.Workspace{{
		Name: "api", CWD: "/tmp/api",
		Tabs: []state.Tab{{Panes: []state.Pane{{Kind: "aider", Name: "aider 1"}}}},
	}}}
	model := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	restored := model.spaces[0].tab().panes[0]
	if restored.kind != "aider" || !restored.resume {
		t.Fatalf("restored pane = %+v, want resumable aider", restored)
	}
}

func TestReRegisterReplacesCustomHarness(t *testing.T) {
	defer resetHarnesses()
	if err := agent.RegisterHarness(agent.HarnessSpec{Kind: "aider", Busy: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := agent.RegisterHarness(agent.HarnessSpec{Kind: "aider", Busy: "new"}); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, spec := range agent.Specs {
		if spec.Kind == "aider" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("aider registered %d times, want 1", count)
	}
	if agentByKind("aider").BusyMatch != "new" {
		t.Fatal("re-registration should replace the entry")
	}
}
