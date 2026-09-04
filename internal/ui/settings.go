package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The settings window is a centered modal with tabbed sections. It reuses
// the body overlay technique of the context menu but owns its own input
// mode, so tab switching and toggling never leak into the panes.

// settingsRow is one toggleable line of the active settings section.
type settingsRow struct {
	label  string
	action string
}

var settingsTabNames = []string{"agents", "notification", "theme", "terminal", "worktree"}

func settingsCheck(on bool) string {
	if on {
		return "[x] "
	}
	return "[ ] "
}

func (m *Model) openSettings() {
	m.mode = modeSettings
	m.settingsIndex = 0
}

func (m *Model) closeSettings() {
	m.mode = modeTerminal
}

// settingsRows returns the rows of the active section.
func (m Model) settingsRows() []settingsRow {
	switch m.settingsTab {
	case 1: // notification
		rows := []settingsRow{
			{settingsCheck(m.soundOn) + "play a sound when an agent finishes", "sound"},
		}
		for _, kind := range soundKindList() {
			mark := "( ) "
			if m.soundKind == kind {
				mark = "(•) "
			}
			rows = append(rows, settingsRow{"  " + mark + kind, "sound:" + kind})
		}
		rows = append(rows, settingsRow{
			settingsCheck(m.notifyOn) + "system notification", "notify",
		})
		return rows
	case 2: // theme
		var rows []settingsRow
		for _, name := range themeNames() {
			mark := "( ) "
			if m.themeName == name {
				mark = "(•) "
			}
			rows = append(rows, settingsRow{mark + name, "theme:" + name})
		}
		return rows
	case 3: // terminal
		return []settingsRow{{settingsCheck(m.autoCopy) + "automatically copy selected text", "auto-copy"}}
	case 4: // worktree
		command := truncate("command: "+m.worktreeCommand(), 48)
		return []settingsRow{{command, "worktree-command"}}
	default: // agents
		installed := map[string]bool{}
		for _, kind := range m.installedAgents() {
			installed[kind] = true
		}
		var rows []settingsRow
		for _, spec := range agentSpecs {
			box := "[x] "
			if m.disabled[spec.kind] {
				box = "[ ] "
			}
			label := box + spec.kind
			if spec.custom {
				label += " (custom)"
			}
			if !installed[spec.kind] {
				label += " (not installed)"
			}
			rows = append(rows, settingsRow{label, "toggle:" + spec.kind})
		}
		return rows
	}
}

// toggleSettingsRow executes one row's action.
func (m *Model) toggleSettingsRow(row settingsRow) tea.Cmd {
	if kind, ok := strings.CutPrefix(row.action, "toggle:"); ok {
		return m.toggleAgent(kind)
	}
	if kind, ok := strings.CutPrefix(row.action, "sound:"); ok {
		m.soundKind = kind
		m.persist()
		// Preview so the choice is audible immediately.
		playSound(kind)
		return nil
	}
	if name, ok := strings.CutPrefix(row.action, "theme:"); ok {
		m.themeName = name
		applyTheme(name)
		m.persist()
		return nil
	}
	if row.action == "sound" {
		m.soundOn = !m.soundOn
		m.persist()
		if m.soundOn {
			playSound(m.soundKind)
		}
	}
	if row.action == "notify" {
		m.notifyOn = !m.notifyOn
		m.persist()
		if m.notifyOn {
			systemNotify()
		}
	}
	if row.action == "auto-copy" {
		m.autoCopy = !m.autoCopy
		m.persist()
	}
	if row.action == "worktree-command" {
		m.mode = modeNewSpace
		m.worktreeEdit = true
		m.input.Placeholder = "worktree command"
		m.input.SetValue(m.worktreeCommand())
		m.input.CursorEnd()
		m.input.Focus()
		return textinput.Blink
	}
	return nil
}

const settingsHint = "enter toggle   tab section   esc close"

// settingsBox returns the modal rect in body coordinates (header excluded),
// centered and sized for the widest section so switching tabs never
// reshapes the window.
func (m Model) settingsBox() rect {
	width := lipgloss.Width(settingsHint)
	rowCount := 0
	tab := m.settingsTab
	for index := range settingsTabNames {
		probe := m
		probe.settingsTab = index
		rows := probe.settingsRows()
		if len(rows) > rowCount {
			rowCount = len(rows)
		}
		for _, row := range rows {
			if w := lipgloss.Width(row.label); w > width {
				width = w
			}
		}
	}
	m.settingsTab = tab
	width += 6 // border + padding
	// The tab row has one leading cell and one trailing spacer per label.
	// Include its full width so it cannot extend beyond the modal border.
	tabsWidth := 1
	for _, name := range settingsTabNames {
		tabsWidth += lipgloss.Width(name) + 3
	}
	width = max(width, tabsWidth+2) // left and right border
	bodyW := max(1, m.width)
	bodyH := max(1, m.height-2)
	// top border, tabs, separator, rows, blank, hint, bottom border. Long
	// sections scroll within the available body instead of leaving the screen.
	height := min(rowCount+6, bodyH)
	return rect{
		x: clampInt((bodyW-width)/2, 0, max(0, bodyW-width)),
		y: clampInt((bodyH-height)/2, 0, max(0, bodyH-height)),
		w: width, h: height,
	}
}

// visibleSettingsRows returns the rows that fit in the settings box and the
// index of the first one. The selected row is always kept in view.
func (m Model) visibleSettingsRows(box rect) ([]settingsRow, int) {
	rows := m.settingsRows()
	limit := max(1, box.h-6)
	if len(rows) <= limit {
		return rows, 0
	}
	offset := clampInt(m.settingsIndex-limit+1, 0, len(rows)-limit)
	return rows[offset : offset+limit], offset
}

// settingsTabCells returns the clickable x ranges of the tab labels,
// relative to the box's left edge.
func (m Model) settingsTabCells() []tabCell {
	cells := make([]tabCell, 0, len(settingsTabNames))
	x := 2
	for index, name := range settingsTabNames {
		width := len(name) + 2
		cells = append(cells, tabCell{from: x, to: x + width, index: index})
		x += width + 1
	}
	return cells
}

func (m Model) updateSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.settingsRows()
	switch m.navigationAction(msg) {
	case "navigate-up":
		m.settingsIndex = (m.settingsIndex - 1 + len(rows)) % len(rows)
		return m, nil
	case "navigate-down":
		m.settingsIndex = (m.settingsIndex + 1) % len(rows)
		return m, nil
	}
	switch msg.String() {
	case "esc", "q", "ctrl+c":
		m.closeSettings()
		return m, nil
	case "tab", "right", "l":
		m.settingsTab = (m.settingsTab + 1) % len(settingsTabNames)
		m.settingsIndex = 0
		return m, nil
	case "shift+tab", "left", "h":
		m.settingsTab = (m.settingsTab - 1 + len(settingsTabNames)) % len(settingsTabNames)
		m.settingsIndex = 0
		return m, nil
	case "enter", " ":
		if m.settingsIndex >= 0 && m.settingsIndex < len(rows) {
			return m, m.toggleSettingsRow(rows[m.settingsIndex])
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateSettingsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	box := m.settingsBox()
	bodyX, bodyY := msg.X, msg.Y-1
	rows := m.settingsRows()
	visible, offset := m.visibleSettingsRows(box)

	if box.hit(bodyX, bodyY) && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		delta := -1
		if msg.Button == tea.MouseButtonWheelDown {
			delta = 1
		}
		m.settingsIndex = clampInt(m.settingsIndex+delta, 0, len(rows)-1)
		return m, nil
	}

	// Hovering highlights rows, like the context menu.
	if msg.Action == tea.MouseActionMotion && box.hit(bodyX, bodyY) {
		index := bodyY - box.y - 3
		if index >= 0 && index < len(visible) {
			m.settingsIndex = offset + index
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if !box.hit(bodyX, bodyY) {
		m.closeSettings()
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// Tab row.
	if bodyY == box.y+1 {
		for _, cell := range m.settingsTabCells() {
			if bodyX-box.x >= cell.from && bodyX-box.x < cell.to {
				m.settingsTab = cell.index
				m.settingsIndex = 0
				return m, nil
			}
		}
		return m, nil
	}
	// Content rows.
	index := bodyY - box.y - 3
	if index >= 0 && index < len(visible) {
		m.settingsIndex = offset + index
		return m, m.toggleSettingsRow(rows[m.settingsIndex])
	}
	return m, nil
}

// overlaySettings draws the settings window over the composed body rows.
func (m Model) overlaySettings(bodyRows []string) {
	box := m.settingsBox()
	border := lipgloss.NewStyle().Foreground(colorAccent)
	fill := lipgloss.NewStyle().Background(colorBarBg)
	normal := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorBarFg)
	active := lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk).Bold(true)
	muted := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorMuted)
	faint := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorFaint)

	innerW := box.w - 2
	pad := func(text string, used int) string {
		return text + strings.Repeat(" ", max(0, innerW-used))
	}

	title := " settings "
	top := "╭" + title + strings.Repeat("─", max(0, innerW-len(title))) + "╮"

	var tabs strings.Builder
	tabs.WriteString(fill.Render(" "))
	used := 1
	for index, name := range settingsTabNames {
		label := " " + name + " "
		if index == m.settingsTab {
			tabs.WriteString(active.Render(label))
		} else {
			tabs.WriteString(muted.Render(label))
		}
		tabs.WriteString(fill.Render(" "))
		used += lipgloss.Width(label) + 1
	}
	tabs.WriteString(fill.Render(strings.Repeat(" ", max(0, innerW-used))))

	rows, offset := m.visibleSettingsRows(box)
	lines := make([]string, 0, box.h)
	lines = append(lines, border.Render(top))
	lines = append(lines, border.Render("│")+tabs.String()+border.Render("│"))
	lines = append(lines, border.Render("│")+faint.Render(" "+strings.Repeat("─", max(0, innerW-2))+" ")+border.Render("│"))
	for index, row := range rows {
		style := normal
		if offset+index == m.settingsIndex {
			style = active
		}
		lines = append(lines, border.Render("│")+style.Render(pad("  "+row.label, lipgloss.Width(row.label)+2))+border.Render("│"))
	}
	lines = append(lines, border.Render("│")+fill.Render(pad("", 0))+border.Render("│"))
	lines = append(lines, border.Render("│")+muted.Render(pad("  "+settingsHint, lipgloss.Width(settingsHint)+2))+border.Render("│"))
	lines = append(lines, border.Render("╰"+strings.Repeat("─", innerW)+"╯"))

	for i, line := range lines {
		y := box.y + i
		if y < 0 || y >= len(bodyRows) {
			continue
		}
		bodyRows[y] = overlayAt(bodyRows[y], line, box.x, box.w)
	}
}
