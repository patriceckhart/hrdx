package ui

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/api"
)

func apiIntPtr(value int) *int { return &value }

func apiCall(t *testing.T, model *Model, method string, payload any) api.Reply {
	t.Helper()
	reply := make(chan api.Reply, 1)
	model.handleAPI(api.Request{Method: method, Payload: payload, Reply: reply})
	select {
	case answer := <-reply:
		return answer
	default:
		t.Fatal("handleAPI did not answer")
		return api.Reply{}
	}
}

func TestAPIStatus(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	answer := apiCall(t, &model, "status", nil)
	if answer.Err != "" {
		t.Fatalf("status error: %s", answer.Err)
	}
	status, ok := answer.Data.(api.Status)
	if !ok {
		t.Fatalf("status data = %T", answer.Data)
	}
	if len(status.Workspaces) != 2 {
		t.Fatalf("workspaces = %d, want 2", len(status.Workspaces))
	}
	first := status.Workspaces[0]
	if first.Name != "api" || !first.Selected || len(first.Tabs) != 1 || len(first.Tabs[0].Panes) != 1 {
		t.Fatalf("workspace 0 = %+v", first)
	}
	if first.Tabs[0].Panes[0].Kind != "zot" {
		t.Fatalf("pane kind = %q, want zot", first.Tabs[0].Panes[0].Kind)
	}
}

func TestAPIWorkspaceCreate(t *testing.T) {
	model := newTestModel("/tmp/api")
	dir := t.TempDir()

	answer := apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: dir, Agent: "shell"})
	if answer.Err != "" {
		t.Fatalf("create error: %s", answer.Err)
	}
	if len(model.spaces) != 2 || model.selected != 1 {
		t.Fatalf("spaces = %d selected = %d, want 2/1", len(model.spaces), model.selected)
	}
	if model.spaces[1].tab().panes[0].kind != "shell" {
		t.Fatalf("pane kind = %q, want shell", model.spaces[1].tab().panes[0].kind)
	}

	// Reopening the same path is rejected.
	if answer = apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: dir}); answer.Err == "" {
		t.Fatal("duplicate create should fail")
	}
	// Bad paths are rejected.
	answer = apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: "/definitely/not/here-1"})
	if answer.Err == "" || answer.Code != api.CodeInvalidParams {
		t.Fatalf("missing dir = %q/%q, want invalid_params", answer.Err, answer.Code)
	}
	// Unknown agents are rejected.
	if answer = apiCall(t, &model, "workspace.create", api.WorkspaceCreate{Path: dir, Agent: "nope"}); answer.Err == "" {
		t.Fatal("unknown agent should fail")
	}
}

func TestAPIWorkspaceClose(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")
	answer := apiCall(t, &model, "workspace.close", api.WorkspaceRef{Workspace: "web"})
	if answer.Err != "" {
		t.Fatalf("close error: %s", answer.Err)
	}
	if len(model.spaces) != 1 || model.spaces[0].name != "api" {
		t.Fatalf("spaces after close = %+v", model.spaces)
	}
	answer = apiCall(t, &model, "workspace.close", api.WorkspaceRef{Workspace: "missing"})
	if answer.Code != api.CodeNotFound {
		t.Fatalf("close missing = %q, want not_found", answer.Code)
	}
}

func TestAPIPaneCreate(t *testing.T) {
	model := newTestModel("/tmp/api", "/tmp/web")

	answer := apiCall(t, &model, "pane.create", api.PaneCreate{Workspace: "web", Kind: "shell", Split: "down"})
	if answer.Err != "" {
		t.Fatalf("pane.create error: %s", answer.Err)
	}
	web := model.spaces[1]
	if len(web.tab().panes) != 2 || web.tab().panes[1].kind != "shell" {
		t.Fatalf("web panes = %+v", web.tab().panes)
	}
	if model.selected != 1 {
		t.Fatalf("selected = %d, want 1 (follows the target workspace)", model.selected)
	}

	if answer = apiCall(t, &model, "pane.create", api.PaneCreate{Split: "tab"}); answer.Err != "" {
		t.Fatalf("tab error: %s", answer.Err)
	}
	if len(web.tabs) != 2 {
		t.Fatalf("web tabs = %d, want 2", len(web.tabs))
	}

	answer = apiCall(t, &model, "pane.create", api.PaneCreate{
		Split: "float", Anchor: "right", WidthPct: apiIntPtr(40), HeightPct: apiIntPtr(30),
	})
	if answer.Err != "" {
		t.Fatalf("float error: %s", answer.Err)
	}
	floating := web.tab().panes[len(web.tab().panes)-1]
	if floating.floating == nil || floating.floating.anchor != "right" ||
		floating.floating.widthPct != 40 || floating.floating.heightPct != 30 {
		t.Fatalf("floating pane = %+v", floating.floating)
	}
	if web.tab().layout.Contains(floating) {
		t.Fatal("floating pane must not join the split tree")
	}
	status := model.apiStatus()
	floatingStatus := status.Workspaces[1].Tabs[1].Panes[len(status.Workspaces[1].Tabs[1].Panes)-1]
	if !floatingStatus.Floating || floatingStatus.Anchor != "right" ||
		floatingStatus.WidthPct != 40 || floatingStatus.HeightPct != 30 {
		t.Fatalf("floating status = %+v", floatingStatus)
	}

	answer = apiCall(t, &model, "pane.create", api.PaneCreate{Workspace: "missing"})
	if answer.Code != api.CodeNotFound {
		t.Fatalf("unknown workspace = %q, want not_found", answer.Code)
	}
	answer = apiCall(t, &model, "pane.create", api.PaneCreate{Split: "diagonal"})
	if answer.Code != api.CodeInvalidParams {
		t.Fatalf("unknown split = %q, want invalid_params", answer.Code)
	}
	for _, payload := range []api.PaneCreate{
		{Split: "float"},
		{Split: "float", Anchor: "corner", WidthPct: apiIntPtr(40), HeightPct: apiIntPtr(30)},
		{Split: "float", WidthPct: apiIntPtr(0), HeightPct: apiIntPtr(30)},
		{Split: "float", WidthPct: apiIntPtr(40), HeightPct: apiIntPtr(101)},
	} {
		if answer = apiCall(t, &model, "pane.create", payload); answer.Code != api.CodeInvalidParams {
			t.Fatalf("invalid float %+v = %q, want invalid_params", payload, answer.Code)
		}
	}
}

func TestAPIPaneMethodsValidation(t *testing.T) {
	model := newTestModel("/tmp/api")

	answer := apiCall(t, &model, "pane.send_text", api.PaneSendText{Pane: 999, Text: "hi"})
	if answer.Code != api.CodeNotFound {
		t.Fatalf("send to missing pane = %q, want not_found", answer.Code)
	}
	// Pane exists but has no PTY yet (startPane command never ran).
	id := model.spaces[0].tab().panes[0].id
	if answer = apiCall(t, &model, "pane.send_text", api.PaneSendText{Pane: id, Text: "hi"}); answer.Err == "" {
		t.Fatal("send to a pane without a terminal should fail")
	}
	if answer = apiCall(t, &model, "pane.read", api.PaneRef{Pane: id}); answer.Err != "" {
		t.Fatalf("read of a starting pane should answer empty, got error %s", answer.Err)
	}
	if answer = apiCall(t, &model, "pane.read", api.PaneRef{Pane: 999}); answer.Code != api.CodeNotFound {
		t.Fatalf("read of missing pane = %q, want not_found", answer.Code)
	}
	if answer = apiCall(t, &model, "pane.busy", api.PaneRef{Pane: id}); answer.Err != "" {
		t.Fatalf("pane.busy error: %s", answer.Err)
	}
	if busy, ok := answer.Data.(bool); !ok || busy {
		t.Fatalf("pane.busy = %v, want false", answer.Data)
	}
}

func TestAPIPaneClose(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.addPane(model.spaces[0], "shell", true)
	id := model.spaces[0].tab().panes[1].id

	answer := apiCall(t, &model, "pane.close", api.PaneRef{Pane: id})
	if answer.Err != "" {
		t.Fatalf("pane.close error: %s", answer.Err)
	}
	if len(model.spaces[0].tab().panes) != 1 {
		t.Fatalf("panes = %d, want 1", len(model.spaces[0].tab().panes))
	}
}

func TestAPIUnknownMethod(t *testing.T) {
	model := newTestModel("/tmp/api")
	answer := apiCall(t, &model, "explode", nil)
	if answer.Code != api.CodeUnknownMethod {
		t.Fatalf("unknown method = %q, want unknown_method", answer.Code)
	}
}

func TestAPIBusyEventsPublished(t *testing.T) {
	model := newTestModel("/tmp/api")
	events := api.NewBroadcaster()
	model.SetEventBroadcaster(events)
	_, channel := events.Subscribe()

	target := model.spaces[0].tab().panes[0]
	// Simulate busy -> idle: mark as busy first, then track while idle.
	model.wasBusy[target.id] = true
	model.trackBusy(target)

	select {
	case event := <-channel:
		if event.Event != api.EventPaneBusyChanged {
			t.Fatalf("event = %q, want pane.busy_changed", event.Event)
		}
		data, ok := event.Data.(api.PaneEvent)
		if !ok || data.Pane != target.id || data.Busy {
			t.Fatalf("event data = %+v, want idle pane %d", event.Data, target.id)
		}
	default:
		t.Fatal("no busy event published")
	}
}

func TestAPIMenuRegisterAndAction(t *testing.T) {
	model := newTestModel("/tmp/api")
	events := api.NewBroadcaster()
	model.SetEventBroadcaster(events)
	_, channel := events.Subscribe()

	registration := api.MenuRegister{Target: "pane", Label: "Run linter", ActionID: "custom.run_linter"}
	answer := apiCall(t, &model, "menu.register", registration)
	if answer.Err != "" {
		t.Fatalf("menu.register error: %s", answer.Err)
	}

	target := model.currentPane()
	model.openMenu(target, rect{x: 2, y: 1})
	items := model.menuItems()
	if got := items[len(items)-1]; got.label != registration.Label || got.action != "custom:"+registration.ActionID {
		t.Fatalf("custom menu item = %+v", got)
	}
	updated, _ := model.runMenuAction("custom:" + registration.ActionID)
	model = updated.(Model)

	select {
	case event := <-channel:
		if event.Event != api.EventMenuAction {
			t.Fatalf("event = %q, want menu.action", event.Event)
		}
		data, ok := event.Data.(api.MenuActionEvent)
		if !ok || data.ActionID != registration.ActionID || data.Target != "pane" || data.Pane != target.id ||
			data.Workspace != "api" || data.Path != "/tmp/api" || data.TabIndex == nil || *data.TabIndex != 0 {
			t.Fatalf("event data = %+v", event.Data)
		}
	default:
		t.Fatal("no menu.action event published")
	}
}

func TestAPIMenuActionContexts(t *testing.T) {
	for _, test := range []struct {
		target string
		open   func(*Model)
	}{
		{target: "tab", open: func(model *Model) {
			model.openTabMenu(model.currentSpace().tab(), rect{x: 2, y: 1})
		}},
		{target: "sidebar", open: func(model *Model) {
			model.openSpaceMenu(model.currentSpace(), rect{x: 2, y: 1})
		}},
	} {
		t.Run(test.target, func(t *testing.T) {
			model := newTestModel("/tmp/api")
			events := api.NewBroadcaster()
			model.SetEventBroadcaster(events)
			_, channel := events.Subscribe()
			registration := api.MenuRegister{Target: test.target, Label: "External action", ActionID: "custom." + test.target}
			if answer := apiCall(t, &model, "menu.register", registration); answer.Err != "" {
				t.Fatal(answer.Err)
			}
			test.open(&model)
			items := model.menuItems()
			if got := items[len(items)-1].action; got != "custom:"+registration.ActionID {
				t.Fatalf("last menu action = %q", got)
			}
			model.runMenuAction("custom:" + registration.ActionID)

			select {
			case event := <-channel:
				data, ok := event.Data.(api.MenuActionEvent)
				if event.Event != api.EventMenuAction || !ok || data.Target != test.target ||
					data.Workspace != "api" || data.Path != "/tmp/api" {
					t.Fatalf("event = %+v", event)
				}
				if test.target == "tab" && (data.TabIndex == nil || *data.TabIndex != 0) {
					t.Fatalf("tab index = %v, want 0", data.TabIndex)
				}
				if test.target == "sidebar" && data.TabIndex != nil {
					t.Fatalf("sidebar event has tab index %v", *data.TabIndex)
				}
			default:
				t.Fatal("no menu.action event published")
			}
		})
	}
}

func TestAPIMenuRegisterValidationAndReplacement(t *testing.T) {
	model := newTestModel("/tmp/api")
	for _, registration := range []api.MenuRegister{
		{Target: "workspace", Label: "Bad target", ActionID: "bad.target"},
		{Target: "pane", Label: "line\nbreak", ActionID: "bad.label"},
		{Target: "pane", Label: "Missing id"},
	} {
		answer := apiCall(t, &model, "menu.register", registration)
		if answer.Code != api.CodeInvalidParams {
			t.Fatalf("registration %+v = %q, want invalid_params", registration, answer.Code)
		}
	}

	first := api.MenuRegister{Target: "pane", Label: "First", ActionID: "same"}
	second := api.MenuRegister{Target: "tab", Label: "Second", ActionID: "same"}
	if answer := apiCall(t, &model, "menu.register", first); answer.Err != "" {
		t.Fatal(answer.Err)
	}
	if answer := apiCall(t, &model, "menu.register", second); answer.Err != "" {
		t.Fatal(answer.Err)
	}
	if len(model.customMenus) != 1 || model.customMenus[0] != second {
		t.Fatalf("registrations = %+v, want replacement %+v", model.customMenus, second)
	}
}
