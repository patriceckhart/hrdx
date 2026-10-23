package ui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Folding is purely visual: folded panes keep running and stay reachable
// through keys, the finder, and the socket API.

const (
	foldOpenGlyph   = "▾"
	foldClosedGlyph = "▸"
)

// spaceNameRow renders a workspace name with its fold toggle at the right
// edge. A folded row also summarizes its panes: a spinner while any agent
// works, an attention dot when one finished unseen, and the pane count.
func (m Model) spaceNameRow(spaceIndex int, rail string, width int) sidebarRow {
	currentSpace := m.spaces[spaceIndex]
	glyph := foldOpenGlyph
	summary, summaryWidth := "", 0
	if currentSpace.folded {
		glyph = foldClosedGlyph
		summary, summaryWidth = m.foldSummary(currentSpace)
	}
	nameWidth := max(1, width-1-summaryWidth-1-lipgloss.Width(glyph)-1)
	name := truncate(currentSpace.name, nameWidth)
	gap := max(1, width-1-lipgloss.Width(name)-summaryWidth-lipgloss.Width(glyph)-1)
	return sidebarRow{
		label: rail + styleSpaceDim.Render(name) + summary + strings.Repeat(" ", gap) + styleNewButton.Render(glyph) + " ",
		kind:  "space", space: spaceIndex, tab: -1, pane: -1, fold: true,
	}
}

func (m Model) foldSummary(target *space) (string, int) {
	panes, busy, attention := 0, false, false
	focused := m.currentPane()
	for _, currentTab := range target.tabs {
		for _, currentPane := range currentTab.panes {
			panes++
			titleState := m.paneAgentTitleState(currentPane)
			if m.paneBusyWithTitle(currentPane, titleState) {
				busy = true
			}
			if m.paneAttention[currentPane.id] ||
				titleState == "attention" && (m.appBlurred || focused != currentPane) {
				attention = true
			}
		}
	}
	text := " " + strconv.Itoa(panes)
	out := stylePaneDim.Render(text)
	switch {
	case busy:
		out = " " + styleDotBusy.Render(spinnerFrames[m.spinFrame%len(spinnerFrames)]) + out
		text = "  " + text
	case attention:
		out = " " + styleDotBusy.Render("●") + out
		text = "  " + text
	}
	return out, lipgloss.Width(text)
}

// foldToggleHit locates the toggle in the rendered label, so group
// indentation is accounted for.
func foldToggleHit(row sidebarRow, x int) bool {
	if !row.fold {
		return false
	}
	plain := ansi.Strip(row.label)
	for _, glyph := range []string{foldOpenGlyph, foldClosedGlyph} {
		if index := strings.LastIndex(plain, glyph); index >= 0 {
			return x == ansi.StringWidth(plain[:index])
		}
	}
	return false
}

func (m *Model) toggleSpaceFold(target *space) {
	if target == nil {
		return
	}
	target.folded = !target.folded
	m.sideScroll = 0
	m.persist()
}
