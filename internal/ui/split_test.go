package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSplitInsertLayoutRemove(t *testing.T) {
	a := &pane{id: 1}
	b := &pane{id: 2}
	c := &pane{id: 3}

	root := leafNode(a)
	if !insertAt(root, a, b, true) {
		t.Fatal("insertAt a/b failed")
	}
	if !insertAt(root, b, c, false) {
		t.Fatal("insertAt b/c failed")
	}

	var panes []paneRect
	var divs []divRect
	layoutNode(root, rect{0, 0, 100, 40}, &panes, &divs)
	if len(panes) != 3 || len(divs) != 2 {
		t.Fatalf("panes=%d divs=%d, want 3/2", len(panes), len(divs))
	}

	// Every pane rect must respect minimums and stay inside the area.
	for _, pr := range panes {
		if pr.r.w < minPaneCols || pr.r.h < minPaneRows {
			t.Fatalf("pane %d rect %+v below minimum", pr.pane.id, pr.r)
		}
		if pr.r.x < 0 || pr.r.y < 0 || pr.r.x+pr.r.w > 100 || pr.r.y+pr.r.h > 40 {
			t.Fatalf("pane %d rect %+v out of bounds", pr.pane.id, pr.r)
		}
	}

	// Removing b promotes c into b's slot; two panes remain.
	if !removeAt(&root, b) {
		t.Fatal("removeAt b failed")
	}
	panes = nil
	divs = nil
	layoutNode(root, rect{0, 0, 100, 40}, &panes, &divs)
	if len(panes) != 2 || len(divs) != 1 {
		t.Fatalf("after remove: panes=%d divs=%d, want 2/1", len(panes), len(divs))
	}
	for _, pr := range panes {
		if pr.pane == b {
			t.Fatal("removed pane still present")
		}
	}
}

func TestDividerDragChangesRatio(t *testing.T) {
	model := newTestModel("/tmp/api")
	currentSpace := model.spaces[0]
	model.addPane(currentSpace, "shell", true)

	_, divs := model.layoutAll(currentSpace.tab())
	if len(divs) != 1 {
		t.Fatalf("divs = %d, want 1", len(divs))
	}
	model.drag = divs[0].node
	model.dragFull = divs[0].full
	model.applyDrag(divs[0].full.x+divs[0].full.w/4, 0)
	if ratio := divs[0].node.Ratio; ratio > 0.3 {
		t.Fatalf("ratio = %f, want roughly 0.25", ratio)
	}
}

func TestFloatingLayoutAnchorsAndClamps(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.width = 127
	model.height = 43
	currentTab := model.spaces[0].tab()
	area := model.terminalArea()

	left := model.addFloatingPane(model.spaces[0], "shell", "left", 40, 30)
	tiny := model.addFloatingPane(model.spaces[0], "shell", "bottom", 1, 1)
	floating := model.floatingLayout(currentTab)
	if len(floating) != 2 {
		t.Fatalf("floating rects = %d, want 2", len(floating))
	}
	if floating[0].pane != left || floating[0].r.x != area.x || floating[0].r.y != (area.h-floating[0].r.h)/2 {
		t.Fatalf("left rect = %+v in area %+v", floating[0].r, area)
	}
	if floating[1].pane != tiny || floating[1].r.h != minPaneRows || floating[1].r.w != minPaneCols ||
		floating[1].r.y+floating[1].r.h != area.y+area.h {
		t.Fatalf("clamped bottom rect = %+v in area %+v", floating[1].r, area)
	}
}

func TestFloatingPaneIsHiddenFromSidebarAndClosesFromTitle(t *testing.T) {
	model := newTestModel("/tmp/api")
	model.width = 127
	model.height = 43
	currentSpace := model.spaces[0]
	floating := model.addFloatingPane(currentSpace, "shell", "center", 40, 30)

	paneRows := 0
	for _, row := range model.sidebarRows() {
		if row.kind == "pane" {
			paneRows++
		}
	}
	if paneRows != 1 {
		t.Fatalf("sidebar pane rows = %d, want only the split pane", paneRows)
	}

	placement := model.floatingLayout(currentSpace.tab())[0].r
	_, _ = model.updateMouse(tea.MouseMsg{
		X:      sidebarWidth + 1 + placement.x + placement.w - 3,
		Y:      2 + placement.y,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	})
	if found, _ := model.paneByID(floating.id); found != nil {
		t.Fatal("floating pane still exists after title-bar close click")
	}
}

func TestInsertAtSideBefore(t *testing.T) {
	first := &pane{id: 1, name: "a"}
	second := &pane{id: 2, name: "b"}
	root := leafNode(first)
	if !insertAtSide(root, first, second, true, true) {
		t.Fatal("insertAtSide failed")
	}
	if root.A.Pane != second || root.B.Pane != first {
		t.Fatalf("order = %v/%v, want new pane first", root.A.Pane.name, root.B.Pane.name)
	}
	if !root.Vertical {
		t.Fatal("split should be vertical")
	}
}
