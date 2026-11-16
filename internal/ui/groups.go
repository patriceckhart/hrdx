package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/patriceckhart/hrdx/internal/api"
	"github.com/patriceckhart/hrdx/internal/gitinfo"
	"github.com/patriceckhart/hrdx/internal/state"
)

func validateGroupPath(path []string) error { return state.ValidateGroupPath(path) }

func groupsForStatus(status api.Status) api.Groups { return api.GroupsFor(status) }

func gitGroupPath(cwd string) []string { return gitinfo.GroupPath(cwd) }

// Groups only rearrange sidebar rows; row targets retain their model indexes.
// The ordered tree keeps siblings in first-workspace order without moving PTYs.
func (m Model) groupSidebarRows(flat []sidebarRow) []sidebarRow {
	grouped := false
	for _, ws := range m.spaces {
		grouped = grouped || len(ws.groupPath) > 0
	}
	if !grouped {
		return flat
	}
	type entry struct {
		name     string
		space    int
		children []*entry
	}
	root := &entry{space: -1}
	blocks := make([][]sidebarRow, len(m.spaces))
	end := 3
	for i := 3; i < len(flat); i++ {
		row := flat[i]
		if row.kind != "space" && row.kind != "pane" && row.kind != "subrepo" && row.kind != "divider" {
			break
		}
		end = i + 1
		if row.kind != "divider" {
			blocks[row.space] = append(blocks[row.space], row)
		}
	}
	for index, ws := range m.spaces {
		node := root
		for _, name := range ws.groupPath {
			var child *entry
			for _, candidate := range node.children {
				if candidate.space == -1 && candidate.name == name {
					child = candidate
					break
				}
			}
			if child == nil {
				child = &entry{name: name, space: -1}
				node.children = append(node.children, child)
			}
			node = child
		}
		node.children = append(node.children, &entry{space: index})
	}
	rows := append([]sidebarRow(nil), flat[:3]...)
	width := m.sidebarContentWidth()
	var render func(*entry, int)
	render = func(node *entry, depth int) {
		indent := strings.Repeat(" ", min(depth*2, max(0, width/3)))
		if node.space >= 0 {
			for _, row := range blocks[node.space] {
				if row.fold {
					// Re-render narrower so the fold toggle keeps its place
					// at the right edge instead of being truncated away.
					rail := ansi.Cut(row.label, 0, 1)
					named := m.spaceNameRow(node.space, rail, width-len(indent))
					named.label = rail + indent + ansi.Cut(named.label, 1, ansi.StringWidth(named.label))
					rows = append(rows, named)
					continue
				}
				rail := ansi.Cut(row.label, 0, 1)
				content := ansi.Cut(row.label, 1, ansi.StringWidth(row.label))
				row.label = ansi.Truncate(rail+indent+content, width, "…")
				rows = append(rows, row)
			}
			return
		}
		if node != root {
			name := node.name
			if m.sideCollapsed {
				name = compactSidebarName(name, 6)
			}
			rows = append(rows, sidebarRow{label: ansi.Truncate(indent+" "+styleSection.Render(name), width, "…"), kind: "group", space: -1, tab: -1, pane: -1})
			depth++
		}
		for _, child := range node.children {
			render(child, depth)
		}
	}
	render(root, 0)
	return append(rows, flat[end:]...)
}
