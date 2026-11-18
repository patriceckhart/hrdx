package ui

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/patriceckhart/hrdx/internal/plugin"
)

type pluginView struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Lines             []string `json:"lines"`
	Width             int      `json:"width_pct"`
	Height            int      `json:"height_pct"`
	Focus             bool     `json:"focus,omitempty"`
	Dock              string   `json:"dock,omitempty"` // "" (floating), right, bottom
	owner, generation string
}

// pluginViewBox returns the frame in body coordinates (below the header).
// Floating views are centered over the whole body. Docked views occupy a
// strip at the edge of the terminal area, after the tab bar row.
func (m Model) pluginViewBox(view *pluginView) rect {
	if view.Dock != "" {
		// Docked views sit inside the room the Files or Changes panel
		// leaves, so the two never overlap.
		area := m.fullTerminalArea()
		area.w = max(minPaneCols, area.w-m.reviewStripWidth(area))
		box := m.dockedViewBox(view, area)
		box.x += m.sidebarContentWidth() + 1
		box.y++ // tab bar row
		return box
	}
	width, height := m.width, max(0, m.height-2)
	if width < 3 || height < 3 {
		return rect{}
	}
	w, h := max(3, width*view.Width/100), max(3, height*view.Height/100)
	return rect{x: (width - w) / 2, y: (height - h) / 2, w: w, h: h}
}

// dockedViewBox is the strip a docked view reserves inside area, in the
// area's own coordinates. Docked views stack outward in open order: the
// first right dock is nearest the terminals.
func (m Model) dockedViewBox(view *pluginView, area rect) rect {
	if area.w < 3 || area.h < 3 {
		return rect{}
	}
	offsetW, offsetH := 0, 0
	for _, id := range m.pluginViewOrder {
		other := m.pluginViews[id]
		if other == nil || other == view {
			break
		}
		if other.Dock == view.Dock {
			switch view.Dock {
			case "right":
				offsetW += max(3, min(area.w-minPaneCols, area.w*other.Width/100))
			case "bottom":
				offsetH += max(3, min(area.h-minPaneRows, area.h*other.Height/100))
			}
		}
	}
	switch view.Dock {
	case "right":
		w := max(3, min(area.w-minPaneCols-offsetW, area.w*view.Width/100))
		if w < 3 {
			return rect{}
		}
		return rect{x: area.x + area.w - offsetW - w, y: area.y, w: w, h: area.h}
	case "bottom":
		h := max(3, min(area.h-minPaneRows-offsetH, area.h*view.Height/100))
		if h < 3 {
			return rect{}
		}
		return rect{x: area.x, y: area.y + area.h - offsetH - h, w: area.w, h: h}
	}
	return rect{}
}

func (m *Model) closePluginView(id string) {
	docked := m.pluginViews[id] != nil && m.pluginViews[id].Dock != ""
	delete(m.pluginViews, id)
	m.pluginViewOrder = slices.DeleteFunc(m.pluginViewOrder, func(current string) bool { return current == id })
	if m.pluginViewFocus == id {
		m.pluginViewFocus = ""
		if m.mode == modePluginView {
			m.mode = modeTerminal
		}
	}
	if docked {
		m.relayoutForDock()
	}
}

// relayoutForDock resizes every workspace's panes after the terminal area
// changed because a docked view opened, resized, or closed. PTYs receive
// their new inner dimensions through the normal resize path.
func (m *Model) relayoutForDock() {
	for _, currentSpace := range m.spaces {
		m.resizePanes(currentSpace)
	}
	m.resizePluginViews()
}

func (m *Model) handlePluginView(request plugin.Request) plugin.Reply {
	failure := func(code, message string) plugin.Reply {
		return plugin.Reply{Error: &plugin.Problem{Code: code, Message: message}}
	}
	if !m.config.PluginViews {
		return failure("disabled", "plugin views require --plugin-views")
	}
	approval, allowed := m.plugins.Authorize(request, "ui.view.contribute")
	if !allowed {
		return failure("denied", "plugin views are not granted")
	}
	var view pluginView
	if json.Unmarshal(request.Params, &view) != nil || !strings.HasPrefix(view.ID, request.Plugin+".") || !plugin.ValidIdentifier(view.ID) {
		return failure("invalid_params", "view ID must belong to the plugin namespace")
	}
	declared := false
	for _, entry := range m.plugins.Registrations() {
		if entry.Package.ID == request.Plugin && slices.Contains(entry.Package.Manifest.Contributes.Views, view.ID) {
			declared = true
		}
	}
	if !declared {
		return failure("denied", "view was not declared in the manifest")
	}
	if request.Method == "ui.view.close" {
		if previous := m.pluginViews[view.ID]; previous != nil && previous.owner != request.Plugin {
			return failure("denied", "view owner does not match")
		}
		m.closePluginView(view.ID)
		return plugin.Reply{Result: map[string]bool{"ok": true}}
	}
	if !plugin.SafeText(view.Title, 80) || len(view.Lines) > 128 || view.Width < 10 || view.Width > 100 || view.Height < 10 || view.Height > 100 {
		return failure("invalid_params", "view needs a title, up to 128 lines, and width_pct/height_pct from 10 to 100")
	}
	if view.Dock != "" && view.Dock != "right" && view.Dock != "bottom" {
		return failure("invalid_params", "dock must be empty, right, or bottom")
	}
	if view.Dock != "" && (view.Width > 50 || view.Height > 50) {
		return failure("invalid_params", "docked views may take at most 50 percent of the terminal area")
	}
	if view.Dock != "" && m.pluginViews[view.ID] != nil && m.pluginViews[view.ID].Dock != view.Dock {
		return failure("invalid_params", "a view cannot move between frame types, close and reopen it")
	}
	for _, line := range view.Lines {
		if !plugin.PlainText(line, 512) {
			return failure("invalid_params", "view lines must be bounded plain text without terminal controls")
		}
	}
	if len(m.pluginViews) >= 8 && m.pluginViews[view.ID] == nil {
		return failure("quota", "at most eight plugin views may be open")
	}
	view.owner, view.generation = request.Plugin, request.Generation
	if m.pluginViews == nil {
		m.pluginViews = make(map[string]*pluginView)
	}
	if m.pluginViews[view.ID] == nil {
		m.pluginViewOrder = append(m.pluginViewOrder, view.ID)
	}
	previous := m.pluginViews[view.ID]
	m.pluginViews[view.ID] = &view
	if view.Dock != "" && (previous == nil || previous.Width != view.Width || previous.Height != view.Height) {
		m.relayoutForDock()
	}
	if view.Focus && plugin.HasGrant(approval, "ui.view.input") && m.drag == nil && m.dragSpace == nil && m.selPane == nil && (m.mode == modeTerminal || m.mode == modePluginView) {
		m.focusPluginView(view.ID)
	}
	box := m.pluginViewBox(&view)
	return plugin.Reply{Result: map[string]int{"width": max(0, box.w-2), "height": max(0, box.h-2)}}
}

func (m *Model) focusPluginView(id string) {
	// Floating views raise on focus. Docked views keep their strip order so
	// focusing one does not shuffle the terminal area.
	if view := m.pluginViews[id]; view != nil && view.Dock == "" {
		m.pluginViewOrder = slices.DeleteFunc(m.pluginViewOrder, func(current string) bool { return current == id })
		m.pluginViewOrder = append(m.pluginViewOrder, id)
	}
	m.pluginViewFocus = id
	m.mode = modePluginView
}

func (m Model) viewInputAllowed(view *pluginView) bool {
	if view == nil || m.plugins == nil {
		return false
	}
	_, allowed := m.plugins.Authorize(plugin.Request{Plugin: view.owner, Generation: view.generation, Context: context.Background()}, "ui.view.input")
	return allowed
}

func (m Model) updatePluginViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !msg.Paste && msg.String() == "esc" {
		m.closePluginView(m.pluginViewFocus)
		return m, nil
	}
	if !msg.Paste && msg.String() == m.prefixTrigger {
		m.mode = modePrefix
		return m, nil
	}
	view := m.pluginViews[m.pluginViewFocus]
	if !m.viewInputAllowed(view) {
		m.syncPluginStates()
		m.mode = modeTerminal
		return m, nil
	}
	box := m.pluginViewBox(view)
	if box.w < 3 || box.h < 3 {
		return m, nil
	}
	m.plugins.Publish(view.owner, view.generation, "view.input", map[string]any{"id": view.ID, "key": msg.String(), "paste": msg.Paste})
	return m, nil
}

// Host menus and settings are handled first. Only visible content coordinates
// go to the topmost view, never input intended for obscured terminals.
func (m *Model) pluginViewMouse(msg tea.MouseMsg) bool {
	if (m.mode != modeTerminal && m.mode != modePluginView) || m.drag != nil || m.dragSpace != nil || m.selPane != nil {
		return false
	}
	for index := len(m.pluginViewOrder) - 1; index >= 0; index-- {
		view := m.pluginViews[m.pluginViewOrder[index]]
		box := m.pluginViewBox(view)
		x, y := msg.X-box.x, msg.Y-1-box.y
		if x < 0 || y < 0 || x >= box.w || y >= box.h {
			continue
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && y == 0 && x >= box.w-3 {
			m.closePluginView(view.ID)
			return true
		}
		if m.viewInputAllowed(view) {
			if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonLeft || msg.Button == tea.MouseButtonMiddle || msg.Button == tea.MouseButtonRight) {
				m.focusPluginView(view.ID)
			}
			if m.mode == modePluginView && m.pluginViewFocus == view.ID && x > 0 && x < box.w-1 && y > 0 && y < box.h-1 {
				m.plugins.Publish(view.owner, view.generation, "view.input", map[string]any{"id": view.ID, "mouse": normalizedPluginMouse(msg, x-1, y-1)})
			}
		}
		return true
	}
	if m.mode == modePluginView && msg.Action == tea.MouseActionPress {
		m.pluginViewFocus = ""
		m.mode = modeTerminal
	}
	return false
}

func normalizedPluginMouse(msg tea.MouseMsg, x, y int) map[string]any {
	button, action := "none", "motion"
	switch msg.Button {
	case tea.MouseButtonLeft:
		button = "left"
	case tea.MouseButtonMiddle:
		button = "middle"
	case tea.MouseButtonRight:
		button = "right"
	case tea.MouseButtonWheelUp:
		button = "wheel_up"
	case tea.MouseButtonWheelDown:
		button = "wheel_down"
	case tea.MouseButtonWheelLeft:
		button = "wheel_left"
	case tea.MouseButtonWheelRight:
		button = "wheel_right"
	case tea.MouseButtonBackward:
		button = "backward"
	case tea.MouseButtonForward:
		button = "forward"
	}
	switch msg.Action {
	case tea.MouseActionPress:
		action = "press"
	case tea.MouseActionRelease:
		action = "release"
	}
	return map[string]any{"x": x, "y": y, "button": button, "action": action, "shift": msg.Shift, "alt": msg.Alt, "ctrl": msg.Ctrl}
}

func (m Model) overlayPluginViews(rows []string) {
	border := lipgloss.NewStyle().Foreground(colorAccent).Background(colorBarBg)
	textStyle := lipgloss.NewStyle().Foreground(colorBarFg).Background(colorBarBg)
	for _, id := range m.pluginViewOrder {
		view := m.pluginViews[id]
		box := m.pluginViewBox(view)
		if box.w < 3 || box.h < 3 {
			continue
		}
		for row := 0; row < box.h && box.y+row < len(rows); row++ {
			var line string
			switch row {
			case 0:
				title := ansiCut(view.Title, 0, max(0, box.w-5))
				line = border.Render("+" + title + strings.Repeat("-", max(0, box.w-3-lipgloss.Width(title))) + "x+")
			case box.h - 1:
				line = border.Render("+" + strings.Repeat("-", box.w-2) + "+")
			default:
				text := ""
				if row-1 < len(view.Lines) {
					text = ansiCut(view.Lines[row-1], 0, box.w-2)
				}
				text += strings.Repeat(" ", max(0, box.w-2-lipgloss.Width(text)))
				line = border.Render("|") + textStyle.Render(text) + border.Render("|")
			}
			rows[box.y+row] = overlayAt(rows[box.y+row], line, box.x, box.w)
		}
	}
}

func (m Model) resizePluginViews() {
	for _, view := range m.pluginViews {
		box := m.pluginViewBox(view)
		m.plugins.Publish(view.owner, view.generation, "view.resize", map[string]any{"id": view.ID, "width": max(0, box.w-2), "height": max(0, box.h-2)})
	}
}
