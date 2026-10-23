package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/continuous"
	"github.com/patriceckhart/hrdx/internal/continuous/continuoustest"
	"github.com/patriceckhart/hrdx/internal/state"
)

// durableHost is a fake host with workspace roots, snapshots, and a
// mutable execution state per conversation.
type durableHost struct {
	*continuoustest.Host
	mu    sync.Mutex
	roots map[string]string
	snaps map[string]map[string]any
}

func newDurableHost() *durableHost {
	h := &durableHost{Host: continuoustest.New(), roots: map[string]string{}, snaps: map[string]map[string]any{}}
	h.On("runtime.status", func(json.RawMessage) (any, string, string) {
		return map[string]any{"version": "continuous", "blocked_runs": []string{}}, "", ""
	})
	h.On("conversation.create", func(params json.RawMessage) (any, string, string) {
		var p struct {
			Workspace string `json:"workspace"`
		}
		_ = json.Unmarshal(params, &p)
		h.mu.Lock()
		defer h.mu.Unlock()
		id, ok := h.roots[p.Workspace]
		if !ok {
			id = "conv-" + string(rune('a'+len(h.roots)))
			h.roots[p.Workspace] = id
			h.snaps[id] = map[string]any{"conversation": map[string]any{"id": id, "revision": 1}}
		}
		return map[string]any{"id": id, "revision": 1}, "", ""
	})
	h.On("conversation.snapshot", func(params json.RawMessage) (any, string, string) {
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(params, &p)
		h.mu.Lock()
		defer h.mu.Unlock()
		snap, ok := h.snaps[p.ID]
		if !ok {
			return nil, "not_found", "conversation not found"
		}
		return snap, "", ""
	})
	return h
}

func (h *durableHost) setRun(conv, phase, outcome string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snaps[conv]["run"] = map[string]any{"id": "run-1", "phase": phase, "outcome": outcome}
}

func (h *durableHost) setApproval(conv string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snaps[conv]["approvals"] = []map[string]any{{"id": "ap-1", "tool": "bash", "state": "pending"}}
}

// newDurableModel builds a model with persistence in a temp dir and one
// external host profile served by host.
func newDurableModel(t *testing.T, host *durableHost) Model {
	t.Helper()
	dir := t.TempDir()
	if err := continuous.UpsertProfile(dir, continuous.Profile{ID: "ext", Address: filepath.Join(dir, "h.sock")}); err != nil {
		t.Fatal(err)
	}
	// A missing zot binary makes view PTY starts fail fast, so tests never
	// launch a real agent; bindings are verified before any start.
	config := Config{Shell: "/bin/sh", AgentBins: map[string]string{"zot": filepath.Join(dir, "missing-zot")}}
	model := New(config, []string{"/tmp/api"}, filepath.Join(dir, "state.json"), state.State{})
	model.width, model.height = 120, 35
	model.cpool = continuous.NewPool(host.Dial)
	model.spaces[0].host = "ext"
	// Tests poll explicitly; mark the timers as running so no tick command
	// is scheduled and drive never waits on one.
	model.durableTicking, model.ticking = true, true
	return model
}

// drive executes a command and feeds durable messages back into the model,
// skipping PTY starts and timers so tests need no processes or sleeps.
func drive(t *testing.T, model Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			t.Fatal("command loop did not settle")
		}
		current := queue[0]
		queue = queue[1:]
		if current == nil {
			continue
		}
		msg := runCmd(current)
		switch value := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, value...)
			continue
		case durablePreparedMsg, durablePollMsg, durableActionMsg, durableAPIReply, durableKickMsg:
		default:
			continue
		}
		updated, next := model.update(msg)
		model = updated.(Model)
		queue = append(queue, next)
	}
	return model
}

// runCmd runs one command. Host commands answer immediately over the fake
// host; timer commands (poll and spinner ticks) are abandoned once the
// guard fires, which only bounds the test, it does not synchronize it.
func runCmd(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(500 * time.Millisecond):
		return nil
	}
}

func TestDurablePaneBindsToHostRoot(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	newPane, err := model.addDurablePane(owner, nil, "right")
	if err != nil {
		t.Fatal(err)
	}
	model = drive(t, model, model.prepareDurable(owner, newPane))
	if newPane.failure != "" || !newPane.durableReady {
		t.Fatalf("pane failure = %q ready = %v", newPane.failure, newPane.durableReady)
	}
	ref := newPane.durable
	if ref.Profile != "ext" || ref.Workspace != "/tmp/api" || ref.Conversation != "conv-a" {
		t.Fatalf("binding = %+v", ref)
	}
	if len(owner.durables) != 1 || owner.durables[0].Conversation != "conv-a" {
		t.Fatalf("workspace associations = %+v", owner.durables)
	}
	command, args, err := model.durableLaunch(newPane)
	if err != nil || command != model.config.binaryFor("zot") || strings.Join(args, " ") != "--continuous "+model.profiles["ext"].Address+" --continuous-workspace /tmp/api" {
		t.Fatalf("launch = %s %v %v", command, args, err)
	}
	// A second independent agent gets its own workspace key and conversation.
	second, _ := model.addDurablePane(owner, nil, "down")
	model = drive(t, model, model.prepareDurable(owner, second))
	if second.durable.Workspace != "/tmp/api#2" || second.durable.Conversation == ref.Conversation {
		t.Fatalf("second binding = %+v", second.durable)
	}
}

func TestDurableAnotherViewSharesConversation(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	first, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, first))
	ref := *first.durable
	view, _ := model.addDurablePane(owner, &ref, "tab")
	model = drive(t, model, model.prepareDurable(owner, view))
	if view.failure != "" || *view.durable != *first.durable {
		t.Fatalf("view = %+v failure %q, want %+v", view.durable, view.failure, ref)
	}
	if len(owner.durables) != 1 {
		t.Fatalf("a second view created a second association: %+v", owner.durables)
	}
}

func TestDurableRestoreRefusesMissingOrMismatchedConversation(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	missing, _ := model.addDurablePane(owner, &state.DurableRef{Profile: "ext", Workspace: "/tmp/api", Conversation: "gone"}, "right")
	model = drive(t, model, model.prepareDurable(owner, missing))
	if missing.durableReady || !strings.Contains(missing.failure, "not found") {
		t.Fatalf("missing conversation failure = %q", missing.failure)
	}
	if len(host.Requests("conversation.create")) != 0 {
		t.Fatal("restoring a missing conversation created a new one")
	}
	// The key now resolves to a different conversation than the one bound.
	host.mu.Lock()
	host.snaps["other"] = map[string]any{"conversation": map[string]any{"id": "other"}}
	host.roots["/tmp/api"] = "conv-x"
	host.mu.Unlock()
	mismatched, _ := model.addDurablePane(owner, &state.DurableRef{Profile: "ext", Workspace: "/tmp/api", Conversation: "other"}, "right")
	model = drive(t, model, model.prepareDurable(owner, mismatched))
	if mismatched.durableReady || !strings.Contains(mismatched.failure, "different conversation") {
		t.Fatalf("mismatch failure = %q", mismatched.failure)
	}
}

func TestDurableUnknownProfileFailsWithoutDialing(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, &state.DurableRef{Profile: "nope"}, "right")
	if cmd := model.prepareDurable(owner, target); cmd != nil {
		t.Fatal("unknown profile produced a host command")
	}
	if !strings.Contains(target.failure, "unknown continuous host profile") {
		t.Fatalf("failure = %q", target.failure)
	}
}

func TestDurableSnapshotRestorePreservesBindings(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	first, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, first))
	model.addPane(owner, "shell", true)

	saved := model.snapshot()
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	var decoded state.State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	ws := decoded.Workspaces[0]
	if ws.Host != "ext" || len(ws.Continuous) != 1 {
		t.Fatalf("saved workspace = %+v", ws)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", decoded)
	var durable, ordinary int
	for _, current := range restored.spaces[0].tabs[0].panes {
		if current.durable != nil {
			durable++
			if current.resume || current.durableReady || *current.durable != *first.durable {
				t.Fatalf("restored durable pane = %+v", current)
			}
		} else {
			ordinary++
		}
	}
	if durable != 1 || ordinary != 2 {
		t.Fatalf("restored durable=%d ordinary=%d, want 1/2", durable, ordinary)
	}
	if restored.spaces[0].host != "ext" || len(restored.spaces[0].durables) != 1 {
		t.Fatalf("restored workspace host=%q durables=%v", restored.spaces[0].host, restored.spaces[0].durables)
	}
}

func TestDurableRestoreIncompleteBindingStaysDurable(t *testing.T) {
	saved := state.State{Workspaces: []state.Workspace{{Name: "api", CWD: "/tmp/api", Tabs: []state.Tab{{
		Panes: []state.Pane{{Kind: "zot", Name: "durable 1", Durable: &state.DurableRef{Profile: "ext"}}},
	}}}}}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	target := restored.spaces[0].tabs[0].panes[0]
	if target.durable == nil || target.resume || target.failure == "" {
		t.Fatalf("incomplete binding restored as %+v", target)
	}
	if cmd := restored.startPane(restored.spaces[0], target); cmd != nil {
		t.Fatal("an unusable binding was started")
	}
}

func TestOrdinaryStateUnchangedByDurableFields(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.addPane(model.spaces[0], "shell", true)
	data, err := json.Marshal(model.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"durable", "continuous"} {
		if strings.Contains(string(data), "\""+field) {
			t.Fatalf("ordinary state contains %q: %s", field, data)
		}
	}
	for _, current := range model.spaces[0].tabs[0].panes {
		if current.durable != nil {
			t.Fatal("ordinary pane became durable")
		}
	}
}

func TestDurablePollStatesAndNotifications(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	conv := target.durable.Conversation
	events := api.NewBroadcaster()
	_, stream := events.Subscribe()
	model.events = events

	host.setRun(conv, "tools", "")
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	info, connected := model.durablePaneInfo(target)
	if !connected || info.exec != continuous.StateRunning || !model.paneBusy(target) {
		t.Fatalf("running state = %+v connected %v", info, connected)
	}

	host.setRun(conv, "done", "completed")
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	if model.paneBusy(target) {
		t.Fatal("finished conversation still busy")
	}
	if !strings.Contains(model.status, "finished") {
		t.Fatalf("status = %q, want a finished notice", model.status)
	}
	found := false
	for len(stream) > 0 {
		event := <-stream
		if event.Event == api.EventContinuousState {
			found = true
		}
	}
	if !found {
		t.Fatal("no continuous.state_changed event published")
	}

	host.setApproval(conv)
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	if info, _ := model.durablePaneInfo(target); info.exec != continuous.StateApproval {
		t.Fatalf("state = %q, want approval", info.exec)
	}
	if label := model.durableSidebarState(target); !strings.Contains(label, "approve") {
		t.Fatalf("sidebar label = %q", label)
	}
}

func TestDurableFirstObservationDoesNotNotify(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	model.status = ""
	host.setRun(target.durable.Conversation, "done", "failed")
	fresh := model
	fresh.durState = map[string]*durableInfo{}
	fresh.hostState = map[string]*hostInfo{}
	fresh.durablePolling = false
	fresh = drive(t, fresh, fresh.pollDurable())
	if fresh.status != "" {
		t.Fatalf("first observation notified: %q", fresh.status)
	}
}

func TestDurableDisconnectNeverReadsAsIdle(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	host.setRun(target.durable.Conversation, "tools", "")
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())

	host.SetDown(true)
	model.cpool.Drop("ext")
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	if model.paneBusy(target) {
		t.Fatal("disconnected durable pane reported busy")
	}
	if label := model.durableSidebarState(target); !strings.Contains(label, "offline") {
		t.Fatalf("sidebar label = %q, want offline", label)
	}
	status := model.durableStatus(*target.durable)
	if status.Connection != "disconnected" || status.Execution != continuous.StateRunning {
		t.Fatalf("api status = %+v, want disconnected with last observed running", status)
	}
	if !strings.Contains(model.status, "disconnected") {
		t.Fatalf("status = %q, want disconnect notice", model.status)
	}
}

func TestDurableCloseViewKeepsAssociation(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	model.removePane(owner, target)
	if len(owner.durables) != 1 {
		t.Fatal("closing the view dropped the conversation association")
	}
	if len(host.Requests("conversation.abort")) != 0 {
		t.Fatal("closing a view aborted host work")
	}
}

func TestDurableAPISubmitRequiresRequestID(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	answer := apiCall(t, &model, api.MethodContinuousSubmit, api.ContinuousSubmit{Profile: "ext", Conversation: "c", Content: "hi"})
	if answer.Code != api.CodeInvalidParams || !strings.Contains(answer.Err, "request_id") {
		t.Fatalf("answer = %+v", answer)
	}
	answer = apiCall(t, &model, api.MethodContinuousSubmit, api.ContinuousSubmit{Profile: "ext", Conversation: "c", Content: "hi", RequestID: "r", Policy: "later"})
	if answer.Code != api.CodeInvalidParams {
		t.Fatalf("bad policy answer = %+v", answer)
	}
	answer = apiCall(t, &model, api.MethodContinuousSnapshot, api.ContinuousTarget{Profile: "missing", Conversation: "c"})
	if answer.Code != api.CodeNotFound {
		t.Fatalf("unknown profile answer = %+v", answer)
	}
}

func TestDurableAPISubmitForwardsRequestID(t *testing.T) {
	host := newDurableHost()
	host.On("conversation.submit", func(params json.RawMessage) (any, string, string) {
		return map[string]any{"id": "sub-1", "state": "queued"}, "", ""
	})
	model := newDurableModel(t, host)
	reply := make(chan api.Reply, 1)
	cmd := model.handleAPI(api.Request{Method: api.MethodContinuousSubmit, Reply: reply,
		Payload: api.ContinuousSubmit{Profile: "ext", Conversation: "conv-a", Content: "do it", RequestID: "req-42", Policy: "steer"}})
	model = drive(t, model, cmd)
	answer := <-reply
	if answer.Err != "" {
		t.Fatalf("submit error: %s", answer.Err)
	}
	requests := host.Requests("conversation.submit")
	if len(requests) != 1 {
		t.Fatalf("submits = %d, want 1", len(requests))
	}
	var sent map[string]any
	_ = json.Unmarshal(requests[0].Params, &sent)
	if sent["request_id"] != "req-42" || sent["policy"] != "steer" || sent["id"] != "conv-a" {
		t.Fatalf("sent = %v", sent)
	}
}

func TestDurableAPIDecideReportsStaleApproval(t *testing.T) {
	host := newDurableHost()
	host.On("approval.decide", func(json.RawMessage) (any, string, string) {
		return nil, "conflict", "approval already decided"
	})
	model := newDurableModel(t, host)
	reply := make(chan api.Reply, 1)
	cmd := model.handleAPI(api.Request{Method: api.MethodContinuousDecide, Reply: reply,
		Payload: api.ContinuousDecide{Profile: "ext", Approval: "ap-1", Allow: true}})
	drive(t, model, cmd)
	answer := <-reply
	if answer.Code != "conflict" {
		t.Fatalf("answer = %+v, want conflict", answer)
	}
}

func TestDurableAPIAttachAnswersAfterBinding(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	reply := make(chan api.Reply, 1)
	cmd := model.handleAPI(api.Request{Method: api.MethodContinuousAttach, Reply: reply,
		Payload: api.ContinuousAttach{Workspace: "api", Split: "tab"}})
	select {
	case answer := <-reply:
		t.Fatalf("attach answered before binding: %+v", answer)
	default:
	}
	model = drive(t, model, cmd)
	answer := <-reply
	if answer.Err != "" {
		t.Fatalf("attach error: %s", answer.Err)
	}
	data := answer.Data.(map[string]any)
	status := data["durable"].(*api.DurableStatus)
	if status.Conversation != "conv-a" || status.Profile != "ext" {
		t.Fatalf("attach status = %+v", status)
	}
	full := model.apiStatus()
	if len(full.Workspaces[0].Continuous) != 1 || full.Workspaces[0].Continuous[0].Views != 1 {
		t.Fatalf("workspace continuous status = %+v", full.Workspaces[0].Continuous)
	}
	var durableCount int
	for _, currentTab := range full.Workspaces[0].Tabs {
		for _, currentPane := range currentTab.Panes {
			if currentPane.Mode == "durable" {
				durableCount++
			} else if currentPane.Durable != nil {
				t.Fatal("ordinary pane carries durable status")
			}
		}
	}
	if durableCount != 1 {
		t.Fatalf("durable panes in status = %d", durableCount)
	}
}

func TestKindPickerOffersDurableLast(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	model.openKindPicker("tab", model.spaces[0], "", rect{x: 1, y: 1})
	items := model.pickItems
	if len(items) < 2 || items[len(items)-1].action != "durable-pick" || items[len(items)-2].action != "kind:shell" {
		t.Fatalf("picker = %+v", items)
	}
	want := 0
	for index, item := range items {
		if item.action == "kind:"+model.config.DefaultAgent {
			want = index
		}
	}
	if model.menuIndex != want || items[model.menuIndex].action == "durable-pick" {
		t.Fatal("durable entry changed the preselected entry")
	}
	plain := newTestModel("/tmp/api")
	plain.openKindPicker("tab", plain.spaces[0], "", rect{x: 1, y: 1})
	for _, item := range plain.pickItems {
		if item.action == "durable-pick" {
			t.Fatal("durable offered without a state directory")
		}
	}
}

func TestSanitizeLineStripsControls(t *testing.T) {
	if got := sanitizeLine("a\x1b[31mb\nc\u009bd", 0); got != "a[31mb cd" {
		t.Fatalf("sanitized = %q", got)
	}
	if got := sanitizeLine("abcdef", 3); got != "abc..." {
		t.Fatalf("truncated = %q", got)
	}
}

func TestDurableMutedCategorySilencesButKeepsAttention(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, nil, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	model.durableMuted["approval"] = true
	model.focusPane(owner, owner.tabs[0].panes[0]) // the ordinary pane, so the view is unfocused
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	model.status = ""
	host.setApproval(target.durable.Conversation)
	model.durablePolling = false
	model = drive(t, model, model.pollDurable())
	if model.status != "" {
		t.Fatalf("muted category notified: %q", model.status)
	}
	if !model.paneAttention[target.id] {
		t.Fatal("muting dropped the attention marker")
	}
	saved := model.snapshot()
	if len(saved.DurableMuted) != 1 || saved.DurableMuted[0] != "approval" {
		t.Fatalf("muted categories not persisted: %v", saved.DurableMuted)
	}
	restored := New(Config{Shell: "/bin/sh"}, nil, "", saved)
	if !restored.durableMuted["approval"] {
		t.Fatal("muted category not restored")
	}
}

func TestDurableSettingsTabBeforePlugins(t *testing.T) {
	host := newDurableHost()
	model := newDurableModel(t, host)
	tabs := model.settingsTabs()
	if tabs[len(tabs)-1] != "durable" {
		t.Fatalf("tabs = %v", tabs)
	}
	model.settingsTab = len(tabs) - 1
	rows := model.settingsRows()
	if len(rows) < len(durableCategories)+2 || rows[0].action != "durable-notify:finished" {
		t.Fatalf("durable rows = %+v", rows)
	}
	for _, row := range rows {
		if row.action == "durable-host:ext" {
			model.toggleSettingsRow(row)
		}
	}
	if model.spaces[0].host != "ext" {
		t.Fatal("selecting a host row did not set the workspace host")
	}
	if plain := newTestModel("/tmp/api"); slices.Contains(plain.settingsTabs(), "durable") {
		t.Fatal("durable tab shown without a state directory")
	}
}

func TestDurableAPITasksBudgetsConfigure(t *testing.T) {
	host := newDurableHost()
	host.On("task.list", func(json.RawMessage) (any, string, string) {
		return map[string]any{"tasks": []map[string]any{{"id": "t1", "state": "running", "kind": "swarm"}}}, "", ""
	})
	host.On("budget.set", func(params json.RawMessage) (any, string, string) {
		return map[string]any{"scope": "conversation", "revision": 2}, "", ""
	})
	model := newDurableModel(t, host)
	call := func(method string, payload any) api.Reply {
		reply := make(chan api.Reply, 1)
		model = drive(t, model, model.handleAPI(api.Request{Method: method, Payload: payload, Reply: reply}))
		return <-reply
	}
	answer := call(api.MethodContinuousTasks, api.ContinuousTarget{Profile: "ext", Conversation: "c"})
	if answer.Err != "" || len(answer.Data.(map[string]any)["tasks"].([]continuous.Task)) != 1 {
		t.Fatalf("tasks = %+v", answer)
	}
	answer = call(api.MethodContinuousBudgetSet, api.ContinuousBudgetSet{Profile: "ext", Scope: "conversation"})
	if answer.Code != api.CodeInvalidParams {
		t.Fatalf("conversation budget without conversation = %+v", answer)
	}
	answer = call(api.MethodContinuousBudgetSet, api.ContinuousBudgetSet{Profile: "ext", Scope: "runtime", LimitUSD: -1})
	if answer.Code != api.CodeInvalidParams {
		t.Fatalf("negative budget = %+v", answer)
	}
	answer = call(api.MethodContinuousBudgetSet, api.ContinuousBudgetSet{Profile: "ext", Scope: "runtime", LimitUSD: 5, WindowSeconds: 60, ExpectedRevision: 1})
	if answer.Err != "" {
		t.Fatalf("budget set = %+v", answer)
	}
	var sent struct {
		Budget           continuous.Budget `json:"budget"`
		ExpectedRevision uint64            `json:"expected_revision"`
	}
	_ = json.Unmarshal(host.Requests("budget.set")[0].Params, &sent)
	if sent.Budget.LimitUSD != 5 || sent.Budget.Window != int64(60*time.Second) || sent.ExpectedRevision != 1 {
		t.Fatalf("budget sent = %+v", sent)
	}
	answer = call(api.MethodContinuousConfigure, api.ContinuousConfigure{Profile: "ext", Conversation: "c"})
	if answer.Code != api.CodeInvalidParams {
		t.Fatalf("empty configure = %+v", answer)
	}
}

func TestSplitPromptFilesAndReadAttachments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("synthetic notes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	text, paths := splitPromptFiles(dir, "review @notes.txt mail me@example.com @sub @@literal")
	if text != "review mail me@example.com @sub @literal" || len(paths) != 1 || paths[0] != "notes.txt" {
		t.Fatalf("split = %q %v", text, paths)
	}
	files, err := readAttachments(dir, paths)
	if err != nil || len(files) != 1 || files[0].Name != "notes.txt" || string(files[0].Data) != "synthetic notes" {
		t.Fatalf("files = %+v %v", files, err)
	}
	if _, err := readAttachments(dir, []string{"sub"}); err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory attachment err = %v", err)
	}
	if _, err := readAttachments(dir, []string{"missing.txt"}); err == nil || strings.Contains(err.Error(), dir) {
		t.Fatalf("missing attachment err = %v (must not echo the resolved path)", err)
	}
	big := filepath.Join(dir, "big.bin")
	if err := os.WriteFile(big, make([]byte, continuous.MaxAttachmentBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttachments(dir, []string{"big.bin"}); err == nil {
		t.Fatal("oversized attachment accepted")
	}
}

func TestDurableAPISubmitFilesChecksCapability(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := newDurableHost()
	host.On("conversation.submit", func(json.RawMessage) (any, string, string) {
		return map[string]any{"id": "s", "state": "queued"}, "", ""
	})
	model := newDurableModel(t, host)
	model.spaces[0].cwd = dir
	submit := func() api.Reply {
		reply := make(chan api.Reply, 1)
		model = drive(t, model, model.handleAPI(api.Request{Method: api.MethodContinuousSubmit, Reply: reply,
			Payload: api.ContinuousSubmit{Profile: "ext", Conversation: "c", RequestID: "r", Files: []string{"a.txt"}}}))
		return <-reply
	}
	if answer := submit(); answer.Code != "unsupported" {
		t.Fatalf("host without attachments capability = %+v", answer)
	}
	if len(host.Requests("conversation.submit")) != 0 {
		t.Fatal("files were sent to a host without the capability")
	}
	host.On("runtime.status", func(json.RawMessage) (any, string, string) {
		return map[string]any{"capabilities": map[string]bool{"attachments": true}}, "", ""
	})
	if answer := submit(); answer.Err != "" {
		t.Fatalf("submit with files = %+v", answer)
	}
	var sent struct {
		Files []continuous.FileAttachment `json:"files"`
	}
	_ = json.Unmarshal(host.Requests("conversation.submit")[0].Params, &sent)
	if len(sent.Files) != 1 || sent.Files[0].Name != "a.txt" || string(sent.Files[0].Data) != "hello" {
		t.Fatalf("sent files = %+v", sent.Files)
	}
}

func stubConversationFlag(t *testing.T, supported bool) {
	t.Helper()
	previous := probeConversationFlag
	probeConversationFlag = func(string) bool { return supported }
	conversationFlagMu.Lock()
	conversationFlagSupport = map[string]bool{}
	conversationFlagMu.Unlock()
	t.Cleanup(func() {
		probeConversationFlag = previous
		conversationFlagMu.Lock()
		conversationFlagSupport = map[string]bool{}
		conversationFlagMu.Unlock()
	})
}

func TestDurableViewAttachesByConversationID(t *testing.T) {
	stubConversationFlag(t, true)
	host := newDurableHost()
	host.mu.Lock()
	host.snaps["child-1"] = map[string]any{"conversation": map[string]any{"id": "child-1", "title": "sub"}}
	host.mu.Unlock()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, &state.DurableRef{Profile: "ext", Conversation: "child-1"}, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	if target.failure != "" || !target.durableReady || target.durable.Workspace != "" {
		t.Fatalf("by-ID view = %+v failure %q", target.durable, target.failure)
	}
	if len(host.Requests("conversation.create")) != 0 {
		t.Fatal("attaching by ID opened a workspace root")
	}
	_, args, err := model.durableLaunch(target)
	if err != nil || strings.Join(args, " ") != "--continuous "+model.profiles["ext"].Address+" --continuous-conversation child-1" {
		t.Fatalf("launch args = %v %v", args, err)
	}
	if _, ok := findDurable(owner, "ext", "child-1"); !ok {
		t.Fatal("child conversation not associated with the workspace")
	}
	// The binding survives a snapshot and restore without a workspace key.
	restored := New(Config{Shell: "/bin/sh"}, nil, "", model.snapshot())
	var found bool
	for _, current := range restored.spaces[0].tabs[0].panes {
		if current.durable != nil && current.durable.Conversation == "child-1" {
			found = current.failure == "" && current.durable.Workspace == ""
		}
	}
	if !found {
		t.Fatal("by-ID binding not restored as a usable durable pane")
	}
}

func TestDurableViewByIDNeedsCapableZot(t *testing.T) {
	stubConversationFlag(t, false)
	host := newDurableHost()
	model := newDurableModel(t, host)
	owner := model.spaces[0]
	target, _ := model.addDurablePane(owner, &state.DurableRef{Profile: "ext", Conversation: "child-1"}, "right")
	model = drive(t, model, model.prepareDurable(owner, target))
	if target.durableReady || !strings.Contains(target.failure, "update zot") {
		t.Fatalf("failure = %q", target.failure)
	}
	if len(host.Requests("conversation.snapshot")) != 0 {
		t.Fatal("contacted the host although zot cannot attach by ID")
	}
}
