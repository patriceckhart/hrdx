package ui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
)

// handleAPI executes one external API request inside the update loop and
// answers on the request's reply channel. It returns follow-up commands
// (pane starts) for methods that create panes.
func (m *Model) handleAPI(request api.Request) tea.Cmd {
	answer := func(data any, code, err string) {
		select {
		case request.Reply <- api.Reply{Data: data, Err: err, Code: code}:
		default:
		}
	}
	ok := func(data any) { answer(data, "", "") }

	switch request.Method {
	case "plugins.status", "plugins.control":
		return m.handlePluginAPI(request)
	case "status":
		ok(m.apiStatus())
		return nil

	case "menu.register":
		payload, valid := request.Payload.(api.MenuRegister)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		if err := m.registerMenu(payload); err != nil {
			answer(nil, api.CodeInvalidParams, err.Error())
			return nil
		}
		ok(map[string]any{"type": "menu_registered", "action_id": strings.TrimSpace(payload.ActionID)})
		return nil

	case "workspace.create":
		payload, valid := request.Payload.(api.WorkspaceCreate)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		return m.apiWorkspaceCreate(payload, answer)

	case "group.list":
		ok(groupsForStatus(m.apiStatus()))
		return nil

	case "workspace.move":
		payload, valid := request.Payload.(api.WorkspaceMove)
		if !valid || payload.GroupPath == nil {
			answer(nil, api.CodeInvalidParams, "group_path is required; use [] for standalone")
			return nil
		}
		if err := validateGroupPath(payload.GroupPath); err != nil {
			answer(nil, api.CodeInvalidParams, err.Error())
			return nil
		}
		target := m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, "workspace not found")
			return nil
		}
		target.groupPath = append([]string(nil), payload.GroupPath...)
		m.persist()
		data := api.WorkspaceEvent{Workspace: target.name, Path: target.cwd, GroupPath: append([]string(nil), target.groupPath...)}
		ok(data)
		m.publish(api.Event{Event: api.EventWorkspaceMoved, Data: data})
		return nil

	case "workspace.close":
		payload, valid := request.Payload.(api.WorkspaceRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target := m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
			return nil
		}
		for index, currentSpace := range m.spaces {
			if currentSpace == target {
				m.selected = index
			}
		}
		m.closeCurrentSpace()
		ok(map[string]any{"type": "workspace_closed", "workspace": target.name})
		m.publish(api.Event{Event: api.EventWorkspaceClosed,
			Data: api.WorkspaceEvent{Workspace: target.name, Path: target.cwd, GroupPath: append([]string(nil), target.groupPath...)}})
		return nil

	case "pane.create":
		payload, valid := request.Payload.(api.PaneCreate)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		return m.apiPaneCreate(payload, answer)

	case "pane.send_text":
		payload, valid := request.Payload.(api.PaneSendText)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		if target.term == nil || !target.running {
			answer(nil, api.CodeError, fmt.Sprintf("pane %d is not running", payload.Pane))
			return nil
		}
		text := payload.Text
		if payload.Enter {
			text += "\r"
		}
		target.term.Write([]byte(text))
		ok(map[string]any{"type": "pane_send_text", "pane_id": payload.Pane})
		return nil

	case "pane.read":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		screen := ""
		if target.term != nil {
			screen = target.term.PlainScreen()
		}
		ok(map[string]any{"type": "pane_read", "pane_id": payload.Pane, "screen": screen})
		return nil

	case "pane.close":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, owner := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		m.focusPane(owner, target)
		for index, currentSpace := range m.spaces {
			if currentSpace == owner {
				m.selected = index
			}
		}
		m.closeCurrentPane()
		ok(map[string]any{"type": "pane_closed", "pane_id": payload.Pane})
		m.publish(api.Event{Event: api.EventPaneClosed,
			Data: api.PaneEvent{Pane: payload.Pane, Name: target.name, Kind: target.kind, Workspace: owner.name}})
		return nil

	case "pane.busy":
		payload, valid := request.Payload.(api.PaneRef)
		if !valid {
			answer(nil, api.CodeInvalidParams, "invalid payload")
			return nil
		}
		target, _ := m.paneByID(payload.Pane)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("pane %d not found", payload.Pane))
			return nil
		}
		ok(m.paneBusy(target))
		return nil
	}

	answer(nil, api.CodeUnknownMethod, "unknown method "+request.Method)
	return nil
}

// publish sends an event to API subscribers when a broadcaster is wired.
func (m *Model) publish(event api.Event) {
	m.events.Publish(event)
}

// registerMenu validates and stores one process-local context-menu entry.
// Re-registering an action id replaces its entry without changing menu order.
func (m *Model) registerMenu(registration api.MenuRegister) error {
	registration.Target = strings.TrimSpace(registration.Target)
	registration.Label = strings.TrimSpace(registration.Label)
	registration.ActionID = strings.TrimSpace(registration.ActionID)
	if registration.Target != "pane" && registration.Target != "tab" && registration.Target != "sidebar" {
		return fmt.Errorf("target must be pane, tab, or sidebar")
	}
	if registration.Label == "" {
		return fmt.Errorf("label is required")
	}
	if len([]rune(registration.Label)) > 80 || strings.IndexFunc(registration.Label, unicode.IsControl) >= 0 {
		return fmt.Errorf("label must be at most 80 characters and contain no control characters")
	}
	if registration.ActionID == "" {
		return fmt.Errorf("action_id is required")
	}
	if len([]rune(registration.ActionID)) > 128 || strings.IndexFunc(registration.ActionID, unicode.IsControl) >= 0 {
		return fmt.Errorf("action_id must be at most 128 characters and contain no control characters")
	}
	for index, current := range m.customMenus {
		if current.ActionID == registration.ActionID {
			m.customMenus[index] = registration
			return nil
		}
	}
	if len(m.customMenus) >= 64 {
		return fmt.Errorf("at most 64 menu entries may be registered")
	}
	m.customMenus = append(m.customMenus, registration)
	return nil
}

// publishMenuAction emits the context of a selected custom menu item.
func (m *Model) publishMenuAction(actionID string, targetPane *pane, targetTab *tab, targetSpace *space) {
	data := api.MenuActionEvent{ActionID: actionID}
	switch {
	case targetSpace != nil:
		data.Target = "sidebar"
		data.Workspace = targetSpace.name
		data.Path = targetSpace.cwd
	case targetTab != nil:
		data.Target = "tab"
		if owner := m.spaceByTab(targetTab); owner != nil {
			data.Workspace = owner.name
			data.Path = owner.cwd
			for index, current := range owner.tabs {
				if current == targetTab {
					tabIndex := index
					data.TabIndex = &tabIndex
					break
				}
			}
		}
	case targetPane != nil:
		data.Target = "pane"
		data.Pane = targetPane.id
		for _, owner := range m.spaces {
			for index, currentTab := range owner.tabs {
				for _, currentPane := range currentTab.panes {
					if currentPane == targetPane {
						data.Workspace = owner.name
						data.Path = owner.cwd
						tabIndex := index
						data.TabIndex = &tabIndex
						m.publish(api.Event{Event: api.EventMenuAction, Data: data})
						return
					}
				}
			}
		}
	}
	m.publish(api.Event{Event: api.EventMenuAction, Data: data})
}

// spaceByRef finds a workspace by name or path.
func (m *Model) spaceByRef(ref string) *space {
	for _, currentSpace := range m.spaces {
		if currentSpace.cwd == ref {
			return currentSpace
		}
	}
	for _, currentSpace := range m.spaces {
		if currentSpace.name == ref {
			return currentSpace
		}
	}
	return nil
}

// apiStatus builds the full state snapshot.
func (m *Model) apiStatus() api.Status {
	status := api.Status{Type: "status", Version: m.config.Version}
	for spaceIndex, currentSpace := range m.spaces {
		ws := api.WorkspaceStatus{
			GroupPath: append([]string(nil), currentSpace.groupPath...),
			Name:      currentSpace.name,
			Path:      currentSpace.cwd,
			Selected:  spaceIndex == m.selected,
			Branch:    m.gitBranch(currentSpace.cwd).value,
		}
		for tabIndex, currentTab := range currentSpace.tabs {
			tabStatus := api.TabStatus{
				Name:   currentTab.name,
				Active: tabIndex == currentSpace.active,
			}
			for _, currentPane := range currentTab.panes {
				paneStatus := api.PaneStatus{
					ID:      currentPane.id,
					Name:    currentPane.name,
					Kind:    currentPane.kind,
					Running: currentPane.running,
					Busy:    m.paneBusy(currentPane),
					Failure: currentPane.failure,
				}
				if placement := currentPane.floating; placement != nil {
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

// apiResolveKind validates a requested pane kind, defaulting to the
// current default agent.
func (m *Model) apiResolveKind(kind string) (string, error) {
	if kind == "" {
		return m.config.DefaultAgent, nil
	}
	if kind == "shell" || isAgentKind(kind) {
		return kind, nil
	}
	return "", fmt.Errorf("unknown kind %q", kind)
}

func (m *Model) apiWorkspaceCreate(payload api.WorkspaceCreate, answer func(any, string, string)) tea.Cmd {
	if err := validateGroupPath(payload.GroupPath); err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	kind, err := m.apiResolveKind(payload.Agent)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	path, err := resolveDir(payload.Path)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	for _, currentSpace := range m.spaces {
		if currentSpace.cwd == path {
			answer(nil, api.CodeError, fmt.Sprintf("%s is already open", path))
			return nil
		}
	}
	newSpace := m.addSpaceKind(path, kind)
	newSpace.groupPath = append([]string(nil), payload.GroupPath...)
	if payload.GroupPath == nil && payload.GroupFromGit {
		newSpace.groupPath = gitGroupPath(path)
	}
	m.selected = len(m.spaces) - 1
	m.persist()
	newPane := newSpace.tab().panes[0]
	answer(map[string]any{
		"type": "workspace_created", "workspace": newSpace.name, "pane_id": newPane.id,
	}, "", "")
	m.publish(api.Event{Event: api.EventWorkspaceCreated,
		Data: api.WorkspaceEvent{Workspace: newSpace.name, Path: newSpace.cwd, GroupPath: append([]string(nil), newSpace.groupPath...)}})
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: newPane.id, Name: newPane.name, Kind: newPane.kind, Workspace: newSpace.name}})
	return m.startPane(newSpace, newPane)
}

func (m *Model) apiPaneCreate(payload api.PaneCreate, answer func(any, string, string)) tea.Cmd {
	kind, err := m.apiResolveKind(payload.Kind)
	if err != nil {
		answer(nil, api.CodeInvalidParams, err.Error())
		return nil
	}
	target := m.currentSpace()
	if payload.Workspace != "" {
		target = m.spaceByRef(payload.Workspace)
		if target == nil {
			answer(nil, api.CodeNotFound, fmt.Sprintf("workspace %q not found", payload.Workspace))
			return nil
		}
	}
	if target == nil {
		answer(nil, api.CodeError, "no workspace open")
		return nil
	}
	for index, currentSpace := range m.spaces {
		if currentSpace == target {
			m.selected = index
		}
	}

	var newPane *pane
	switch strings.ToLower(strings.TrimSpace(payload.Split)) {
	case "", "right":
		newPane = m.addPaneSide(target, kind, true, false)
	case "down":
		newPane = m.addPaneSide(target, kind, false, false)
	case "tab":
		newPane = m.addTab(target, kind)
	case "float":
		anchor := strings.ToLower(strings.TrimSpace(payload.Anchor))
		if anchor == "" {
			anchor = "center"
		}
		if anchor != "center" && anchor != "top" && anchor != "bottom" && anchor != "left" && anchor != "right" {
			answer(nil, api.CodeInvalidParams, "anchor must be center, top, bottom, left, or right")
			return nil
		}
		if payload.WidthPct == nil || payload.HeightPct == nil {
			answer(nil, api.CodeInvalidParams, "width_pct and height_pct are required for float")
			return nil
		}
		if *payload.WidthPct < 1 || *payload.WidthPct > 100 || *payload.HeightPct < 1 || *payload.HeightPct > 100 {
			answer(nil, api.CodeInvalidParams, "width_pct and height_pct must be between 1 and 100")
			return nil
		}
		newPane = m.addFloatingPane(target, kind, anchor, *payload.WidthPct, *payload.HeightPct)
	default:
		answer(nil, api.CodeInvalidParams, fmt.Sprintf("unknown split %q (right, down, tab, float)", payload.Split))
		return nil
	}
	m.resizePanes(target)
	m.persist()
	answer(map[string]any{
		"type": "pane_created", "workspace": target.name, "pane_id": newPane.id,
	}, "", "")
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: newPane.id, Name: newPane.name, Kind: newPane.kind, Workspace: target.name}})
	return m.startPane(target, newPane)
}

// StartAPIServer creates and starts the socket server; send forwards
// requests into the running Bubble Tea program (program.Send).
func StartAPIServer(socket string, send func(api.Request), events *api.Broadcaster) (*api.Server, error) {
	server := api.NewServer(socket, send, events)
	if err := server.Start(); err != nil {
		return nil, err
	}
	return server, nil
}
