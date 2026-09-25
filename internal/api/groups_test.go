package api

import (
	"encoding/json"
	"testing"
)

func TestGroupPathWireTypes(t *testing.T) {
	socket, _ := echoServer(t, nil)
	for _, method := range []string{"workspace.create", "workspace.move"} {
		for _, value := range []string{`"group"`, `[1]`, `{}`} {
			result := call(t, socket, `{"id":"g","method":"`+method+`","params":{"group_path":`+value+`}}`)
			if result.Error == nil || result.Error.Code != CodeInvalidParams {
				t.Fatalf("%s %s: %+v", method, value, result)
			}
		}
	}
	for _, value := range []string{`{}`, `{"group_path":null}`, `{"group_path":[]}`, `{"group_path":["one","two"]}`} {
		var params WorkspaceCreate
		if err := json.Unmarshal([]byte(value), &params); err != nil {
			t.Fatal(err)
		}
		if value == `{"group_path":[]}` && params.GroupPath == nil {
			t.Fatal("explicit standalone lost")
		}
		data, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip WorkspaceCreate
		if err := json.Unmarshal(data, &roundTrip); err != nil {
			t.Fatal(err)
		}
		if (params.GroupPath == nil) != (roundTrip.GroupPath == nil) {
			t.Fatal("explicit override lost on marshal")
		}
	}
}
