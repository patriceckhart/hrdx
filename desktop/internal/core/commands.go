package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/patriceckhart/hrdx/desktop/internal/prefs"
	"github.com/patriceckhart/hrdx/internal/agent"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/layout"
	"github.com/patriceckhart/hrdx/internal/sound"
	"github.com/patriceckhart/hrdx/internal/state"
	"github.com/patriceckhart/hrdx/internal/term"
	"github.com/patriceckhart/hrdx/internal/theme"
)

// Commands called by the bridge. Each runs on the loop and returns after the
// mutation, so the next emitted view reflects it.

// Activate loads saved state, adopts a launcher, and starts every pane. It is
// used after the app started blocked because another frontend owned the
// holder and the user retried successfully. It fails if workspaces are
// already open.
func (c *Core) Activate(statePath string, saved state.State, launcher Launcher, cwds []string) error {
	return c.do(func() error {
		m := c.m
		if len(m.spaces) > 0 {
			return fmt.Errorf("already active")
		}
		if statePath != m.config.StatePath {
			m.config.StatePath = statePath
			m.loadConfigFiles()
		}
		m.launcher = launcher
		m.blocked = ""
		m.applySettings(saved)
		m.restore(saved)
		for _, cwd := range cwds {
			m.openWorkspace(cwd)
		}
		m.gcHolderSessions()
		m.startAll()
		// Scan now instead of waiting for the first ticker interval.
		m.refreshSubrepos()
		m.persist()
		return nil
	})
}

// SetBlocked marks the app as blocked by another frontend. While blocked,
// workspaces and panes cannot be created.
func (c *Core) SetBlocked(message string) {
	c.post(func() { c.m.blocked = message })
}

// Snapshot returns the current view.
func (c *Core) Snapshot() (View, error) {
	var out View
	err := c.do(func() error { out = c.m.view(); return nil })
	return out, err
}

// WorkspaceCreate opens a directory as a workspace.
func (c *Core) WorkspaceCreate(path, kind string, groupPath []string) error {
	return c.do(func() error {
		_, err := c.m.createWorkspace(path, kind, groupPath, false)
		return err
	})
}

// WorkspaceSelect selects a workspace by index.
func (c *Core) WorkspaceSelect(index int) error {
	return c.do(func() error {
		if index < 0 || index >= len(c.m.spaces) {
			return fmt.Errorf("workspace %d not found", index)
		}
		c.m.selected = index
		delete(c.m.paneAttention, c.m.focusedID())
		c.m.persist()
		return nil
	})
}

// WorkspaceClose closes a workspace and kills its panes.
func (c *Core) WorkspaceClose(index int) error {
	return c.do(func() error { return c.m.closeWorkspace(index) })
}

// WorkspaceReorder moves a workspace in the sidebar order.
func (c *Core) WorkspaceReorder(from, to int) error {
	return c.do(func() error {
		m := c.m
		if from < 0 || from >= len(m.spaces) || to < 0 || to >= len(m.spaces) {
			return fmt.Errorf("workspace index out of range")
		}
		if from == to {
			return nil
		}
		selected := m.currentSpace()
		moved := m.spaces[from]
		m.spaces = slices.Delete(m.spaces, from, from+1)
		m.spaces = slices.Insert(m.spaces, to, moved)
		m.selected = max(0, m.spaceIndex(selected))
		m.persist()
		return nil
	})
}

// WorkspaceMove changes a workspace's group path.
func (c *Core) WorkspaceMove(index int, groupPath []string) error {
	return c.do(func() error {
		if index < 0 || index >= len(c.m.spaces) {
			return fmt.Errorf("workspace %d not found", index)
		}
		if err := state.ValidateGroupPath(groupPath); err != nil {
			return err
		}
		c.m.moveWorkspace(c.m.spaces[index], groupPath)
		return nil
	})
}

// SubrepoOpen starts a pane of kind in a nested repository of workspace.
// where is tab, right, or down. The pane keeps the repository as its start
// directory across restarts.
func (c *Core) SubrepoOpen(workspace int, path, kind, where string) error {
	return c.do(func() error {
		m := c.m
		owner, err := m.subrepoOwner(workspace, path)
		if err != nil {
			return err
		}
		if where != "tab" && where != "right" && where != "down" {
			return fmt.Errorf("unknown placement %q (tab, right, down)", where)
		}
		if m.blocked != "" {
			return errors.New(m.blocked)
		}
		kind, err = m.resolveKind(kind)
		if err != nil {
			return err
		}
		m.selected = m.spaceIndex(owner)
		var created *pane
		switch where {
		case "tab":
			created = m.addTab(owner, kind)
		case "right":
			created = m.addPane(owner, kind, true, false)
		default:
			created = m.addPane(owner, kind, false, false)
		}
		if filepath.Clean(path) != filepath.Clean(owner.cwd) {
			created.cwd = filepath.Clean(path)
		}
		m.persist()
		m.publish(api.Event{Event: api.EventPaneCreated,
			Data: api.PaneEvent{Pane: created.id, Name: created.name, Kind: created.kind, Workspace: owner.name}})
		m.startPane(owner, created)
		return nil
	})
}

// SubrepoHide removes a nested repository from a workspace's list and stops
// scanning below it.
func (c *Core) SubrepoHide(workspace int, path string) error {
	return c.do(func() error {
		owner, err := c.m.spaceAt(workspace)
		if err != nil {
			return err
		}
		return c.m.hideSubrepo(owner, path)
	})
}

// SubrepoUnhide shows every hidden folder of a workspace again.
func (c *Core) SubrepoUnhide(workspace int) error {
	return c.do(func() error {
		owner, err := c.m.spaceAt(workspace)
		if err != nil {
			return err
		}
		c.m.unhideSubrepos(owner)
		return nil
	})
}

// WorkspaceSetFolded folds or unfolds a workspace's sidebar rows.
func (c *Core) WorkspaceSetFolded(index int, folded bool) error {
	return c.do(func() error {
		if index < 0 || index >= len(c.m.spaces) {
			return fmt.Errorf("workspace %d not found", index)
		}
		c.m.spaces[index].folded = folded
		c.m.persist()
		return nil
	})
}

// WorkspaceRename renames a workspace.
func (c *Core) WorkspaceRename(index int, name string) error {
	return c.do(func() error {
		if index < 0 || index >= len(c.m.spaces) {
			return fmt.Errorf("workspace %d not found", index)
		}
		clean, err := cleanName(name)
		if err != nil {
			return err
		}
		c.m.spaces[index].name = clean
		c.m.persist()
		return nil
	})
}

// TabCreate adds a tab with one pane of kind to the selected workspace.
func (c *Core) TabCreate(kind string) error {
	return c.do(func() error {
		_, err := c.m.createPane(c.m.currentSpace(), kind, "tab", "", nil, nil)
		return err
	})
}

// TabSelect activates a tab.
func (c *Core) TabSelect(workspace, index int) error {
	return c.do(func() error {
		owner, err := c.m.spaceAt(workspace)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(owner.tabs) {
			return fmt.Errorf("tab %d not found", index)
		}
		c.m.selected = workspace
		owner.active = index
		delete(c.m.paneAttention, c.m.focusedID())
		c.m.persist()
		return nil
	})
}

// TabClose closes a tab and its panes. The last tab is refused.
func (c *Core) TabClose(workspace, index int) error {
	return c.do(func() error {
		owner, err := c.m.spaceAt(workspace)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(owner.tabs) {
			return fmt.Errorf("tab %d not found", index)
		}
		if len(owner.tabs) == 1 {
			return fmt.Errorf("cannot close the last tab")
		}
		c.m.removeTab(owner, owner.tabs[index])
		c.m.persist()
		return nil
	})
}

// TabRename names a tab; an empty name restores the default label.
func (c *Core) TabRename(workspace, index int, name string) error {
	return c.do(func() error {
		owner, err := c.m.spaceAt(workspace)
		if err != nil {
			return err
		}
		if index < 0 || index >= len(owner.tabs) {
			return fmt.Errorf("tab %d not found", index)
		}
		name = strings.TrimSpace(name)
		if name != "" {
			if name, err = cleanName(name); err != nil {
				return err
			}
		}
		owner.tabs[index].name = name
		c.m.persist()
		return nil
	})
}

// PaneCreate splits the focused pane. split is right, down, or tab; an
// empty split follows the default split direction.
func (c *Core) PaneCreate(kind, split string) error {
	return c.do(func() error {
		m := c.m
		owner := m.currentSpace()
		if owner == nil {
			return fmt.Errorf("no workspace open")
		}
		if split == "" || split == "primary" || split == "secondary" {
			vertical := m.preferredVertical()
			if split == "secondary" {
				vertical = !vertical
			}
			split = "down"
			if vertical {
				split = "right"
			}
		}
		_, err := m.createPane(owner, kind, split, "", nil, nil)
		return err
	})
}

// PaneCreateFloat opens an ephemeral floating pane, never persisted.
func (c *Core) PaneCreateFloat(kind, anchor string, widthPct, heightPct int) error {
	return c.do(func() error {
		_, err := c.m.createPane(c.m.currentSpace(), kind, "float", anchor, &widthPct, &heightPct)
		return err
	})
}

// PaneFocus focuses a pane and its tab and workspace.
func (c *Core) PaneFocus(id int) error {
	return c.do(func() error {
		target, owner, _ := c.m.paneByID(id)
		if target == nil {
			return fmt.Errorf("pane %d not found", id)
		}
		c.m.focusPane(owner, target)
		return nil
	})
}

// PaneClose kills a pane and promotes its sibling.
func (c *Core) PaneClose(id int) error {
	return c.do(func() error { return c.m.closePane(id) })
}

// PaneMove rearranges split panes of one tab. zone is "center" to swap the
// two panes, or "left", "right", "top", "bottom" to place pane on that
// side of target. Floating panes and panes in different tabs are refused.
func (c *Core) PaneMove(paneID, targetID int, zone string) error {
	return c.do(func() error {
		m := c.m
		moved, owner, movedTab := m.paneByID(paneID)
		target, _, targetTab := m.paneByID(targetID)
		if moved == nil || target == nil {
			return fmt.Errorf("pane not found")
		}
		if movedTab != targetTab || moved.floating != nil || target.floating != nil {
			return fmt.Errorf("panes can only be rearranged within one tab's split layout")
		}
		ok := false
		switch zone {
		case "center":
			ok = layout.Swap(movedTab.layout, moved, target)
		case "left", "right", "top", "bottom":
			vertical := zone == "left" || zone == "right"
			before := zone == "left" || zone == "top"
			ok = layout.Move(&movedTab.layout, moved, target, vertical, before)
		default:
			return fmt.Errorf("unknown drop zone %q", zone)
		}
		if !ok {
			return nil
		}
		m.focusPane(owner, moved)
		m.persist()
		return nil
	})
}

// PaneRename renames a pane.
func (c *Core) PaneRename(id int, name string) error {
	return c.do(func() error {
		target, _, _ := c.m.paneByID(id)
		if target == nil {
			return fmt.Errorf("pane %d not found", id)
		}
		clean, err := cleanName(name)
		if err != nil {
			return err
		}
		target.name = clean
		c.m.persist()
		return nil
	})
}

// PaneRestart relaunches an exited or failed pane in place.
func (c *Core) PaneRestart(id int) error {
	return c.do(func() error {
		target, owner, _ := c.m.paneByID(id)
		if target == nil {
			return fmt.Errorf("pane %d not found", id)
		}
		if target.durable != nil {
			return fmt.Errorf("durable agent panes are not available in the hrdx app yet")
		}
		if target.running || target.starting {
			return nil
		}
		target.term = nil
		target.session = 0
		target.resume = agent.IsAgent(target.kind)
		c.m.startPane(owner, target)
		return nil
	})
}

// Attach is the frontend's view of one pane at mount time.
type Attach struct {
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	Preamble string `json:"preamble"`
	Running  bool   `json:"running"`
}

// PaneAttachInfo returns the mode preamble and size for a mounting view.
func (c *Core) PaneAttachInfo(id int) (Attach, error) {
	var out Attach
	err := c.do(func() error {
		target, _, _ := c.m.paneByID(id)
		if target == nil {
			return fmt.Errorf("pane %d not found", id)
		}
		out = Attach{Cols: target.cols, Rows: target.rows, Running: target.running}
		if target.term != nil {
			out.Preamble = target.term.ModePreamble()
		}
		target.visible = true
		return nil
	})
	return out, err
}

// PaneInput writes input to a pane. It is the hot path: fire and forget, no
// reply channel.
func (c *Core) PaneInput(id int, data []byte) {
	c.post(func() {
		target, _, _ := c.m.paneByID(id)
		if target != nil && target.term != nil && target.running {
			target.term.Write(data)
		}
	})
}

// PanePaste writes pasted text the way the TUI does: wrapped in bracketed
// paste markers when the program enabled them, and always for agents, whose
// one-time DECSET 2004 may have been missed on reattach. Without the markers
// an agent reads every newline as Enter and submits line by line.
func (c *Core) PanePaste(id int, text string) {
	c.post(func() {
		target, _, _ := c.m.paneByID(id)
		if target == nil || target.term == nil || !target.running || text == "" {
			return
		}
		bracketed := target.term.BracketedPaste() || c.m.paneAgentKind(target) != ""
		target.term.Write(term.EncodePaste(text, bracketed))
	})
}

// PaneResize resizes a pane's VT model and PTY. Requests below 2x2 are
// ignored; hidden panes keep their size until shown.
func (c *Core) PaneResize(id, cols, rows int) {
	c.post(func() {
		target, _, _ := c.m.paneByID(id)
		if target == nil || cols < 2 || rows < 2 {
			return
		}
		if target.cols == cols && target.rows == rows {
			return
		}
		target.cols, target.rows = cols, rows
		if target.term != nil {
			target.term.Resize(cols, rows)
		}
	})
}

// SplitSetRatio sets the ratio of the split at path in the active tab.
func (c *Core) SplitSetRatio(path []int, ratio float64) error {
	return c.do(func() error {
		owner := c.m.currentSpace()
		if owner == nil {
			return fmt.Errorf("no workspace open")
		}
		node := layout.At(owner.tab().layout, path)
		if node == nil || node.IsLeaf() {
			return fmt.Errorf("no split at %v", path)
		}
		node.Ratio = clampFloat(ratio, 0.1, 0.9)
		c.m.persist()
		return nil
	})
}

// SplitEqualize resets every ratio in the active tab, or the split at path
// when path is not empty.
func (c *Core) SplitEqualize(path []int) error {
	return c.do(func() error {
		owner := c.m.currentSpace()
		if owner == nil {
			return nil
		}
		root := owner.tab().layout
		if len(path) == 0 {
			layout.Equalize(root)
		} else if node := layout.At(root, path); node != nil && !node.IsLeaf() {
			node.Ratio = 0.5
		}
		c.m.persist()
		return nil
	})
}

// SplitDirectionCycle advances right, down, auto.
func (c *Core) SplitDirectionCycle() error {
	return c.do(func() error { c.m.cycleSplitDirection(); return nil })
}

// Navigate runs a keyboard navigation action.
func (c *Core) Navigate(action string) error {
	return c.do(func() error {
		m := c.m
		switch action {
		case "pane-next":
			m.cyclePane(1)
		case "pane-prev":
			m.cyclePane(-1)
		case "tab-next":
			m.selectTab(1)
		case "tab-prev":
			m.selectTab(-1)
		case "space-next":
			m.selectSpace(1)
		case "space-prev":
			m.selectSpace(-1)
		case "agent-cycle":
			m.cycleAgent()
		case "sidebar-toggle":
			m.sidebarCollapsed = !m.sidebarCollapsed
			m.persist()
		case "close-pane":
			if focused := m.currentPane(); focused != nil {
				return m.closePane(focused.id)
			}
		case "close-space":
			return m.closeWorkspace(m.selected)
		case "equalize":
			if owner := m.currentSpace(); owner != nil {
				layout.Equalize(owner.tab().layout)
				m.persist()
			}
		default:
			if strings.HasPrefix(action, "tab-") && len(action) == 5 {
				index := int(action[4] - '1')
				if index < 0 {
					index = 9
				}
				if owner := m.currentSpace(); owner != nil && index < len(owner.tabs) {
					owner.active = index
					m.persist()
				}
				return nil
			}
			return fmt.Errorf("unknown action %q", action)
		}
		return nil
	})
}

// SetWindowFocused records window focus for attention indicators.
func (c *Core) SetWindowFocused(focused bool) {
	c.post(func() {
		c.m.windowFocused = focused
		if focused {
			delete(c.m.paneAttention, c.m.focusedID())
		}
	})
}

// MenuAction publishes a selected custom menu entry with its context. A
// target that disappeared is ignored.
func (c *Core) MenuAction(actionID, target string, paneID, workspace, tabIndex int) error {
	return c.do(func() error {
		m := c.m
		if !slices.ContainsFunc(m.customMenus, func(r api.MenuRegister) bool { return r.ActionID == actionID }) {
			return nil
		}
		data := api.MenuActionEvent{ActionID: actionID, Target: target}
		switch target {
		case "sidebar":
			owner, err := m.spaceAt(workspace)
			if err != nil {
				return nil
			}
			data.Workspace, data.Path = owner.name, owner.cwd
		case "tab":
			owner, err := m.spaceAt(workspace)
			if err != nil || tabIndex < 0 || tabIndex >= len(owner.tabs) {
				return nil
			}
			data.Workspace, data.Path = owner.name, owner.cwd
			index := tabIndex
			data.TabIndex = &index
		case "pane":
			found, owner, _ := m.paneByID(paneID)
			if found == nil {
				return nil
			}
			data.Pane = paneID
			data.Workspace, data.Path = owner.name, owner.cwd
			for index, current := range owner.tabs {
				if slices.Contains(current.panes, found) {
					tabIndexCopy := index
					data.TabIndex = &tabIndexCopy
				}
			}
		default:
			return nil
		}
		m.publish(api.Event{Event: api.EventMenuAction, Data: data})
		return nil
	})
}

// Settings returns the settings dialog content.
func (c *Core) Settings(notifySupported bool) (SettingsView, error) {
	var out SettingsView
	err := c.do(func() error { out = c.m.settings(notifySupported); return nil })
	return out, err
}

// SettingsPatch is a partial settings update; nil fields are unchanged.
type SettingsPatch struct {
	AgentEnabled     map[string]bool `json:"agent_enabled,omitempty"`
	DefaultAgent     *string         `json:"default_agent,omitempty"`
	Sound            *bool           `json:"sound,omitempty"`
	SoundKind        *string         `json:"sound_kind,omitempty"`
	Notify           *bool           `json:"notify,omitempty"`
	Theme            *string         `json:"theme,omitempty"`
	AutoCopy         *bool           `json:"auto_copy,omitempty"`
	SplitDirection   *string         `json:"split_direction,omitempty"`
	WorkspaceFolding *bool           `json:"workspace_folding,omitempty"`
	SidebarCollapsed *bool           `json:"sidebar_collapsed,omitempty"`
	// SubrepoDepth sets how many folder levels are searched for nested git
	// repositories, 0 turns the sidebar list off.
	SubrepoDepth *int         `json:"subrepo_depth,omitempty"`
	Prefs        *prefs.Prefs `json:"prefs,omitempty"`
}

// SettingsSet applies a patch and persists it immediately.
func (c *Core) SettingsSet(patch SettingsPatch) error {
	return c.do(func() error {
		m := c.m
		for kind, enabled := range patch.AgentEnabled {
			if !agent.IsAgent(kind) {
				return fmt.Errorf("unknown agent %q", kind)
			}
			if enabled {
				delete(m.disabled, kind)
				continue
			}
			remaining := 0
			for _, spec := range agent.Specs {
				if !m.disabled[spec.Kind] && spec.Kind != kind {
					remaining++
				}
			}
			if remaining == 0 {
				return fmt.Errorf("cannot disable the last agent")
			}
			m.disabled[kind] = true
			if m.config.DefaultAgent == kind {
				m.config.DefaultAgent = m.availableAgents()[0]
			}
		}
		if patch.DefaultAgent != nil {
			if !agent.IsAgent(*patch.DefaultAgent) {
				return fmt.Errorf("unknown agent %q", *patch.DefaultAgent)
			}
			m.config.DefaultAgent = *patch.DefaultAgent
		}
		if patch.Sound != nil {
			m.soundOn = *patch.Sound
		}
		if patch.SoundKind != nil {
			if !sound.IsKind(*patch.SoundKind) {
				return fmt.Errorf("unknown sound %q", *patch.SoundKind)
			}
			m.soundKind = *patch.SoundKind
		}
		if patch.Notify != nil {
			m.notifyOn = *patch.Notify
		}
		if patch.Theme != nil {
			if !theme.IsName(*patch.Theme) {
				return fmt.Errorf("unknown theme %q", *patch.Theme)
			}
			m.themeName = *patch.Theme
		}
		if patch.AutoCopy != nil {
			m.autoCopy = *patch.AutoCopy
		}
		if patch.SplitDirection != nil {
			m.splitDirection = validSplitDirection(*patch.SplitDirection)
		}
		if patch.WorkspaceFolding != nil {
			m.foldEnabled = *patch.WorkspaceFolding
		}
		if patch.SidebarCollapsed != nil {
			m.sidebarCollapsed = *patch.SidebarCollapsed
		}
		if patch.SubrepoDepth != nil {
			depth := *patch.SubrepoDepth
			if depth < 0 || depth > gitinfo.MaxSubrepoDepth {
				return fmt.Errorf("subrepo depth must be between 0 and %d", gitinfo.MaxSubrepoDepth)
			}
			m.setSubrepoDepth(depth)
		}
		m.persist()
		if patch.Prefs != nil {
			next := *patch.Prefs
			next.Window = m.prefs.Window
			m.prefs = next.Normalized()
			m.savePrefs()
		}
		return nil
	})
}

// SetWindow records the window geometry for desktop.json.
func (c *Core) SetWindow(window prefs.Window) {
	c.post(func() {
		w := window
		c.m.prefs.Window = &w
	})
}

// Prefs returns the current desktop preferences.
func (c *Core) Prefs() prefs.Prefs {
	var out prefs.Prefs
	_ = c.do(func() error { out = c.m.prefs; return nil })
	return out
}

// DismissProblems clears the banner messages.
func (c *Core) DismissProblems() {
	c.post(func() { c.m.problems = nil })
}

// Problem adds a banner message.
func (c *Core) Problem(message string) {
	c.post(func() { c.m.problems = append(c.m.problems, message) })
}

func (m *model) spaceAt(index int) (*space, error) {
	if index < 0 || index >= len(m.spaces) {
		return nil, fmt.Errorf("workspace %d not found", index)
	}
	return m.spaces[index], nil
}

// cleanName trims a user-supplied name and rejects blank or control-laden
// values.
func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name must not be empty")
	}
	if len([]rune(name)) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("name must be at most 80 characters and contain no control characters")
	}
	return name, nil
}
