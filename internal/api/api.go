// Package api exposes an external control API on a unix domain socket
// next to the state file. The protocol is newline-delimited JSON: one
// request per line, answered by one response line with the same id.
//
//	{"id": "req_1", "method": "status", "params": {}}
//	{"id": "req_1", "result": {...}}
//	{"id": "req_2", "error": {"code": "not_found", "message": "..."}}
//
// Event subscriptions keep the connection open after the initial
// response; later lines are pushed events.
//
// The server never touches UI state directly: every request is
// forwarded into the TUI's update loop as a message and answered over a
// reply channel, so the API sees the same single-threaded model the
// keyboard does.
package api

import "sync"

// Request is delivered to the TUI as a Bubble Tea message. Reply must
// receive exactly one Reply; the channel is buffered so the UI never
// blocks on a vanished client.
type Request struct {
	Method  string
	Payload any
	Reply   chan Reply
}

// Reply carries the answer for one Request. Code classifies errors
// ("not_found", "invalid_params", ...); empty Err means success.
type Reply struct {
	Data any
	Err  string
	Code string
}

// Error codes used on the wire.
const (
	CodeError         = "error"
	CodeNotFound      = "not_found"
	CodeInvalidParams = "invalid_params"
	CodeUnknownMethod = "unknown_method"
	CodeTimeout       = "timeout"
)

// WorkspaceCreate opens a directory as a new workspace.
type WorkspaceCreate struct {
	GroupFromGit bool     `json:"group_from_git,omitempty"`
	GroupPath    []string `json:"group_path"`
	Path         string   `json:"path"`
	Agent        string   `json:"agent,omitempty"` // agent kind or "shell"; empty: default agent
}

// WorkspaceRef targets a workspace by name or path.
type WorkspaceRef struct {
	Workspace string `json:"workspace"`
}

type WorkspaceMove struct {
	Workspace string   `json:"workspace"`
	GroupPath []string `json:"group_path"`
}

type GroupStatus struct {
	GroupPath []string `json:"group_path"`
}

type Groups struct {
	Type   string        `json:"type"`
	Groups []GroupStatus `json:"groups"`
}

// PaneCreate adds a pane to a workspace.
type PaneCreate struct {
	Workspace string `json:"workspace,omitempty"`  // name or path; empty: selected workspace
	Kind      string `json:"kind,omitempty"`       // agent kind or "shell"; empty: default agent
	Split     string `json:"split,omitempty"`      // "right" (default), "down", "tab", "float"
	Anchor    string `json:"anchor,omitempty"`     // float only: center (default), top, bottom, left, right
	WidthPct  *int   `json:"width_pct,omitempty"`  // required for float, percentage of the pane area
	HeightPct *int   `json:"height_pct,omitempty"` // required for float, percentage of the pane area
}

// PaneRef targets a pane by id.
type PaneRef struct {
	Pane int `json:"pane_id"`
}

// PaneSendText types text into a pane.
type PaneSendText struct {
	Pane  int    `json:"pane_id"`
	Text  string `json:"text"`
	Enter bool   `json:"enter,omitempty"` // append a carriage return
}

// AgentWait blocks until a pane's agent reaches a state.
type AgentWait struct {
	Pane      int    `json:"pane_id"`
	Until     string `json:"until"` // "idle" or "busy"
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// MenuRegister adds an ephemeral entry to one context menu. Registrations
// live until hrdx exits; registering an existing action id replaces it.
type MenuRegister struct {
	Target   string `json:"target"` // "pane", "tab", or "sidebar"
	Label    string `json:"label"`
	ActionID string `json:"action_id"`
}

// PluginControl manages an already approved plugin in the experimental runtime.
// The trusted control socket does not create approvals or grant capabilities.
type PluginControl struct {
	Plugin string `json:"plugin"`
	Action string `json:"action"` // start, stop, restart, reload
}

// PluginStatus is the public lifecycle representation, not a live registration.
type PluginStatus struct {
	ID              string         `json:"id"`
	Name            string         `json:"name,omitempty"`
	Version         string         `json:"version,omitempty"`
	Protocol        int            `json:"protocol,omitempty"`
	PackagePath     string         `json:"package_path,omitempty"`
	RequestedGrants []string       `json:"requested_grants,omitempty"`
	GrantedGrants   []string       `json:"granted_grants,omitempty"`
	WorkspaceScopes []string       `json:"workspace_scopes,omitempty"`
	InstanceScope   bool           `json:"instance_scope,omitempty"`
	Actions         []string       `json:"actions,omitempty"`
	Views           []string       `json:"views,omitempty"`
	Providers       []string       `json:"providers,omitempty"`
	Markers         []string       `json:"activation_markers,omitempty"`
	Config          map[string]any `json:"config,omitempty"`
	State           string         `json:"state"`
	Generation      string         `json:"generation,omitempty"`
	Error           string         `json:"error,omitempty"`
}

type PluginStatuses struct {
	Type    string         `json:"type"`
	Plugins []PluginStatus `json:"plugins"`
}

// PaneStatus describes one pane in a status reply.
type PaneStatus struct {
	ID        int    `json:"pane_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Running   bool   `json:"running"`
	Busy      bool   `json:"busy"`
	Failure   string `json:"failure,omitempty"`
	Floating  bool   `json:"floating,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
	WidthPct  int    `json:"width_pct,omitempty"`
	HeightPct int    `json:"height_pct,omitempty"`
}

// TabStatus describes one tab in a status reply.
type TabStatus struct {
	Name   string       `json:"name,omitempty"`
	Active bool         `json:"active"`
	Panes  []PaneStatus `json:"panes"`
}

// WorkspaceStatus describes one workspace in a status reply.
type WorkspaceStatus struct {
	GroupPath []string    `json:"group_path,omitempty"`
	Name      string      `json:"name"`
	Path      string      `json:"path"`
	Selected  bool        `json:"selected"`
	Branch    string      `json:"branch,omitempty"`
	Tabs      []TabStatus `json:"tabs"`
}

// Status is the full state snapshot returned by the status method.
type Status struct {
	Type       string            `json:"type"`
	Version    string            `json:"version"`
	Workspaces []WorkspaceStatus `json:"workspaces"`
}

// Event is one pushed subscription event.
type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data,omitempty"`
}

// Event names.
const (
	EventWorkspaceMoved   = "workspace.moved"
	EventWorkspaceCreated = "workspace.created"
	EventWorkspaceClosed  = "workspace.closed"
	EventPaneCreated      = "pane.created"
	EventPaneClosed       = "pane.closed"
	EventPaneBusyChanged  = "pane.busy_changed"
	EventMenuAction       = "menu.action"
)

// PaneEvent is the data of pane lifecycle and busy events.
type PaneEvent struct {
	Pane      int    `json:"pane_id"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Busy      bool   `json:"busy,omitempty"`
}

// WorkspaceEvent is the data of workspace lifecycle events.
type WorkspaceEvent struct {
	GroupPath []string `json:"group_path,omitempty"`
	Workspace string   `json:"workspace"`
	Path      string   `json:"path,omitempty"`
}

// MenuActionEvent identifies a selected custom menu action and the UI
// context in which it was selected. TabIndex is present for tab and pane
// targets, including index zero.
type MenuActionEvent struct {
	ActionID  string `json:"action_id"`
	Target    string `json:"target"`
	Pane      int    `json:"pane_id,omitempty"`
	Workspace string `json:"workspace,omitempty"`
	Path      string `json:"path,omitempty"`
	TabIndex  *int   `json:"tab_index,omitempty"`
}

// Broadcaster fans events out to subscribers. Publish never blocks:
// slow subscribers drop events instead of stalling the UI.
type Broadcaster struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{subs: map[int]chan Event{}}
}

// Subscribe registers a listener. The returned channel is buffered;
// call Unsubscribe with the id when done.
func (b *Broadcaster) Subscribe() (int, <-chan Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	channel := make(chan Event, 64)
	b.subs[id] = channel
	return id, channel
}

func (b *Broadcaster) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if channel, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(channel)
	}
}

// Publish delivers an event to every subscriber without blocking.
func (b *Broadcaster) Publish(event Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, channel := range b.subs {
		select {
		case channel <- event:
		default:
		}
	}
}
