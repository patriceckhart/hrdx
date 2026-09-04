package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseWorktreePorcelainFiltersOpenAndPreservesPaths(t *testing.T) {
	openPath := filepath.Clean("/tmp/project")
	output := strings.Join([]string{
		"worktree /tmp/project",
		"HEAD 1111111111111111111111111111111111111111",
		"branch refs/heads/main",
		"",
		"worktree /tmp/feature branch",
		"HEAD abcdef0123456789012345678901234567890123",
		"branch refs/heads/feature/auth",
		"",
		"worktree /tmp/detached",
		"HEAD 2222222222222222222222222222222222222222",
		"detached",
	}, "\n")
	items := parseWorktreePorcelain(output, map[string]bool{openPath: true})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].action != "worktree:/tmp/feature branch" || !strings.HasPrefix(items[0].label, "feature/auth  ") {
		t.Fatalf("feature item = %+v", items[0])
	}
	if items[1].action != "worktree:/tmp/detached" || !strings.HasPrefix(items[1].label, "2222222  ") {
		t.Fatalf("detached item = %+v", items[1])
	}
}

func TestResolveWorktreePathAllowsNewNameOnly(t *testing.T) {
	root := t.TempDir()
	path, err := resolveWorktreePath(root, "feature-auth")
	want := filepath.Join(root, ".worktrees", "feature-auth")
	if err != nil || path != want {
		t.Fatalf("resolve new worktree = %q/%v, want %q", path, err, want)
	}
	if _, err := resolveWorktreePath(root, "../outside"); err == nil {
		t.Fatal("resolve accepted a path traversal")
	}
	if _, err := resolveWorktreePath(root, ""); err == nil {
		t.Fatal("resolve accepted an empty name")
	}
}

func TestGitCommonDirForLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, ".git")
	linked := filepath.Join(root, "linked")
	if err := os.MkdirAll(filepath.Join(common, "worktrees", "feature"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+filepath.Join(common, "worktrees", "feature")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(common, "worktrees", "feature", "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := gitCommonDir(linked)
	if !ok || got != common {
		t.Fatalf("common dir = %q/%v, want %q/true", got, ok, common)
	}
}
