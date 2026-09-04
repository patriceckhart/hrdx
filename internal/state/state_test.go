package state

import (
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	zero := 0
	one := 1
	original := State{
		Selected:        1,
		DisableAutoCopy: true,
		Workspaces: []Workspace{
			{
				Name:     "api",
				CWD:      "/tmp/api",
				Panes:    []Pane{{Kind: "zot", Name: "zot 1"}, {Kind: "shell", Name: "shell 1"}},
				Selected: 1,
				Layout: &Node{
					Vertical: true,
					Ratio:    0.3,
					A:        &Node{Pane: &zero},
					B:        &Node{Pane: &one},
				},
			},
		},
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Workspaces) != 1 || loaded.Selected != 1 || !loaded.DisableAutoCopy {
		t.Fatalf("loaded = %+v", loaded)
	}
	ws := loaded.Workspaces[0]
	if ws.Name != "api" || ws.CWD != "/tmp/api" || len(ws.Panes) != 2 || ws.Selected != 1 {
		t.Fatalf("workspace = %+v", ws)
	}
	if ws.Layout == nil || !ws.Layout.Vertical || ws.Layout.Ratio != 0.3 {
		t.Fatalf("layout = %+v", ws.Layout)
	}
	if ws.Layout.A == nil || ws.Layout.A.Pane == nil || *ws.Layout.A.Pane != 0 {
		t.Fatalf("layout.A = %+v", ws.Layout.A)
	}
}

func TestLoadMissingFile(t *testing.T) {
	loaded, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil || len(loaded.Workspaces) != 0 {
		t.Fatalf("Load missing = %+v, %v", loaded, err)
	}
}
