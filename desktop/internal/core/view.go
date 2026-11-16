package core

import (
	"slices"
	"time"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/keymap"
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/sound"
	"github.com/patriceckhart/hrdx/internal/theme"
)

// View is the plain-data view model the frontend renders. The frontend holds
// no model state beyond transient UI.
type View struct {
	Version          string             `json:"version"`
	Workspaces       []WorkspaceView    `json:"workspaces"`
	Selected         int                `json:"selected"`
	SidebarCollapsed bool               `json:"sidebar_collapsed"`
	SidebarWidth     int                `json:"sidebar_width"`
	SplitDirection   string             `json:"split_direction"`
	WorkspaceFolding bool               `json:"workspace_folding"`
	DefaultAgent     string             `json:"default_agent"`
	Agents           []AgentView        `json:"agents"`
	Theme            ThemeView          `json:"theme"`
	Keymap           map[string]string  `json:"keymap"`
	Prefix           string             `json:"prefix"`
	NavKeys          map[string]string  `json:"nav_keys"`
	Persisting       bool               `json:"persisting"`
	AutoCopy         bool               `json:"auto_copy"`
	Menus            []api.MenuRegister `json:"menus"`
	Prefs            prefs.Prefs        `json:"prefs"`
	Problems         []string           `json:"problems,omitempty"`
	Blocked          string             `json:"blocked,omitempty"`
}

// WorkspaceView is one sidebar workspace.
type WorkspaceView struct {
	GroupPath []string  `json:"group_path,omitempty"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch,omitempty"`
	Folded    bool      `json:"folded,omitempty"`
	Active    int       `json:"active"`
	Tabs      []TabView `json:"tabs"`
}

// TabView is one tab with its split tree and floats.
type TabView struct {
	Name    string     `json:"name,omitempty"`
	Focused int        `json:"focused_pane_id"`
	Layout  *NodeView  `json:"layout"`
	Panes   []PaneView `json:"panes"`
	Floats  []PaneView `json:"floats,omitempty"`
}

// NodeView mirrors the split tree. Vertical keeps its state.Node meaning:
// children side by side.
type NodeView struct {
	PaneID   int       `json:"pane_id,omitempty"`
	Vertical bool      `json:"vertical,omitempty"`
	Ratio    float64   `json:"ratio,omitempty"`
	A        *NodeView `json:"a,omitempty"`
	B        *NodeView `json:"b,omitempty"`
}

// PaneView is one pane. ID equals api.PaneStatus.ID for the same pane.
type PaneView struct {
	ID        int    `json:"pane_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Title     string `json:"title,omitempty"`
	Running   bool   `json:"running"`
	Starting  bool   `json:"starting,omitempty"`
	Busy      bool   `json:"busy"`
	Attention bool   `json:"attention,omitempty"`
	Unseen    bool   `json:"unseen,omitempty"`
	Failure   string `json:"failure,omitempty"`
	KittyKeys bool   `json:"kitty_keys,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
	WidthPct  int    `json:"width_pct,omitempty"`
	HeightPct int    `json:"height_pct,omitempty"`
	Agent     string `json:"agent,omitempty"` // agent running in the pane, also for shells
}

// AgentView is one agent kind in pickers and settings.
type AgentView struct {
	Kind      string `json:"kind"`
	Installed bool   `json:"installed"`
	Enabled   bool   `json:"enabled"`
	Custom    bool   `json:"custom,omitempty"`
}

// ThemeView carries the resolved palette of the selected theme.
type ThemeView struct {
	Name   string            `json:"name"`
	Colors map[string]string `json:"colors"`
}

var branchCache = struct {
	entries map[string]branchEntry
}{entries: map[string]branchEntry{}}

type branchEntry struct {
	value   string
	checked time.Time
}

// branch reads a workspace branch, cached briefly so 60 Hz views stay cheap.
func branch(cwd string) string {
	if entry, ok := branchCache.entries[cwd]; ok && time.Since(entry.checked) < 5*time.Second {
		return entry.value
	}
	value := gitinfo.Branch(cwd)
	branchCache.entries[cwd] = branchEntry{value, time.Now()}
	return value
}

func (m *model) view() View {
	keys := keymap.BuildPrefixKeys(m.keyOverrides)
	prefix := "ctrl+b"
	if override := m.keyOverrides["prefix"]; override != "" {
		prefix = override
	}
	out := View{
		Version: m.config.Version, Selected: m.selected,
		SidebarCollapsed: m.sidebarCollapsed, SidebarWidth: m.prefs.SidebarWidth,
		SplitDirection: m.splitDirection, WorkspaceFolding: m.foldEnabled,
		DefaultAgent: m.config.DefaultAgent, Keymap: keys, Prefix: prefix,
		NavKeys:    keymap.BuildNavigationKeys(m.keyOverrides),
		Persisting: m.launcher != nil && m.launcher.Persistent(),
		AutoCopy:   m.autoCopy, Menus: slices.Clone(m.customMenus), Prefs: m.prefs,
		Problems: slices.Clone(m.problems), Blocked: m.blocked,
		Theme: ThemeView{Name: m.themeName, Colors: theme.Resolve(m.themeName)},
	}
	installed := m.installedAgentsCached()
	for _, spec := range agent.Specs {
		out.Agents = append(out.Agents, AgentView{Kind: spec.Kind, Installed: installed[spec.Kind],
			Enabled: !m.disabled[spec.Kind], Custom: spec.Custom})
	}
	focused := m.currentPane()
	for _, current := range m.spaces {
		ws := WorkspaceView{GroupPath: slices.Clone(current.groupPath), Name: current.name,
			Path: current.cwd, Branch: branch(current.cwd), Folded: current.folded, Active: current.active}
		for _, currentTab := range current.tabs {
			tv := TabView{Name: currentTab.name, Layout: nodeView(currentTab.layout)}
			if len(currentTab.panes) > 0 {
				tv.Focused = currentTab.panes[clamp(currentTab.selected, 0, len(currentTab.panes)-1)].id
			}
			for _, target := range currentTab.panes {
				pv := m.paneView(target, focused)
				if target.floating != nil {
					tv.Floats = append(tv.Floats, pv)
				} else {
					tv.Panes = append(tv.Panes, pv)
				}
			}
			ws.Tabs = append(ws.Tabs, tv)
		}
		out.Workspaces = append(out.Workspaces, ws)
	}
	return out
}

func (m *model) paneView(target *pane, focused *pane) PaneView {
	busy, titleState := m.paneState(target)
	pv := PaneView{ID: target.id, Name: target.name, Kind: target.kind,
		Running: target.running, Starting: target.starting, Busy: busy, Failure: target.failure,
		Unseen: m.paneAttention[target.id], Agent: m.paneAgentKind(target)}
	if titleState == "attention" {
		pv.Attention = m.paneAttention[target.id] || !m.windowFocused || focused != target
	}
	if target.term != nil {
		pv.Title = target.term.Title()
		pv.KittyKeys = target.term.KittyKeys()
	}
	if target.floating != nil {
		pv.Anchor = target.floating.anchor
		pv.WidthPct = target.floating.widthPct
		pv.HeightPct = target.floating.heightPct
	}
	return pv
}

func nodeView(n *layout.Node[*pane]) *NodeView {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return &NodeView{PaneID: n.Pane.id}
	}
	return &NodeView{Vertical: n.Vertical, Ratio: n.Ratio, A: nodeView(n.A), B: nodeView(n.B)}
}

var installedCache struct {
	value   map[string]bool
	checked time.Time
}

// installedAgentsCached avoids PATH lookups on every frame.
func (m *model) installedAgentsCached() map[string]bool {
	if installedCache.value == nil || time.Since(installedCache.checked) > 10*time.Second {
		installedCache.value = m.installedAgents()
		installedCache.checked = time.Now()
	}
	return installedCache.value
}

// SettingsView is the settings dialog content.
type SettingsView struct {
	Agents           []AgentView `json:"agents"`
	DefaultAgent     string      `json:"default_agent"`
	Sound            bool        `json:"sound"`
	SoundKind        string      `json:"sound_kind"`
	SoundKinds       []string    `json:"sound_kinds"`
	Notify           bool        `json:"notify"`
	NotifySupported  bool        `json:"notify_supported"`
	Theme            string      `json:"theme"`
	Themes           []string    `json:"themes"`
	AutoCopy         bool        `json:"auto_copy"`
	SplitDirection   string      `json:"split_direction"`
	WorkspaceFolding bool        `json:"workspace_folding"`
	Prefs            prefs.Prefs `json:"prefs"`
}

func (m *model) settings(notifySupported bool) SettingsView {
	installed := m.installedAgents()
	out := SettingsView{DefaultAgent: m.config.DefaultAgent, Sound: m.soundOn, SoundKind: m.soundKind,
		SoundKinds: sound.Kinds(), Notify: m.notifyOn, NotifySupported: notifySupported,
		Theme: m.themeName, Themes: theme.Names(), AutoCopy: m.autoCopy,
		SplitDirection: m.splitDirection, WorkspaceFolding: m.foldEnabled, Prefs: m.prefs}
	for _, spec := range agent.Specs {
		out.Agents = append(out.Agents, AgentView{Kind: spec.Kind, Installed: installed[spec.Kind],
			Enabled: !m.disabled[spec.Kind], Custom: spec.Custom})
	}
	return out
}
