package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// findCandidate is one jump target of the fuzzy finder: a pane addressed
// by its workspace, tab, and pane indices.
type findCandidate struct {
	label      string
	spaceIndex int
	tabIndex   int
	paneIndex  int
}

// findVisibleRows caps the candidate list of the finder box.
const findVisibleRows = 10

func (m *Model) openFind() (tea.Model, tea.Cmd) {
	m.mode = modeFind
	m.findIndex = 0
	m.providerQuery = ""
	m.providerRows = nil
	m.input.Placeholder = "group, workspace, tab, or pane"
	m.input.SetValue("")
	m.input.Focus()
	return *m, textinput.Blink
}

func (m *Model) closeFind() {
	m.mode = modeTerminal
	m.providerQuery = ""
	m.providerRows = nil
	m.input.Blur()
}

// findCandidates lists every pane whose label matches the typed query,
// hierarchically labeled group › workspace › tab › pane.
func (m Model) findCandidates() []findCandidate {
	query := strings.ToLower(strings.TrimSpace(m.input.Value()))
	var out []findCandidate
	for spaceIndex, currentSpace := range m.spaces {
		for tabIndex, currentTab := range currentSpace.tabs {
			for paneIndex, currentPane := range currentTab.panes {
				label := currentSpace.name
				if len(currentSpace.groupPath) > 0 {
					label = strings.Join(currentSpace.groupPath, " › ") + " › " + label
				}
				if len(currentSpace.tabs) > 1 {
					label += " › " + strings.TrimSpace(m.tabLabel(currentTab, tabIndex))
				}
				label += " › " + m.paneDisplayName(currentPane)
				if fuzzyMatch(strings.ToLower(label), query) {
					out = append(out, findCandidate{label, spaceIndex, tabIndex, paneIndex})
				}
			}
		}
	}
	return out
}

// fuzzyMatch reports whether needle appears in haystack as a subsequence.
func fuzzyMatch(haystack, needle string) bool {
	runes := []rune(needle)
	if len(runes) == 0 {
		return true
	}
	pos := 0
	for _, r := range haystack {
		if r == runes[pos] {
			pos++
			if pos == len(runes) {
				return true
			}
		}
	}
	return false
}

// jumpTo focuses the candidate's pane and leaves find mode.
func (m *Model) jumpTo(chosen findCandidate) {
	if chosen.spaceIndex < 0 || chosen.spaceIndex >= len(m.spaces) {
		return
	}
	m.selected = chosen.spaceIndex
	owner := m.spaces[chosen.spaceIndex]
	owner.active = clampInt(chosen.tabIndex, 0, max(0, len(owner.tabs)-1))
	currentTab := owner.tab()
	currentTab.selected = clampInt(chosen.paneIndex, 0, max(0, len(currentTab.panes)-1))
	m.resizePanes(owner)
	m.clearFocusedAttention()
	m.closeFind()
}

func (m Model) updateFindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	candidates := m.findCandidates()
	total := len(candidates) + len(m.providerRows)
	switch m.navigationAction(msg) {
	case "navigate-up":
		if total > 0 {
			m.findIndex = (m.findIndex - 1 + total) % total
		}
		return m, nil
	case "navigate-down":
		if total > 0 {
			m.findIndex = (m.findIndex + 1) % total
		}
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.closeFind()
		return m, nil
	case "ctrl+p":
		if total > 0 {
			m.findIndex = (m.findIndex - 1 + total) % total
		}
		return m, nil
	case "ctrl+n":
		if total > 0 {
			m.findIndex = (m.findIndex + 1) % total
		}
		return m, nil
	case "enter":
		index := clampInt(m.findIndex, 0, max(0, total-1))
		if index < len(candidates) {
			m.jumpTo(candidates[index])
			return m, nil
		}
		if index-len(candidates) < len(m.providerRows) {
			return m, m.selectProviderRow(m.providerRows[index-len(candidates)])
		}
		m.closeFind()
		return m, nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.findIndex = 0
		cmd = tea.Batch(cmd, m.pluginProviderQuery())
	}
	return m, cmd
}

// findBox centers the finder over the body, sized to the widest label.
func (m Model) findBox() rect {
	width := 36
	candidates := m.findCandidates()
	for _, candidate := range candidates {
		if w := lipgloss.Width(candidate.label) + 6; w > width {
			width = w
		}
	}
	for _, candidate := range m.providerRows {
		if w := lipgloss.Width(candidate.label()) + 6; w > width {
			width = w
		}
	}
	bodyW := max(1, m.width)
	bodyH := max(1, m.height-2)
	width = min(width, max(20, bodyW-4))
	rows := min(len(candidates)+len(m.providerRows), findVisibleRows)
	if rows == 0 {
		rows = 1
	}
	height := rows + 4 // top border, query, separator, rows, bottom border
	return rect{
		x: clampInt((bodyW-width)/2, 0, max(0, bodyW-width)),
		y: clampInt((bodyH-height)/2, 0, max(0, bodyH-height)),
		w: width, h: height,
	}
}

// overlayFind draws the finder box over the composed body rows.
func (m Model) overlayFind(bodyRows []string) {
	box := m.findBox()
	border := lipgloss.NewStyle().Foreground(colorAccent)
	normal := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorBarFg)
	active := lipgloss.NewStyle().Background(colorAccent).Foreground(colorInk).Bold(true)
	muted := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorMuted)
	faint := lipgloss.NewStyle().Background(colorBarBg).Foreground(colorFaint)

	innerW := box.w - 2
	pad := func(text string) string {
		return text + strings.Repeat(" ", max(0, innerW-lipgloss.Width(text)))
	}

	candidates := m.findCandidates()
	labels := make([]string, 0, len(candidates)+len(m.providerRows))
	for _, candidate := range candidates {
		labels = append(labels, candidate.label)
	}
	for _, candidate := range m.providerRows {
		labels = append(labels, candidate.label())
	}
	selected := clampInt(m.findIndex, 0, max(0, len(labels)-1))
	start := 0
	if selected >= findVisibleRows {
		start = selected - findVisibleRows + 1
	}

	title := " find "
	lines := make([]string, 0, box.h)
	lines = append(lines, border.Render("╭"+title+strings.Repeat("─", max(0, innerW-len(title)))+"╮"))
	query := " > " + m.input.Value()
	lines = append(lines, border.Render("│")+normal.Render(pad(truncate(query, innerW)))+border.Render("│"))
	lines = append(lines, border.Render("│")+faint.Render(pad(" "+strings.Repeat("─", max(0, innerW-2))+" "))+border.Render("│"))
	if len(labels) == 0 {
		lines = append(lines, border.Render("│")+muted.Render(pad("  no matches"))+border.Render("│"))
	}
	for index := start; index < len(labels) && index < start+findVisibleRows; index++ {
		style := normal
		if index == selected {
			style = active
		}
		lines = append(lines, border.Render("│")+style.Render(pad(truncate("  "+labels[index], innerW)))+border.Render("│"))
	}
	lines = append(lines, border.Render("╰"+strings.Repeat("─", max(0, innerW))+"╯"))

	for i, line := range lines {
		y := box.y + i
		if y < 0 || y >= len(bodyRows) {
			continue
		}
		bodyRows[y] = overlayAt(bodyRows[y], line, box.x, box.w)
	}
}
