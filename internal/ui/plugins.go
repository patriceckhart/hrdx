package ui

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/plugin"
)

type pluginEventMsg struct{ event plugin.Event }
type pluginActivationMsg struct {
	id         string
	actionID   string
	generation string
	target     string
	pane       *pane
	tab        *tab
	space      *space
	err        error
}
type pluginResultMsg struct {
	id         string
	generation string
	result     json.RawMessage
	err        error
}
type pluginStatusText struct {
	generation, text string
	priority         int
}

// SetPlugins is called before the program starts. Processes activate lazily.
func (m *Model) SetPlugins(runtime *plugin.Runtime) {
	m.plugins = runtime
	m.pluginStates = make(map[string]plugin.Status)
	m.pluginTexts = make(map[string]pluginStatusText)
	m.pluginNotices = make(map[string]time.Time)
	m.pluginSubscriptions = make(map[string]*pluginSubscription)
	m.pluginPanes = make(map[int]pluginPaneOwner)
	m.syncPluginStates()
}

// closePluginPanes removes temporary panes whose owning generation is gone.
// Durable panes created through pane.create are never touched here.
func (m *Model) closePluginPanes() {
	for id, owner := range m.pluginPanes {
		status := m.pluginStates[owner.plugin]
		live, space := m.paneByID(id)
		if live == nil || live.floating == nil {
			delete(m.pluginPanes, id)
			continue
		}
		if status.State == "ready" && status.Generation == owner.generation && !m.quitting {
			continue
		}
		delete(m.pluginPanes, id)
		m.removePane(space, live)
		m.resizePanes(space)
	}
}

func (m *Model) syncPluginStates() {
	if m.plugins == nil {
		return
	}
	for _, status := range m.plugins.Statuses() {
		m.pluginStates[status.ID] = status
		if status.State != "ready" || m.pluginTexts[status.ID].generation != status.Generation {
			delete(m.pluginTexts, status.ID)
		}
	}
	for id, view := range m.pluginViews {
		status := m.pluginStates[view.owner]
		if status.State != "ready" || status.Generation != view.generation {
			m.closePluginView(id)
		}
	}
	m.closePluginPanes()
	if m.mode == modeMenu {
		m.menuIndex = clampInt(m.menuIndex, 0, max(0, len(m.menuItems())-1))
		m.scrollMenuIntoView()
	}
}

func waitPluginEvent(runtime *plugin.Runtime) tea.Cmd {
	if runtime == nil {
		return nil
	}
	return func() tea.Msg {
		select {
		case event := <-runtime.Events():
			return pluginEventMsg{event}
		case <-runtime.Done():
			return nil
		}
	}
}

func (m *Model) handlePluginEvent(event plugin.Event) tea.Cmd {
	m.syncPluginStates()
	var command tea.Cmd
	if event.Request != nil {
		command = m.handlePluginRequest(*event.Request)
	}
	if event.Status != nil && event.Status.Error != "" {
		current := m.pluginStates[event.Status.ID]
		if current.Generation == event.Status.Generation && current.State == event.Status.State {
			command = m.flashStatus(event.Status.ID + ": " + event.Status.Error)
		}
	}
	return tea.Batch(command, waitPluginEvent(m.plugins))
}

// pluginMenuOwner resolves the workspace a menu belongs to so activation
// markers can hide irrelevant plugin actions.
func (m Model) pluginMenuOwner() *space {
	if m.menuSpace != nil {
		return m.menuSpace
	}
	if m.menuTab != nil {
		return m.spaceByTab(m.menuTab)
	}
	if m.menuPane != nil {
		_, owner := m.paneByID(m.menuPane.id)
		return owner
	}
	return nil
}

// pluginActive reports whether a plugin's declared activation markers match
// the workspace. Results are cached per workspace path for one render cycle
// window so menus do not stat the filesystem repeatedly.
func (m Model) pluginActive(entry plugin.Registration, owner *space) bool {
	if owner == nil {
		return len(entry.Package.Manifest.Activation.Markers) == 0
	}
	return plugin.ActivatesFor(entry.Package.Manifest, owner.cwd)
}

func (m Model) pluginMenuItems(target string) []menuItem {
	if m.plugins == nil {
		return nil
	}
	if target == "sidebar" {
		target = "workspace"
	}
	owner := m.pluginMenuOwner()
	var items []menuItem
	for _, entry := range m.plugins.Registrations() {
		id := entry.Package.ID
		status := m.pluginStates[id]
		if plugin.HasGrant(entry.Approval, "ui.action.contribute") && (status.State == "approved" || status.State == "ready") && m.pluginActive(entry, owner) {
			for _, action := range entry.Package.Manifest.Contributes.Actions {
				if slices.Contains(action.Targets, target) {
					items = append(items, menuItem{action.Label + " (" + id + ")", "plugin-action:" + id + "/" + action.ID})
				}
			}
		}
		if target == "workspace" {
			items = append(items, menuItem{"Start/restart " + id, "plugin-restart:" + id}, menuItem{"Stop " + id, "plugin-stop:" + id})
		}
		if len(items) >= 64 {
			return items[:64]
		}
	}
	return items
}

func (m *Model) runPluginMenu(action string, targetPane *pane, targetTab *tab, targetSpace *space) tea.Cmd {
	if m.plugins == nil || m.quitting {
		return nil
	}
	if id, ok := strings.CutPrefix(action, "plugin-stop:"); ok {
		m.plugins.Stop(id)
		m.syncPluginStates()
		return m.flashInfo("Plugin stopped: " + id)
	}
	if id, ok := strings.CutPrefix(action, "plugin-restart:"); ok {
		runtime := m.plugins
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), plugin.CallTimeout)
			defer cancel()
			return pluginResultMsg{id: id, err: runtime.Restart(ctx, id)}
		}
	}
	if id, ok := strings.CutPrefix(action, "plugin-reload:"); ok {
		runtime := m.plugins
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), plugin.CallTimeout)
			defer cancel()
			return pluginResultMsg{id: id, err: runtime.Reload(ctx, id)}
		}
	}
	ref, ok := strings.CutPrefix(action, "plugin-action:")
	if !ok {
		return nil
	}
	id, actionID, ok := strings.Cut(ref, "/")
	if !ok {
		return nil
	}
	var owner *space
	target := "pane"
	if targetSpace != nil {
		target, owner = "workspace", targetSpace
	} else if targetTab != nil {
		target, owner = "tab", m.spaceByTab(targetTab)
	} else if targetPane != nil {
		_, owner = m.paneByID(targetPane.id)
	}
	// Captured targets must still belong to this model. No dangling selection.
	if owner == nil || !slices.Contains(m.spaces, owner) {
		return nil
	}
	for _, entry := range m.plugins.Registrations() {
		if entry.Package.ID != id || !plugin.HasGrant(entry.Approval, "ui.action.contribute") {
			continue
		}
		status := m.pluginStates[id]
		if status.State != "approved" && status.State != "ready" {
			return nil
		}
		for _, declaration := range entry.Package.Manifest.Contributes.Actions {
			if declaration.ID != actionID || !slices.Contains(declaration.Targets, target) {
				continue
			}
			m.status = id + ": activating plugin action"
			m.statusIsInfo = true
			m.statusSeq++
			runtime := m.plugins
			return func() tea.Msg {
				generation, err := runtime.Activate(context.Background(), id)
				return pluginActivationMsg{id: id, actionID: actionID, generation: generation, target: target, pane: targetPane, tab: targetTab, space: owner, err: err}
			}
		}
	}
	return nil
}

func (m *Model) handlePluginActivation(message pluginActivationMsg) tea.Cmd {
	m.syncPluginStates()
	if message.err != nil {
		return m.flashStatus(message.id + ": plugin activation failed")
	}
	if message.space == nil || !slices.Contains(m.spaces, message.space) {
		return m.flashStatus(message.id + ": action target is no longer available")
	}
	if message.tab != nil && m.spaceByTab(message.tab) != message.space {
		return m.flashStatus(message.id + ": action target is no longer available")
	}
	if message.pane != nil {
		live, owner := m.paneByID(message.pane.id)
		if live != message.pane || owner != message.space {
			return m.flashStatus(message.id + ": action target is no longer available")
		}
	}
	if m.plugins == nil {
		return m.flashStatus(message.id + ": plugin action is no longer authorized")
	}
	approval, allowed := m.plugins.Authorize(plugin.Request{Plugin: message.id, Generation: message.generation, Context: context.Background()}, "ui.action.contribute")
	if !allowed {
		return m.flashStatus(message.id + ": plugin action is no longer authorized")
	}
	params := map[string]any{"action_id": message.actionID, "target": message.target}
	inScope := plugin.AllowsWorkspace(approval, message.space.cwd)
	if inScope && plugin.HasGrant(approval, "workspace.read") {
		params["workspace"] = message.space.cwd
	}
	if inScope && message.pane != nil && plugin.HasGrant(approval, "pane.read_metadata") {
		params["pane_id"] = message.pane.id
	}
	runtime := m.plugins
	return func() tea.Msg {
		result, err := runtime.CallGeneration(context.Background(), message.id, message.generation, "command.invoke", params)
		return pluginResultMsg{id: message.id, generation: message.generation, result: result, err: err}
	}
}

func (m *Model) handlePluginResult(message pluginResultMsg) tea.Cmd {
	m.syncPluginStates()
	if message.err != nil {
		return m.flashStatus(message.id + ": plugin operation failed")
	}
	var result struct {
		Notification string `json:"notification"`
	}
	if len(message.result) == 0 {
		return nil
	}
	if json.Unmarshal(message.result, &result) != nil || !plugin.SafeText(result.Notification, 240) || m.plugins == nil {
		return nil
	}
	if _, allowed := m.plugins.Authorize(plugin.Request{Plugin: message.id, Generation: message.generation, Context: context.Background()}, "ui.notification"); !allowed {
		return nil
	}
	for _, entry := range m.plugins.Registrations() {
		if entry.Package.ID == message.id && plugin.HasGrant(entry.Approval, "ui.notification") {
			return m.pluginNotice(message.id, result.Notification)
		}
	}
	return nil
}

func (m *Model) pluginNotice(id, text string) tea.Cmd {
	if time.Since(m.pluginNotices[id]) < time.Second {
		return nil
	}
	m.pluginNotices[id] = time.Now()
	return m.flashInfo(id + ": " + text)
}

// pluginNoticeError shares the rate limit and clears with the normal footer
// expiry. Errors never linger longer than host errors.
func (m *Model) pluginNoticeError(id, text string) tea.Cmd {
	if time.Since(m.pluginNotices[id]) < time.Second {
		return nil
	}
	m.pluginNotices[id] = time.Now()
	return m.flashStatus(id + ": " + text)
}

// pluginFooter orders contributions by priority, then ID, and lets the
// footer renderer clip by display width.
func (m Model) pluginFooter() string {
	var ids []string
	for id := range m.pluginTexts {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int {
		if pa, pb := m.pluginTexts[a].priority, m.pluginTexts[b].priority; pa != pb {
			return pb - pa
		}
		return strings.Compare(a, b)
	})
	var texts []string
	for _, id := range ids {
		texts = append(texts, id+": "+m.pluginTexts[id].text)
	}
	return strings.Join(texts, "  ")
}

func (m *Model) handlePluginRequest(request plugin.Request) tea.Cmd {
	reply := func(result any, code, message string) {
		response := plugin.Reply{Result: result}
		if code != "" {
			response.Error = &plugin.Problem{Code: code, Message: message}
		}
		select {
		case request.Reply <- response:
		default:
		}
	}
	deny := func() tea.Cmd { reply(nil, "denied", "operation or scope is not granted"); return nil }
	invalid := func() tea.Cmd { reply(nil, "invalid_params", "invalid method parameters"); return nil }
	capabilities := map[string]string{
		"ui.notify": "ui.notification", "ui.status": "ui.status.contribute",
		"events.subscribe": "host.events.subscribe", "events.unsubscribe": "host.events.subscribe",
		"ui.view.open": "ui.view.contribute", "ui.view.update": "ui.view.contribute", "ui.view.close": "ui.view.contribute",
		"status": "workspace.read", "group.list": "workspace.read", "workspace.move": "workspace.move", "pane.read": "pane.read_screen",
		"pane.close": "pane.close", "pane.create": "pane.create", "pane.send_text": "pane.send_input", "workspace.create": "workspace.create", "workspace.close": "workspace.close",
	}
	capability, supported := capabilities[request.Method]
	if !supported {
		reply(nil, "unknown_method", "unsupported plugin method")
		return nil
	}
	if m.plugins == nil || m.quitting {
		return deny()
	}
	approval, allowed := m.plugins.Authorize(request, capability)
	if !allowed {
		return deny()
	}
	decode := func(target any) bool {
		return len(request.Params) > 0 && string(request.Params) != "null" && json.Unmarshal(request.Params, target) == nil
	}
	scope := func(owner *space) bool { return owner != nil && plugin.AllowsWorkspace(approval, owner.cwd) }
	workspace := func(path string) *space {
		for _, owner := range m.spaces {
			if owner.cwd == path {
				return owner
			}
		}
		return nil
	}
	var payload any
	switch request.Method {
	case "ui.view.open", "ui.view.update", "ui.view.close":
		response := m.handlePluginView(request)
		select {
		case request.Reply <- response:
		default:
		}
		return nil
	case "ui.notify", "ui.status":
		var params struct {
			Text     string `json:"text"`
			Severity string `json:"severity"` // info (default), error
			Priority int    `json:"priority"` // status ordering, higher first
		}
		if !decode(&params) {
			return invalid()
		}
		clearing := request.Method == "ui.status" && params.Text == ""
		if (!clearing && !plugin.SafeText(params.Text, 240)) || (params.Severity != "" && params.Severity != "info" && params.Severity != "error") || params.Priority < 0 || params.Priority > 100 {
			return invalid()
		}
		reply(map[string]bool{"ok": true}, "", "")
		if request.Method == "ui.status" {
			// Keep the latest value for the next render, including the final
			// transition in a burst. Transport limits bound update volume.
			// An empty text clears the contribution.
			if params.Text == "" {
				delete(m.pluginTexts, request.Plugin)
				return nil
			}
			m.pluginTexts[request.Plugin] = pluginStatusText{generation: request.Generation, text: params.Text, priority: params.Priority}
			return nil
		}
		if params.Severity == "error" {
			return m.pluginNoticeError(request.Plugin, params.Text)
		}
		return m.pluginNotice(request.Plugin, params.Text)
	case "events.subscribe":
		var params struct {
			Events []string `json:"events"`
		}
		if !decode(&params) || len(params.Events) != 1 || params.Events[0] != "snapshot.changed" {
			return invalid()
		}
		if !plugin.HasGrant(approval, "workspace.read") {
			return deny()
		}
		m.pluginSubscriptions[request.Plugin] = &pluginSubscription{generation: request.Generation}
		reply(map[string]any{"sequence": 0, "snapshot": m.pluginSnapshot(approval)}, "", "")
		if !m.pluginTicking {
			m.pluginTicking = true
			return pluginTick()
		}
		return nil
	case "events.unsubscribe":
		delete(m.pluginSubscriptions, request.Plugin)
		reply(map[string]bool{"ok": true}, "", "")
		return nil
	case "group.list":
		reply(groupsForStatus(m.pluginSnapshot(approval)), "", "")
		return nil
	case "status":
		reply(m.pluginSnapshot(approval), "", "")
		return nil
	case "pane.read", "pane.close":
		var params api.PaneRef
		if !decode(&params) {
			return invalid()
		}
		_, owner := m.paneByID(params.Pane)
		if !scope(owner) {
			return deny()
		}
		payload = params
	case "pane.send_text":
		var params api.PaneSendText
		if !decode(&params) || len(params.Text) > plugin.MaxInputBytes || strings.ContainsRune(params.Text, 0) {
			return invalid()
		}
		target, owner := m.paneByID(params.Pane)
		if !scope(owner) {
			return deny()
		}
		if target.term == nil || !target.running {
			reply(nil, "unavailable", "pane is not running")
			return nil
		}
		text := params.Text
		if params.Enter {
			text += "\r"
		}
		// Bounded and ordered behind keyboard input. A stalled child never
		// blocks the UI loop, and a runaway peer gets busy instead of growth.
		if !target.term.TryWrite([]byte(text)) {
			reply(nil, "busy", "pane input queue is full")
			return nil
		}
		reply(map[string]any{"ok": true, "queued": target.term.QueuedInput()}, "", "")
		return nil
	case "pane.create":
		var params api.PaneCreate
		if !decode(&params) {
			return invalid()
		}
		owner := workspace(params.Workspace)
		if !scope(owner) {
			return deny()
		}
		// Splits and tabs become durable host-owned panes. Floats are
		// temporary, owned by this connection generation, and closed with it.
		params.Split = strings.ToLower(strings.TrimSpace(params.Split))
		if params.Split != "" && params.Split != "right" && params.Split != "down" && params.Split != "tab" && params.Split != "float" {
			return invalid()
		}
		kind, err := m.apiResolveKind(params.Kind)
		if err != nil {
			return invalid()
		}
		params.Kind = kind
		temporary := params.Split == "float"
		if temporary {
			if params.WidthPct == nil || params.HeightPct == nil || *params.WidthPct < 10 || *params.WidthPct > 100 || *params.HeightPct < 10 || *params.HeightPct > 100 {
				return invalid()
			}
			anchor := strings.ToLower(strings.TrimSpace(params.Anchor))
			if anchor != "" && anchor != "center" && anchor != "top" && anchor != "bottom" && anchor != "left" && anchor != "right" {
				return invalid()
			}
			if len(m.pluginPanes) >= 4 {
				reply(nil, "busy", "at most four temporary plugin panes may exist")
				return nil
			}
		}
		for index, current := range m.spaces {
			if current == owner {
				m.selected = index
			}
		}
		params.Workspace = ""
		if temporary {
			answer := make(chan api.Reply, 1)
			command := m.handleAPI(api.Request{Method: request.Method, Payload: params, Reply: answer})
			select {
			case response := <-answer:
				if response.Err != "" {
					reply(nil, api.CodeError, "host operation failed")
					return command
				}
				if created, ok := response.Data.(map[string]any); ok {
					if id, ok := created["pane_id"].(int); ok {
						m.pluginPanes[id] = pluginPaneOwner{plugin: request.Plugin, generation: request.Generation}
					}
				}
				reply(response.Data, "", "")
			default:
				reply(nil, "error", "host operation did not answer")
			}
			return command
		}
		payload = params
	case "workspace.move":
		var params api.WorkspaceMove
		if !decode(&params) {
			return invalid()
		}
		if !scope(workspace(params.Workspace)) {
			return deny()
		}
		payload = params
	case "workspace.close":
		var params api.WorkspaceRef
		if !decode(&params) {
			return invalid()
		}
		owner := workspace(params.Workspace)
		if !scope(owner) {
			return deny()
		}
		for index, current := range m.spaces {
			if current == owner {
				m.selected = index
			}
		}
		m.closeCurrentSpace()
		m.publish(api.Event{Event: api.EventWorkspaceClosed, Data: api.WorkspaceEvent{Workspace: owner.name, Path: owner.cwd, GroupPath: append([]string(nil), owner.groupPath...)}})
		reply(map[string]bool{"ok": true}, "", "")
		return nil
	case "workspace.create":
		var params api.WorkspaceCreate
		if !decode(&params) || !filepath.IsAbs(params.Path) {
			return invalid()
		}
		if !plugin.AllowsWorkspace(approval, params.Path) {
			return deny()
		}
		payload = params
	}
	answer := make(chan api.Reply, 1)
	command := m.handleAPI(api.Request{Method: request.Method, Payload: payload, Reply: answer})
	select {
	case response := <-answer:
		if response.Err != "" {
			code := response.Code
			if code == "" {
				code = api.CodeError
			}
			reply(nil, code, "host operation failed")
		} else {
			reply(response.Data, "", "")
		}
	default:
		reply(nil, "error", "host operation did not answer")
	}
	return command
}
