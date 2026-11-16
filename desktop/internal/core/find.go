package core

import (
	"strconv"
	"strings"
)

// FindCandidate is one finder jump target.
type FindCandidate struct {
	Label  string `json:"label"`
	PaneID int    `json:"pane_id"`
}

// fuzzyMatch reports whether needle appears in haystack as a subsequence.
// Same rule as the TUI finder.
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

// tabLabel mirrors the TUI's default tab label: its name, or its index and
// the first pane's kind.
func tabLabel(current *tab, index int) string {
	if current.name != "" {
		return current.name
	}
	label := strconv.Itoa(index + 1)
	if len(current.panes) > 0 {
		label += " " + current.panes[0].kind
	}
	return label
}

func (m *model) find(query string) []FindCandidate {
	query = strings.ToLower(strings.TrimSpace(query))
	out := []FindCandidate{}
	for _, current := range m.spaces {
		for tabIndex, currentTab := range current.tabs {
			for _, target := range currentTab.panes {
				label := current.name
				if len(current.groupPath) > 0 {
					label = strings.Join(current.groupPath, " › ") + " › " + label
				}
				if len(current.tabs) > 1 {
					label += " › " + tabLabel(currentTab, tabIndex)
				}
				name := target.name
				if kind := m.paneAgentKind(target); kind != "" && kind != target.kind {
					name = kind + " (" + target.name + ")"
				}
				label += " › " + name
				if fuzzyMatch(strings.ToLower(label), query) {
					out = append(out, FindCandidate{Label: label, PaneID: target.id})
				}
			}
		}
	}
	return out
}

// Find returns the finder candidates matching query.
func (c *Core) Find(query string) ([]FindCandidate, error) {
	var out []FindCandidate
	err := c.do(func() error { out = c.m.find(query); return nil })
	return out, err
}
