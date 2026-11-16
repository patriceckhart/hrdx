package gitinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBranchFromHeadAndGitFile(t *testing.T) {
	repo := t.TempDir()
	if Branch(repo) != "" {
		t.Fatal("non-repo has a branch")
	}
	gitDir := filepath.Join(repo, ".git")
	if err := os.Mkdir(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Branch(repo); got != "main" {
		t.Fatalf("branch = %q", got)
	}
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Branch(linked); got != "main" {
		t.Fatalf("linked branch = %q", got)
	}
}
