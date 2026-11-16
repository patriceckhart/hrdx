package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompleteDir(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"apple", "apricot", "banana", ".hidden"} {
		if err := os.Mkdir(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "afile"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	matches := CompleteDir(base + "/")
	if len(matches) != 3 {
		t.Fatalf("matches = %v, want apple/apricot/banana", matches)
	}

	matches = CompleteDir(base + "/ap")
	if len(matches) != 2 || filepath.Base(matches[0]) != "apple" || filepath.Base(matches[1]) != "apricot" {
		t.Fatalf("prefix matches = %v", matches)
	}

	matches = CompleteDir(base + "/ban")
	if len(matches) != 1 || filepath.Base(matches[0]) != "banana" {
		t.Fatalf("single match = %v", matches)
	}

	if matches := CompleteDir(base + "/zzz"); len(matches) != 0 {
		t.Fatalf("no-match = %v, want empty", matches)
	}
}

func TestCommonPrefix(t *testing.T) {
	if got := CommonPrefix([]string{"/a/apple", "/a/apricot"}); got != "/a/ap" {
		t.Fatalf("commonPrefix = %q, want /a/ap", got)
	}
	if got := CommonPrefix(nil); got != "" {
		t.Fatalf("commonPrefix(nil) = %q", got)
	}
}
