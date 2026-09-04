package ui

import "github.com/patriceckhart/hrdx/internal/state"

// snapshot converts the live model into its serializable form.
func (m *Model) snapshot() state.State {
	saved := state.State{
		Selected: m.selected, Sound: m.soundOn, SoundKind: m.soundKind,
		Notify: m.notifyOn, Theme: m.themeName,
		SidebarCollapsed: m.sideCollapsed, DisableAutoCopy: !m.autoCopy,
	}
	for _, spec := range agentSpecs {
		if m.disabled[spec.kind] {
			saved.DisabledAgents = append(saved.DisabledAgents, spec.kind)
		}
	}
	for _, currentSpace := range m.spaces {
		ws := state.Workspace{
			Name:   currentSpace.name,
			CWD:    currentSpace.cwd,
			Active: currentSpace.active,
		}
		for _, currentTab := range currentSpace.tabs {
			savedTab := state.Tab{Name: currentTab.name}
			index := make(map[*pane]int, len(currentTab.panes))
			for paneIndex, currentPane := range currentTab.panes {
				if currentPane.floating != nil {
					continue
				}
				index[currentPane] = len(savedTab.Panes)
				if paneIndex == currentTab.selected {
					savedTab.Selected = len(savedTab.Panes)
				}
				savedTab.Panes = append(savedTab.Panes, state.Pane{
					Kind: currentPane.kind, Name: currentPane.name, Session: currentPane.session,
				})
			}
			savedTab.Layout = snapshotNode(currentTab.layout, index)
			ws.Tabs = append(ws.Tabs, savedTab)
		}
		saved.Workspaces = append(saved.Workspaces, ws)
	}
	return saved
}

func snapshotNode(n *splitNode, index map[*pane]int) *state.Node {
	if n == nil {
		return nil
	}
	if n.pane != nil {
		if i, ok := index[n.pane]; ok {
			return &state.Node{Pane: &i}
		}
		return nil
	}
	return &state.Node{
		Vertical: n.vertical,
		Ratio:    n.ratio,
		A:        snapshotNode(n.a, index),
		B:        snapshotNode(n.b, index),
	}
}

// restore rebuilds workspaces from a saved state. Agent panes are marked to
// resume their latest session. Legacy states without tabs are upgraded to a
// single tab.
func (m *Model) restore(saved state.State) {
	for _, ws := range saved.Workspaces {
		savedTabs := ws.Tabs
		if len(savedTabs) == 0 && len(ws.Panes) > 0 {
			savedTabs = []state.Tab{{Panes: ws.Panes, Layout: ws.Layout, Selected: ws.Selected}}
		}
		if ws.CWD == "" || len(savedTabs) == 0 {
			continue
		}
		restored := &space{name: ws.Name, cwd: ws.CWD, active: ws.Active}
		for _, savedTab := range savedTabs {
			if len(savedTab.Panes) == 0 {
				continue
			}
			restoredTab := &tab{name: savedTab.Name}
			for _, savedPane := range savedTab.Panes {
				kind := savedPane.Kind
				if !isAgentKind(kind) && kind != "shell" {
					kind = "shell"
				}
				restoredTab.panes = append(restoredTab.panes, &pane{
					id:      m.nextID,
					name:    savedPane.Name,
					kind:    kind,
					resume:  isAgentKind(kind),
					session: savedPane.Session,
				})
				m.nextID++
			}
			restoredTab.layout = restoreNode(savedTab.Layout, restoredTab.panes)
			if restoredTab.layout == nil || !layoutComplete(restoredTab.layout, restoredTab.panes) {
				restoredTab.layout = fallbackLayout(restoredTab.panes)
			}
			restoredTab.selected = clampInt(savedTab.Selected, 0, len(restoredTab.panes)-1)
			restored.tabs = append(restored.tabs, restoredTab)
		}
		if len(restored.tabs) == 0 {
			continue
		}
		restored.active = clampInt(restored.active, 0, len(restored.tabs)-1)
		m.spaces = append(m.spaces, restored)
	}
	m.selected = clampInt(saved.Selected, 0, max(0, len(m.spaces)-1))
}

func restoreNode(n *state.Node, panes []*pane) *splitNode {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		if *n.Pane < 0 || *n.Pane >= len(panes) {
			return nil
		}
		return leafNode(panes[*n.Pane])
	}
	a := restoreNode(n.A, panes)
	b := restoreNode(n.B, panes)
	if a == nil || b == nil {
		return nil
	}
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	return &splitNode{vertical: n.Vertical, ratio: ratio, a: a, b: b}
}

// layoutComplete verifies every pane appears exactly once in the tree.
func layoutComplete(root *splitNode, panes []*pane) bool {
	seen := make(map[*pane]int)
	root.walk(func(current *pane) { seen[current]++ })
	if len(seen) != len(panes) {
		return false
	}
	for _, currentPane := range panes {
		if seen[currentPane] != 1 {
			return false
		}
	}
	return true
}

// fallbackLayout stacks all panes in vertical splits when the saved tree is
// missing or inconsistent with the pane list.
func fallbackLayout(panes []*pane) *splitNode {
	if len(panes) == 0 {
		return nil
	}
	root := leafNode(panes[0])
	for _, currentPane := range panes[1:] {
		insertAt(root, panes[0], currentPane, true)
	}
	return root
}

// persist writes the current state to disk, surfacing errors in the footer.
func (m *Model) persist() {
	if m.statePath == "" {
		return
	}
	if err := state.Save(m.statePath, m.snapshot()); err != nil {
		m.status = "state save failed: " + err.Error()
		m.statusIsInfo = false
	}
}
