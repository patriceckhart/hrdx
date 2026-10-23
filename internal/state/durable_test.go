package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyStateLoadsWithoutDurableFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{"workspaces":[{"name":"api","cwd":"/tmp/api","tabs":[{"panes":[{"kind":"zot","name":"zot 1","session":4}],"selected":0}]}],"selected":0}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := loaded.Workspaces[0]
	pane := ws.Tabs[0].Panes[0]
	if pane.Durable != nil || ws.Continuous != nil || ws.Host != "" || pane.Session != 4 {
		t.Fatalf("legacy pane = %+v workspace = %+v", pane, ws)
	}
}

func TestDurableFieldsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	ref := DurableRef{Profile: "local-abc", Workspace: "/tmp/api#2", Conversation: "conv-1", Title: "review"}
	original := State{Workspaces: []Workspace{{
		Name: "api", CWD: "/tmp/api", Host: "ext",
		Continuous: []DurableRef{ref},
		Tabs:       []Tab{{Panes: []Pane{{Kind: "zot", Name: "durable 1", Session: 9, Durable: &ref}, {Kind: "shell", Name: "shell 1"}}}},
	}}}
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := loaded.Workspaces[0]
	if ws.Host != "ext" || len(ws.Continuous) != 1 || ws.Continuous[0] != ref {
		t.Fatalf("workspace = %+v", ws)
	}
	panes := ws.Tabs[0].Panes
	if panes[0].Durable == nil || *panes[0].Durable != ref || panes[0].Session != 9 || panes[1].Durable != nil {
		t.Fatalf("panes = %+v", panes)
	}
	if !ref.Valid() || (DurableRef{Profile: "p", Workspace: "w"}).Valid() || !(DurableRef{Profile: "p", Conversation: "c"}).Valid() {
		t.Fatal("DurableRef.Valid misreports completeness")
	}
}
