package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/state"
)

func TestWorkspaceGroupsLifecycle(t *testing.T) {
	model := newTestModel(t.TempDir())
	model.events = api.NewBroadcaster()
	id, events := model.events.Subscribe()
	defer model.events.Unsubscribe(id)
	path := []string{"project", "feature", "review"}
	dir := t.TempDir()
	result := apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: dir, Agent: "shell", GroupPath: path})
	if result.Err != "" {
		t.Fatal(result.Err)
	}
	created := (<-events).Data.(api.WorkspaceEvent)
	if !reflect.DeepEqual(created.GroupPath, path) {
		t.Fatalf("created: %+v", created)
	}
	<-events // pane.created
	target := model.spaces[1]
	tab, pane, layout := target.tab(), target.tab().panes[0], target.tab().layout
	pane.session = 123
	model.selected = 0
	result = apiCall(t, &model, "workspace.move", api.WorkspaceMove{Workspace: dir, GroupPath: []string{"other"}})
	if result.Err != "" {
		t.Fatal(result.Err)
	}
	event := <-events
	if event.Event != api.EventWorkspaceMoved || event.Data.(api.WorkspaceEvent).GroupPath[0] != "other" {
		t.Fatalf("move: %+v", event)
	}
	if model.selected != 0 || target.tab() != tab || tab.panes[0] != pane || tab.layout != layout || pane.session != 123 {
		t.Fatal("move changed execution state")
	}
	path[0] = "mutated"
	if created.GroupPath[0] != "project" {
		t.Fatal("event aliases request")
	}
	groups := apiCall(t, &model, "group.list", nil).Data.(api.Groups)
	if len(groups.Groups) != 1 || groups.Groups[0].GroupPath[0] != "other" {
		t.Fatalf("stale groups: %+v", groups)
	}
	result = apiCall(t, &model, "workspace.close", api.WorkspaceRef{Workspace: dir})
	if result.Err != "" {
		t.Fatal(result.Err)
	}
	closed := (<-events).Data.(api.WorkspaceEvent)
	if closed.GroupPath[0] != "other" {
		t.Fatalf("closed: %+v", closed)
	}
	if groups := apiCall(t, &model, "group.list", nil).Data.(api.Groups); len(groups.Groups) != 0 {
		t.Fatal(groups)
	}
}

func TestGroupValidationAndRestore(t *testing.T) {
	model := newTestModel(t.TempDir())
	ws := model.spaces[0]
	ws.groupPath = []string{"project", "sub/group"}
	for _, path := range [][]string{nil, {""}, {"  "}, {"bad\x1b[0m"}, {strings.Repeat("x", 81)}, {string([]byte{255})}} {
		result := apiCall(t, &model, "workspace.move", api.WorkspaceMove{Workspace: ws.cwd, GroupPath: path})
		if result.Code != api.CodeInvalidParams || ws.groupPath[0] != "project" {
			t.Fatalf("invalid move %+v: %+v", path, result)
		}
	}
	if result := apiCall(t, &model, "workspace.move", api.WorkspaceMove{Workspace: "missing", GroupPath: []string{}}); result.Code != api.CodeNotFound {
		t.Fatal(result)
	}
	snapshot := model.snapshot()
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var saved state.State
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	restored := newTestModel()
	restored.restore(saved)
	if !reflect.DeepEqual(restored.apiStatus().Workspaces[0].GroupPath, ws.groupPath) {
		t.Fatal("group lost on restore")
	}
	snapshot.Workspaces[0].GroupPath[0] = "changed"
	if ws.groupPath[0] != "project" {
		t.Fatal("snapshot aliases live group")
	}
	saved.Workspaces[0].GroupPath = []string{"\n"}
	restored = newTestModel()
	restored.restore(saved)
	if len(restored.spaces[0].groupPath) != 0 {
		t.Fatal("unsafe saved group restored")
	}
	if result := apiCall(t, &model, "workspace.move", api.WorkspaceMove{Workspace: ws.cwd, GroupPath: []string{}}); result.Err != "" || len(ws.groupPath) != 0 {
		t.Fatal(result)
	}
}

func TestNestedGroupSidebarTargets(t *testing.T) {
	model := newTestModel(t.TempDir(), t.TempDir(), t.TempDir())
	model.spaces[0].groupPath = []string{"界", "nested", "deep"}
	model.spaces[2].groupPath = []string{"界", "nested"}
	model.addTab(model.spaces[0], "shell")
	model.addFloatingPane(model.spaces[0], "shell", "center", 40, 30)
	for _, compact := range []bool{false, true} {
		model.sideCollapsed = compact
		rows := model.sidebarRows()
		counts := make(map[int]int)
		groups := 0
		order := []int{}
		for _, row := range rows {
			if row.kind == "group" {
				groups++
				if row.space != -1 || row.tab != -1 || row.pane != -1 {
					t.Fatal("group is interactive")
				}
			}
			if row.kind == "space" {
				counts[row.space]++
				order = append(order, row.space)
			}
			if row.kind == "pane" && model.spaces[row.space].tabs[row.tab].panes[row.pane].floating != nil {
				t.Fatal("float in sidebar")
			}
			if lipgloss.Width(row.label) > model.sidebarContentWidth() {
				t.Fatalf("row too wide: %q", row.label)
			}
		}
		if groups != 3 || !reflect.DeepEqual(order, []int{0, 2, 1}) || counts[0] != 1 || counts[1] != 1 || counts[2] != 1 {
			t.Fatalf("group rows: %d %v", groups, order)
		}
	}
}

func TestGitGroupDefaultAndOverride(t *testing.T) {
	root := t.TempDir()
	repo, linked := filepath.Join(root, "repo"), filepath.Join(root, "linked")
	gitDir := filepath.Join(repo, ".git")
	worktreeDir := filepath.Join(gitDir, "worktrees", "linked")
	for _, path := range []string{worktreeDir, linked} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for path, content := range map[string]string{
		filepath.Join(gitDir, "HEAD"):           "ref: refs/heads/main\n",
		filepath.Join(linked, ".git"):           "gitdir: ../repo/.git/worktrees/linked\n",
		filepath.Join(worktreeDir, "commondir"): "../..\n",
		filepath.Join(worktreeDir, "HEAD"):      "ref: refs/heads/feature\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, cwd := range []string{repo, linked} {
		if got := gitGroupPath(cwd); !reflect.DeepEqual(got, []string{"repo"}) {
			t.Fatalf("git group %s: %v", cwd, got)
		}
	}
	for _, test := range []struct {
		group []string
		auto  bool
		want  []string
	}{
		{nil, false, nil}, {nil, true, []string{"repo"}}, {[]string{}, true, nil}, {[]string{"explicit"}, true, []string{"explicit"}},
	} {
		model := newTestModel()
		result := apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: linked, Agent: "shell", GroupFromGit: test.auto, GroupPath: test.group})
		if result.Err != "" || !reflect.DeepEqual(model.spaces[0].groupPath, test.want) {
			t.Fatalf("git create: %+v %v", result, model.spaces[0].groupPath)
		}
		if readGitBranch(linked) != "feature" {
			t.Fatal("branch metadata changed")
		}
	}
	if gitGroupPath(t.TempDir()) != nil {
		t.Fatal("non-Git directory grouped")
	}
}

func TestPluginGroupsAreScoped(t *testing.T) {
	model, generation := pluginModel(t, []string{"workspace.read", "workspace.move"})
	model.spaces[1].groupPath = []string{"private"}
	reply, _ := pluginRequest(t, &model, generation, "workspace.move", api.WorkspaceMove{Workspace: model.spaces[0].cwd, GroupPath: []string{"visible", "nested"}})
	if reply.Error != nil {
		t.Fatal(reply.Error)
	}
	reply, _ = pluginRequest(t, &model, generation, "group.list", map[string]any{})
	groups := reply.Result.(api.Groups)
	if len(groups.Groups) != 2 || groups.Groups[0].GroupPath[0] != "visible" {
		t.Fatalf("scoped groups: %+v", groups)
	}
	reply, _ = pluginRequest(t, &model, generation, "workspace.move", api.WorkspaceMove{Workspace: model.spaces[1].cwd, GroupPath: []string{}})
	if reply.Error == nil || reply.Error.Code != "denied" {
		t.Fatal("foreign move permitted")
	}
	denied, gen := pluginModel(t, []string{"workspace.read"})
	reply, _ = pluginRequest(t, &denied, gen, "workspace.move", api.WorkspaceMove{Workspace: denied.spaces[0].cwd, GroupPath: []string{}})
	if reply.Error == nil || reply.Error.Code != "denied" {
		t.Fatal("move without grant permitted")
	}
}
