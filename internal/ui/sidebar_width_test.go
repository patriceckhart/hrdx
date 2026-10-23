package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestClampSidebarWidth(t *testing.T) {
	for _, test := range []struct{ width, window, want int }{
		{0, 200, sidebarWidth},
		{5, 200, minSidebarWidth},
		{50, 200, 50},
		{150, 400, maxSidebarWidth},
		{70, 100, 50},             // the terminal keeps half the window
		{70, 30, minSidebarWidth}, // tiny windows still get a usable sidebar
		{40, 0, 40},               // before the first size message
	} {
		if got := clampSidebarWidth(test.width, test.window); got != test.want {
			t.Errorf("clampSidebarWidth(%d, %d) = %d, want %d", test.width, test.window, got, test.want)
		}
	}
}

func TestSidebarBorderDragResizesAndPersists(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 160, 40
	before := model.terminalArea().w

	press := tea.MouseMsg{X: sidebarWidth, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	updated, _ := model.updateMouse(press)
	model = updated.(Model)
	if !model.dragSidebar {
		t.Fatal("pressing the sidebar border should start a resize")
	}
	updated, _ = model.updateMouse(tea.MouseMsg{X: 44, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	model = updated.(Model)
	updated, _ = model.updateMouse(tea.MouseMsg{X: 46, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	model = updated.(Model)

	if model.dragSidebar || model.sidebarContentWidth() != 46 {
		t.Fatalf("width = %d, dragging = %v", model.sidebarContentWidth(), model.dragSidebar)
	}
	if got := model.terminalArea().w; got != before-(46-sidebarWidth) {
		t.Fatalf("terminal width = %d, want %d", got, before-(46-sidebarWidth))
	}
	for _, row := range model.sidebarRows() {
		if ansi.StringWidth(row.label) > 46 {
			t.Fatalf("sidebar row wider than sidebar: %q", row.label)
		}
	}

	saved := model.snapshot()
	if saved.SidebarWidth != 46 {
		t.Fatalf("saved width = %d", saved.SidebarWidth)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	restored.width = 160
	if restored.sidebarContentWidth() != 46 {
		t.Fatalf("restored width = %d", restored.sidebarContentWidth())
	}

	// Collapsing ignores the custom width and expanding brings it back.
	model.toggleSidebar()
	if model.sidebarContentWidth() != collapsedSidebarWidth {
		t.Fatal("collapsed sidebar should keep its compact width")
	}
	model.toggleSidebar()
	if model.sidebarContentWidth() != 46 {
		t.Fatal("expanding should restore the dragged width")
	}

	// Dragging back to the default stores the zero value.
	model.setSidebarWidth(sidebarWidth)
	if model.snapshot().SidebarWidth != 0 {
		t.Fatal("default width should not be persisted")
	}
}

func TestCollapsedSidebarBorderDoesNotResize(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 160, 40
	model.sideCollapsed = true
	updated, _ := model.updateMouse(tea.MouseMsg{X: collapsedSidebarWidth, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if updated.(Model).dragSidebar {
		t.Fatal("the compact sidebar has a fixed width")
	}
}
