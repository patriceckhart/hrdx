package core

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/paths"
	"github.com/patriceckhart/hrdx/internal/state"
)

const (
	splitDirRight = ""
	splitDirDown  = "down"
	splitDirAuto  = "auto"
)

var splitDirections = []string{splitDirRight, splitDirDown, splitDirAuto}

func validSplitDirection(value string) string {
	switch value {
	case splitDirDown, splitDirAuto:
		return value
	}
	return splitDirRight
}

// resolveKind validates a requested pane kind, defaulting to the current
// default agent.
func (m *model) resolveKind(kind string) (string, error) {
	if kind == "" {
		return m.config.DefaultAgent, nil
	}
	if kind == "shell" || agent.IsAgent(kind) {
		return kind, nil
	}
	return "", fmt.Errorf("unknown kind %q", kind)
}

func (m *model) currentSpace() *space {
	if m.selected < 0 || m.selected >= len(m.spaces) {
		return nil
	}
	return m.spaces[m.selected]
}

func (m *model) currentPane() *pane {
	current := m.currentSpace()
	if current == nil {
		return nil
	}
	currentTab := current.tab()
	if len(currentTab.panes) == 0 {
		return nil
	}
	currentTab.selected = clamp(currentTab.selected, 0, len(currentTab.panes)-1)
	return currentTab.panes[currentTab.selected]
}

func (m *model) paneByID(id int) (*pane, *space, *tab) {
	for _, current := range m.spaces {
		for _, currentTab := range current.tabs {
			for _, currentPane := range currentTab.panes {
				if currentPane.id == id {
					return currentPane, current, currentTab
				}
			}
		}
	}
	return nil, nil, nil
}

func (m *model) spaceIndex(target *space) int {
	for index, current := range m.spaces {
		if current == target {
			return index
		}
	}
	return -1
}

// spaceByRef finds a workspace by path first, then by name.
func (m *model) spaceByRef(ref string) *space {
	for _, current := range m.spaces {
		if current.cwd == ref {
			return current
		}
	}
	for _, current := range m.spaces {
		if current.name == ref {
			return current
		}
	}
	return nil
}

// openWorkspace adds a --cwd directory unless it is already open.
func (m *model) openWorkspace(path string) {
	resolved, err := paths.ResolveDir(path)
	if err != nil {
		m.problems = append(m.problems, err.Error())
		return
	}
	for _, current := range m.spaces {
		if current.cwd == resolved {
			return
		}
	}
	m.addSpace(resolved, m.config.DefaultAgent)
}

func (m *model) addSpace(path, kind string) *space {
	created := &space{name: filepath.Base(path), cwd: path, tabs: []*tab{{}}}
	m.spaces = append(m.spaces, created)
	m.addPane(created, kind, true, false)
	return created
}

func (m *model) newPane(owner *space, kind string) *pane {
	count := 1
	for _, currentTab := range owner.tabs {
		for _, existing := range currentTab.panes {
			if existing.kind == kind {
				count++
			}
		}
	}
	created := &pane{id: m.nextID, name: fmt.Sprintf("%s %d", kind, count), kind: kind}
	m.nextID++
	return created
}

// addPane splits the focused pane of the active tab. vertical places the new
// pane beside it; before places it left of or above it.
func (m *model) addPane(owner *space, kind string, vertical, before bool) *pane {
	currentTab := owner.tab()
	created := m.newPane(owner, kind)
	if currentTab.layout == nil {
		currentTab.layout = layout.Leaf(created)
	} else {
		focused := currentTab.panes[clamp(currentTab.selected, 0, len(currentTab.panes)-1)]
		if focused.floating != nil || !currentTab.layout.Contains(focused) {
			currentTab.layout.Walk(func(candidate *pane) { focused = candidate })
		}
		layout.InsertAtSide(currentTab.layout, focused, created, vertical, before)
	}
	currentTab.panes = append(currentTab.panes, created)
	currentTab.selected = len(currentTab.panes) - 1
	return created
}

func (m *model) addTab(owner *space, kind string) *pane {
	owner.tabs = append(owner.tabs, &tab{})
	owner.active = len(owner.tabs) - 1
	return m.addPane(owner, kind, true, false)
}

func (m *model) addFloat(owner *space, kind, anchor string, widthPct, heightPct int) *pane {
	currentTab := owner.tab()
	created := m.newPane(owner, kind)
	created.floating = &floatPlacement{anchor: anchor, widthPct: widthPct, heightPct: heightPct}
	currentTab.panes = append(currentTab.panes, created)
	currentTab.selected = len(currentTab.panes) - 1
	return created
}

// focusPane selects target's tab and pane. A floating pane moves to the top
// of the stacking order.
func (m *model) focusPane(owner *space, target *pane) {
	for tabIndex, currentTab := range owner.tabs {
		for index, current := range currentTab.panes {
			if current != target {
				continue
			}
			owner.active = tabIndex
			if target.floating != nil && index != len(currentTab.panes)-1 {
				currentTab.panes = append(currentTab.panes[:index], currentTab.panes[index+1:]...)
				currentTab.panes = append(currentTab.panes, target)
				currentTab.selected = len(currentTab.panes) - 1
			} else {
				currentTab.selected = index
			}
			if index := m.spaceIndex(owner); index >= 0 {
				m.selected = index
			}
			delete(m.paneAttention, target.id)
			return
		}
	}
}

// forget drops per-pane bookkeeping and closes its process.
func (m *model) forget(target *pane) {
	delete(m.wasBusy, target.id)
	delete(m.finishSeq, target.id)
	delete(m.paneAttention, target.id)
	if target.term != nil {
		target.term.Close()
	}
	if m.hooks.PaneGone != nil {
		m.hooks.PaneGone(target.id)
	}
}

// removePane closes a pane and removes it from its tab, promoting its
// sibling. The last pane of a tab takes the tab with it, unless it is the
// workspace's only tab.
func (m *model) removePane(owner *space, target *pane) {
	for _, currentTab := range owner.tabs {
		index := -1
		for i, current := range currentTab.panes {
			if current == target {
				index = i
			}
		}
		if index < 0 {
			continue
		}
		m.forget(target)
		layout.RemoveAt(&currentTab.layout, target)
		currentTab.panes = append(currentTab.panes[:index], currentTab.panes[index+1:]...)
		if currentTab.selected >= len(currentTab.panes) {
			currentTab.selected = max(0, len(currentTab.panes)-1)
		}
		if len(currentTab.panes) == 0 && len(owner.tabs) > 1 {
			m.removeTab(owner, currentTab)
		}
		m.persist()
		return
	}
}

func (m *model) removeTab(owner *space, target *tab) {
	for _, current := range target.panes {
		m.forget(current)
	}
	for index, current := range owner.tabs {
		if current == target {
			owner.tabs = append(owner.tabs[:index], owner.tabs[index+1:]...)
			break
		}
	}
	owner.active = clamp(owner.active, 0, max(0, len(owner.tabs)-1))
}

func (m *model) removeSpace(index int) {
	if index < 0 || index >= len(m.spaces) {
		return
	}
	for _, currentTab := range m.spaces[index].tabs {
		for _, current := range currentTab.panes {
			m.forget(current)
		}
	}
	m.spaces = append(m.spaces[:index], m.spaces[index+1:]...)
	if m.selected >= len(m.spaces) {
		m.selected = max(0, len(m.spaces)-1)
	} else if m.selected > index {
		m.selected--
	}
	m.persist()
}

// orderedPanes lists split panes in layout order, then floats in stacking
// order.
func orderedPanes(currentTab *tab) []*pane {
	ordered := make([]*pane, 0, len(currentTab.panes))
	currentTab.layout.Walk(func(target *pane) { ordered = append(ordered, target) })
	for _, target := range currentTab.panes {
		if target.floating != nil {
			ordered = append(ordered, target)
		}
	}
	return ordered
}

func (m *model) cyclePane(delta int) {
	current := m.currentSpace()
	if current == nil {
		return
	}
	ordered := orderedPanes(current.tab())
	focused := m.currentPane()
	for index, target := range ordered {
		if target == focused {
			m.focusPane(current, ordered[(index+delta+len(ordered))%len(ordered)])
			return
		}
	}
}

func (m *model) selectSpace(delta int) {
	if len(m.spaces) == 0 {
		return
	}
	m.selected = (m.selected + delta + len(m.spaces)) % len(m.spaces)
	delete(m.paneAttention, m.focusedID())
	m.persist()
}

func (m *model) selectTab(delta int) {
	current := m.currentSpace()
	if current == nil || len(current.tabs) == 0 {
		return
	}
	current.active = (current.active + delta + len(current.tabs)) % len(current.tabs)
	delete(m.paneAttention, m.focusedID())
	m.persist()
}

func (m *model) focusedID() int {
	if focused := m.currentPane(); focused != nil {
		return focused.id
	}
	return 0
}

func (m *model) cycleSplitDirection() {
	for index, value := range splitDirections {
		if value == m.splitDirection {
			m.splitDirection = splitDirections[(index+1)%len(splitDirections)]
			m.persist()
			return
		}
	}
	m.splitDirection = splitDirRight
	m.persist()
}

// preferredVertical resolves the default split direction. Auto splits the
// focused pane along its longer side; terminal cells are about twice as tall
// as wide, so the width counts half.
func (m *model) preferredVertical() bool {
	switch m.splitDirection {
	case splitDirDown:
		return false
	case splitDirAuto:
		if focused := m.currentPane(); focused != nil && focused.cols > 0 && focused.rows > 0 {
			return focused.cols >= 2*focused.rows
		}
	}
	return true
}

// installedAgents returns the agent kinds whose binary resolves on PATH.
func (m *model) installedAgents() map[string]bool {
	found := map[string]bool{}
	for _, spec := range agent.Specs {
		if _, err := exec.LookPath(agent.BinaryFor(spec.Kind, m.config.AgentBins)); err == nil {
			found[spec.Kind] = true
		}
	}
	return found
}

// availableAgents returns installed, enabled agents, falling back to every
// enabled kind so pickers always offer something.
func (m *model) availableAgents() []string {
	installed := m.installedAgents()
	var found []string
	for _, spec := range agent.Specs {
		if installed[spec.Kind] && !m.disabled[spec.Kind] {
			found = append(found, spec.Kind)
		}
	}
	if len(found) == 0 {
		for _, spec := range agent.Specs {
			if !m.disabled[spec.Kind] {
				found = append(found, spec.Kind)
			}
		}
	}
	if len(found) == 0 {
		found = []string{m.config.DefaultAgent}
	}
	return found
}

func (m *model) cycleAgent() {
	available := m.availableAgents()
	for index, kind := range available {
		if kind == m.config.DefaultAgent {
			m.config.DefaultAgent = available[(index+1)%len(available)]
			return
		}
	}
	m.config.DefaultAgent = available[0]
}

// createWorkspace opens path as a workspace with a first pane of kind. It
// mirrors the API's workspace.create validation.
func (m *model) createWorkspace(path, kind string, groupPath []string, groupFromGit bool) (*space, error) {
	if m.blocked != "" {
		return nil, apiError{api.CodeError, m.blocked}
	}
	if err := state.ValidateGroupPath(groupPath); err != nil {
		return nil, apiError{api.CodeInvalidParams, err.Error()}
	}
	kind, err := m.resolveKind(kind)
	if err != nil {
		return nil, apiError{api.CodeInvalidParams, err.Error()}
	}
	resolved, err := paths.ResolveDir(strings.TrimSpace(path))
	if err != nil {
		return nil, apiError{api.CodeInvalidParams, err.Error()}
	}
	for _, current := range m.spaces {
		if current.cwd == resolved {
			return nil, apiError{api.CodeError, fmt.Sprintf("%s is already open", resolved)}
		}
	}
	created := m.addSpace(resolved, kind)
	created.groupPath = append([]string(nil), groupPath...)
	if groupPath == nil && groupFromGit {
		created.groupPath = gitGroupPath(resolved)
	}
	m.selected = len(m.spaces) - 1
	m.persist()
	first := created.tab().panes[0]
	m.publish(api.Event{Event: api.EventWorkspaceCreated,
		Data: api.WorkspaceEvent{Workspace: created.name, Path: created.cwd, GroupPath: append([]string(nil), created.groupPath...)}})
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: first.id, Name: first.name, Kind: first.kind, Workspace: created.name}})
	m.startPane(created, first)
	return created, nil
}

// createPane adds a pane to owner. split is right, down, tab, or float.
func (m *model) createPane(owner *space, kind, split, anchor string, widthPct, heightPct *int) (*pane, error) {
	if m.blocked != "" {
		return nil, apiError{api.CodeError, m.blocked}
	}
	kind, err := m.resolveKind(kind)
	if err != nil {
		return nil, apiError{api.CodeInvalidParams, err.Error()}
	}
	if owner == nil {
		return nil, apiError{api.CodeError, "no workspace open"}
	}
	m.selected = m.spaceIndex(owner)
	var created *pane
	switch strings.ToLower(strings.TrimSpace(split)) {
	case "", "right":
		created = m.addPane(owner, kind, true, false)
	case "down":
		created = m.addPane(owner, kind, false, false)
	case "tab":
		created = m.addTab(owner, kind)
	case "float":
		anchor = strings.ToLower(strings.TrimSpace(anchor))
		if anchor == "" {
			anchor = "center"
		}
		switch anchor {
		case "center", "top", "bottom", "left", "right":
		default:
			return nil, apiError{api.CodeInvalidParams, "anchor must be center, top, bottom, left, or right"}
		}
		if widthPct == nil || heightPct == nil {
			return nil, apiError{api.CodeInvalidParams, "width_pct and height_pct are required for float"}
		}
		if *widthPct < 1 || *widthPct > 100 || *heightPct < 1 || *heightPct > 100 {
			return nil, apiError{api.CodeInvalidParams, "width_pct and height_pct must be between 1 and 100"}
		}
		created = m.addFloat(owner, kind, anchor, *widthPct, *heightPct)
	default:
		return nil, apiError{api.CodeInvalidParams, fmt.Sprintf("unknown split %q (right, down, tab, float)", split)}
	}
	m.persist()
	m.publish(api.Event{Event: api.EventPaneCreated,
		Data: api.PaneEvent{Pane: created.id, Name: created.name, Kind: created.kind, Workspace: owner.name}})
	m.startPane(owner, created)
	return created, nil
}

// closePane closes one pane by id and publishes pane.closed.
func (m *model) closePane(id int) error {
	target, owner, _ := m.paneByID(id)
	if target == nil {
		return apiError{api.CodeNotFound, fmt.Sprintf("pane %d not found", id)}
	}
	m.removePane(owner, target)
	m.publish(api.Event{Event: api.EventPaneClosed,
		Data: api.PaneEvent{Pane: id, Name: target.name, Kind: target.kind, Workspace: owner.name}})
	return nil
}

// closeWorkspace closes a workspace and kills its panes.
func (m *model) closeWorkspace(index int) error {
	if index < 0 || index >= len(m.spaces) {
		return apiError{api.CodeNotFound, "workspace not found"}
	}
	target := m.spaces[index]
	m.removeSpace(index)
	m.publish(api.Event{Event: api.EventWorkspaceClosed,
		Data: api.WorkspaceEvent{Workspace: target.name, Path: target.cwd, GroupPath: append([]string(nil), target.groupPath...)}})
	return nil
}

func (m *model) publish(event api.Event) {
	m.events.Publish(event)
}

// apiError carries an API error code through ordinary error returns.
type apiError struct {
	code, message string
}

func (e apiError) Error() string { return e.message }
