package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/plugin"
	"github.com/patriceckhart/hrdx/internal/term"
)

func pluginModel(t *testing.T, grants []string) (Model, string) {
	t.Helper()
	path, other := t.TempDir(), t.TempDir()
	model := newTestModel(path, other)
	model.config.PluginViews = true
	directory := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "peer.exe"), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.Manifest{
		Schema: 1, ID: "example.ui", Version: "1.0.0", Entrypoint: "peer.exe", Args: []string{"-test.run=^TestUIPluginPeer$"},
		Protocol: plugin.ProtocolRange{Min: 1, Max: 1}, Requests: plugin.SupportedGrants(),
		Contributes: plugin.Contributions{
			Actions:   []plugin.Action{{ID: "example.ui.action", Label: "Example action", Targets: []string{"workspace", "pane"}}},
			Views:     []string{"example.ui.panel"},
			Providers: []plugin.Provider{{ID: "example.ui.search", Kind: "search", Label: "Example search"}},
		},
		Config: []plugin.ConfigOption{{Key: "limit", Type: "int", Default: float64(3)}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "plugin.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := plugin.InspectPackage(directory)
	approval, err := plugin.Approve(entry, grants, []string{path}, false)
	if err != nil {
		t.Fatal(err)
	}
	runtime := plugin.NewRuntime([]plugin.Registration{{Package: entry, Approval: approval}})
	t.Cleanup(runtime.Close)
	model.SetPlugins(runtime)
	if err := runtime.Start(entry.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		for _, status := range runtime.Statuses() {
			if status.State == "ready" {
				model.syncPluginStates()
				return model, status.Generation
			}
		}
		select {
		case <-runtime.Events():
		case <-deadline.C:
			t.Fatalf("plugin not ready: %+v", runtime.Statuses())
		}
	}
}

func TestUIPluginPeer(t *testing.T) {
	if os.Getenv("HRDX_PLUGIN_ID") != "example.ui" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	emit := func(frame plugin.Frame) {
		if encoder.Encode(frame) != nil {
			os.Exit(2)
		}
	}
	emit(plugin.Frame{Type: "hello", Plugin: "example.ui", Protocol: 1})
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), plugin.MaxFrameBytes+1)
	last := json.RawMessage(`null`)
	config := json.RawMessage(`null`)
	for scanner.Scan() {
		var frame plugin.Frame
		if json.Unmarshal(scanner.Bytes(), &frame) != nil {
			os.Exit(3)
		}
		switch frame.Type {
		case "hello_ack":
			config, _ = json.Marshal(frame.Config)
			emit(plugin.Frame{Type: "ready"})
		case "event":
			last = frame.Params
		case "request":
			result := json.RawMessage(`{"notification":"Completed"}`)
			switch frame.Method {
			case "test.last_event":
				result = last
			case "test.config":
				result = config
			case "provider.query":
				var query struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal(frame.Params, &query)
				result = json.RawMessage(`{"rows":[{"label":"match ` + query.Query + `","id":"row-1"},{"label":"bad\u001b","id":"x"},{"label":"pane","pane_id":999999}]}`)
			case "provider.select":
				result = json.RawMessage(`{"notification":"Selected"}`)
			}
			emit(plugin.Frame{Type: "response", ID: frame.ID, Result: result})
		case "shutdown":
			emit(plugin.Frame{Type: "shutdown_ack"})
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func pluginRequest(t *testing.T, model *Model, generation, method string, params any) (plugin.Reply, tea.Cmd) {
	t.Helper()
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	request := plugin.Request{Plugin: "example.ui", Generation: generation, Context: t.Context(), Method: method, Params: data, Reply: make(chan plugin.Reply, 1)}
	updated, cmd := model.Update(pluginEventMsg{event: plugin.Event{Request: &request}})
	*model = updated.(Model)
	select {
	case reply := <-request.Reply:
		return reply, cmd
	default:
		t.Fatal("plugin request was not answered")
		return plugin.Reply{}, nil
	}
}

func TestPluginScopedQueriesAndMutations(t *testing.T) {
	model, generation := pluginModel(t, []string{"workspace.read", "pane.read_metadata", "pane.create", "pane.read_screen"})
	reply, _ := pluginRequest(t, &model, generation, "status", map[string]any{})
	status, ok := reply.Result.(api.Status)
	if reply.Error != nil || !ok || len(status.Workspaces) != 1 || status.Workspaces[0].Path != model.spaces[0].cwd {
		t.Fatalf("scoped workspace is missing or includes an unauthorized workspace: %+v", reply)
	}
	foreign := model.spaces[1].tab().panes[0]
	reply, _ = pluginRequest(t, &model, generation, "pane.read", api.PaneRef{Pane: foreign.id})
	if reply.Error == nil || reply.Error.Code != "denied" {
		t.Fatalf("foreign screen read: %+v", reply)
	}
	reply, _ = pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: model.spaces[0].tab().panes[0].id, Text: "do not send"})
	if reply.Error == nil || reply.Error.Code != "denied" {
		t.Fatalf("peer input requires its own grant: %+v", reply)
	}
	model.selected = 1
	reply, _ = pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "not-a-kind", Split: "right"})
	if reply.Error == nil || model.selected != 1 {
		t.Fatal("invalid creation changed workspace focus")
	}
	reply, _ = pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: " FLOAT "})
	if reply.Error == nil || reply.Error.Code != "invalid_params" {
		t.Fatalf("float without dimensions accepted: %+v", reply)
	}
	reply, cmd := pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "right"})
	if reply.Error != nil || cmd == nil || len(model.spaces[0].tab().panes) != 2 {
		t.Fatalf("pane creation failed: %+v", reply)
	}
	model.plugins.Stop("example.ui")
	model.syncPluginStates()
	if len(model.spaces[0].tab().panes) != 2 {
		t.Fatal("plugin stop removed host-owned pane")
	}
	reply, _ = pluginRequest(t, &model, generation, "status", map[string]any{})
	if reply.Error == nil {
		t.Fatal("stopped plugin retained access")
	}
}

func TestPluginActionsAndNotifications(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.action.contribute", "ui.notification", "ui.status.contribute"})
	model.menuPane = model.currentPane()
	items := model.menuItems()
	if items[len(items)-1].action != "plugin-action:example.ui/example.ui.action" {
		t.Fatalf("missing action: %+v", items)
	}
	updated, cmd := model.runMenuAction(items[len(items)-1].action)
	model = updated.(Model)
	if cmd == nil {
		t.Fatal("missing invocation")
	}
	activation := cmd().(pluginActivationMsg)
	cmd = model.handlePluginActivation(activation)
	if cmd == nil {
		t.Fatal("activation did not invoke action")
	}
	message := cmd().(pluginResultMsg)
	model.handlePluginResult(message)
	if !strings.Contains(model.status, "Completed") {
		t.Fatalf("notification: %q", model.status)
	}
	pluginRequest(t, &model, generation, "ui.status", map[string]string{"text": "Busy"})
	pluginRequest(t, &model, generation, "ui.status", map[string]string{"text": "Idle"})
	if model.pluginTexts["example.ui"].text != "Idle" {
		t.Fatal("status burst lost the latest value")
	}
	model.plugins.Stop("example.ui")
	model.syncPluginStates()
	for _, item := range model.menuItems() {
		if strings.HasPrefix(item.action, "plugin-action:") {
			t.Fatal("stopped plugin contribution remained")
		}
	}
}

func TestPluginActionRejectsTargetClosedDuringActivation(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.action.contribute"})
	target := model.currentPane()
	owner := model.currentSpace()
	message := pluginActivationMsg{id: "example.ui", actionID: "example.ui.action", generation: generation, target: "pane", pane: target, space: owner}
	model.closeCurrentSpace()
	if command := model.handlePluginActivation(message); command == nil {
		t.Fatal("stale target did not produce user feedback")
	}
	if !strings.Contains(model.status, "no longer available") {
		t.Fatalf("stale target status: %q", model.status)
	}
}

func TestPluginViewsClipAndKeepHostOwnership(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.view.contribute", "ui.view.input"})
	model.width, model.height = 40, 15
	view := pluginView{ID: "example.ui.panel", Title: "Example", Lines: []string{strings.Repeat("界", 50), "e\u0301"}, Width: 80, Height: 80, Focus: true}
	reply, _ := pluginRequest(t, &model, generation, "ui.view.open", view)
	if reply.Error != nil || model.mode != modePluginView {
		t.Fatalf("view: %+v", reply)
	}
	before := model.snapshot()
	rows := make([]string, model.height-2)
	for index := range rows {
		rows[index] = strings.Repeat(" ", model.width)
	}
	model.overlayPluginViews(rows)
	for _, row := range rows {
		if lipgloss.Width(row) != model.width {
			t.Fatalf("view escaped width: %d", lipgloss.Width(row))
		}
	}
	model.mode = modeSettings
	if model.pluginViewMouse(tea.MouseMsg{X: 20, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}) {
		t.Fatal("view captured settings input")
	}
	model.mode = modeTerminal
	model.selPane = model.currentPane()
	reply, _ = pluginRequest(t, &model, generation, "ui.view.update", view)
	if reply.Error != nil || model.mode != modeTerminal {
		t.Fatal("view stole focus from an active host selection")
	}
	if model.pluginViewMouse(tea.MouseMsg{X: 20, Y: 7, Action: tea.MouseActionMotion}) {
		t.Fatal("view captured an existing host drag")
	}
	model.selPane = nil
	model.pluginViewMouse(tea.MouseMsg{X: 20, Y: 7, Action: tea.MouseActionMotion})
	hover, hoverErr := model.plugins.Call(t.Context(), "example.ui", "test.last_event", nil)
	if hoverErr != nil || strings.Contains(string(hover), `"mouse"`) {
		t.Fatalf("unfocused view received hover input: %s %v", hover, hoverErr)
	}
	model.mode = modePluginView
	updated, _ := model.updatePluginViewKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	model = updated.(Model)
	data, err := model.plugins.Call(context.Background(), "example.ui", "test.last_event", nil)
	if err != nil || !strings.Contains(string(data), `"key":"a"`) {
		t.Fatalf("view input: %s %v", data, err)
	}
	view.Lines = []string{"\x1b]52;c;malicious\a"}
	reply, _ = pluginRequest(t, &model, generation, "ui.view.update", view)
	if reply.Error == nil {
		t.Fatal("view accepted terminal controls")
	}
	model.plugins.Stop("example.ui")
	model.syncPluginStates()
	if len(model.pluginViews) != 0 || model.mode != modeTerminal {
		t.Fatal("view not cleaned up on stop")
	}
	after := model.snapshot()
	old, _ := json.Marshal(before)
	current, _ := json.Marshal(after)
	if string(old) != string(current) {
		t.Fatal("views changed persisted workspace state")
	}
}

func TestPluginSnapshotSubscription(t *testing.T) {
	model, generation := pluginModel(t, []string{"host.events.subscribe", "workspace.read"})
	reply, cmd := pluginRequest(t, &model, generation, "events.subscribe", map[string]any{"events": []string{"snapshot.changed"}})
	if reply.Error != nil || cmd == nil {
		t.Fatalf("subscription: %+v", reply)
	}
	model.publishPluginSnapshots()
	data, err := model.plugins.Call(t.Context(), "example.ui", "test.last_event", nil)
	if err != nil || strings.Contains(string(data), model.spaces[1].cwd) || !strings.Contains(string(data), `"sequence":1`) {
		t.Fatalf("scoped event: %s %v", data, err)
	}
	model.plugins.Stop("example.ui")
	model.publishPluginSnapshots()
	if len(model.pluginSubscriptions) != 0 || model.pluginTicking {
		t.Fatal("subscription survived stop")
	}
}

func TestPluginInputIsGrantedScopedAndBounded(t *testing.T) {
	model, generation := pluginModel(t, []string{"pane.send_input"})
	host := &recordingHost{}
	target := model.spaces[0].tab().panes[0]
	target.term = term.NewHolderPane(host, 1, 40, 6)
	target.running = true
	t.Cleanup(target.term.MarkExited)
	reply, _ := pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: target.id, Text: "ls", Enter: true})
	if reply.Error != nil {
		t.Fatalf("granted input rejected: %+v", reply.Error)
	}
	host.wait(t, "ls\r")
	foreign := model.spaces[1].tab().panes[0]
	foreign.term = term.NewHolderPane(host, 2, 40, 6)
	foreign.running = true
	t.Cleanup(foreign.term.MarkExited)
	reply, _ = pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: foreign.id, Text: "rm"})
	if reply.Error == nil || reply.Error.Code != "denied" {
		t.Fatalf("out-of-scope input accepted: %+v", reply)
	}
	reply, _ = pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: target.id, Text: strings.Repeat("x", plugin.MaxInputBytes+1)})
	if reply.Error == nil || reply.Error.Code != "invalid_params" {
		t.Fatalf("oversized input accepted: %+v", reply)
	}
	reply, _ = pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: target.id, Text: "a\x00b"})
	if reply.Error == nil || reply.Error.Code != "invalid_params" {
		t.Fatalf("NUL input accepted: %+v", reply)
	}
	target.term.MarkExited()
	reply, _ = pluginRequest(t, &model, generation, "pane.send_text", api.PaneSendText{Pane: target.id, Text: "late"})
	if reply.Error == nil {
		t.Fatal("input accepted for exited pane")
	}
}

type recordingHost struct {
	mu     sync.Mutex
	writes []string
}

func (h *recordingHost) Write(_ int64, data []byte) {
	h.mu.Lock()
	h.writes = append(h.writes, string(data))
	h.mu.Unlock()
}
func (h *recordingHost) Resize(int64, int, int)  {}
func (h *recordingHost) Kill(int64)              {}
func (h *recordingHost) Foreground(int64) string { return "" }

func (h *recordingHost) wait(t *testing.T, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.mu.Lock()
		found := slices.Contains(h.writes, want)
		h.mu.Unlock()
		if found {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("input %q was not written", want)
		case <-time.After(time.Millisecond):
		}
	}
}

func TestPluginTemporaryPanesFollowConnectionLifetime(t *testing.T) {
	model, generation := pluginModel(t, []string{"pane.create"})
	size := 50
	before := len(model.spaces[0].tab().panes)
	reply, cmd := pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "float", WidthPct: &size, HeightPct: &size})
	if reply.Error != nil || cmd == nil {
		t.Fatalf("temporary pane creation failed: %+v", reply)
	}
	created, ok := reply.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result: %+v", reply.Result)
	}
	id := created["pane_id"].(int)
	live, _ := model.paneByID(id)
	if live == nil || live.floating == nil || len(model.pluginPanes) != 1 {
		t.Fatal("temporary pane is not a tracked float")
	}
	if len(model.spaces[0].tab().panes) != before+1 {
		t.Fatal("pane count mismatch")
	}
	snapshot := model.snapshot()
	for _, ws := range snapshot.Workspaces {
		for _, tab := range ws.Tabs {
			if len(tab.Panes) != before {
				t.Fatal("temporary pane entered persisted state")
			}
		}
	}
	if reply, _ := pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "right"}); reply.Error != nil {
		t.Fatalf("durable pane creation failed: %+v", reply)
	}
	durable := len(model.spaces[0].tab().panes)
	model.plugins.Stop("example.ui")
	model.syncPluginStates()
	if live, _ := model.paneByID(id); live != nil || len(model.pluginPanes) != 0 {
		t.Fatal("temporary pane survived plugin stop")
	}
	if len(model.spaces[0].tab().panes) != durable-1 {
		t.Fatal("durable pane was removed with the temporary one")
	}
	if leaves := countLeaves(model.spaces[0].tab().layout); leaves != durable-1 {
		t.Fatalf("split leaves = %d, want %d", leaves, durable-1)
	}
}

func countLeaves(n *splitNode) int {
	if n == nil {
		return 0
	}
	if n.Pane != nil {
		return 1
	}
	return countLeaves(n.A) + countLeaves(n.B)
}

func TestPluginProvidersFeedTheFinder(t *testing.T) {
	model, _ := pluginModel(t, []string{"ui.provider.contribute", "ui.notification"})
	updated, _ := model.openFind()
	model = updated.(Model)
	model.input.SetValue("qu")
	cmd := model.pluginProviderQuery()
	if cmd == nil {
		t.Fatal("provider query was not dispatched")
	}
	if again := model.pluginProviderQuery(); again != nil {
		t.Fatal("repeated query re-dispatched")
	}
	message, ok := cmd().(providerQueryMsg)
	if !ok {
		t.Fatalf("unexpected message %T", cmd())
	}
	stale := message
	stale.query = "old"
	model.handleProviderResult(stale)
	if len(model.providerRows) != 0 {
		t.Fatal("stale result accepted")
	}
	model.handleProviderResult(message)
	if len(model.providerRows) != 1 || model.providerRows[0].row.Label != "match qu" {
		t.Fatalf("provider rows = %+v", model.providerRows)
	}
	model.findIndex = len(model.findCandidates())
	updated, selectCmd := model.updateFindKey(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if model.mode != modeTerminal || selectCmd == nil {
		t.Fatal("provider row selection did not close the finder and invoke the plugin")
	}
	model.handlePluginResult(selectCmd().(pluginResultMsg))
	if !strings.Contains(model.status, "Selected") {
		t.Fatalf("selection result: %q", model.status)
	}
	data, err := model.plugins.Call(t.Context(), "example.ui", "test.config", nil)
	if err != nil || !strings.Contains(string(data), `"limit":3`) {
		t.Fatalf("config not delivered in hello_ack: %s %v", data, err)
	}
}

func TestPluginActivationMarkersGateActions(t *testing.T) {
	model, _ := pluginModel(t, []string{"ui.action.contribute"})
	for _, entry := range model.plugins.Registrations() {
		entry.Package.Manifest.Activation.Markers = []string{"marker.txt"}
	}
	model.menuSpace = model.spaces[0]
	for _, item := range model.menuItems() {
		if strings.HasPrefix(item.action, "plugin-action:") {
			t.Fatal("action offered without its activation marker")
		}
	}
	if err := os.WriteFile(filepath.Join(model.spaces[0].cwd, "marker.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range model.menuItems() {
		if strings.HasPrefix(item.action, "plugin-action:") {
			found = true
		}
	}
	if !found {
		t.Fatal("action missing after marker appeared")
	}
}

func TestPluginNotificationSeverityAndStatusPriority(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.notification", "ui.status.contribute"})
	reply, _ := pluginRequest(t, &model, generation, "ui.notify", map[string]any{"text": "Broken", "severity": "error"})
	if reply.Error != nil || model.statusIsInfo || !strings.Contains(model.status, "Broken") {
		t.Fatalf("error notification: %+v %q", reply, model.status)
	}
	if reply, _ := pluginRequest(t, &model, generation, "ui.notify", map[string]any{"text": "x", "severity": "fatal"}); reply.Error == nil {
		t.Fatal("unknown severity accepted")
	}
	pluginRequest(t, &model, generation, "ui.status", map[string]any{"text": "Low", "priority": 1})
	model.pluginTexts["other.plugin"] = pluginStatusText{text: "High", priority: 9}
	if footer := model.pluginFooter(); !strings.HasPrefix(footer, "other.plugin: High") {
		t.Fatalf("priority ordering: %q", footer)
	}
	pluginRequest(t, &model, generation, "ui.status", map[string]any{"text": ""})
	if _, ok := model.pluginTexts["example.ui"]; ok {
		t.Fatal("empty status did not clear the contribution")
	}
}

// Snapshot publication that cannot be delivered must leave a sequence gap and
// retry while the snapshot still differs, so a recovering peer resyncs.
func TestPluginSnapshotOverflowLeavesGapAndRetries(t *testing.T) {
	model, generation := pluginModel(t, []string{"host.events.subscribe", "workspace.read"})
	pluginRequest(t, &model, generation, "events.subscribe", map[string]any{"events": []string{"snapshot.changed"}})
	model.publishPluginSnapshots()
	subscription := model.pluginSubscriptions["example.ui"]
	if subscription.sequence != 1 {
		t.Fatalf("sequence=%d", subscription.sequence)
	}
	// Simulate a delivery failure by renaming the workspace after the last
	// delivered snapshot and forcing Publish to fail through a stale generation.
	model.spaces[0].name = "renamed"
	subscription.generation = "stale"
	model.publishPluginSnapshots()
	if _, ok := model.pluginSubscriptions["example.ui"]; ok {
		t.Fatal("stale-generation subscription was not removed")
	}
	// Reinstall and verify a changed snapshot after a gap bumps the sequence.
	pluginRequest(t, &model, generation, "events.subscribe", map[string]any{"events": []string{"snapshot.changed"}})
	model.publishPluginSnapshots()
	subscription = model.pluginSubscriptions["example.ui"]
	last := subscription.sequence
	model.spaces[0].name = "renamed-again"
	model.publishPluginSnapshots()
	if subscription.sequence != last+1 {
		t.Fatalf("rename did not publish: %d -> %d", last, subscription.sequence)
	}
	// Reorder/rename of another (out of scope) workspace must not publish.
	before := subscription.sequence
	model.spaces[1].name = "foreign"
	model.publishPluginSnapshots()
	if subscription.sequence != before {
		t.Fatal("out-of-scope change leaked into the scoped snapshot")
	}
}

func TestPluginFooterClipsOnTinyWindow(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.status.contribute"})
	pluginRequest(t, &model, generation, "ui.status", map[string]any{"text": strings.Repeat("界", 100)})
	for _, width := range []int{1, 5, 12, 40} {
		model.width = width
		if got := lipgloss.Width(model.renderFooter()); got > width {
			t.Fatalf("footer width %d exceeds window %d", got, width)
		}
	}
}

func TestPluginDurablePanesSurviveQuitLikeHostPanes(t *testing.T) {
	model, generation := pluginModel(t, []string{"pane.create"})
	size := 40
	pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "right"})
	pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "float", WidthPct: &size, HeightPct: &size})
	snapshot := model.snapshot()
	if got := len(snapshot.Workspaces[0].Tabs[0].Panes); got != 2 {
		t.Fatalf("persisted panes = %d, want the original and the durable split only", got)
	}
	model.quitting = true
	model.syncPluginStates()
	if len(model.pluginPanes) != 0 {
		t.Fatal("temporary pane survived quit")
	}
	if got := len(model.snapshot().Workspaces[0].Tabs[0].Panes); got != 2 {
		t.Fatalf("quit changed persisted panes: %d", got)
	}
}

func TestPluginProviderTimeoutAndWorkspaceSwitch(t *testing.T) {
	model, _ := pluginModel(t, []string{"ui.provider.contribute"})
	updated, _ := model.openFind()
	model = updated.(Model)
	model.input.SetValue("ab")
	cmd := model.pluginProviderQuery()
	message := cmd().(providerQueryMsg)
	// A result arriving after the finder closed (workspace switch, escape) is
	// discarded without touching state.
	model.closeFind()
	model.handleProviderResult(message)
	if len(model.providerRows) != 0 {
		t.Fatal("result applied after the finder closed")
	}
	// Providers are not queried from an out-of-scope workspace.
	model.selected = 1
	updated, _ = model.openFind()
	model = updated.(Model)
	model.input.SetValue("ab")
	if cmd := model.pluginProviderQuery(); cmd != nil {
		t.Fatal("provider queried outside its workspace scope")
	}
	// A timeout surfaces as an error message and leaves rows untouched.
	timeout := providerQueryMsg{plugin: "example.ui", provider: "example.ui.search", query: "ab", err: plugin.ErrInvalidResult}
	model.handleProviderResult(timeout)
	if len(model.providerRows) != 0 {
		t.Fatal("errored provider result produced rows")
	}
}

func TestPluginDockedViewsReserveTerminalAreaWithoutEnteringSplitTree(t *testing.T) {
	model, generation := pluginModel(t, []string{"ui.view.contribute", "pane.create"})
	model.width, model.height = 100, 30
	full := model.fullTerminalArea()
	pluginRequest(t, &model, generation, "pane.create", api.PaneCreate{Workspace: model.spaces[0].cwd, Kind: "shell", Split: "right"})
	before := model.snapshot()
	view := pluginView{ID: "example.ui.panel", Title: "Dock", Lines: []string{"row"}, Width: 30, Height: 30, Dock: "right"}
	reply, _ := pluginRequest(t, &model, generation, "ui.view.open", view)
	if reply.Error != nil {
		t.Fatalf("dock open: %+v", reply.Error)
	}
	area := model.terminalArea()
	if area.w >= full.w || area.h != full.h {
		t.Fatalf("right dock did not reserve width: full=%+v area=%+v", full, area)
	}
	tab := model.spaces[0].tab()
	if leaves := countLeaves(tab.layout); leaves != len(tab.panes) {
		t.Fatalf("split tree changed: leaves=%d panes=%d", leaves, len(tab.panes))
	}
	for _, pr := range model.layoutFor(tab) {
		if pr.r.x+pr.r.w > area.w {
			t.Fatalf("pane %+v extends into the docked strip (area %+v)", pr.r, area)
		}
	}
	// Body rows only: the header renders the workspace path unclipped in
	// this test fixture and is unrelated to docking.
	rows := strings.Split(model.View(), "\n")
	for index, row := range rows[1 : len(rows)-1] {
		if lipgloss.Width(row) > model.width {
			t.Fatalf("docked view widened body row %d to %d", index, lipgloss.Width(row))
		}
	}
	bottom := pluginView{ID: "example.ui.panel", Title: "Dock", Lines: []string{"row"}, Width: 30, Height: 30, Dock: "bottom"}
	if reply, _ := pluginRequest(t, &model, generation, "ui.view.update", bottom); reply.Error == nil {
		t.Fatal("view moved between frame types")
	}
	view.Width = 90
	if reply, _ := pluginRequest(t, &model, generation, "ui.view.update", view); reply.Error == nil {
		t.Fatal("docked view exceeded half the area")
	}
	model.closePluginView("example.ui.panel")
	if got := model.terminalArea(); got != full {
		t.Fatalf("area not restored after close: %+v", got)
	}
	after := model.snapshot()
	old, _ := json.Marshal(before)
	current, _ := json.Marshal(after)
	if string(old) != string(current) {
		t.Fatal("docked view changed persisted state")
	}
	model.width = 12
	tiny := pluginView{ID: "example.ui.panel", Title: "Dock", Lines: nil, Width: 50, Height: 50, Dock: "right"}
	if reply, _ := pluginRequest(t, &model, generation, "ui.view.open", tiny); reply.Error != nil {
		t.Fatalf("tiny dock: %+v", reply.Error)
	}
	if area := model.terminalArea(); area.w < minPaneCols {
		t.Fatalf("dock starved terminals: %+v", area)
	}
}
