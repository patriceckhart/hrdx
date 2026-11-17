package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/state"
)

// handleAPI answers one socket request on the loop. Results match the TUI's
// shapes byte for byte for every method in scope; plugins.* and
// continuous.* are answered with unknown_method.
func (m *model) handleAPI(request api.Request) {
	answer := func(data any, code, message string) {
		select {
		case request.Reply <- api.Reply{Data: data, Err: message, Code: code}:
		default:
		}
	}
	ok := func(data any) { answer(data, "", "") }
	fail := func(err error) {
		var coded apiError
		if errors.As(err, &coded) {
			answer(nil, coded.code, coded.message)
			return
		}
		answer(nil, api.CodeError, err.Error())
	}
	invalid := func() { answer(nil, api.CodeInvalidParams, "invalid payload") }

	switch request.Method {
	case "status":
		ok(m.apiStatus())

	case "group.list":
		ok(api.GroupsFor(m.apiStatus()))

	case "menu.register":
		payload, valid := request.Payload.(api.MenuRegister)
		if !valid {
			invalid()
			return
		}
		menus, err := api.RegisterMenu(m.customMenus, payload)
		if err != nil {
			answer(nil, api.CodeInvalidParams, err.Error())
			return
		}
		m.customMenus = menus
		ok(map[string]any{"type": "menu_registered", "action_id": strings.TrimSpace(payload.ActionID)})

	case "workspace.create":
		payload, valid := request.Payload.(api.WorkspaceCreate)
		if !valid {
			invalid()
			return
		}
		created, err := m.createWorkspace(payload.Path, payload.Agent, payload.GroupPath, payload.GroupFromGit)
		if err != nil {
			fail(err)
			return
		}
		ok(map[string]any{"type": "workspace_created", "workspace": created.name, "pane_id": created.tab().panes[0].id})

	case "workspace.move":
		payload, valid := request.Payload.(api.WorkspaceMove)
		if !valid || payload.GroupPath == nil {
			answer(nil, api.CodeInvalidParams, "group_path is required; use [] for standalone")
			return
		}
		if err := state.ValidateGroupPath(payload.GroupPath); err != nil {
			answer(nil, api.CodeInvalidParams, err.Error())
			return
		}
		target := m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, "workspace not found")
			return
		}
		data := m.moveWorkspace(target, payload.GroupPath)
		ok(data)

	case "workspace.close":
		payload, valid := request.Payload.(api.WorkspaceRef)
		if !valid {
			invalid()
			return
		}
		target := m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
			return
		}
		name := target.name
		_ = m.closeWorkspace(m.spaceIndex(target))
		ok(map[string]any{"type": "workspace_closed", "workspace": name})

	case "pane.create":
		payload, valid := request.Payload.(api.PaneCreate)
		if !valid {
			invalid()
			return
		}
		owner := m.currentSpace()
		if payload.Workspace != "" {
			if _, err := m.resolveKind(payload.Kind); err != nil {
				answer(nil, api.CodeInvalidParams, err.Error())
				return
			}
			owner = m.spaceByRef(payload.Workspace)
			if owner == nil {
				answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
				return
			}
		}
		created, err := m.createPane(owner, payload.Kind, payload.Split, payload.Anchor, payload.WidthPct, payload.HeightPct)
		if err != nil {
			fail(err)
			return
		}
		ok(map[string]any{"type": "pane_created", "workspace": owner.name, "pane_id": created.id})

	case "pane.send_text":
		payload, valid := request.Payload.(api.PaneSendText)
		if !valid {
			invalid()
			return
		}
		target, _, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return
		}
		if target.term == nil || !target.running {
			answer(nil, api.CodeError, fmt.Sprintf("pane %d is not running", payload.Pane))
			return
		}
		text := payload.Text
		if payload.Enter {
			text += "\r"
		}
		target.term.Write([]byte(text))
		ok(map[string]any{"type": "pane_send_text", "pane_id": payload.Pane})

	case "pane.read":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			invalid()
			return
		}
		target, _, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return
		}
		screen := ""
		if target.term != nil {
			screen = target.term.PlainScreen()
		}
		ok(map[string]any{"type": "pane_read", "pane_id": payload.Pane, "screen": screen})

	case "pane.close":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			invalid()
			return
		}
		target, owner, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return
		}
		m.focusPane(owner, target)
		_ = m.closePane(payload.Pane)
		ok(map[string]any{"type": "pane_closed", "pane_id": payload.Pane})

	case "pane.busy":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			invalid()
			return
		}
		target, _, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return
		}
		ok(m.paneBusy(target))

	default:
		answer(nil, api.CodeUnknownMethod, "unknown method "+request.Method)
	}
}

// moveWorkspace changes a workspace's group and publishes workspace.moved.
func (m *model) moveWorkspace(target *space, groupPath []string) api.WorkspaceEvent {
	target.groupPath = append([]string(nil), groupPath...)
	m.persist()
	data := api.WorkspaceEvent{Workspace: target.name, Path: target.cwd, GroupPath: append([]string(nil), target.groupPath...)}
	m.publish(api.Event{Event: api.EventWorkspaceMoved, Data: data})
	return data
}

// apiStatus builds the full status snapshot in the TUI's shape. Durable
// fields stay empty: the desktop app does not implement them.
func (m *model) apiStatus() api.Status {
	status := api.Status{Type: "status", Version: m.config.Version}
	for spaceIndex, current := range m.spaces {
		ws := api.WorkspaceStatus{
			GroupPath: append([]string(nil), current.groupPath...),
			Name:      current.name, Path: current.cwd,
			Selected: spaceIndex == m.selected, Branch: branch(current.cwd),
			ContinuousHost: current.host,
			Subrepos:       m.apiSubrepos(current.cwd),
		}
		for tabIndex, currentTab := range current.tabs {
			tabStatus := api.TabStatus{Name: currentTab.name, Active: tabIndex == current.active}
			for _, target := range currentTab.panes {
				paneStatus := api.PaneStatus{ID: target.id, Name: target.name, Kind: target.kind,
					Running: target.running, Busy: m.paneBusy(target), Failure: target.failure}
				if target.durable != nil {
					paneStatus.Mode = "durable"
				}
				if placement := target.floating; placement != nil {
					paneStatus.Floating = true
					paneStatus.Anchor = placement.anchor
					paneStatus.WidthPct = placement.widthPct
					paneStatus.HeightPct = placement.heightPct
				}
				tabStatus.Panes = append(tabStatus.Panes, paneStatus)
			}
			ws.Tabs = append(ws.Tabs, tabStatus)
		}
		status.Workspaces = append(status.Workspaces, ws)
	}
	return status
}
