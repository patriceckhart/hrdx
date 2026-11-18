package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// runReview executes cmd and feeds every non-tick result back, so the
// panel's async loads complete without timers.
func runReview(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0 && steps < 200; steps++ {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		msg := next()
		switch msg := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
			continue
		case reviewTickMsg:
			continue
		}
		updated, follow := model.update(msg)
		model = updated.(Model)
		queue = append(queue, follow)
	}
	return model
}

func reviewRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	files := map[string]string{"main.go": "package main\n\nfunc main() {}\n", "pkg/util.go": "package pkg\n"}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() { run() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func reviewModel(t *testing.T, root string) Model {
	t.Helper()
	original := reviewTickFunc
	reviewTickFunc = func(int) tea.Cmd { return nil }
	t.Cleanup(func() { reviewTickFunc = original })
	model := newTestModel(root)
	model.width, model.height = 140, 30
	return model
}

func reviewKey(t *testing.T, model Model, key tea.KeyMsg) Model {
	t.Helper()
	updated, cmd := model.updateKey(key)
	return runReview(t, updated.(Model), cmd)
}

func prefix(t *testing.T, model Model, key rune) Model {
	t.Helper()
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyCtrlB})
	return reviewKey(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
}

func TestReviewChangesPanel(t *testing.T) {
	model := reviewModel(t, reviewRepo(t))
	paneWidth := model.terminalArea().w

	model = prefix(t, model, 'g')
	if model.review.kind != reviewChanges || model.mode != modeReview {
		t.Fatalf("ctrl+b g: kind %q mode %d", model.review.kind, model.mode)
	}
	// The panel takes columns from the panes, which resize their PTYs.
	if model.terminalArea().w >= paneWidth {
		t.Fatalf("pane area %d did not shrink from %d", model.terminalArea().w, paneWidth)
	}
	rows := model.reviewRows()
	if len(rows) != 2 || rows[0].path != "main.go" || rows[0].mark != "M" || rows[1].path != "notes.md" || rows[1].mark != "A" {
		t.Fatalf("change rows = %+v", rows)
	}
	view := ansi.Strip(model.View())
	if !strings.Contains(view, "Changes") || !strings.Contains(view, "2 files +2 -1") {
		t.Fatalf("header missing:\n%s", view)
	}

	// Enter opens the diff, highlighted and numbered.
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.review.codePath != "main.go" || len(model.review.code) == 0 {
		t.Fatalf("diff not open: %+v", model.review)
	}
	view = ansi.Strip(model.View())
	if !strings.Contains(view, "3 - func main() {}") || !strings.Contains(view, "3 + func main() { run() }") {
		t.Fatalf("diff rows missing:\n%s", view)
	}
	// Esc goes back to the list, a second Esc closes the panel and gives
	// the panes their width back.
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.review.codePath != "" || model.review.kind != reviewChanges {
		t.Fatal("esc should return to the list")
	}
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.review.kind != reviewNone || model.mode != modeTerminal || model.terminalArea().w != paneWidth {
		t.Fatalf("esc should close: kind %q mode %d width %d", model.review.kind, model.mode, model.terminalArea().w)
	}
}

func TestReviewFilesPanel(t *testing.T) {
	model := reviewModel(t, reviewRepo(t))
	model = prefix(t, model, 'e')
	if model.review.kind != reviewFiles {
		t.Fatalf("ctrl+b e opened %q", model.review.kind)
	}
	names := func() []string {
		var out []string
		for _, row := range model.reviewRows() {
			out = append(out, row.path+":"+row.mark)
		}
		return out
	}
	if got := strings.Join(names(), " "); got != "pkg: main.go:M notes.md:A" {
		t.Fatalf("top level = %s", got)
	}
	// Enter on the folder expands it, left collapses it again.
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if got := strings.Join(names(), " "); got != "pkg: pkg/util.go: main.go:M notes.md:A" {
		t.Fatalf("expanded = %s", got)
	}
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if len(model.reviewRows()) != 3 {
		t.Fatalf("collapse failed: %v", names())
	}
	// Down to main.go, enter shows the highlighted preview.
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.review.codePath != "main.go" || len(model.review.code) != 3 {
		t.Fatalf("preview = %q %d lines", model.review.codePath, len(model.review.code))
	}
	if got := ansi.Strip(model.review.code[0]); !strings.HasSuffix(got, "package main") {
		t.Fatalf("preview line = %q", got)
	}
	// Tab switches to Changes, the prefix key toggles it off.
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model = reviewKey(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.review.kind != reviewChanges {
		t.Fatalf("tab switched to %q", model.review.kind)
	}
	model = prefix(t, model, 'g')
	if model.review.kind != reviewNone {
		t.Fatal("ctrl+b g should close the open Changes panel")
	}
}

// The panel follows the focused pane's folder and drops results that
// arrive for the folder it left.
func TestReviewFollowsFocusAndDropsStaleResults(t *testing.T) {
	first, second := reviewRepo(t), reviewRepo(t)
	model := reviewModel(t, first)
	model.addSpace(second)
	model.selected = 0
	model = prefix(t, model, 'e')
	stale := model.review.gen

	model.selected = 1
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = runReview(t, updated.(Model), cmd)
	if model.review.dir != second || model.review.gen == stale {
		t.Fatalf("panel stayed on %q gen %d", model.review.dir, model.review.gen)
	}
	updated, _ = model.update(reviewListingMsg{gen: stale, dir: first, entries: nil})
	if len(updated.(Model).review.listings[""]) == 0 {
		t.Fatal("a stale listing replaced the current one")
	}
}

func TestReviewMouse(t *testing.T) {
	model := reviewModel(t, reviewRepo(t))
	model = prefix(t, model, 'g')
	box := model.reviewBox()

	// Clicking the first change opens its diff.
	updated, cmd := model.updateMouse(tea.MouseMsg{X: box.x + 4, Y: 1 + box.y + 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = runReview(t, updated.(Model), cmd)
	if model.review.codePath != "main.go" {
		t.Fatalf("click opened %q", model.review.codePath)
	}
	// A click on a pane moves the keyboard back to the terminal.
	updated, _ = model.updateMouse(tea.MouseMsg{X: model.sidebarContentWidth() + 5, Y: 5, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	model = updated.(Model)
	if model.mode != modeTerminal || model.review.kind != reviewChanges {
		t.Fatalf("pane click: mode %d kind %q", model.mode, model.review.kind)
	}
	// The x in the header closes the panel.
	updated, _ = model.updateMouse(tea.MouseMsg{X: box.x + box.w - 3, Y: 1 + box.y + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if updated.(Model).review.kind != reviewNone {
		t.Fatal("close button did not close the panel")
	}
}

// File contents must never reach the host terminal as escape sequences.
func TestReviewSanitizesFileContents(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evil.txt"), []byte("safe\x1b]0;pwned\x07\x1b[2Jtext\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := loadPreview(1, root, "evil.txt")().(reviewCodeMsg)
	if len(msg.lines) != 1 || strings.Contains(msg.lines[0], "\x1b]") || strings.Contains(msg.lines[0], "\x1b[2J") || strings.Contains(msg.lines[0], "\x07") {
		t.Fatalf("unsafe preview line %q", msg.lines)
	}
	if !strings.Contains(ansi.Strip(msg.lines[0]), "safe]0;pwned[2Jtext") {
		t.Fatalf("text lost: %q", ansi.Strip(msg.lines[0]))
	}
}

func TestReviewOutsideGit(t *testing.T) {
	model := reviewModel(t, t.TempDir())
	model = prefix(t, model, 'g')
	if view := ansi.Strip(model.View()); !strings.Contains(view, "Not a git repository") {
		t.Fatalf("no notice outside git:\n%s", view)
	}
}

// A narrow window leaves the panes their minimum width, and every panel
// row fits its strip exactly, also with wide characters in names.
func TestReviewWidths(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"日本語のとても長いファイル名.txt", "plain.txt"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, width := range []int{50, 90, 160} {
		model := reviewModel(t, root)
		model.width = width
		model = prefix(t, model, 'e')
		if model.terminalArea().w < minPaneCols {
			t.Fatalf("width %d: panes squeezed to %d", width, model.terminalArea().w)
		}
		box := model.reviewBox()
		if box.w == 0 {
			continue // too narrow for a panel, the panes keep the room
		}
		rows := strings.Split(model.View(), "\n")[1:] // below the header
		for index := box.y; index < box.y+box.h; index++ {
			strip := ansi.Strip(ansiCut(rows[index], box.x, box.x+box.w))
			if got := ansi.StringWidth(strip); got != box.w {
				t.Fatalf("width %d row %d: %d cells, want %d: %q", width, index, got, box.w, strip)
			}
			if r := []rune(strip); r[0] != '│' && r[0] != '╭' && r[0] != '├' && r[0] != '╰' {
				t.Fatalf("width %d row %d: border lost: %q", width, index, strip)
			}
		}
	}
}
