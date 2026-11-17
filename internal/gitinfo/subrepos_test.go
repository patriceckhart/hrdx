package gitinfo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestCollectSubreposFillsPathsAndStatus(t *testing.T) {
	root := t.TempDir()
	makeRepo(t, filepath.Join(root, "api"))
	makeRepo(t, filepath.Join(root, "services", "web"))
	makeRepo(t, filepath.Join(root, "node_modules", "pkg"))

	got := CollectSubrepos(root, 2, map[string]bool{}, func(dir string) RepoStatus {
		return RepoStatus{Branch: "b-" + filepath.Base(dir), Changed: 2}
	})
	var names []string
	for _, repo := range got {
		if !filepath.IsAbs(repo.Path) || repo.Branch != "b-"+filepath.Base(repo.Path) || repo.Changed != 2 {
			t.Fatalf("repo = %+v", repo)
		}
		names = append(names, repo.Rel)
	}
	if strings.Join(names, ",") != "api,services/web" {
		t.Fatalf("rel paths = %v", names)
	}
	if hidden := CollectSubrepos(root, 2, map[string]bool{"services": true}, ReadStatusStub); len(hidden) != 1 {
		t.Fatalf("hidden folder still listed: %+v", hidden)
	}
}

func ReadStatusStub(string) RepoStatus { return RepoStatus{} }

func TestParseStatusCounts(t *testing.T) {
	got := ParseStatus("# branch.head dev\n# branch.ab +1 -2\n1 .M x\n? y\nu UU z\n")
	if got != (RepoStatus{Branch: "dev", Ahead: 1, Behind: 2, Changed: 2, Conflicts: 1}) {
		t.Fatalf("status = %+v", got)
	}
}

func TestCleanSubrepoRel(t *testing.T) {
	for in, want := range map[string]string{
		"api": "api", "a/./b": "a/b", " web ": "web",
		"": "", ".": "", "..": "", "../x": "", "/abs": "",
	} {
		if got := CleanSubrepoRel(in); got != want {
			t.Errorf("CleanSubrepoRel(%q) = %q, want %q", in, got, want)
		}
	}
}
