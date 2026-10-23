package ui

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/term"
)

// The zot pane sits in the second, unfocused workspace.
func programStatusPane(t *testing.T) (Model, *pane) {
	t.Helper()
	model := newTestModel("/tmp/api", "/tmp/web")
	target := model.spaces[1].tab().panes[0]
	if target.kind != "zot" {
		t.Fatalf("pane kind = %q, want zot", target.kind)
	}
	target.term = term.NewHolderPane(nil, 1, 80, 24)
	target.running = true
	return model, target
}

// Themes such as zot's arc spinners draw no braille glyphs.
func TestProgramStatusWorkingIsBusyWithoutSpinner(t *testing.T) {
	model, target := programStatusPane(t)
	target.term.Feed([]byte("\x1b]7501;state=working:app=zot\x1b\\◜ thinking"))
	if !model.paneBusy(target) {
		t.Fatal("working report must read as busy without a braille spinner")
	}
	if got, want := model.sidebarPaneIcon(target), paneIconCell(styleDotBusy, spinnerFrames[0]); got != want {
		t.Fatalf("icon = %q, want animated spinner %q", got, want)
	}

	model.trackBusy(target)
	target.term.Feed([]byte("\x1b]7501;state=done:app=zot\x1b\\"))
	if cmd := model.trackBusy(target); cmd == nil {
		t.Fatal("working -> done must schedule the finish confirmation")
	}
}

func TestProgramStatusIdleOverridesScreenSpinner(t *testing.T) {
	model, target := programStatusPane(t)
	target.term.Feed([]byte("\x1b]7501;state=idle:app=zot\x1b\\example ⠋ ⠙ ⠚"))
	if model.paneBusy(target) {
		t.Fatal("idle report must override braille glyphs in transcript text")
	}
	for _, state := range []string{"done", "error"} {
		target.term.Feed([]byte("\x1b]7501;state=" + state + ":app=zot\x1b\\"))
		if model.paneBusy(target) {
			t.Fatalf("%s report must read as idle", state)
		}
	}
}

func TestProgramStatusBlockedRequestsAttention(t *testing.T) {
	model, target := programStatusPane(t)
	target.term.Feed([]byte("\x1b]7501;state=blocked:app=zot:kind=permission\x1b\\⠋"))
	if model.paneBusy(target) {
		t.Fatal("blocked report must not read as busy")
	}
	if got, want := model.sidebarPaneIcon(target), paneIconCell(styleDotBusy, "●"); got != want {
		t.Fatalf("unfocused blocked icon = %q, want orange circle %q", got, want)
	}
}

// Claude Code cannot be made to report through ConPTY, so its title decides.
func TestClaudeTitleDrivesBusyState(t *testing.T) {
	model, target := programStatusPane(t)
	target.kind = "claude"
	steps := []struct {
		title string
		busy  bool
	}{
		{"✳ Claude Code", false},
		{"◐ Claude Code", true},
		{"◑ Counting from one to five", true},
		{"✳ Counting from one to five", false},
	}
	target.term.Feed([]byte("example ⠋ in transcript"))
	for _, step := range steps {
		target.term.Feed([]byte("\x1b]0;" + step.title + "\x07"))
		if got := model.paneBusy(target); got != step.busy {
			t.Fatalf("title %q: busy = %v, want %v", step.title, got, step.busy)
		}
	}
}

func TestProgramStatusAbsentFallsBackToScreen(t *testing.T) {
	model, target := programStatusPane(t)
	target.term.Feed([]byte("⠋ Working"))
	if !model.paneBusy(target) {
		t.Fatal("without a report a visible spinner must still read as busy")
	}
	target.term.Feed([]byte("\x1b]7501;state=idle:app=zot\x1b\\\x1b]7501;state=clear\x1b\\"))
	if !model.paneBusy(target) {
		t.Fatal("a cleared status must hand detection back to the screen")
	}
}
