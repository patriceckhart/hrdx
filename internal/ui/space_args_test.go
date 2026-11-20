package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/state"
)

func typeText(t *testing.T, model Model, text string) Model {
	t.Helper()
	for _, r := range text {
		updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = updated.(Model)
	}
	return model
}

// pickAgentForNewSpace runs the new-workspace flow up to the agent choice.
func pickAgentForNewSpace(t *testing.T, path, kind string) Model {
	t.Helper()
	model := newTestModel(t.TempDir())
	model.openKindPicker("space", nil, path, rect{})
	updated, _ := model.runKindPick(kind)
	return updated.(Model)
}

func TestNewWorkspaceAsksForAgentArguments(t *testing.T) {
	path := t.TempDir()
	model := pickAgentForNewSpace(t, path, "zot")
	if model.mode != modeSpaceArgs || len(model.spaces) != 1 {
		t.Fatalf("mode %d spaces %d, want the arguments prompt and no workspace yet", model.mode, len(model.spaces))
	}
	if footer := model.renderFooter(); !strings.Contains(footer, "NEW WORKSPACE") || !strings.Contains(footer, "arguments") {
		t.Fatalf("footer = %q", footer)
	}

	model = typeText(t, model, `--codemode --prompt "fix it"`)
	updated, cmd := model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeTerminal || len(model.spaces) != 2 || model.selected != 1 || cmd == nil {
		t.Fatalf("mode %d spaces %d selected %d", model.mode, len(model.spaces), model.selected)
	}
	first := model.spaces[1].tab().panes[0]
	want := []string{"--codemode", "--prompt", "fix it"}
	if first.kind != "zot" || !slices.Equal(first.args, want) {
		t.Fatalf("pane %q args %q, want %q", first.kind, first.args, want)
	}
	// The arguments are saved and come back after a restart.
	saved := model.snapshot()
	if got := saved.Workspaces[1].Tabs[0].Panes[0].Args; !slices.Equal(got, want) {
		t.Fatalf("saved args = %q", got)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if got := restored.spaces[1].tab().panes[0].args; !slices.Equal(got, want) {
		t.Fatalf("restored args = %q", got)
	}
}

func TestNewWorkspaceArgumentsAreOptional(t *testing.T) {
	model := pickAgentForNewSpace(t, t.TempDir(), "zot")
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if len(model.spaces) != 2 || len(model.spaces[1].tab().panes[0].args) != 0 {
		t.Fatalf("enter on an empty line should create the workspace without arguments")
	}
	if saved := model.snapshot(); saved.Workspaces[1].Tabs[0].Panes[0].Args != nil {
		t.Fatal("empty arguments must not be written to the state file")
	}
}

func TestNewWorkspaceArgumentsCancelAndErrors(t *testing.T) {
	model := pickAgentForNewSpace(t, t.TempDir(), "zot")
	// An unterminated quote keeps the prompt open with its text.
	model = typeText(t, model, `--prompt "open`)
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeSpaceArgs || len(model.spaces) != 1 || model.input.Value() != `--prompt "open` {
		t.Fatalf("bad quote: mode %d spaces %d input %q", model.mode, len(model.spaces), model.input.Value())
	}
	// Esc cancels the whole workspace.
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.mode != modeTerminal || len(model.spaces) != 1 || model.argsPath != "" {
		t.Fatalf("esc: mode %d spaces %d", model.mode, len(model.spaces))
	}
}

// Shells take no agent arguments and start right away, as before.
func TestNewShellWorkspaceSkipsArguments(t *testing.T) {
	model := pickAgentForNewSpace(t, t.TempDir(), "shell")
	if model.mode != modeTerminal || len(model.spaces) != 2 {
		t.Fatalf("mode %d spaces %d", model.mode, len(model.spaces))
	}
}

// Arguments follow the agent's own and the command-line zot flags, and the
// resume flag still lands where the agent expects it.
func TestPaneArgumentsReachTheAgent(t *testing.T) {
	model := New(Config{Shell: "/bin/sh", ZotArgs: []string{"--provider", "x"}}, []string{t.TempDir()}, "", state.State{})
	target := model.spaces[0].tab().panes[0]
	target.args = []string{"--codemode"}
	if got := model.extraAgentArgs(target); !slices.Equal(got, []string{"--provider", "x", "--codemode"}) {
		t.Fatalf("extra args = %q", got)
	}
	// The config slice must not grow when pane arguments are appended.
	if len(model.config.ZotArgs) != 2 {
		t.Fatalf("config args changed: %q", model.config.ZotArgs)
	}
	spec := agentByKind("zot")
	launch := spec.LaunchArgs(model.extraAgentArgs(target), true)
	if !slices.Contains(launch, "--codemode") || !slices.Contains(launch, "--continue") {
		t.Fatalf("launch args = %q", launch)
	}
}
