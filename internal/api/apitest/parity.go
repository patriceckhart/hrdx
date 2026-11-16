// Package apitest holds the socket API parity script shared by the TUI and
// hrdx Desktop. Both frontends answer the same requests, and their encoded
// answers must match after path normalization.
package apitest

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/patriceckhart/hrdx/internal/api"
)

// Step is one request of the parity script.
type Step struct {
	Method  string
	Payload any
}

// Script returns the parity request script. The frontend under test starts
// with one workspace at dirA whose first pane runs the default agent; dirB is
// an existing directory that is not open yet. Pane processes are not started,
// so every pane reports running=false.
func Script(dirA, dirB string) []Step {
	width, height := 40, 30
	return []Step{
		{"status", nil},
		{"group.list", nil},
		{"workspace.create", api.WorkspaceCreate{Path: dirB, Agent: "shell", GroupPath: []string{"g1", "g2"}}},
		{"workspace.create", api.WorkspaceCreate{Path: dirB, Agent: "shell"}},
		{"workspace.create", api.WorkspaceCreate{Path: dirB, Agent: "nope"}},
		{"workspace.create", api.WorkspaceCreate{Path: filepath.Join(dirB, "missing"), Agent: "shell"}},
		{"group.list", nil},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "down"}},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "tab"}},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "float", Anchor: "left", WidthPct: &width, HeightPct: &height}},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "float"}},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "float", Anchor: "corner", WidthPct: &width, HeightPct: &height}},
		{"pane.create", api.PaneCreate{Kind: "shell", Split: "sideways"}},
		{"pane.create", api.PaneCreate{Workspace: "missing", Kind: "shell"}},
		{"pane.create", api.PaneCreate{Workspace: dirA, Kind: "nope"}},
		{"pane.send_text", api.PaneSendText{Pane: 2, Text: "x"}},
		{"pane.send_text", api.PaneSendText{Pane: 999, Text: "x"}},
		{"pane.read", api.PaneRef{Pane: 2}},
		{"pane.read", api.PaneRef{Pane: 999}},
		{"pane.busy", api.PaneRef{Pane: 2}},
		{"pane.busy", api.PaneRef{Pane: 999}},
		{"menu.register", api.MenuRegister{Target: "pane", Label: " Open ", ActionID: " open "}},
		{"menu.register", api.MenuRegister{Target: "window", Label: "x", ActionID: "x"}},
		{"workspace.move", api.WorkspaceMove{Workspace: dirA, GroupPath: []string{"moved"}}},
		{"workspace.move", api.WorkspaceMove{Workspace: dirA}},
		{"workspace.move", api.WorkspaceMove{Workspace: "missing", GroupPath: []string{}}},
		{"workspace.move", api.WorkspaceMove{Workspace: dirA, GroupPath: []string{" "}}},
		{"status", nil},
		{"pane.close", api.PaneRef{Pane: 3}},
		{"pane.close", api.PaneRef{Pane: 999}},
		{"workspace.close", api.WorkspaceRef{Workspace: "missing"}},
		{"workspace.close", api.WorkspaceRef{Workspace: dirB}},
		{"status", nil},
		{"bogus.method", nil},
	}
}

// Encode marshals one answer for comparison, with workspace paths replaced
// by $DIRA, $DIRB and names derived from them by dira, dirb.
func Encode(method string, reply api.Reply, dirs ...string) (json.RawMessage, error) {
	encoded, err := json.Marshal(map[string]any{"method": method, "result": reply.Data, "code": reply.Code, "error": reply.Err})
	if err != nil {
		return nil, err
	}
	for index, dir := range dirs {
		placeholder := "$DIR" + string(rune('A'+index))
		quoted, _ := json.Marshal(dir)
		encoded = bytes.ReplaceAll(encoded, bytes.Trim(quoted, `"`), []byte(placeholder))
		name, _ := json.Marshal(filepath.Base(dir))
		encoded = bytes.ReplaceAll(encoded, name, []byte(`"`+strings.ToLower(placeholder[1:])+`"`))
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, encoded); err != nil {
		return nil, err
	}
	return compact.Bytes(), nil
}
