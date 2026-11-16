package core

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/api/apitest"
	"github.com/patriceckhart/hrdx/internal/state"
)

// TestAPIParityWithTUI replays the shared parity script and compares every
// answer with the fixture recorded from the TUI model. Regenerate it with the
// command in internal/ui/api_parity_test.go when the TUI's answers change.
func TestAPIParityWithTUI(t *testing.T) {
	data, err := os.ReadFile("testdata/api_parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var want []json.RawMessage
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	dirA, dirB := t.TempDir(), t.TempDir()
	// No launcher: like the TUI test model, panes never start.
	m := newModel(Config{Version: "test", DefaultAgent: "zot", Shell: "/bin/sh"}, state.State{}, nil, Hooks{}, nil, prefsZero())
	m.post = func(func()) {}
	m.openWorkspace(dirA)

	script := apitest.Script(dirA, dirB)
	if len(script) != len(want) {
		t.Fatalf("fixture has %d answers, script %d; re-record the fixture", len(want), len(script))
	}
	for index, step := range script {
		reply := make(chan api.Reply, 1)
		m.handleAPI(api.Request{Method: step.Method, Payload: step.Payload, Reply: reply})
		got, err := apitest.Encode(step.Method, <-reply, dirA, dirB)
		if err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		_ = json.Compact(&compact, want[index])
		if !bytes.Equal(got, compact.Bytes()) {
			t.Errorf("step %d %s\n got: %s\nwant: %s", index, step.Method, got, compact.Bytes())
		}
	}
}

func TestOutOfScopeMethodsAreUnknown(t *testing.T) {
	m := newModel(Config{DefaultAgent: "zot"}, state.State{}, nil, Hooks{}, nil, prefsZero())
	for _, method := range []string{"plugins.status", "plugins.control", "continuous.list", "continuous.attach"} {
		reply := make(chan api.Reply, 1)
		m.handleAPI(api.Request{Method: method, Reply: reply})
		if answer := <-reply; answer.Code != api.CodeUnknownMethod {
			t.Fatalf("%s = %+v", method, answer)
		}
	}
}

func TestEventsPublishedForAPIMutations(t *testing.T) {
	events := api.NewBroadcaster()
	id, channel := events.Subscribe()
	defer events.Unsubscribe(id)
	dir := t.TempDir()
	m := newModel(Config{DefaultAgent: "zot"}, state.State{}, nil, Hooks{}, events, prefsZero())
	reply := make(chan api.Reply, 1)
	m.handleAPI(api.Request{Method: "workspace.create", Payload: api.WorkspaceCreate{Path: dir, Agent: "shell"}, Reply: reply})
	<-reply
	var names []string
	for len(channel) > 0 {
		names = append(names, (<-channel).Event)
	}
	if len(names) != 2 || names[0] != api.EventWorkspaceCreated || names[1] != api.EventPaneCreated {
		t.Fatalf("events = %v", names)
	}
}
