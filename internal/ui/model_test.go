package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/holder"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
)

func newTestModel(paths ...string) Model {
	model := New(Config{Shell: "/bin/sh"}, paths, "", state.State{})
	model.width, model.height = 120, 35
	return model
}

func TestNewCreatesSpaceWithZotPane(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	if len(model.spaces) != 2 {
		t.Fatalf("spaces = %d, want 2", len(model.spaces))
	}
	first := model.spaces[0].tab()
	if model.spaces[0].name != "api" || len(first.panes) != 1 {
		t.Fatalf("space[0] = %+v, want one zot pane named api", model.spaces[0])
	}
	if first.panes[0].kind != "zot" {
		t.Fatalf("pane kind = %q, want zot", first.panes[0].kind)
	}
}

func TestPrefixSwitchesSpaces(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	if model.mode != modePrefix {
		t.Fatalf("mode = %d, want modePrefix", model.mode)
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	model = updated.(Model)
	if model.selected != 1 {
		t.Fatalf("selected space = %d, want 1", model.selected)
	}
	if model.mode != modeTerminal {
		t.Fatalf("mode = %d, want modeTerminal after prefix command", model.mode)
	}
}

func TestTabsAddAndSwitch(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]

	model.addTab(currentSpace, "zot")
	if len(currentSpace.tabs) != 2 || currentSpace.active != 1 {
		t.Fatalf("tabs = %d active = %d, want 2/1", len(currentSpace.tabs), currentSpace.active)
	}
	if len(currentSpace.tab().panes) != 1 || currentSpace.tab().panes[0].kind != "zot" {
		t.Fatalf("new tab panes = %+v, want one zot pane", currentSpace.tab().panes)
	}

	model.selectTab(1)
	if currentSpace.active != 0 {
		t.Fatalf("active = %d, want wrap to 0", currentSpace.active)
	}
	model.selectTab(-1)
	if currentSpace.active != 1 {
		t.Fatalf("active = %d, want 1", currentSpace.active)
	}
}

func TestTabHit(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "zot")

	// Labels are " 1 " (0..2) and " 2 " (3..5), then " + " (6..8).
	if index, isNew := model.tabHit(currentSpace, 1); index != 0 || isNew {
		t.Fatalf("hit(1) = %d/%v, want 0/false", index, isNew)
	}
	if index, isNew := model.tabHit(currentSpace, 4); index != 1 || isNew {
		t.Fatalf("hit(4) = %d/%v, want 1/false", index, isNew)
	}
	if _, isNew := model.tabHit(currentSpace, 7); !isNew {
		t.Fatal("hit(7) should be the + button")
	}
}

func TestSidebarHitMapsHierarchyRows(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")

	// Singleton tabs are elided: 0 top spacer, 1 WORKSPACES, 2 section gap,
	// 3 space api, 4 pane api/zot, 5 divider, 6 space web, 7 pane web/zot,
	// 8 blank, 9 new.
	if gap := model.sidebarHit(2); gap.kind != "" || gap.label != "" {
		t.Fatalf("row 2 = %+v, want blank section gap", gap)
	}
	hit := model.sidebarHit(3)
	if hit.kind != "space" || hit.space != 0 {
		t.Fatalf("row 3 = %+v, want space 0", hit)
	}
	hit = model.sidebarHit(4)
	if hit.kind != "pane" || hit.space != 0 || hit.tab != 0 || hit.pane != 0 {
		t.Fatalf("row 4 = %+v, want space 0 tab 0 pane 0", hit)
	}
	if hit = model.sidebarHit(5); hit.kind != "divider" || !strings.HasPrefix(hit.label, " ") || strings.HasPrefix(hit.label, "  ") || lipgloss.Width(hit.label) != sidebarWidth-1 || strings.Contains(hit.label, "└") {
		t.Fatalf("row 5 = %+v, want a divider aligned with workspace text and one cell from the right border", hit)
	}
	if hit = model.sidebarHit(9); hit.kind != "new" {
		t.Fatalf("row 9 = %+v, want new workspace", hit)
	} else if !strings.Contains(hit.label, "+  new workspace") {
		t.Fatalf("row 9 label = %q, want two cells between icon and text", hit.label)
	}

	panes, tabs := 0, 0
	workspacesLabel := false
	for _, row := range model.sidebarRows() {
		if strings.Contains(row.label, "AGENTS") {
			t.Fatalf("unified sidebar contains an AGENTS section label: %q", row.label)
		}
		if strings.Contains(row.label, "WORKSPACES") {
			workspacesLabel = true
		}
		switch row.kind {
		case "pane":
			panes++
		case "tab":
			tabs++
		}
	}
	if !workspacesLabel {
		t.Fatal("unified sidebar lacks the WORKSPACES section label")
	}
	if panes != 2 || tabs != 0 {
		t.Fatalf("pane/tab rows = %d/%d, want 2/0 for singleton tabs", panes, tabs)
	}
}

func TestBranchStaysWithWorkspaceName(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	model.selected = 1
	model.branches["/tmp/api"] = branchInfo{value: "main", checked: time.Now()}
	rows := model.sidebarRows()
	workspaceRow, branchRow := rows[3], rows[4]
	if workspaceRow.kind != "space" || branchRow.kind != "space" || !strings.Contains(branchRow.label, "main") {
		t.Fatalf("branch must immediately follow its workspace: %+v, %+v", workspaceRow, branchRow)
	}
	if leading := len(branchRow.label) - len(strings.TrimLeft(branchRow.label, " ")); leading != 1 {
		t.Fatalf("branch indentation = %d, want 1 to align with workspace name", leading)
	}
}

func TestSelectedWorkspaceRailSpansHierarchy(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.selected = 0
	model.branches["/tmp/api"] = branchInfo{value: "main", checked: time.Now()}
	model.addTab(model.spaces[0], "shell")

	rail := styleSpaceSel.Render("▍")
	rows := model.sidebarRows()
	for index := 3; index <= 6; index++ {
		if !strings.HasPrefix(rows[index].label, rail) {
			t.Fatalf("row %d does not continue selected workspace rail: %q", index, rows[index].label)
		}
	}
}

func TestInactiveWorkspaceDoesNotShowAnActiveTab(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	model.addTab(model.spaces[1], "shell")
	model.selected = 0

	for _, row := range model.sidebarRows() {
		if row.kind == "pane" && row.space == 1 && strings.Contains(row.label, "▸") {
			t.Fatalf("inactive workspace pane shows an active-tab marker: %q", row.label)
		}
	}
}

func TestSidebarPaneIconsAlignAcrossTabbedAndUntabbedWorkspaces(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	model.addTab(model.spaces[1], "shell")

	columns := map[int]int{}
	for _, row := range model.sidebarRows() {
		if row.kind != "pane" {
			continue
		}
		icon := strings.Index(row.label, "○")
		if icon < 0 {
			t.Fatalf("pane row has no starting icon: %q", row.label)
		}
		column := lipgloss.Width(row.label[:icon])
		if previous, ok := columns[row.space]; ok && previous != column {
			t.Fatalf("workspace %d icon columns differ: %d and %d", row.space, previous, column)
		}
		columns[row.space] = column
	}
	if columns[0] != columns[1] {
		t.Fatalf("untabbed/tabbed icon columns = %d/%d, want equal", columns[0], columns[1])
	}
}

func TestSidebarPaneRowsUseSharedShellAndAgentSymbols(t *testing.T) {
	model := newTestModel("/tmp/api")
	rows := model.sidebarRows()
	workspacePane := rows[4]
	if !strings.Contains(workspacePane.label, "○") || !strings.Contains(workspacePane.label, "starting") {
		t.Fatalf("starting agent row lacks shared pane symbol or state: %q", workspacePane.label)
	}
	for _, frame := range spinnerFrames {
		if strings.Contains(workspacePane.label, frame) {
			t.Fatalf("idle agent row contains an animated spinner: %q", workspacePane.label)
		}
	}

	shell := &pane{kind: "shell", running: true}
	if got, want := paneTypeStateIcon(shell, false, false, false, 0), paneIconCell(styleDotOn, "●"); got != want {
		t.Fatalf("running shell icon = %q, want %q", got, want)
	}
	agent := &pane{kind: "zot", running: true}
	if got, want := paneTypeStateIcon(agent, true, false, false, 0), paneIconCell(styleDotOn, "●"); got != want {
		t.Fatalf("idle agent icon = %q, want shared shell icon %q", got, want)
	}
	if got, want := paneTypeStateIcon(agent, true, true, false, 0), paneIconCell(styleDotBusy, spinnerFrames[0]); got != want {
		t.Fatalf("busy agent icon = %q, want animated spinner %q", got, want)
	}
	if got, want := paneTypeStateIcon(agent, true, false, true, 0), paneIconCell(styleDotBusy, "●"); got != want {
		t.Fatalf("completed agent icon = %q, want static orange circle %q", got, want)
	}
	if got, want := paneTypeStateIcon(agent, true, true, true, 0), paneIconCell(styleDotBusy, spinnerFrames[0]); got != want {
		t.Fatalf("busy attentive agent icon = %q, want spinner to take precedence %q", got, want)
	}
	if width := lipgloss.Width(paneTypeStateIcon(agent, true, false, false, 0)); width != 2 {
		t.Fatalf("agent icon width = %d, want fixed two-cell column", width)
	}
}

func TestFooterShowsAgentSummaryOnRight(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	if got := model.agentSummary(); got != "2 agents | 0 busy" {
		t.Fatalf("agentSummary = %q, want 2 agents | 0 busy", got)
	}
	footer := model.renderFooter()
	if !strings.Contains(footer, "2 agents | 0 busy") {
		t.Fatalf("footer missing agent summary: %q", footer)
	}
	if width := lipgloss.Width(footer); width != model.width {
		t.Fatalf("footer width = %d, want %d", width, model.width)
	}

	single := newTestModel("/tmp/api")
	if got := single.agentSummary(); got != "1 agent | 0 busy" {
		t.Fatalf("singular agentSummary = %q", got)
	}
}

func TestMenuFooterShowsCustomNavigationKeys(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.keyOverrides = map[string]string{"navigate-up": "home", "navigate-down": "end"}
	model.navKeys = buildNavigationKeys(model.keyOverrides)
	model.openKindPicker("tab", model.currentSpace(), "", rect{x: 1, y: 1})

	footer := model.renderFooter()
	if !strings.Contains(footer, "click or arrows + enter, esc closes") {
		t.Fatalf("menu footer lost default hint: %q", footer)
	}
	if !strings.Contains(footer, "home/end") {
		t.Fatalf("menu footer = %q, want custom picker keys", footer)
	}
}

func TestFooterInputDoesNotWrapOnNarrowWindow(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.width = 45
	model.mode = modeNewSpace
	model.input.Focus()
	model.input.SetValue("~")
	model.completions = []string{
		"/Users/pat/Developer/alpha",
		"/Users/pat/Developer/beta",
		"/Users/pat/Developer/gamma",
	}

	footer := model.renderFooter()
	if width := lipgloss.Width(footer); width != model.width {
		t.Fatalf("footer width = %d, want %d: %q", width, model.width, footer)
	}
	if !strings.Contains(footer, "NEW WORKSPACE") || !strings.Contains(footer, "~") {
		t.Fatalf("footer lost prompt or input: %q", footer)
	}
	if strings.Contains(footer, "agent") {
		t.Fatalf("footer should hide summary before wrapping: %q", footer)
	}
}

func TestSidebarScrollClamps(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.height = 6 // tiny: 4 sidebar rows, two pinned for settings + blank
	total := len(model.sidebarRows())
	model.sideScroll = 1000
	if got := model.sidebarOffset(total); got != total-2 {
		t.Fatalf("offset = %d, want %d", got, total-2)
	}
	model.sideScroll = -5
	if got := model.sidebarOffset(total); got != 0 {
		t.Fatalf("offset = %d, want 0", got)
	}
}

func TestSidebarOverflowMarkersUseIconSpacing(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.height = 6
	if sidebar := model.renderSidebar(); !strings.Contains(sidebar, " ↓  more") {
		t.Fatalf("sidebar missing aligned lower overflow marker: %q", sidebar)
	}
	model.sideScroll = 1
	if sidebar := model.renderSidebar(); !strings.Contains(sidebar, " ↑  more") {
		t.Fatalf("sidebar missing aligned upper overflow marker: %q", sidebar)
	}
}

func TestCollapsedSidebarKeepsCompactNames(t *testing.T) {
	model := newTestModel("/tmp/long-workspace")
	model.branches["/tmp/long-workspace"] = branchInfo{value: "feature/sidebar", checked: time.Now()}
	currentSpace := model.spaces[0]
	currentSpace.tabs[0].panes[0].name = "important agent"
	currentSpace.tabs[0].name = "long tab name"
	model.addTab(currentSpace, "shell")
	model.sideCollapsed = true

	rows := model.sidebarRows()
	if got := rows[1].label; strings.Contains(got, "WORKSPACES") || !strings.Contains(got, "→") {
		t.Fatalf("collapsed header = %q, want only the expand arrow", got)
	}
	if !model.sidebarToggleHit(1, 1) || model.sidebarToggleHit(collapsedSidebarWidth-2, 1) {
		t.Fatal("collapsed expand arrow is not aligned with workspace names")
	}
	if got := rows[3].label; !strings.Contains(got, "long-w...") {
		t.Fatalf("collapsed workspace row = %q, want six characters and an ellipsis", got)
	}
	if got := rows[4].label; !strings.Contains(got, "featur...") {
		t.Fatalf("collapsed branch row = %q, want six characters and an ellipsis", got)
	}
	if got := rows[5].label; strings.Contains(got, "important") || strings.Contains(got, "agent") {
		t.Fatalf("collapsed pane row exposes the full pane name: %q", got)
	} else if !strings.Contains(got, "○") || !strings.Contains(got, "im...") || !strings.HasSuffix(got, " ") {
		t.Fatalf("collapsed pane row = %q, want state icon, two-character name, and trailing space", got)
	}
	for _, row := range rows {
		if row.kind == "new" || strings.Contains(row.label, "new workspace") {
			t.Fatalf("collapsed sidebar exposes new workspace row: %+v", row)
		}
		if width := lipgloss.Width(row.label); width > collapsedSidebarWidth {
			t.Fatalf("collapsed sidebar row width = %d, want at most %d: %q", width, collapsedSidebarWidth, row.label)
		}
	}
	sidebar := model.renderSidebar()
	if strings.Contains(sidebar, "settings") || !strings.Contains(sidebar, "⚙") {
		t.Fatalf("collapsed settings row = %q, want gear without label", sidebar)
	}
}

func TestMouseToggleCollapsesAndExpandsSidebar(t *testing.T) {
	model := newTestModel("/tmp/api")
	wide := model.terminalArea().w

	updated, _ := model.updateMouse(tea.MouseMsg{X: sidebarWidth - 2, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if !got.sideCollapsed {
		t.Fatal("clicking the sidebar arrow should collapse it")
	}
	if got.terminalArea().w != wide+sidebarWidth-collapsedSidebarWidth {
		t.Fatalf("collapsed terminal width = %d, want %d", got.terminalArea().w, wide+sidebarWidth-collapsedSidebarWidth)
	}

	updated, _ = got.updateMouse(tea.MouseMsg{X: 1, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got = updated.(Model)
	if got.sideCollapsed {
		t.Fatal("clicking the sidebar arrow should expand it")
	}
}

func TestMouseSelectsSpace(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	// Sidebar row 6 (space web) is screen Y=7.
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.selected != 1 {
		t.Fatalf("selected = %d, want 1", got.selected)
	}
}

func TestMouseSelectsSidebarTabAndPane(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "shell")
	currentSpace.active = 0
	target := currentSpace.tabs[1].panes[0]
	model.paneAttention[target.id] = true

	activeTabPane := model.sidebarRows()[4].label
	if !strings.Contains(activeTabPane, styleSpaceSel.Render("▸")) ||
		!strings.Contains(activeTabPane, styleSpaceSel.Render("1")) ||
		!strings.Contains(activeTabPane, "zot 1") {
		t.Fatalf("first pane row should include the selected tab marker and number: %q", activeTabPane)
	}

	// Sidebar rows: 3 workspace, 4 tab 1's first pane, 5 tab 2's first pane.
	// Screen Y is sidebar row + 1.
	updated, _ := model.updateMouse(tea.MouseMsg{X: 7, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.spaces[0].active != 1 || got.spaces[0].tabs[1].selected != 0 {
		t.Fatalf("active tab/pane = %d/%d, want 1/0 after clicking combined tab/pane row",
			got.spaces[0].active, got.spaces[0].tabs[1].selected)
	}
	if got.paneAttention[target.id] {
		t.Fatal("clicking combined tab/pane row did not clear focused attention")
	}
}

func TestMouseSidebarPaneClickPersists(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	model := New(Config{Shell: "/bin/sh"}, []string{"/tmp/api"}, statePath, state.State{})
	model.width, model.height = 120, 35
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "shell")
	currentSpace.active = 0

	// Sidebar rows: 3 workspace, 4 tab 1's first pane, 5 tab 2's first pane.
	// Screen Y is sidebar row + 1, so Y=6 is tab 2's combined row.
	model.updateMouse(tea.MouseMsg{X: 7, Y: 6, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})

	saved, err := state.Load(statePath)
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if len(saved.Workspaces) != 1 {
		t.Fatalf("saved workspaces = %d, want 1", len(saved.Workspaces))
	}
	if saved.Workspaces[0].Active != 1 {
		t.Fatalf("saved active tab = %d, want 1 after clicking a pane on another tab",
			saved.Workspaces[0].Active)
	}
}

func TestMoveSpaceTo(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web", "/tmp/cli")
	api, web, cli := model.spaces[0], model.spaces[1], model.spaces[2]

	if !model.moveSpaceTo(api, 2) {
		t.Fatal("moveSpaceTo(api, 2) reported no change")
	}
	if model.spaces[0] != web || model.spaces[1] != cli || model.spaces[2] != api {
		t.Fatalf("order = %s/%s/%s, want web/cli/api",
			model.spaces[0].name, model.spaces[1].name, model.spaces[2].name)
	}
	if model.selected != 2 {
		t.Fatalf("selected = %d, want 2 (follows the moved space)", model.selected)
	}

	if !model.moveSpaceTo(api, 0) {
		t.Fatal("moveSpaceTo(api, 0) reported no change")
	}
	if model.spaces[0] != api || model.spaces[1] != web || model.spaces[2] != cli {
		t.Fatalf("order = %s/%s/%s, want api/web/cli",
			model.spaces[0].name, model.spaces[1].name, model.spaces[2].name)
	}
	if model.moveSpaceTo(api, 0) {
		t.Fatal("moveSpaceTo to the same index should report no change")
	}
	if model.moveSpaceTo(api, 5) {
		t.Fatal("moveSpaceTo out of range should report no change")
	}
}

func TestSidebarDragReordersSpaces(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	api, web := model.spaces[0], model.spaces[1]

	// Rows: 0 spacer, 1 WORKSPACES, 2 section gap, 3 space api, 4 pane,
	// 5 divider, 6 space web, 7 pane. Screen Y is row + 1.
	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.dragSpace != api {
		t.Fatal("press on a workspace row should arm the drag")
	}

	// Drag down onto web's pane row: api moves below web.
	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 8, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion})
	got = updated.(Model)
	if got.spaces[0] != web || got.spaces[1] != api {
		t.Fatalf("order after drag = %s/%s, want web/api", got.spaces[0].name, got.spaces[1].name)
	}
	if got.selected != 1 {
		t.Fatalf("selected = %d, want 1 (follows the dragged space)", got.selected)
	}

	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 8, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	got = updated.(Model)
	if got.dragSpace != nil || got.dragMoved {
		t.Fatal("release should clear the drag state")
	}
}

func TestSidebarClickWithoutMotionKeepsOrder(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	api := model.spaces[0]

	updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	updated, _ = got.updateMouse(tea.MouseMsg{X: 3, Y: 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	got = updated.(Model)
	if got.spaces[0] != api {
		t.Fatal("a plain click must not reorder workspaces")
	}
	if got.selected != 0 || got.dragSpace != nil {
		t.Fatalf("selected = %d dragSpace = %v, want 0/nil", got.selected, got.dragSpace)
	}
}

func TestMouseTabBarAddsTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	// Tab bar is screen row 1; "+" for a single tab sits at local x 3..5.
	// The click opens the kind picker; choosing a kind adds the tab.
	updated, _ := model.updateMouse(tea.MouseMsg{X: sidebarWidth + 1 + 4, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(Model)
	if got.mode != modeMenu || got.pickAction != "tab" {
		t.Fatalf("mode/pick = %d/%q, want menu/tab picker", got.mode, got.pickAction)
	}
	updated, _ = got.runKindPick("shell")
	got = updated.(Model)
	if len(got.spaces[0].tabs) != 2 {
		t.Fatalf("tabs = %d, want 2 after pick", len(got.spaces[0].tabs))
	}
	if got.spaces[0].tab().panes[0].kind != "shell" {
		t.Fatalf("pane kind = %q, want shell", got.spaces[0].tab().panes[0].kind)
	}
}

func TestEncodeSGRMouseWheel(t *testing.T) {
	got := encodeSGRMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress}, 4, 9)
	if string(got) != "\x1b[<64;5;10M" {
		t.Fatalf("wheel encoding = %q", got)
	}
}

func TestParseCSIU(t *testing.T) {
	code, mods, ok := parseCSIU([]byte("\x1b[49;5u"))
	if !ok || code != '1' || mods&modCtrl == 0 {
		t.Fatalf("parseCSIU = %q/%d/%v, want '1'/ctrl/true", code, mods, ok)
	}
	code, mods, ok = parseCSIU([]byte("\x1b[98;5u"))
	if !ok || code != 'b' || mods&modCtrl == 0 {
		t.Fatalf("parseCSIU = %q/%d/%v, want 'b'/ctrl/true", code, mods, ok)
	}
	if _, _, ok := parseCSIU([]byte("\x1b[5~")); ok {
		t.Fatal("parseCSIU accepted a non CSI-u sequence")
	}
}

func TestShellArgsForPlatform(t *testing.T) {
	if args := shellArgsFor("windows"); len(args) != 0 {
		t.Fatalf("Windows shell args = %q, want none", args)
	}
	args := shellArgsFor("linux")
	if len(args) != 1 || args[0] != "-l" {
		t.Fatalf("Unix shell args = %q, want [-l]", args)
	}
}

func TestCyclePaneUsesLayoutOrder(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	first := currentSpace.tab().panes[0]
	second := model.addPane(currentSpace, "shell", true)
	third := model.addPaneSide(currentSpace, "zot", true, true)

	// The split tree is first, third, second even though the pane slice is
	// first, second, third. Start focused on third.
	if model.currentPane() != third {
		t.Fatal("third pane should start focused")
	}
	model.cyclePane(1)
	if model.currentPane() != second {
		t.Fatal("next should follow layout order")
	}
	model.cyclePane(1)
	if model.currentPane() != first {
		t.Fatal("next should wrap from the last pane to the first")
	}
	model.cyclePane(-1)
	if model.currentPane() != second {
		t.Fatal("previous should wrap backward")
	}
}

func TestPrefixTabKeepsCyclingUntilEscape(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	first := currentSpace.tab().panes[0]
	second := model.addPane(currentSpace, "shell", true)

	updated, _ := model.updateKey(tea.KeyMsg{Type: tea.KeyCtrlB})
	model = updated.(Model)
	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.currentPane() != first || model.mode != modePrefix {
		t.Fatal("tab should focus the next pane and remain in prefix mode")
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyTab})
	model = updated.(Model)
	if model.currentPane() != second || model.mode != modePrefix {
		t.Fatal("a second tab should continue cycling panes")
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	model = updated.(Model)
	if model.currentPane() != first || model.mode != modePrefix {
		t.Fatal("shift+tab should cycle backward and remain in prefix mode")
	}

	updated, _ = model.updateKey(tea.KeyMsg{Type: tea.KeyEsc})
	model = updated.(Model)
	if model.mode != modeTerminal {
		t.Fatal("escape should leave prefix mode")
	}
}

func TestResolveDirRejectsMissing(t *testing.T) {
	if _, err := resolveDir("/definitely/not/here-12345"); err == nil {
		t.Fatal("resolveDir accepted a missing directory")
	}
	dir := t.TempDir()
	path, err := resolveDir(dir)
	if err != nil || path == "" {
		t.Fatalf("resolveDir(%s) = %q/%v", dir, path, err)
	}
}

func TestHolderSessionMatchesWorkspace(t *testing.T) {
	workspace := t.TempDir()
	sessions := []holder.SessionInfo{
		{ID: 7, CWD: workspace},
		{ID: 8, CWD: t.TempDir()},
	}
	if !holderSessionMatchesWorkspace(sessions, 7, workspace) {
		t.Fatal("matching holder session was rejected")
	}
	if holderSessionMatchesWorkspace(sessions, 8, workspace) {
		t.Fatal("session from another workspace was accepted")
	}
	if holderSessionMatchesWorkspace(sessions, 9, workspace) {
		t.Fatal("missing holder session was accepted")
	}
}

func TestAnsiCutWideRunes(t *testing.T) {
	// 4 cells of plain text.
	if got := ansiCut("abcd", 0, 2); got != "ab" {
		t.Fatalf("cut = %q, want ab", got)
	}
	// CJK: each rune is 2 cells. Cutting at 2 keeps exactly one rune.
	if got := ansiCut("世界", 0, 2); got != "世" {
		t.Fatalf("cut = %q, want 世", got)
	}
	// Cutting at 3 splits the second rune: its first cell becomes a space.
	if got := ansiCut("世界", 0, 3); got != "世 " {
		t.Fatalf("cut = %q, want '世 '", got)
	}
	// Escapes survive and do not count as cells.
	if got := ansiCut("\x1b[31mab\x1b[0mcd", 0, 3); got != "\x1b[31mab\x1b[0mc" {
		t.Fatalf("cut = %q", got)
	}
	// A from-cut starting inside a wide rune pads the tail cell.
	if got := ansiCut("世x", 1, 3); got != " x" {
		t.Fatalf("cut = %q, want ' x'", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello world", 8); got != "hello w…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncate("hi", 8); got != "hi" {
		t.Fatalf("truncate = %q", got)
	}
}

func TestSidebarTabContextMenuClosesOnlyThatTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	space := model.spaces[0]
	model.addTab(space, "shell")

	tabRow := -1
	for index, row := range model.sidebarRows() {
		if row.kind == "pane" && row.space == 0 && row.tab == 1 && row.pane == 0 {
			tabRow = index
			break
		}
	}
	if tabRow < 0 {
		t.Fatal("could not find second tab in sidebar")
	}
	updated, _ := model.updateMouse(tea.MouseMsg{
		X: 1, Y: tabRow + 1, Button: tea.MouseButtonRight, Action: tea.MouseActionPress,
	})
	opened := updated.(Model)
	if opened.menuTab != space.tabs[1] || opened.menuSpace != nil {
		t.Fatalf("sidebar tab opened wrong menu: tab=%p space=%p", opened.menuTab, opened.menuSpace)
	}

	updated, _ = opened.runMenuAction("tab-close")
	closed := updated.(Model)
	if len(closed.spaces) != 1 || len(closed.spaces[0].tabs) != 1 {
		t.Fatalf("tab close changed workspace count or left tabs: spaces=%d tabs=%d", len(closed.spaces), len(closed.spaces[0].tabs))
	}
}

func TestTabMenuHidesCloseForOnlyTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.menuTab = currentSpace.tab()

	for _, item := range model.menuItems() {
		if item.action == "tab-close" {
			t.Fatal("close tab should be hidden when the workspace has only one tab")
		}
	}

	model.addTab(currentSpace, "zot")
	if items := model.menuItems(); items[len(items)-1].action != "tab-close" {
		t.Fatal("close tab should be available when another tab is open")
	}
}

func TestPaneMenuHidesCloseForOnlyPaneInTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	target := currentSpace.tab().panes[0]
	model.menuPane = target

	// Panes in other tabs do not make the target pane closable.
	model.addTab(currentSpace, "zot")
	for _, item := range model.menuItems() {
		if item.action == "close" {
			t.Fatal("close pane should be hidden when its tab has only one pane")
		}
	}

	currentSpace.active = 0
	model.addPane(currentSpace, "shell", true)
	if items := model.menuItems(); items[len(items)-1].action != "close" {
		t.Fatal("close pane should be available when another pane is open in the same tab")
	}
}

func TestExitedPaneAutoCloses(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	second := model.addPane(currentSpace, "shell", true)
	if len(currentSpace.tab().panes) != 2 {
		t.Fatal("setup: expected two panes")
	}
	exited := term.NewHolderPane(nil, 0, 80, 24)
	exited.MarkExited() // Close is a no-op on an exited pane
	second.term = exited

	updated, _ := model.Update(paneUpdateMsg{id: second.id, open: false})
	got := updated.(Model)
	if n := len(got.spaces[0].tab().panes); n != 1 {
		t.Fatalf("panes after exit = %d, want 1 (auto close)", n)
	}
}

func TestCloseCurrentPaneClamps(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addPane(currentSpace, "shell", true)
	currentTab := currentSpace.tab()
	if currentTab.selected != 1 {
		t.Fatalf("selected pane = %d, want 1", currentTab.selected)
	}
	model.paneAttention[currentTab.panes[0].id] = true
	model.closeCurrentPane()
	if len(currentTab.panes) != 1 || currentTab.selected != 0 {
		t.Fatalf("panes = %d selected = %d, want 1/0", len(currentTab.panes), currentTab.selected)
	}
	if !strings.HasPrefix(currentTab.panes[0].name, "zot") {
		t.Fatalf("remaining pane = %q, want the zot pane", currentTab.panes[0].name)
	}
	if model.paneAttention[currentTab.panes[0].id] {
		t.Fatal("newly focused sibling retained attention after pane close")
	}
}

func TestCloseLastPaneClosesTab(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addTab(currentSpace, "zot")
	if len(currentSpace.tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(currentSpace.tabs))
	}
	model.closeCurrentPane()
	if len(currentSpace.tabs) != 1 {
		t.Fatalf("tabs = %d, want 1 after closing the tab's only pane", len(currentSpace.tabs))
	}
}

func TestGitBranchDetection(t *testing.T) {
	dir := t.TempDir()
	if branch := readGitBranch(dir); branch != "" {
		t.Fatalf("branch in non-repo = %q, want empty", branch)
	}
	gitDir := dir + "/.git"
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitDir+"/HEAD", []byte("ref: refs/heads/feature/tabs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if branch := readGitBranch(dir); branch != "feature/tabs" {
		t.Fatalf("branch = %q, want feature/tabs", branch)
	}
	if err := os.WriteFile(gitDir+"/HEAD", []byte("0123456789abcdef0123456789abcdef01234567\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if branch := readGitBranch(dir); branch != "0123456" {
		t.Fatalf("detached branch = %q, want short hash", branch)
	}
}

func TestBlurredAgentFinishGetsAttentionUntilAppFocusReturns(t *testing.T) {
	model := newTestModel("/tmp/api")
	target := model.currentPane()
	updated, _ := model.Update(tea.BlurMsg{})
	model = updated.(Model)
	model.soundSeq[target.id] = 1
	updated, _ = model.Update(soundConfirmMsg{id: target.id, seq: 1})
	model = updated.(Model)
	if !model.paneAttention[target.id] {
		t.Fatal("agent completion while application was blurred did not set attention")
	}

	updated, _ = model.Update(tea.FocusMsg{})
	model = updated.(Model)
	if model.paneAttention[target.id] {
		t.Fatal("application focus did not acknowledge the focused pane")
	}
}

func TestFocusNavigationClearsAttention(t *testing.T) {
	t.Run("tab", func(t *testing.T) {
		model := newTestModel("/tmp/api")
		owner := model.currentSpace()
		first := owner.tab().panes[0]
		model.addTab(owner, "shell")
		model.paneAttention[first.id] = true
		model.selectTab(-1)
		if model.paneAttention[first.id] {
			t.Fatal("tab selection did not clear attention")
		}
	})

	t.Run("pane", func(t *testing.T) {
		model := newTestModel("/tmp/api")
		owner := model.currentSpace()
		first := owner.tab().panes[0]
		model.addPane(owner, "shell", true)
		model.paneAttention[first.id] = true
		model.cyclePane(1)
		if model.paneAttention[first.id] {
			t.Fatal("pane cycling did not clear attention")
		}
	})
}

func TestRemovePaneClearsCompletionState(t *testing.T) {
	model := newTestModel("/tmp/api")
	owner := model.currentSpace()
	target := model.addPane(owner, "shell", true)
	model.wasBusy[target.id] = true
	model.soundSeq[target.id] = 2
	model.paneAttention[target.id] = true

	model.removePane(owner, target)
	if model.wasBusy[target.id] || model.soundSeq[target.id] != 0 || model.paneAttention[target.id] {
		t.Fatal("removed pane retained completion state")
	}
}
