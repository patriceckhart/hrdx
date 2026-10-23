package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func prefixKey(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

// splitWith runs one prefix key on a fresh single-pane model and reports
// whether the resulting split is side by side.
func splitWith(t *testing.T, direction, key string, width, height int) bool {
	t.Helper()
	model := newTestModel(t.TempDir())
	model.width, model.height = width, height
	model.splitDirection = direction
	updated, _ := model.runPrefix(prefixKey(key))
	model = updated.(Model)
	root := model.currentSpace().tab().layout
	if root == nil || root.pane != nil {
		t.Fatalf("key %q with %q did not split", key, direction)
	}
	return root.vertical
}

func TestDefaultSplitDirectionDrivesPrimaryKeys(t *testing.T) {
	for _, test := range []struct {
		direction string
		key       string
		vertical  bool
	}{
		// Default keeps today's behavior.
		{splitDirRight, "a", true},
		{splitDirRight, "A", false},
		{splitDirRight, "s", true},
		{splitDirRight, "S", false},
		// "below" swaps the primary and shifted keys.
		{splitDirDown, "a", false},
		{splitDirDown, "A", true},
		{splitDirDown, "s", false},
		{splitDirDown, "S", true},
		// tmux-style keys never move.
		{splitDirDown, "%", true},
		{splitDirDown, "|", true},
		{splitDirRight, "\"", false},
		{splitDirRight, "-", false},
	} {
		if got := splitWith(t, test.direction, test.key, 160, 40); got != test.vertical {
			t.Errorf("direction %q key %q: vertical = %v, want %v", test.direction, test.key, got, test.vertical)
		}
	}
}

func TestAutoSplitFollowsLongerSide(t *testing.T) {
	if !splitWith(t, splitDirAuto, "s", 200, 40) {
		t.Fatal("wide pane should split right")
	}
	if splitWith(t, splitDirAuto, "s", 70, 60) {
		t.Fatal("tall pane should split below")
	}
	if splitWith(t, splitDirAuto, "S", 200, 40) {
		t.Fatal("shifted key should take the other direction")
	}
}

func TestPickerFollowsDefaultSplit(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 160, 40
	model.splitDirection = splitDirDown
	updated, _ := model.runPrefix(prefixKey("c"))
	if got := updated.(Model).pickAction; got != "split-down" {
		t.Fatalf("c opened %q", got)
	}
	updated, _ = model.runPrefix(prefixKey("C"))
	if got := updated.(Model).pickAction; got != "split-right" {
		t.Fatalf("C opened %q", got)
	}
}

func TestSplitDirectionCycleAndSettings(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 160, 40
	if model.prefixKeys["v"] != "split-cycle" {
		t.Fatal("v should cycle the split direction")
	}
	for _, want := range []string{splitDirDown, splitDirAuto, splitDirRight} {
		updated, _ := model.runPrefix(prefixKey("v"))
		model = updated.(Model)
		if model.splitDirection != want {
			t.Fatalf("cycle = %q, want %q", model.splitDirection, want)
		}
	}

	model.settingsTab = 3
	var chosen settingsRow
	for _, row := range model.settingsRows() {
		if row.action == "split-dir:auto" {
			chosen = row
		}
	}
	if chosen.label != "  ( ) auto (longer side)" {
		t.Fatalf("settings row = %q", chosen.label)
	}
	model.toggleSettingsRow(chosen)
	saved := model.snapshot()
	if saved.SplitDirection != splitDirAuto {
		t.Fatalf("saved = %q", saved.SplitDirection)
	}
	if restored := New(model.config, nil, "", saved); restored.splitDirection != splitDirAuto {
		t.Fatal("restore should keep the direction")
	}
	saved.SplitDirection = "diagonal"
	if restored := New(model.config, nil, "", saved); restored.splitDirection != splitDirRight {
		t.Fatal("unknown values fall back to right")
	}
}
