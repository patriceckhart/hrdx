package ui

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// threeTabs returns a model with tabs named a, b, c in one workspace and
// tab a active.
func threeTabs(t *testing.T) (Model, *space, [3]*tab) {
	t.Helper()
	model := newTestModel("/tmp/api")
	owner := model.spaces[0]
	model.addTab(owner, "shell")
	model.addTab(owner, "shell")
	for index, name := range []string{"a", "b", "c"} {
		owner.tabs[index].name = name
	}
	owner.active = 0
	return model, owner, [3]*tab{owner.tabs[0], owner.tabs[1], owner.tabs[2]}
}

func tabOrder(owner *space) string {
	out := ""
	for _, current := range owner.tabs {
		out += current.name
	}
	return out
}

func pressPrefix(t *testing.T, model Model, key rune) Model {
	t.Helper()
	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	updated, _ = updated.(Model).updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	return updated.(Model)
}

func TestPrefixMovesActiveTab(t *testing.T) {
	model, owner, tabs := threeTabs(t)

	model = pressPrefix(t, model, '>')
	if tabOrder(owner) != "bac" || owner.active != 1 || owner.tabs[owner.active] != tabs[0] {
		t.Fatalf("after > order = %s active = %d, want bac and a still active", tabOrder(owner), owner.active)
	}
	model = pressPrefix(t, model, '>')
	model = pressPrefix(t, model, '>') // already last: stays put, no wrap
	if tabOrder(owner) != "bca" || owner.active != 2 {
		t.Fatalf("at the end order = %s active = %d, want bca/2", tabOrder(owner), owner.active)
	}
	model = pressPrefix(t, model, '<')
	if tabOrder(owner) != "bac" || owner.active != 1 || model.mode != modeTerminal {
		t.Fatalf("after < order = %s active = %d mode = %d", tabOrder(owner), owner.active, model.mode)
	}
}

func TestMoveTabToBounds(t *testing.T) {
	_, owner, tabs := threeTabs(t)
	if moveTabTo(owner, tabs[0], -1) || moveTabTo(owner, tabs[2], 3) || moveTabTo(owner, tabs[1], 1) {
		t.Fatal("out-of-range or same-index moves must report no change")
	}
	if moveTabTo(owner, &tab{}, 0) {
		t.Fatal("a tab from another workspace must not move")
	}
	if !moveTabTo(owner, tabs[2], 0) || tabOrder(owner) != "cab" || owner.active != 0 {
		t.Fatalf("order = %s active = %d, want cab/0", tabOrder(owner), owner.active)
	}
}

// Moving a tab keeps its panes and layout attached, so a snapshot saves the
// new order with each tab's own content.
func TestMovedTabOrderPersists(t *testing.T) {
	model, owner, tabs := threeTabs(t)
	paneOfC := tabs[2].panes[0].id
	moveTabTo(owner, tabs[2], 0)

	saved := model.snapshot()
	saved0 := saved.Workspaces[0]
	if saved0.Tabs[0].Name != "c" || saved0.Tabs[1].Name != "a" || saved0.Tabs[2].Name != "b" || saved0.Active != 0 {
		t.Fatalf("saved tabs = %s %s %s active %d", saved0.Tabs[0].Name, saved0.Tabs[1].Name, saved0.Tabs[2].Name, saved0.Active)
	}
	if owner.tabs[0].panes[0].id != paneOfC || !layoutComplete(owner.tabs[0].layout, owner.tabs[0].panes) {
		t.Fatal("moved tab lost its panes or layout")
	}
}

func TestTabMenuOffersOnlyPossibleMoves(t *testing.T) {
	model, owner, tabs := threeTabs(t)
	actions := func(target *tab) []string {
		model.openTabMenu(target, rect{})
		var out []string
		for _, item := range model.menuItems() {
			out = append(out, item.action)
		}
		model.closeMenu()
		return out
	}
	if got := actions(tabs[0]); slices.Contains(got, "tab-move-left") || !slices.Contains(got, "tab-move-right") {
		t.Fatalf("first tab menu = %v", got)
	}
	if got := actions(tabs[1]); !slices.Contains(got, "tab-move-left") || !slices.Contains(got, "tab-move-right") {
		t.Fatalf("middle tab menu = %v", got)
	}
	if got := actions(tabs[2]); !slices.Contains(got, "tab-move-left") || slices.Contains(got, "tab-move-right") {
		t.Fatalf("last tab menu = %v", got)
	}

	// The menu action moves the tab it was opened on.
	model.openTabMenu(tabs[1], rect{})
	updated, _ := model.runMenuAction("tab-move-left")
	if tabOrder(owner) != "bac" || updated.(Model).mode != modeTerminal {
		t.Fatalf("menu move order = %s", tabOrder(owner))
	}

	// A single tab has nothing to move or close.
	single := newTestModel("/tmp/web")
	single.openTabMenu(single.spaces[0].tabs[0], rect{})
	for _, item := range single.menuItems() {
		if item.action == "tab-move-left" || item.action == "tab-move-right" || item.action == "tab-close" {
			t.Fatalf("single tab menu offers %q", item.action)
		}
	}
}

func TestTabBarDragReordersTabs(t *testing.T) {
	model, owner, tabs := threeTabs(t)
	// Labels " a ", " b ", " c " cover local x 0..2, 3..5, 6..8.
	at := func(localX int) int { return model.sidebarContentWidth() + 1 + localX }

	updated, _ := model.updateMouse(tea.MouseMsg{X: at(1), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	if model.dragTab != tabs[0] || owner.active != 0 {
		t.Fatal("press on a tab should select it and arm the drag")
	}
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(7), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if tabOrder(owner) != "bca" || owner.active != 2 {
		t.Fatalf("after drag order = %s active = %d, want bca/2", tabOrder(owner), owner.active)
	}
	// Motion off the tab bar is ignored.
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(1), Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if tabOrder(owner) != "bca" {
		t.Fatalf("motion off the bar moved tabs: %s", tabOrder(owner))
	}
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(7), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	model = updated.(Model)
	if model.dragTab != nil || model.dragMoved {
		t.Fatal("release should clear the drag state")
	}
}

func TestTabBarClickWithoutMotionKeepsOrder(t *testing.T) {
	model, owner, _ := threeTabs(t)
	x := model.sidebarContentWidth() + 1 + 4
	updated, _ := model.updateMouse(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	updated, _ = updated.(Model).updateMouse(tea.MouseMsg{X: x, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	model = updated.(Model)
	if tabOrder(owner) != "abc" || owner.active != 1 || model.dragTab != nil {
		t.Fatalf("click order = %s active = %d", tabOrder(owner), owner.active)
	}
}

// A narrow tab dragged onto a wide neighbour must not flip back and forth:
// the move is kept only when the pointer lands inside the dragged tab.
func TestTabBarDragAcrossWideTabIsStable(t *testing.T) {
	model, owner, tabs := threeTabs(t)
	tabs[1].name = "wide-tab-name" // " wide-tab-name " spans local x 3..17
	at := func(localX int) int { return model.sidebarContentWidth() + 1 + localX }

	updated, _ := model.updateMouse(tea.MouseMsg{X: at(1), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	// Just inside the wide tab: moving a there would put the pointer over
	// the wide tab again, so nothing changes yet.
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(4), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if tabOrder(owner) != "awide-tab-namec" {
		t.Fatalf("early swap: %s", tabOrder(owner))
	}
	// Past the wide tab's width: a lands at index 1 under the pointer.
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(16), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	model = updated.(Model)
	if owner.tabs[1] != tabs[0] {
		t.Fatalf("order after crossing = %s", tabOrder(owner))
	}
	// More motion within the same cell keeps the order.
	updated, _ = model.updateMouse(tea.MouseMsg{X: at(17), Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	if owner.tabs[1] != tabs[0] {
		t.Fatalf("flip-flop: %s", tabOrder(owner))
	}
	_ = updated
}
