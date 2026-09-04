package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
)

func TestSettingsOpenToggleClose(t *testing.T) {
	model := newTestModel("/tmp/api")

	// ctrl+b , opens the settings window.
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{','}})
	model = updated.(Model)
	if model.mode != modeSettings {
		t.Fatalf("mode = %d, want modeSettings", model.mode)
	}

	// Enter toggles the selected agent row.
	first := model.settingsRows()[0]
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	kind := first.action[len("toggle:"):]
	if !model.disabled[kind] {
		t.Fatalf("agent %q should be disabled after enter", kind)
	}

	// Tab switches to the sound section.
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.settingsTab != 1 {
		t.Fatalf("settingsTab = %d, want 1", model.settingsTab)
	}
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if !model.soundOn {
		t.Fatal("sound should be on after toggling in the sound tab")
	}

	// Esc closes.
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after esc", model.mode)
	}
}

func TestAutoCopySettingDefaultsOnAndPersists(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, nil, "", state.State{})
	if !model.autoCopy {
		t.Fatal("legacy state should default automatic copying to on")
	}
	model.settingsTab = 3
	rows := model.settingsRows()
	if len(rows) != 1 || rows[0].action != "auto-copy" || rows[0].label != "[x] automatically copy selected text" {
		t.Fatalf("terminal settings rows = %+v", rows)
	}

	model.toggleSettingsRow(rows[0])
	if model.autoCopy || !model.snapshot().DisableAutoCopy {
		t.Fatal("automatic copying should be off and persisted after toggling")
	}
	if restored := New(Config{Shell: "/bin/sh"}, nil, "", model.snapshot()); restored.autoCopy {
		t.Fatal("restored model should keep automatic copying off")
	}
}

func TestSelectionRespectsAutoCopySetting(t *testing.T) {
	for _, test := range []struct {
		name     string
		autoCopy bool
		wantCopy string
	}{
		{name: "enabled", autoCopy: true, wantCopy: "hello"},
		{name: "disabled", autoCopy: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := newTestModel("/tmp/api")
			target := model.currentPane()
			target.term = term.NewHolderPane(&keyboardCaptureHost{}, 1, 80, 24)
			target.term.Feed([]byte("hello"))
			target.term.StartSelection(0, 0)
			target.term.ExtendSelection(4, 0)
			model.selPane = target
			model.selRect = rect{w: 80, h: 24}
			model.autoCopy = test.autoCopy
			copied := ""
			model.clipboardCopy = func(text string) { copied = text }

			updated, _ := model.updateMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
			model = updated.(Model)
			if copied != test.wantCopy {
				t.Fatalf("clipboard text = %q, want %q", copied, test.wantCopy)
			}
			if test.autoCopy && model.status != "copied 5 chars" {
				t.Fatalf("status = %q, want copy confirmation", model.status)
			}
			if !test.autoCopy && model.status != "" {
				t.Fatalf("status = %q, want no copy confirmation", model.status)
			}
			if !target.term.HasSelection() {
				t.Fatal("completed selection should remain highlighted")
			}
		})
	}
}

func TestThemeSettingsScrollAndSelectBundledPreset(t *testing.T) {
	defer resetThemes()
	model := newTestModel("/tmp/api")
	model.height = 20
	model.settingsTab = 2
	rows := model.settingsRows()
	if len(rows) <= model.height {
		t.Fatalf("theme rows = %d, want a scrollable section", len(rows))
	}
	model.settingsIndex = len(rows) - 1
	box := model.settingsBox()
	visible, offset := model.visibleSettingsRows(box)
	if box.h > model.height-2 || offset+len(visible) != len(rows) {
		t.Fatalf("box=%+v visible=%d offset=%d rows=%d", box, len(visible), offset, len(rows))
	}

	model.toggleSettingsRow(rows[model.settingsIndex])
	if model.themeName != "coffee" || colorAccent != "#d8a657" {
		t.Fatalf("selected theme = %q accent = %q", model.themeName, colorAccent)
	}
}

func TestSettingsCustomNavigationKeys(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.navKeys = buildNavigationKeys(map[string]string{
		"navigate-up": "home", "navigate-down": "end",
	})
	model.openSettings()
	model.settingsIndex = 1

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyHome})
	model = updated.(Model)
	if model.settingsIndex != 0 {
		t.Fatalf("settingsIndex after home = %d, want 0", model.settingsIndex)
	}
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEnd})
	model = updated.(Model)
	if model.settingsIndex != 1 {
		t.Fatalf("settingsIndex after end = %d, want 1", model.settingsIndex)
	}
}

func TestSettingsMouseOutsideCloses(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.openSettings()
	updated, _ := model.updateMouse(tea.MouseMsg{X: 0, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after outside click", got.mode)
	}
}

func TestSettingsSidebarEntryOpens(t *testing.T) {
	model := newTestModel("/tmp/api")
	// The pinned settings row sits above the trailing blank line; body
	// height is m.height-2, so its body row is height-2-2 and its screen
	// Y is one more than that.
	y := model.height - 3
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeSettings {
		t.Fatalf("mode = %d, want modeSettings after sidebar click", got.mode)
	}
}

func TestTrackBusyDebouncesSound(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.soundOn = true
	target := model.spaces[0].tab().panes[0]

	// Idle pane that was never busy: no command.
	if cmd := model.trackBusy(target); cmd != nil {
		t.Fatal("idle pane must not schedule a sound")
	}

	// Simulate a busy phase, then idle: a confirm command is scheduled.
	model.wasBusy[target.id] = true
	if cmd := model.trackBusy(target); cmd == nil {
		t.Fatal("busy -> idle should schedule the confirm tick")
	}
	if model.wasBusy[target.id] {
		t.Fatal("transition should consume the busy mark")
	}

	// Attention still needs a debounced completion when sound is off.
	model.soundOn = false
	model.wasBusy[target.id] = true
	if cmd := model.trackBusy(target); cmd == nil {
		t.Fatal("sound off must still schedule completion confirmation")
	}
}

func TestAgentFinishSetsAttentionWhenUnfocused(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	target := model.spaces[1].tab().panes[0]
	model.wasBusy[target.id] = true
	if cmd := model.trackBusy(target); cmd == nil {
		t.Fatal("busy -> idle should schedule confirmation")
	}

	updated, _ := model.Update(soundConfirmMsg{id: target.id, seq: model.soundSeq[target.id]})
	model = updated.(Model)
	if !model.paneAttention[target.id] {
		t.Fatal("confirmed unfocused agent finish did not set attention")
	}
}

func TestFocusedAndStaleAgentFinishDoNotSetAttention(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	focused := model.spaces[0].tab().panes[0]
	model.soundSeq[focused.id] = 1
	updated, _ := model.Update(soundConfirmMsg{id: focused.id, seq: 1})
	model = updated.(Model)
	if model.paneAttention[focused.id] {
		t.Fatal("focused agent finish set attention")
	}

	target := model.spaces[1].tab().panes[0]
	model.soundSeq[target.id] = 2
	updated, _ = model.Update(soundConfirmMsg{id: target.id, seq: 1})
	model = updated.(Model)
	if model.paneAttention[target.id] {
		t.Fatal("stale finish timer set attention")
	}
}
