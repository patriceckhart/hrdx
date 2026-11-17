package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"

	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/state"
)

func makeSubrepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stubSubrepoStatus keeps tests independent of an installed git.
func stubSubrepoStatus(t *testing.T) {
	t.Helper()
	previous := readSubrepoStatus
	readSubrepoStatus = func(string) gitinfo.RepoStatus { return gitinfo.RepoStatus{Branch: "dev", Changed: 3, Ahead: 1} }
	t.Cleanup(func() { readSubrepoStatus = previous })
}

func subrepoRels(view View) []string {
	var out []string
	for _, repo := range view.Workspaces[0].Subrepos {
		out = append(out, repo.Rel)
	}
	return out
}

func TestSubreposOffByDefault(t *testing.T) {
	stubSubrepoStatus(t)
	root := t.TempDir()
	makeSubrepo(t, filepath.Join(root, "api"))
	h := newHarness(t, state.State{}, root)
	if repos := h.snapshot().Workspaces[0].Subrepos; repos != nil {
		t.Fatalf("subrepos listed while the setting is off: %+v", repos)
	}
}

func TestSubrepoSettingScansViewAndStatus(t *testing.T) {
	stubSubrepoStatus(t)
	root := t.TempDir()
	makeSubrepo(t, filepath.Join(root, "api"))
	makeSubrepo(t, filepath.Join(root, "packages", "web"))
	h := newHarness(t, state.State{}, root)

	one := 1
	if err := h.core.SettingsSet(SettingsPatch{SubrepoDepth: &one}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("depth 1 scan", func(m *model) bool { return len(m.subrepos[root].repos) == 1 })
	if got := subrepoRels(h.snapshot()); !slices.Equal(got, []string{"api"}) {
		t.Fatalf("depth 1 = %v", got)
	}

	two := 2
	if err := h.core.SettingsSet(SettingsPatch{SubrepoDepth: &two}); err != nil {
		t.Fatal(err)
	}
	h.waitFor("depth 2 scan", func(m *model) bool { return len(m.subrepos[root].repos) == 2 })
	view := h.snapshot()
	if got := subrepoRels(view); !slices.Equal(got, []string{"api", "packages/web"}) {
		t.Fatalf("depth 2 = %v", got)
	}
	if repo := view.Workspaces[0].Subrepos[0]; repo.Branch != "dev" || repo.Changed != 3 || repo.Ahead != 1 {
		t.Fatalf("status not carried into the view: %+v", repo)
	}

	reply := h.request("status", nil)
	status := reply.Data.(api.Status)
	if len(status.Workspaces[0].Subrepos) != 2 || status.Workspaces[0].Subrepos[1].Name != "packages/web" {
		t.Fatalf("status subrepos = %+v", status.Workspaces[0].Subrepos)
	}

	bad := 4
	if err := h.core.SettingsSet(SettingsPatch{SubrepoDepth: &bad}); err == nil {
		t.Fatal("depth above the maximum must be refused")
	}
	zero := 0
	if err := h.core.SettingsSet(SettingsPatch{SubrepoDepth: &zero}); err != nil {
		t.Fatal(err)
	}
	if repos := h.snapshot().Workspaces[0].Subrepos; repos != nil {
		t.Fatalf("turning the setting off must clear the list: %+v", repos)
	}
}

func TestSubrepoOpenUsesRepositoryDirectory(t *testing.T) {
	stubSubrepoStatus(t)
	root := t.TempDir()
	api := filepath.Join(root, "api")
	makeSubrepo(t, api)
	h := newHarness(t, state.State{SubrepoDepth: 1}, root)
	h.waitFor("scan", func(m *model) bool { return len(m.subrepos[root].repos) == 1 })

	if err := h.core.SubrepoOpen(0, api, "shell", "tab"); err != nil {
		t.Fatal(err)
	}
	h.waitFor("pane launched in the repo", func(m *model) bool {
		launcher := m.launcher.(*fakeLauncher)
		launcher.mu.Lock()
		defer launcher.mu.Unlock()
		last := launcher.launched[len(launcher.launched)-1]
		return last.CWD == api
	})
	view := h.snapshot()
	if len(view.Workspaces[0].Tabs) != 2 {
		t.Fatal("tab placement must open a new tab")
	}
	var saved []byte
	h.waitFor("pane cwd persisted", func(m *model) bool {
		saved, _ = os.ReadFile(m.config.StatePath)
		var st state.State
		if json.Unmarshal(saved, &st) != nil || len(st.Workspaces) == 0 || len(st.Workspaces[0].Tabs) < 2 {
			return false
		}
		return st.Workspaces[0].Tabs[1].Panes[0].CWD == api
	})

	if err := h.core.SubrepoOpen(0, filepath.Join(root, "elsewhere"), "shell", "right"); err == nil {
		t.Fatal("a path that is not a listed repository must be refused")
	}
	if err := h.core.SubrepoOpen(0, api, "shell", "sideways"); err == nil {
		t.Fatal("unknown placement must be refused")
	}
}

func TestSubrepoHideAndUnhide(t *testing.T) {
	stubSubrepoStatus(t)
	root := t.TempDir()
	makeSubrepo(t, filepath.Join(root, "api"))
	makeSubrepo(t, filepath.Join(root, "web"))
	h := newHarness(t, state.State{SubrepoDepth: 1}, root)
	h.waitFor("scan", func(m *model) bool { return len(m.subrepos[root].repos) == 2 })

	if err := h.core.SubrepoHide(0, filepath.Join(root, "api")); err != nil {
		t.Fatal(err)
	}
	view := h.snapshot()
	if got := subrepoRels(view); !slices.Equal(got, []string{"web"}) || view.Workspaces[0].HiddenSubrepos != 1 {
		t.Fatalf("after hide: %v, hidden %d", got, view.Workspaces[0].HiddenSubrepos)
	}
	// A later scan keeps skipping the folder.
	_ = h.core.do(func() error { h.core.m.refreshSubrepos(); return nil })
	h.waitFor("rescan settles", func(m *model) bool { return !m.subrepos[root].pending })
	if got := subrepoRels(h.snapshot()); !slices.Equal(got, []string{"web"}) {
		t.Fatalf("hidden folder came back after rescan: %v", got)
	}
	if err := h.core.SubrepoHide(0, filepath.Join(root, "..", "outside")); err == nil {
		t.Fatal("hiding a path outside the workspace must be refused")
	}

	if err := h.core.SubrepoUnhide(0); err != nil {
		t.Fatal(err)
	}
	h.waitFor("both back", func(m *model) bool { return len(m.subrepos[root].repos) == 2 })
	if hidden := h.snapshot().Workspaces[0].HiddenSubrepos; hidden != 0 {
		t.Fatalf("hidden count after unhide = %d", hidden)
	}
}

// The app starts through Activate, and new workspaces arrive later. Both
// must scan at once instead of waiting a full gitinfo.SubrepoInterval.
func TestSubreposScanImmediatelyOnActivateAndCreate(t *testing.T) {
	stubSubrepoStatus(t)
	first, second := t.TempDir(), t.TempDir()
	makeSubrepo(t, filepath.Join(first, "api"))
	makeSubrepo(t, filepath.Join(second, "web"))

	c := New(Config{Version: "test", DefaultAgent: "zot", Shell: "/bin/sh", StatePath: t.TempDir() + "/state.json",
		FrameInterval: time.Millisecond, SaveDelay: time.Millisecond},
		state.State{}, nil, Hooks{}, nil, prefs.Prefs{}, nil)
	go c.Run()
	t.Cleanup(c.Stop)
	h := &harness{t: t, core: c}

	saved := state.State{SubrepoDepth: 1, Workspaces: []state.Workspace{{Name: "first", CWD: first,
		Tabs: []state.Tab{{Panes: []state.Pane{{Kind: "shell", Name: "s"}}}}}}}
	if err := c.Activate("", saved, newFakeLauncher(true), nil); err != nil {
		t.Fatal(err)
	}
	// Well under the ticker interval: only an immediate scan can pass.
	h.waitFor("scan after Activate", func(m *model) bool { return len(m.subrepos[first].repos) == 1 })

	if err := c.WorkspaceCreate(second, "shell", nil); err != nil {
		t.Fatal(err)
	}
	h.waitFor("scan after workspace create", func(m *model) bool { return len(m.subrepos[second].repos) == 1 })
}

func TestStaleSubrepoScanIsDropped(t *testing.T) {
	m := newModel(Config{DefaultAgent: "zot"}, state.State{SubrepoDepth: 1}, nil, Hooks{}, nil, prefsZero())
	m.subrepoGen = 2
	m.applySubrepos(1, "/w", []gitinfo.Subrepo{{Path: "/w/api", Rel: "api"}})
	if _, ok := m.subrepos["/w"]; ok {
		t.Fatal("a scan from an older generation must be ignored")
	}
}

func TestRestoreCleansHiddenSubrepos(t *testing.T) {
	dir := t.TempDir()
	saved := state.State{SubrepoDepth: 9, Workspaces: []state.Workspace{{Name: "w", CWD: dir,
		HiddenSubrepos: []string{"web", "../escape", "api", "web", "/abs"},
		Tabs:           []state.Tab{{Panes: []state.Pane{{Kind: "shell", Name: "s"}}}}}}}
	m := newModel(Config{DefaultAgent: "zot"}, saved, nil, Hooks{}, nil, prefsZero())
	if m.subrepoDepth != gitinfo.MaxSubrepoDepth {
		t.Fatalf("depth = %d, want clamped to %d", m.subrepoDepth, gitinfo.MaxSubrepoDepth)
	}
	if got := m.spaces[0].hiddenSubrepos; !slices.Equal(got, []string{"api", "web"}) {
		t.Fatalf("hidden = %v", got)
	}
	if out := m.snapshot(); !slices.Equal(out.Workspaces[0].HiddenSubrepos, []string{"api", "web"}) || out.SubrepoDepth != 3 {
		t.Fatalf("snapshot = %+v depth %d", out.Workspaces[0].HiddenSubrepos, out.SubrepoDepth)
	}
}
