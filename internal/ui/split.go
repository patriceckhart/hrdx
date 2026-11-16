package ui

import "github.com/patriceckhart/hrdx/internal/layout"

// splitNode is the shared layout tree specialized to live TUI panes.
type splitNode = layout.Node[*pane]

func leafNode(target *pane) *splitNode { return layout.Leaf(target) }

func insertAt(n *splitNode, target, add *pane, vertical bool) bool {
	return layout.InsertAt(n, target, add, vertical)
}

func insertAtSide(n *splitNode, target, add *pane, vertical, before bool) bool {
	return layout.InsertAtSide(n, target, add, vertical, before)
}

func removeAt(root **splitNode, target *pane) bool { return layout.RemoveAt(root, target) }

type rect struct{ x, y, w, h int }

type paneRect struct {
	pane *pane
	r    rect
}

type divRect struct {
	node *splitNode // the split this boundary belongs to
	r    rect       // grabbable boundary cells (adjacent pane borders)
	full rect       // the split's whole rect, for drag ratio math
}

const (
	minPaneCols = 10
	minPaneRows = 4
)

// splitRects computes the child rects of a split node. Panes touch; each
// pane draws its own border, so the boundary is the two adjacent border
// cells, returned as the grabbable divider strip.
func splitRects(n *splitNode, r rect) (ra, rd, rb rect) {
	if n.Vertical {
		aw := int(float64(r.w) * n.Ratio)
		aw = clampInt(aw, minPaneCols, r.w-minPaneCols)
		ra = rect{r.x, r.y, aw, r.h}
		rb = rect{r.x + aw, r.y, r.w - aw, r.h}
		rd = rect{r.x + aw - 1, r.y, 2, r.h}
		return ra, rd, rb
	}
	ah := int(float64(r.h) * n.Ratio)
	ah = clampInt(ah, minPaneRows, r.h-minPaneRows)
	ra = rect{r.x, r.y, r.w, ah}
	rb = rect{r.x, r.y + ah, r.w, r.h - ah}
	rd = rect{r.x, r.y + ah - 1, r.w, 2}
	return ra, rd, rb
}

func layoutNode(n *splitNode, r rect, panes *[]paneRect, divs *[]divRect) {
	if n == nil {
		return
	}
	if n.Pane != nil {
		*panes = append(*panes, paneRect{n.Pane, r})
		return
	}
	ra, rd, rb := splitRects(n, r)
	*divs = append(*divs, divRect{node: n, r: rd, full: r})
	layoutNode(n.A, ra, panes, divs)
	layoutNode(n.B, rb, panes, divs)
}

func clampInt(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func clampFloat(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func (r rect) hit(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// inner returns the content rect inside a pane's border.
func (r rect) inner() rect {
	return rect{r.x + 1, r.y + 1, max(1, r.w-2), max(1, r.h-2)}
}
