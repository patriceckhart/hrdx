package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/patriceckhart/hrdx/internal/state"
)

func makeRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stubGitStatus(t *testing.T, status func(string) subrepo) {
	t.Helper()
	previous := readGitStatusFunc
	readGitStatusFunc = status
	t.Cleanup(func() { readGitStatusFunc = previous })
}

func TestScanSubreposRespectsDepthAndSkips(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, root) // the workspace itself is never listed
	makeRepo(t, filepath.Join(root, "api"))
	makeRepo(t, filepath.Join(root, "api", "nested")) // inside a found repo
	makeRepo(t, filepath.Join(root, "services", "web"))
	makeRepo(t, filepath.Join(root, "a", "b", "deep"))
	makeRepo(t, filepath.Join(root, ".hidden"))
	makeRepo(t, filepath.Join(root, "node_modules", "pkg"))
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A worktree or submodule has a .git file instead of a directory.
	if err := os.MkdirAll(filepath.Join(root, "worktree"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "worktree", ".git"), []byte("gitdir: ../x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rel := func(paths []string) string {
		var out []string
		for _, path := range paths {
			value, _ := filepath.Rel(root, path)
			out = append(out, filepath.ToSlash(value))
		}
		return strings.Join(out, ",")
	}
	for depth, want := range map[int]string{
		0: "",
		1: "api,worktree",
		2: "api,services/web,worktree",
		3: "a/b/deep,api,services/web,worktree",
	} {
		if got := rel(scanSubrepos(root, depth, subrepoLimit, nil)); got != want {
			t.Errorf("depth %d = %q, want %q", depth, got, want)
		}
	}
	if got := scanSubrepos(root, 3, 2, nil); len(got) != 2 {
		t.Errorf("limit 2 returned %d repos", len(got))
	}
	hidden := map[string]bool{"api": true, "services": true}
	if got := rel(scanSubrepos(root, 3, subrepoLimit, hidden)); got != "a/b/deep,worktree" {
		t.Errorf("hidden scan = %q", got)
	}
	if got := rel(scanSubrepos(root, 3, 2, map[string]bool{"a": true})); got != "api,services/web" {
		t.Errorf("hidden folders must not count toward the limit: %q", got)
	}
}

func TestParseGitStatus(t *testing.T) {
	output := strings.Join([]string{
		"# branch.oid 0123456789abcdef",
		"# branch.head feat/x",
		"# branch.upstream origin/feat/x",
		"# branch.ab +2 -3",
		"1 .M N... 100644 100644 100644 aaa bbb file.go",
		"2 R. N... 100644 100644 100644 aaa bbb R100 new.go\told.go",
		"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go",
		"? untracked.txt",
		"! ignored.txt",
	}, "\n")
	got := parseGitStatus(output)
	want := subrepo{branch: "feat/x", ahead: 2, behind: 3, changed: 3, conflicts: 1}
	if got != want {
		t.Fatalf("parseGitStatus = %+v, want %+v", got, want)
	}

	detached := parseGitStatus("# branch.oid 0123456789abcdef\n# branch.head (detached)\n")
	if detached.branch != "0123456" {
		t.Fatalf("detached branch = %q", detached.branch)
	}
	if empty := parseGitStatus("# branch.ab +x -\n"); empty != (subrepo{}) {
		t.Fatalf("malformed counts = %+v", empty)
	}
}

func TestSubrepoRefreshFillsSidebarAndAPI(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, filepath.Join(root, "api"))
	stubGitStatus(t, func(string) subrepo { return subrepo{branch: "dev", changed: 4, ahead: 1} })

	model := newTestModel(root)
	model.width, model.height = 100, 30
	if rows := model.subrepoRows(0, " ", sidebarWidth); rows != nil {
		t.Fatal("disabled setting must not render sub-repositories")
	}

	command := model.setSubrepoDepth(1)
	if command == nil {
		t.Fatal("enabling should start a refresh")
	}
	updated, _ := model.update(command())
	model = updated.(Model)
	if !model.subrepos[root].pending {
		t.Fatal("tick should mark the workspace pending")
	}
	updated, _ = model.update(refreshSubrepos(model.subrepoGen, root, 1, nil)())
	model = updated.(Model)

	var found sidebarRow
	for _, row := range model.sidebarRows() {
		if row.kind == "subrepo" {
			found = row
		}
	}
	plain := ansi.Strip(found.label)
	if !strings.Contains(plain, "api dev") || !strings.Contains(plain, "●4") || !strings.Contains(plain, "↑1") {
		t.Fatalf("subrepo row = %q", plain)
	}
	if width := ansi.StringWidth(found.label); width > sidebarWidth {
		t.Fatalf("subrepo row width %d exceeds sidebar", width)
	}

	status := model.apiStatus()
	if len(status.Workspaces[0].Subrepos) != 1 || status.Workspaces[0].Subrepos[0].Changed != 4 {
		t.Fatalf("api subrepos = %+v", status.Workspaces[0].Subrepos)
	}

	stale := subreposMsg{gen: model.subrepoGen, root: root, repos: model.subrepos[root].repos}
	model.setSubrepoDepth(0)
	updated, _ = model.update(stale)
	model = updated.(Model)
	if len(model.subrepos[root].repos) != 0 || model.apiStatus().Workspaces[0].Subrepos != nil {
		t.Fatal("stale scan must not repopulate a disabled cache")
	}
}

func TestSubrepoPickerOpensPaneInRepository(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	makeRepo(t, repo)
	model := newTestModel(root)
	model.width, model.height = 100, 30
	model.subrepoDepth = 1
	model.subrepos[root] = subrepoCache{repos: []subrepo{{path: repo, rel: "api"}}}

	model.openSubrepoPicker(model.spaces[0], repo, rect{})
	items := model.menuItems()
	if len(items) != 5 || items[0].action != "subrepo:tab" {
		t.Fatalf("picker items = %+v", items)
	}
	updated, _ := model.runMenuAction("subrepo:tab")
	model = updated.(Model)
	if model.mode != modeMenu || model.pickAction != "subrepo-tab" {
		t.Fatalf("expected kind picker, got mode %v action %q", model.mode, model.pickAction)
	}
	updated, _ = model.runMenuAction("kind:shell")
	model = updated.(Model)

	owner := model.spaces[0]
	if len(owner.tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(owner.tabs))
	}
	newPane := owner.tab().panes[0]
	if newPane.kind != "shell" || newPane.cwd != repo {
		t.Fatalf("new pane = kind %q cwd %q", newPane.kind, newPane.cwd)
	}

	saved := model.snapshot()
	if saved.SubrepoDepth != 1 || saved.Workspaces[0].Tabs[1].Panes[0].CWD != repo {
		t.Fatalf("snapshot = %+v", saved.Workspaces[0].Tabs[1].Panes)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if restored.subrepoDepth != 1 || restored.spaces[0].tabs[1].panes[0].cwd != repo {
		t.Fatal("restore should keep depth and pane directory")
	}

	saved.Workspaces[0].Tabs[1].Panes[0].CWD = filepath.Join(root, "gone")
	if restored := New(Config{Shell: "/bin/sh"}, nil, "", saved); restored.spaces[0].tabs[1].panes[0].cwd != "" {
		t.Fatal("missing pane directory should be dropped")
	}
}

func TestSubrepoLeftClickOnlyOnText(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	model := newTestModel(root)
	model.width, model.height = 100, 30
	model.subrepoDepth = 1
	model.subrepos[root] = subrepoCache{repos: []subrepo{{path: repo, rel: "api", branch: "dev"}}}

	rowY := -1
	var label string
	for index, row := range model.sidebarRows() {
		if row.kind == "subrepo" {
			rowY, label = index, row.label
		}
	}
	if rowY < 0 {
		t.Fatal("no subrepo row rendered")
	}
	plain := ansi.Strip(label)
	textStart := ansi.StringWidth(plain[:strings.Index(plain, "api")])
	textEnd := ansi.StringWidth(strings.TrimRight(plain, " "))

	click := func(button tea.MouseButton, x int) Model {
		updated, _ := model.updateMouse(tea.MouseMsg{X: x, Y: rowY + 1, Button: button, Action: tea.MouseActionPress})
		return updated.(Model)
	}
	for _, x := range []int{0, textStart - 1, textEnd, sidebarWidth - 2} {
		if got := click(tea.MouseButtonLeft, x); got.mode == modeMenu {
			t.Fatalf("left click at blank column %d opened the picker (%q)", x, plain)
		}
	}
	for _, x := range []int{textStart, textEnd - 1} {
		if got := click(tea.MouseButtonLeft, x); got.mode != modeMenu || got.pickAction != "subrepo" {
			t.Fatalf("left click on text column %d should open the picker", x)
		}
	}
	if got := click(tea.MouseButtonRight, sidebarWidth-2); got.mode != modeMenu || got.pickAction != "subrepo" {
		t.Fatal("right click anywhere on the row should open the picker")
	}
}

func TestSubrepoPickerHighlightsTarget(t *testing.T) {
	root := t.TempDir()
	api, web := filepath.Join(root, "api"), filepath.Join(root, "web")
	model := newTestModel(root)
	model.width, model.height = 100, 30
	model.subrepoDepth = 1
	model.subrepos[root] = subrepoCache{repos: []subrepo{{path: api, rel: "api"}, {path: web, rel: "web"}}}

	targeted := func(m Model) []string {
		var out []string
		for _, repo := range m.subrepos[root].repos {
			if m.subrepoTargeted(repo.path) {
				out = append(out, repo.rel)
			}
		}
		return out
	}
	if got := targeted(model); len(got) != 0 {
		t.Fatalf("no picker open, highlighted %v", got)
	}

	model.openSubrepoPicker(model.spaces[0], web, rect{})
	if got := targeted(model); len(got) != 1 || got[0] != "web" {
		t.Fatalf("location picker highlighted %v", got)
	}
	for _, step := range []string{"subrepo:tab", "subrepo:space"} {
		probe := model
		updated, _ := probe.runMenuAction(step)
		if got := targeted(updated.(Model)); len(got) != 1 || got[0] != "web" {
			t.Fatalf("agent picker after %s highlighted %v", step, got)
		}
	}

	model.closeMenu()
	if got := targeted(model); len(got) != 0 {
		t.Fatalf("closed picker still highlights %v", got)
	}
	model.openKindPicker("space", nil, web, rect{})
	if got := targeted(model); len(got) != 0 {
		t.Fatalf("unrelated picker highlighted %v", got)
	}
}

func TestHideAndUnhideSubrepos(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, filepath.Join(root, "api"))
	makeRepo(t, filepath.Join(root, "web"))
	// collectSubrepos calls the stub from several goroutines.
	var mu sync.Mutex
	var scanned []string
	stubGitStatus(t, func(dir string) subrepo {
		mu.Lock()
		defer mu.Unlock()
		scanned = append(scanned, filepath.Base(dir))
		return subrepo{branch: "main"}
	})
	scannedNames := func() string {
		mu.Lock()
		defer mu.Unlock()
		sorted := append([]string(nil), scanned...)
		slices.Sort(sorted)
		return strings.Join(sorted, ",")
	}
	model := newTestModel(root)
	model.width, model.height = 100, 30
	model.subrepoDepth = 1
	// A scan that started before hiding still reports both folders.
	stale := refreshSubrepos(model.subrepoGen, root, 1, nil)()

	model.subrepos[root] = subrepoCache{repos: collectSubrepos(root, 1, nil)}
	model.openSubrepoPicker(model.spaces[0], filepath.Join(root, "api"), rect{})
	items := model.menuItems()
	if items[len(items)-1].action != "subrepo:hide" {
		t.Fatalf("picker items = %+v", items)
	}
	updated, _ := model.runMenuAction("subrepo:hide")
	model = updated.(Model)
	if model.mode != modeTerminal {
		t.Fatal("hiding should close the menu")
	}
	names := func(m Model) string {
		var out []string
		for _, repo := range m.subrepos[root].repos {
			out = append(out, repo.rel)
		}
		return strings.Join(out, ",")
	}
	if got := names(model); got != "web" {
		t.Fatalf("visible after hide = %q", got)
	}
	updated, _ = model.update(stale)
	model = updated.(Model)
	if got := names(model); got != "web" {
		t.Fatalf("stale scan resurrected a hidden folder: %q", got)
	}

	mu.Lock()
	scanned = nil
	mu.Unlock()
	updated, _ = model.update(refreshSubrepos(model.subrepoGen, root, 1, model.hiddenSubreposFor(root))())
	model = updated.(Model)
	if got := scannedNames(); got != "web" {
		t.Fatalf("git ran for %v", got)
	}

	saved := model.snapshot()
	if got := strings.Join(saved.Workspaces[0].HiddenSubrepos, ","); got != "api" {
		t.Fatalf("saved hidden = %q", got)
	}
	saved.Workspaces[0].HiddenSubrepos = append(saved.Workspaces[0].HiddenSubrepos, "../outside", "", "/abs", "web/")
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if got := strings.Join(sortedKeys(restored.spaces[0].hiddenSubrepos), ","); got != "api,web" {
		t.Fatalf("restored hidden = %q", got)
	}

	model.openSpaceMenu(model.spaces[0], rect{})
	items = model.menuItems()
	last := items[len(items)-1]
	if last.action != "space-unhide-subrepos" || last.label != "Show hidden folders (1)" {
		t.Fatalf("workspace menu = %+v", items)
	}
	updated, command := model.runMenuAction(last.action)
	model = updated.(Model)
	if len(model.spaces[0].hiddenSubrepos) != 0 || command == nil {
		t.Fatal("unhide should clear the list and rescan")
	}
	model.openSpaceMenu(model.spaces[0], rect{})
	for _, item := range model.menuItems() {
		if item.action == "space-unhide-subrepos" {
			t.Fatal("menu entry should disappear when nothing is hidden")
		}
	}
}

func TestSubrepoPickerOpenAsWorkspace(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "api")
	makeRepo(t, repo)
	model := newTestModel(root)
	model.openSubrepoPicker(model.spaces[0], repo, rect{})
	updated, _ := model.runMenuAction("subrepo:space")
	model = updated.(Model)
	updated, _ = model.runMenuAction("kind:shell")
	model = updated.(Model)
	if len(model.spaces) != 2 || model.spaces[1].cwd != repo || model.selected != 1 {
		t.Fatalf("workspaces = %d, selected %d", len(model.spaces), model.selected)
	}
}

func TestSubrepoDepthSettingIsClampedOnLoad(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, nil, "", state.State{SubrepoDepth: 99})
	if model.subrepoDepth != maxSubrepoDepth {
		t.Fatalf("depth = %d", model.subrepoDepth)
	}
}

func TestSubrepoSettingCheckboxRevealsDepth(t *testing.T) {
	model := New(Config{Shell: "/bin/sh"}, nil, "", state.State{})
	model.settingsTab = 3
	// Other terminal settings may be added around these rows.
	actions := func() string {
		var out []string
		for _, row := range model.settingsRows() {
			if row.action == "auto-copy" || strings.HasPrefix(row.action, "subrepos") {
				out = append(out, row.action)
			}
		}
		return strings.Join(out, " ")
	}
	row := func(action string) settingsRow {
		for _, current := range model.settingsRows() {
			if current.action == action {
				return current
			}
		}
		t.Fatalf("no settings row %q", action)
		return settingsRow{}
	}

	if got := actions(); got != "auto-copy subrepos" {
		t.Fatalf("off rows = %q", got)
	}
	if label := row("subrepos").label; !strings.HasPrefix(label, "[ ] ") {
		t.Fatalf("off label = %q", label)
	}

	if model.toggleSettingsRow(row("subrepos")) == nil || model.subrepoDepth != 1 {
		t.Fatalf("enable: depth = %d", model.subrepoDepth)
	}
	if got := actions(); got != "auto-copy subrepos subrepos:1 subrepos:2 subrepos:3" {
		t.Fatalf("on rows = %q", got)
	}
	if label := row("subrepos").label; !strings.HasPrefix(label, "[x] ") {
		t.Fatalf("on label = %q", label)
	}
	if label := row("subrepos:1").label; label != "  (•) search 1 folder level" {
		t.Fatalf("depth label = %q", label)
	}

	model.toggleSettingsRow(row("subrepos:3"))
	model.toggleSettingsRow(row("subrepos"))
	if model.subrepoDepth != 0 || actions() != "auto-copy subrepos" {
		t.Fatalf("disable: depth = %d rows = %q", model.subrepoDepth, actions())
	}
	model.toggleSettingsRow(row("subrepos"))
	if model.subrepoDepth != 3 {
		t.Fatalf("re-enable depth = %d, want 3", model.subrepoDepth)
	}
}

func TestSubrepoRowsIndentNestedFolders(t *testing.T) {
	root := t.TempDir()
	model := newTestModel(root)
	model.width, model.height = 100, 30
	model.subrepoDepth = 3
	model.subrepos[root] = subrepoCache{repos: []subrepo{
		{path: filepath.Join(root, "a", "b", "deep"), rel: "a/b/deep"},
		{path: filepath.Join(root, "api"), rel: "api", branch: "main"},
		{path: filepath.Join(root, "services", "auth"), rel: "services/auth"},
		{path: filepath.Join(root, "services", "web"), rel: "services/web", branch: "dev"},
	}}
	var lines []string
	var kinds []int
	for _, row := range model.subrepoRows(0, " ", model.sidebarContentWidth()) {
		lines = append(lines, strings.TrimRight(ansi.Strip(row.label), " "))
		kinds = append(kinds, row.subrepo)
	}
	want := []string{
		"  ├ a/",
		"    └ b/",
		"      └ deep",
		"  ├ api main",
		"  └ services/",
		"    ├ auth",
		"    └ web dev",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if got := fmt.Sprint(kinds); got != "[-1 -1 0 1 -1 2 3]" {
		t.Fatalf("row repo indexes = %s", got)
	}

	for index, row := range model.sidebarRows() {
		if row.kind != "subrepo" || row.subrepo >= 0 {
			continue
		}
		for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight} {
			updated, _ := model.updateMouse(tea.MouseMsg{X: 5, Y: index + 1, Button: button, Action: tea.MouseActionPress})
			if updated.(Model).mode == modeMenu {
				t.Fatalf("folder row %q opened a menu", ansi.Strip(row.label))
			}
		}
	}
}
