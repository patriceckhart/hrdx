package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestGroupedSidebarSelectionRailStaysAtLeftEdge(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, path := range [][]string{{"Demo"}, {"Demo", "Reviews"}, {"Demo", "Reviews", "Ready"}} {
			model := newTestModel(t.TempDir())
			model.sideCollapsed = compact
			model.selected = 0
			model.spaces[0].groupPath = path
			count := 0
			for _, row := range model.sidebarRows() {
				if row.kind != "space" && row.kind != "pane" {
					continue
				}
				label := ansi.Strip(row.label)
				if !strings.HasPrefix(label, "▍") {
					t.Errorf("compact=%v depth=%d kind=%s: selection rail is not at left edge: %q", compact, len(path), row.kind, label)
				}
				if ansi.StringWidth(row.label) > model.sidebarContentWidth() {
					t.Errorf("row exceeds sidebar width: %q", label)
				}
				count++
			}
			if count < 2 {
				t.Fatal("expected workspace and pane rows")
			}
		}
	}
}
