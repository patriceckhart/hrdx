package core

import (
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/keymap"
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/sound"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/theme"
)

func newModel(config Config, saved state.State, launcher Launcher, hooks Hooks, events *api.Broadcaster, desktopPrefs prefs.Prefs) *model {
	m := &model{
		config: config, launcher: launcher, hooks: hooks, events: events,
		prefs: desktopPrefs.Normalized(), nextID: 1,
		disabled: map[string]bool{}, wasBusy: map[int]bool{}, finishSeq: map[int]uint64{},
		paneAttention: map[int]bool{}, windowFocused: true,
		subrepos: map[string]subrepoCache{},
	}
	m.loadConfigFiles()
	m.applySettings(saved)
	m.restore(saved)
	return m
}

// loadConfigFiles reads harness.json, keys.json, sounds.json, and themes
// beside the state file.
func (m *model) loadConfigFiles() {
	if m.config.StatePath != "" {
		dir := filepath.Dir(m.config.StatePath)
		overrides, problem := keymap.Load(dir)
		m.keyOverrides = overrides
		m.problems = append(m.problems, agent.LoadHarnesses(dir), problem, sound.Load(dir), theme.Load(dir))
	}
	m.problems = slices.DeleteFunc(m.problems, func(s string) bool { return s == "" })
	if !agent.IsAgent(m.config.DefaultAgent) {
		m.config.DefaultAgent = "zot"
	}
}

// applySettings copies shared settings from a saved state.
func (m *model) applySettings(saved state.State) {
	for _, kind := range saved.DisabledAgents {
		if agent.IsAgent(kind) {
			m.disabled[kind] = true
		} else {
			// A harness missing on this machine keeps its setting for the TUI.
			m.unknownDisabled = append(m.unknownDisabled, kind)
		}
	}
	m.soundOn = saved.Sound
	m.soundKind = saved.SoundKind
	if !sound.IsKind(m.soundKind) {
		m.soundKind = sound.DefaultKind
	}
	m.notifyOn = saved.Notify
	m.themeName = saved.Theme
	if !theme.IsName(m.themeName) {
		m.themeName = theme.DefaultName
	}
	m.autoCopy = !saved.DisableAutoCopy
	m.sidebarCollapsed = saved.SidebarCollapsed
	m.splitDirection = validSplitDirection(saved.SplitDirection)
	m.foldEnabled = saved.WorkspaceFolding
	m.tuiSidebarWidth = saved.SidebarWidth
	m.durableMuted = append([]string(nil), saved.DurableMuted...)
	m.subrepoDepth = clamp(saved.SubrepoDepth, 0, gitinfo.MaxSubrepoDepth)
}

// cleanHidden keeps valid, distinct hidden sub-repository paths in sorted
// order, like the TUI, dropping any that could point outside the workspace.
func cleanHidden(saved []string) []string {
	var out []string
	for _, rel := range saved {
		if rel = gitinfo.CleanSubrepoRel(rel); rel != "" && !slices.Contains(out, rel) {
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out
}

// restore rebuilds workspaces from saved state. Agent panes resume their
// latest session. Legacy states without tabs become a single tab. Durable
// bindings are kept as placeholders so the TUI finds them intact.
func (m *model) restore(saved state.State) {
	for _, ws := range saved.Workspaces {
		savedTabs := ws.Tabs
		if len(savedTabs) == 0 && len(ws.Panes) > 0 {
			savedTabs = []state.Tab{{Panes: ws.Panes, Layout: ws.Layout, Selected: ws.Selected}}
		}
		if ws.CWD == "" || len(savedTabs) == 0 {
			continue
		}
		restored := &space{
			name: ws.Name, cwd: ws.CWD, active: ws.Active, folded: ws.Folded,
			host: ws.Host, continuous: append([]state.DurableRef(nil), ws.Continuous...),
			hiddenSubrepos: cleanHidden(ws.HiddenSubrepos),
		}
		if state.ValidateGroupPath(ws.GroupPath) == nil {
			restored.groupPath = append([]string(nil), ws.GroupPath...)
		}
		for _, savedTab := range savedTabs {
			if len(savedTab.Panes) == 0 {
				continue
			}
			restoredTab := &tab{name: savedTab.Name}
			for _, savedPane := range savedTab.Panes {
				kind := savedPane.Kind
				if !agent.IsAgent(kind) && kind != "shell" && savedPane.Durable == nil {
					kind = "shell"
				}
				restoredPane := &pane{
					id: m.nextID, name: savedPane.Name, kind: kind,
					resume: agent.IsAgent(kind), session: savedPane.Session,
					cwd:  restoredPaneDir(savedPane.CWD),
					args: append([]string(nil), savedPane.Args...),
				}
				if savedPane.Durable != nil {
					ref := *savedPane.Durable
					restoredPane.durable = &ref
					restoredPane.kind = savedPane.Kind
					restoredPane.cwd = savedPane.CWD
					restoredPane.resume = false
					restoredPane.failure = "Durable agent panes are not available in the hrdx app yet"
				}
				restoredTab.panes = append(restoredTab.panes, restoredPane)
				m.nextID++
			}
			restoredTab.layout = layout.Restore(savedTab.Layout, restoredTab.panes)
			restoredTab.selected = clamp(savedTab.Selected, 0, len(restoredTab.panes)-1)
			restored.tabs = append(restored.tabs, restoredTab)
		}
		if len(restored.tabs) == 0 {
			continue
		}
		restored.active = clamp(restored.active, 0, len(restored.tabs)-1)
		m.spaces = append(m.spaces, restored)
	}
	m.selected = clamp(saved.Selected, 0, max(0, len(m.spaces)-1))
}

// restoredPaneDir drops a saved directory that no longer exists, so the pane
// falls back to the workspace directory.
func restoredPaneDir(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return ""
	}
	return path
}

// snapshot converts the live model into its serializable form. Floating
// panes are ephemeral and never persisted.
func (m *model) snapshot() state.State {
	saved := state.State{
		Selected: m.selected, Sound: m.soundOn, SoundKind: m.soundKind,
		Notify: m.notifyOn, Theme: m.themeName, DisableAutoCopy: !m.autoCopy,
		SidebarCollapsed: m.sidebarCollapsed, SidebarWidth: m.tuiSidebarWidth,
		SplitDirection: m.splitDirection, WorkspaceFolding: m.foldEnabled,
		SubrepoDepth: m.subrepoDepth, DurableMuted: append([]string(nil), m.durableMuted...),
	}
	for _, spec := range agent.Specs {
		if m.disabled[spec.Kind] {
			saved.DisabledAgents = append(saved.DisabledAgents, spec.Kind)
		}
	}
	saved.DisabledAgents = append(saved.DisabledAgents, m.unknownDisabled...)
	for _, current := range m.spaces {
		ws := state.Workspace{
			GroupPath: append([]string(nil), current.groupPath...),
			Name:      current.name, CWD: current.cwd, Active: current.active,
			Host: current.host, Continuous: append([]state.DurableRef(nil), current.continuous...),
			Folded: current.folded, HiddenSubrepos: append([]string(nil), current.hiddenSubrepos...),
		}
		for _, currentTab := range current.tabs {
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
				savedPane := state.Pane{Kind: currentPane.kind, Name: currentPane.name,
					Session: currentPane.session, CWD: currentPane.cwd,
					Args: append([]string(nil), currentPane.args...)}
				if currentPane.durable != nil {
					ref := *currentPane.durable
					savedPane.Durable = &ref
				}
				savedTab.Panes = append(savedTab.Panes, savedPane)
			}
			savedTab.Layout = layout.ToState(currentTab.layout, index)
			ws.Tabs = append(ws.Tabs, savedTab)
		}
		saved.Workspaces = append(saved.Workspaces, ws)
	}
	return saved
}

// persist schedules a debounced state write.
func (m *model) persist() {
	if m.config.StatePath == "" || m.quitting {
		return
	}
	m.saveDue = true
	if m.saveTimer != nil {
		return
	}
	m.saveTimer = time.AfterFunc(m.config.SaveDelay, func() {
		m.post(func() {
			m.saveTimer = nil
			m.flush()
		})
	})
}

// flush writes pending state now.
func (m *model) flush() {
	if !m.saveDue || m.config.StatePath == "" {
		return
	}
	m.saveDue = false
	if err := state.Save(m.config.StatePath, m.snapshot()); err != nil {
		m.problems = append(m.problems, "state save failed: "+err.Error())
	}
}

// savePrefs writes desktop.json immediately; it is small and changes rarely.
func (m *model) savePrefs() {
	if err := prefs.Save(prefs.Path(m.config.StatePath), m.prefs); err != nil {
		m.problems = append(m.problems, "desktop.json save failed: "+err.Error())
	}
}
