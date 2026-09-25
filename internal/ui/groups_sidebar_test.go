package ui

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
)

func TestGroupedSidebarMouseAndScroll(t *testing.T) {
	for _, compact := range []bool{false, true} {
		model := newTestModel(t.TempDir(), t.TempDir(), t.TempDir())
		model.sideCollapsed = compact
		model.height = 40
		model.spaces[0].groupPath = []string{"group", "nested"}
		model.spaces[2].groupPath = []string{"group"}
		for rowIndex, row := range model.sidebarRows() {
			if row.kind != "group" && row.kind != "pane" {
				continue
			}
			model.selected = 1
			for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight} {
				updated, _ := model.updateMouse(tea.MouseMsg{X: 3, Y: rowIndex + 1, Button: button, Action: tea.MouseActionPress})
				got := updated.(Model)
				if row.kind == "group" {
					if got.selected != 1 || got.mode != modeTerminal || got.dragSpace != nil {
						t.Fatal("group heading changed focus or armed drag")
					}
				} else {
					target := model.spaces[row.space].tabs[row.tab].panes[row.pane]
					if got.currentPane() != target {
						t.Fatal("grouped pane click used visual index as model index")
					}
					if button == tea.MouseButtonRight && got.menuPane != target {
						t.Fatal("wrong context target")
					}
				}
			}
		}
		model.height = 9
		model.sideScroll = 4
		rows := model.sidebarRows()
		if got := model.sidebarHit(1); got != rows[5] {
			t.Fatalf("scrolled target: %+v want %+v", got, rows[5])
		}
	}
}

func TestGroupPathsBoundariesAndIdentity(t *testing.T) {
	if err := validateGroupPath([]string{strings.Repeat("界", 80)}); err != nil {
		t.Fatal(err)
	}
	deep := make([]string, 2048)
	for i := range deep {
		deep[i] = "a"
	}
	if err := validateGroupPath(deep); err != nil {
		t.Fatal(err)
	}
	if validateGroupPath(append(deep, "a")) == nil {
		t.Fatal("oversized path accepted")
	}
	model := newTestModel(t.TempDir(), t.TempDir(), t.TempDir())
	model.spaces[0].groupPath = []string{"a/b"}
	model.spaces[1].groupPath = []string{"a", "b"}
	model.spaces[2].groupPath = []string{"a", "b"}
	groups := groupsForStatus(model.apiStatus())
	want := []api.GroupStatus{{GroupPath: []string{"a/b"}}, {GroupPath: []string{"a"}}, {GroupPath: []string{"a", "b"}}}
	if !reflect.DeepEqual(groups.Groups, want) {
		t.Fatalf("path identity: %+v", groups)
	}
	model.spaces[0].name = model.spaces[2].cwd
	result := apiCall(t, &model, "workspace.move", api.WorkspaceMove{Workspace: model.spaces[2].cwd, GroupPath: []string{"moved"}})
	if result.Err != "" || model.spaces[2].groupPath[0] != "moved" || model.spaces[0].groupPath[0] != "a/b" {
		t.Fatal("exact path did not outrank name collision")
	}
}
