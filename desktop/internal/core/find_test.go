package core

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

// Same cases as internal/ui TestFuzzyMatch.
func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             bool
	}{
		{"api › zot 1", "", true},
		{"api › zot 1", "apzot", true},
		{"api › zot 1", "z1", true},
		{"api › zot 1", "xyz", false},
		{"web › shell 2", "wsh2", true},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.haystack, c.needle); got != c.want {
			t.Fatalf("fuzzyMatch(%q, %q) = %v, want %v", c.haystack, c.needle, got, c.want)
		}
	}
}

func TestFindLabelsHierarchy(t *testing.T) {
	dir := t.TempDir()
	saved := state.State{Workspaces: []state.Workspace{{GroupPath: []string{"work"}, Name: "api", CWD: dir, Tabs: []state.Tab{
		{Panes: []state.Pane{{Kind: "zot", Name: "zot 1"}}},
		{Name: "logs", Panes: []state.Pane{{Kind: "shell", Name: "shell 1"}}},
	}}}}
	m := newModel(Config{DefaultAgent: "zot"}, saved, nil, Hooks{}, nil, prefsZero())
	all := m.find("")
	if len(all) != 2 || all[0].Label != "work › api › 1 zot › zot 1" || all[1].Label != "work › api › logs › shell 1" {
		t.Fatalf("candidates = %+v", all)
	}
	if got := m.find("LOGS"); len(got) != 1 || got[0].PaneID != all[1].PaneID {
		t.Fatalf("filtered = %+v", got)
	}
}
