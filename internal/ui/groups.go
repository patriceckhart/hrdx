package ui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
	"github.com/patriceckhart/hrdx/internal/api"
)

func validateGroupPath(path []string) error {
	size := 0
	for _, name := range path {
		size += len(name) + 1
		if !utf8.ValidString(name) || strings.TrimSpace(name) == "" || len([]rune(name)) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return fmt.Errorf("group names must contain 1 to 80 nonblank characters and no controls")
		}
	}
	if size > 4096 {
		return fmt.Errorf("group_path must be at most 4096 bytes including separators")
	}
	return nil
}

func groupsForStatus(status api.Status) api.Groups {
	result := api.Groups{Type: "groups", Groups: []api.GroupStatus{}}
	seen := map[string]bool{}
	for _, ws := range status.Workspaces {
		for depth := 1; depth <= len(ws.GroupPath); depth++ {
			path := ws.GroupPath[:depth]
			key := strings.Join(path, "\x00")
			if !seen[key] {
				seen[key] = true
				result.Groups = append(result.Groups, api.GroupStatus{GroupPath: append([]string(nil), path...)})
			}
		}
	}
	return result
}

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
		if row.kind != "space" && row.kind != "pane" && row.kind != "divider" {
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
