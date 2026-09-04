package ui

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

func TestInterpolateWorktreeCommandQuotesValues(t *testing.T) {
	got := interpolateWorktreeCommand("git worktree add {{path}} {{name}}", map[string]string{
		"path": "/tmp/a path",
		"name": "feature/x",
	})
	if runtime.GOOS == "windows" {
		if !strings.Contains(got, `"/tmp/a path"`) || !strings.Contains(got, "feature/x") {
			t.Fatalf("command = %q", got)
		}
	} else if !strings.Contains(got, "'/tmp/a path'") || !strings.Contains(got, "'feature/x'") {
		t.Fatalf("command = %q", got)
	}
}

func TestDefaultWorktreeCommandUsesCompletePathPlaceholder(t *testing.T) {
	if want := "git worktree add {{path}} -b {{name}}"; defaultWorktreeCommand != want {
		t.Fatalf("default command = %q, want %q", defaultWorktreeCommand, want)
	}
	got := interpolateWorktreeCommand(defaultWorktreeCommand, map[string]string{
		"path": "/tmp/repo with spaces/.worktrees/feature",
		"name": "feature",
	})
	want := "git worktree add '/tmp/repo with spaces/.worktrees/feature' -b 'feature'"
	if runtime.GOOS == "windows" {
		want = `git worktree add "/tmp/repo with spaces/.worktrees/feature" -b feature`
	}
	if got != want {
		t.Fatalf("interpolated default command = %q, want %q", got, want)
	}
}

func TestSidebarGroupsMainAndLinkedWorktree(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	path := filepath.Join(repo, ".worktrees", "feature")
	git(t, repo, "worktree", "add", path, "-b", "feature")
	middle := t.TempDir()
	model := New(Config{Shell: "/bin/sh"}, []string{repo, middle, path}, "", state.State{})
	groups, branches := 0, 0
	for _, row := range model.sidebarRows() {
		if row.kind == "group" {
			groups++
		}
		if row.kind == "space" {
			branches++
		}
	}
	if groups != 1 || branches != 3 {
		t.Fatalf("groups/branches = %d/%d, want 1/3", groups, branches)
	}
	rows := model.sidebarRows()
	groupIndex := -1
	for index, row := range rows {
		if row.kind == "group" {
			groupIndex = index
		}
	}
	branchOrder := []int{}
	for _, row := range rows[groupIndex+1:] {
		if row.kind == "space" {
			branchOrder = append(branchOrder, row.space)
		}
	}
	if groupIndex < 0 || len(branchOrder) < 3 || branchOrder[0] != 0 || branchOrder[1] != 2 {
		t.Fatalf("group rows are not contiguous: %+v", rows)
	}
}

func TestWorktreePickerFiltersOpenWorktrees(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	openPath := filepath.Join(repo, ".worktrees", "open")
	closedPath := filepath.Join(repo, ".worktrees", "closed")
	git(t, repo, "worktree", "add", openPath, "-b", "open")
	git(t, repo, "worktree", "add", closedPath, "-b", "closed")
	model := New(Config{Shell: "/bin/sh"}, []string{repo, openPath}, "", state.State{})
	opened, _ := model.openWorktreePicker(model.currentSpace(), rect{})
	picker := opened.(Model)
	if len(picker.pickItems) != 1 || !strings.Contains(picker.pickItems[0].label, "closed") {
		t.Fatalf("picker items = %+v, want only closed worktree", picker.pickItems)
	}
}

func TestCreateWorktreeUsesDefaultCommand(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	model := Model{}
	path, err := model.createWorktree(&space{cwd: repo}, "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(path, filepath.Join(repo, ".worktrees", "feature")) {
		t.Fatalf("path = %q", path)
	}
	if got := readGitBranch(path); got != "feature" {
		t.Fatalf("branch = %q, want feature", got)
	}
}

func TestCreateWorktreeFindsManagerSelectedPath(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	model := Model{config: Config{WorktreeCommand: "git worktree add ../custom-{{name}} -b {{name}}"}}
	path, err := model.createWorktree(&space{cwd: repo}, "feature")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(repo), "custom-feature")
	if !samePath(path, want) {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if got := readGitBranch(path); got != "feature" {
		t.Fatalf("branch = %q, want feature", got)
	}
}

func TestGitWorktreeRoot(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "init")
	path := filepath.Join(repo, ".worktrees", "feature")
	git(t, repo, "worktree", "add", path, "-b", "feature")
	if got := readGitCommonDir(repo); !samePath(got, repo) {
		t.Fatalf("main readGitCommonDir = %q, want %q", got, repo)
	}
	if got := readGitCommonDir(path); !samePath(got, repo) {
		t.Fatalf("linked readGitCommonDir = %q, want %q", got, repo)
	}
	if got := gitWorktreeRoot(repo); !samePath(got, repo) {
		t.Fatalf("main gitWorktreeRoot = %q, want %q", got, repo)
	}
	if got := gitWorktreeRoot(path); !samePath(got, repo) {
		t.Fatalf("linked gitWorktreeRoot = %q, want %q", got, repo)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
