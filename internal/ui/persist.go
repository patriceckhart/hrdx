package ui

import (
	"os"
	"path/filepath"

	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/state"
)

// snapshot converts the live model into its serializable form.
func (m *Model) snapshot() state.State {
	saved := state.State{
		Selected: m.selected, Sound: m.soundOn, SoundKind: m.soundKind,
		Notify: m.notifyOn, Theme: m.themeName, DisableAutoCopy: !m.autoCopy,
		SidebarCollapsed: m.sideCollapsed, SidebarWidth: m.sideWidth,
		SplitDirection: m.splitDirection, WorkspaceFolding: m.foldEnabled,
		SubrepoDepth: m.subrepoDepth,
	}
	for _, category := range durableCategories {
		if m.durableMuted[category] {
			saved.DurableMuted = append(saved.DurableMuted, category)
		}
	}
	for _, spec := range agent.Specs {
		if m.disabled[spec.Kind] {
			saved.DisabledAgents = append(saved.DisabledAgents, spec.Kind)
		}
	}
	for _, currentSpace := range m.spaces {
		ws := state.Workspace{
			GroupPath:  append([]string(nil), currentSpace.groupPath...),
			Name:       currentSpace.name,
			CWD:        currentSpace.cwd,
			Active:     currentSpace.active,
			Host:       currentSpace.host,
			Continuous: append([]state.DurableRef(nil), currentSpace.durables...),
			Folded:     currentSpace.folded,

			HiddenSubrepos: sortedKeys(currentSpace.hiddenSubrepos),
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
				saved := state.Pane{
					Kind: currentPane.kind, Name: currentPane.name, Session: currentPane.session,
					CWD: currentPane.cwd, Args: append([]string(nil), currentPane.args...),
				}
				if currentPane.durable != nil {
					ref := *currentPane.durable
					saved.Durable = &ref
				}
				savedTab.Panes = append(savedTab.Panes, saved)
			}
			savedTab.Layout = layout.ToState(currentTab.layout, index)
			ws.Tabs = append(ws.Tabs, savedTab)
		}
		saved.Workspaces = append(saved.Workspaces, ws)
	}
	return saved
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
		restored := &space{name: ws.Name, cwd: ws.CWD, active: ws.Active, host: ws.Host, folded: ws.Folded}
		for _, ref := range ws.Continuous {
			if ref.Valid() {
				associate(restored, ref)
			}
		}
		for _, rel := range ws.HiddenSubrepos {
			if rel = cleanSubrepoRel(rel); rel != "" {
				if restored.hiddenSubrepos == nil {
					restored.hiddenSubrepos = map[string]bool{}
				}
				restored.hiddenSubrepos[rel] = true
			}
		}
		if validateGroupPath(ws.GroupPath) == nil {
			restored.groupPath = append([]string(nil), ws.GroupPath...)
		}
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
				restoredPane := &pane{
					id:      m.nextID,
					name:    savedPane.Name,
					kind:    kind,
					resume:  isAgentKind(kind),
					session: savedPane.Session,
					cwd:     restoredPaneDir(savedPane.CWD),
					args:    append([]string(nil), savedPane.Args...),
				}
				if ref := savedPane.Durable; ref != nil {
					// A durable view relaunches by attaching to its exact
					// conversation, never by resuming an unrelated session. An
					// incomplete binding stays a durable pane that reports its
					// failure; it is never silently turned into an ordinary agent.
					copied := *ref
					restoredPane.kind = "zot"
					restoredPane.resume = false
					restoredPane.durable = &copied
					if copied.Valid() {
						associate(restored, copied)
					} else {
						restoredPane.failure = "durable binding is incomplete, close this view"
					}
				}
				restoredTab.panes = append(restoredTab.panes, restoredPane)
				m.nextID++
			}
			restoredTab.layout = layout.Restore(savedTab.Layout, restoredTab.panes)
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

// restoredPaneDir drops a saved directory that no longer exists, so the
// pane falls back to the workspace directory.
func restoredPaneDir(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return ""
	}
	return path
}

// layoutComplete verifies every pane appears exactly once in the tree.
func layoutComplete(root *splitNode, panes []*pane) bool { return layout.Complete(root, panes) }

// fallbackLayout stacks all panes in side-by-side splits.
func fallbackLayout(panes []*pane) *splitNode { return layout.Fallback(panes) }

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
