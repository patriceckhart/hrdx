package core

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("app/main.go", "package main\n")
	write("README.md", "# x\n")
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write("app/main.go", "package main\n\nfunc main() {}\n")
	return root
}

// The panels follow the focused pane's start folder, list and preview files
// inside it only, and diff only files that have changes.
func TestReviewFollowsFocusedPaneFolder(t *testing.T) {
	root := gitRepo(t)
	saved := state.State{Workspaces: []state.Workspace{{Name: "w", CWD: root, Tabs: []state.Tab{{
		Panes: []state.Pane{{Kind: "shell", Name: "s", CWD: filepath.Join(root, "app")}},
	}}}}}
	h := newHarness(t, saved)

	dir, err := h.core.ReviewDir()
	if err != nil || dir != filepath.Join(root, "app") {
		t.Fatalf("dir = %q %v", dir, err)
	}
	listing := h.core.ReviewList("")
	if listing.Error != "" || len(listing.Entries) != 1 || listing.Entries[0].Name != "main.go" || listing.Marks["main.go"] != "M" {
		t.Fatalf("listing = %+v", listing)
	}
	if preview := h.core.ReviewPreview("main.go"); len(preview.Lines) != 3 || preview.Lines[0][0].Class != "keyword" {
		t.Fatalf("preview = %+v", preview)
	}
	if escaped := h.core.ReviewPreview("../README.md"); len(escaped.Lines) != 0 {
		t.Fatal("preview escaped the panel folder")
	}
	if escaped := h.core.ReviewList(".."); escaped.Error == "" {
		t.Fatal("listing escaped the panel folder")
	}

	changes := h.core.ReviewChanges()
	if changes.Error != "" || len(changes.Files) != 1 || changes.Files[0].Path != "app/main.go" {
		t.Fatalf("changes = %+v", changes)
	}
	diff := h.core.ReviewDiff("app/main.go")
	kinds := ""
	for _, line := range diff.Lines {
		kinds += line.Kind
	}
	if !strings.HasPrefix(kinds, "@ ") || !strings.Contains(kinds, "+") {
		t.Fatalf("diff = %+v", diff)
	}
	// Unchanged files are not diffable, so the panel cannot read them.
	if unchanged := h.core.ReviewDiff("README.md"); len(unchanged.Lines) != 0 || unchanged.Note == "" {
		t.Fatalf("unchanged diff = %+v", unchanged)
	}
}

func TestReviewWithoutWorkspace(t *testing.T) {
	h := newHarness(t, state.State{})
	if _, err := h.core.ReviewDir(); err == nil {
		t.Fatal("no workspace should be an error")
	}
	if listing := h.core.ReviewList(""); listing.Error == "" {
		t.Fatal("listing without a workspace")
	}
}
