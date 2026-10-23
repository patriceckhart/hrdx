package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func sidebarText(m Model) []string {
	var out []string
	for _, row := range m.sidebarRows() {
		out = append(out, strings.TrimRight(ansi.Strip(row.label), " "))
	}
	return out
}

func spaceRowIndex(t *testing.T, m Model, space int) (int, sidebarRow) {
	t.Helper()
	for index, row := range m.sidebarRows() {
		if row.fold && row.space == space {
			return index, row
		}
	}
	t.Fatalf("no name row for workspace %d", space)
	return 0, sidebarRow{}
}

func TestFoldToggleHidesWorkspaceRows(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	model.width, model.height = 120, 40
	model.foldEnabled = true
	paneRowsOf := func(space int) int {
		count := 0
		for _, row := range model.sidebarRows() {
			if row.kind == "pane" && row.space == space {
				count++
			}
		}
		return count
	}
	paneRows := func() int { return paneRowsOf(0) }
	if paneRows() == 0 {
		t.Fatal("expanded workspace should list its panes")
	}

	index, row := spaceRowIndex(t, model, 0)
	plain := ansi.Strip(row.label)
	if !strings.Contains(plain, foldOpenGlyph) || ansi.StringWidth(row.label) != model.sidebarContentWidth() {
		t.Fatalf("name row = %q (width %d)", plain, ansi.StringWidth(row.label))
	}
	toggleX := ansi.StringWidth(plain[:strings.LastIndex(plain, foldOpenGlyph)])
	if toggleX != model.sidebarContentWidth()-2 {
		t.Fatalf("toggle at column %d", toggleX)
	}

	// A click next to the toggle only selects the workspace.
	updated, _ := model.updateMouse(tea.MouseMsg{X: toggleX - 2, Y: index + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	updated, _ = model.updateMouse(tea.MouseMsg{X: toggleX - 2, Y: index + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	model = updated.(Model)
	if model.spaces[0].folded {
		t.Fatal("clicking beside the toggle must not fold")
	}

	updated, _ = model.updateMouse(tea.MouseMsg{X: toggleX, Y: index + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	if !model.spaces[0].folded || paneRows() != 0 {
		t.Fatalf("folded = %v, pane rows = %d", model.spaces[0].folded, paneRows())
	}
	_, row = spaceRowIndex(t, model, 0)
	plain = ansi.Strip(row.label)
	if !strings.Contains(plain, foldClosedGlyph) || !strings.Contains(plain, " 1") {
		t.Fatalf("folded row = %q", plain)
	}
	if paneRowsOf(1) == 0 {
		t.Fatal("folding one workspace hid another")
	}
	if len(model.spaces[0].tabs[0].panes) != 1 {
		t.Fatal("folding must not touch panes")
	}

	saved := model.snapshot()
	if !saved.Workspaces[0].Folded || saved.Workspaces[1].Folded {
		t.Fatalf("saved folds = %v %v", saved.Workspaces[0].Folded, saved.Workspaces[1].Folded)
	}
	restored := New(model.config, nil, "", saved)
	if !restored.spaces[0].folded {
		t.Fatal("restore should keep the fold")
	}
	model.openSpaceMenu(model.spaces[0], rect{})
	items := model.menuItems()
	if items[len(items)-1] != (menuItem{"Expand", "space-fold"}) {
		t.Fatalf("folded menu = %+v", items)
	}
	updated, _ = model.runMenuAction("space-fold")
	model = updated.(Model)
	if model.spaces[0].folded || paneRows() == 0 {
		t.Fatal("expand should show the panes again")
	}
	model.openSpaceMenu(model.spaces[0], rect{})
	items = model.menuItems()
	if items[len(items)-1] != (menuItem{"Collapse", "space-fold"}) {
		t.Fatalf("expanded menu = %+v", items)
	}
}

func TestFoldSummaryShowsAttention(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	model.width, model.height = 120, 40
	model.foldEnabled = true
	model.spaces[0].folded = true
	id := model.spaces[0].tabs[0].panes[0].id
	_, row := spaceRowIndex(t, model, 0)
	if strings.Contains(ansi.Strip(row.label), "●") {
		t.Fatal("no attention yet")
	}
	model.paneAttention[id] = true
	_, row = spaceRowIndex(t, model, 0)
	if !strings.Contains(ansi.Strip(row.label), "● 1") {
		t.Fatalf("folded summary = %q", ansi.Strip(row.label))
	}
}

func TestFoldToggleStaysVisibleInGroups(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 120, 40
	model.foldEnabled = true
	model.spaces[0].groupPath = []string{"team", "nested"}
	model.spaces[0].name = strings.Repeat("long-workspace-name", 3)
	index, row := spaceRowIndex(t, model, 0)
	plain := ansi.Strip(row.label)
	if !strings.Contains(plain, foldOpenGlyph) || ansi.StringWidth(row.label) > model.sidebarContentWidth() {
		t.Fatalf("grouped name row = %q", plain)
	}
	toggleX := ansi.StringWidth(plain[:strings.LastIndex(plain, foldOpenGlyph)])
	updated, _ := model.updateMouse(tea.MouseMsg{X: toggleX, Y: index + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !updated.(Model).spaces[0].folded {
		t.Fatal("grouped toggle should fold")
	}
}

func TestFoldingSettingDefaultsOffAndKeepsFolds(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir())
	model.width, model.height = 120, 40
	model.spaces[0].folded = true

	noToggles := func() bool {
		for _, line := range sidebarText(model) {
			if strings.ContainsAny(line, foldOpenGlyph+foldClosedGlyph) {
				return false
			}
		}
		return true
	}
	paneRows := func() int {
		count := 0
		for _, row := range model.sidebarRows() {
			if row.kind == "pane" && row.space == 0 {
				count++
			}
		}
		return count
	}
	menuHasFold := func() bool {
		model.openSpaceMenu(model.spaces[0], rect{})
		defer model.closeMenu()
		for _, item := range model.menuItems() {
			if item.action == "space-fold" {
				return true
			}
		}
		return false
	}

	// Off by default: no toggles, no menu entry, and a saved fold is ignored.
	if model.foldEnabled || !noToggles() || paneRows() == 0 || menuHasFold() {
		t.Fatal("folding should be off by default and leave the sidebar unchanged")
	}
	index, row := 0, sidebarRow{}
	for i, current := range model.sidebarRows() {
		if current.kind == "space" && current.space == 0 {
			index, row = i, current
			break
		}
	}
	if row.fold {
		t.Fatal("name row must not be a fold target while disabled")
	}
	updated, _ := model.updateMouse(tea.MouseMsg{X: model.sidebarContentWidth() - 2, Y: index + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !updated.(Model).spaces[0].folded {
		t.Fatal("clicking while disabled must not change the saved fold")
	}

	// Enabling through settings brings the toggles and the saved fold back.
	model.settingsTab = 3
	var setting settingsRow
	for _, current := range model.settingsRows() {
		if current.action == "workspace-folding" {
			setting = current
		}
	}
	if setting.label != "[ ] allow folding workspaces in the sidebar" {
		t.Fatalf("setting row = %q", setting.label)
	}
	model.toggleSettingsRow(setting)
	if !model.foldEnabled || noToggles() || paneRows() != 0 || !menuHasFold() {
		t.Fatal("enabling should show toggles and apply the saved fold")
	}
	saved := model.snapshot()
	if !saved.WorkspaceFolding || !saved.Workspaces[0].Folded {
		t.Fatal("setting and fold should be saved")
	}
	if restored := New(model.config, nil, "", saved); !restored.foldEnabled {
		t.Fatal("restore should keep the setting")
	}

	// Disabling again shows everything but keeps the fold for later.
	model.toggleSettingsRow(setting)
	if model.foldEnabled || paneRows() == 0 || !model.snapshot().Workspaces[0].Folded {
		t.Fatal("disabling should expand the view without forgetting folds")
	}
}

func TestCompactSidebarHasNoFoldToggle(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.width, model.height = 120, 40
	model.foldEnabled = true
	model.sideCollapsed = true
	for _, line := range sidebarText(model) {
		if strings.ContainsAny(line, foldOpenGlyph+foldClosedGlyph) {
			t.Fatalf("compact sidebar shows a fold toggle: %q", line)
		}
	}
}
