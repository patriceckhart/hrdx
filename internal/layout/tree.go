// Package layout owns the binary split tree shared by every hrdx frontend:
// insertion, removal with sibling promotion, equalizing, completeness
// validation, the fallback layout, and conversion to and from state.Node.
// Pixel or cell geometry stays with each frontend.
package layout

import "github.com/patriceckhart/hrdx/internal/state"

// Node is a binary layout tree over pane handles of type P. A node is either
// a leaf holding one pane, or a split with two children. Vertical means the
// children sit side by side (the divider is a vertical line). The zero P
// marks a split node, so P must be a comparable handle such as a pointer.
type Node[P comparable] struct {
	Pane     P
	Vertical bool
	Ratio    float64
	A, B     *Node[P]
}

// Leaf returns a single-pane tree.
func Leaf[P comparable](pane P) *Node[P] { return &Node[P]{Pane: pane} }

// IsLeaf reports whether n holds a pane.
func (n *Node[P]) IsLeaf() bool {
	var zero P
	return n.Pane != zero
}

// Walk visits every pane in order, first child first.
func (n *Node[P]) Walk(visit func(P)) {
	if n == nil {
		return
	}
	if n.IsLeaf() {
		visit(n.Pane)
		return
	}
	n.A.Walk(visit)
	n.B.Walk(visit)
}

// Contains reports whether target is a leaf of n.
func (n *Node[P]) Contains(target P) bool {
	found := false
	n.Walk(func(current P) {
		if current == target {
			found = true
		}
	})
	return found
}

// InsertAt turns the leaf holding target into a split of target and add.
func InsertAt[P comparable](n *Node[P], target, add P, vertical bool) bool {
	return InsertAtSide(n, target, add, vertical, false)
}

// InsertAtSide splits the target leaf. With before the new pane takes the
// first slot (left of or above the target), otherwise the second.
func InsertAtSide[P comparable](n *Node[P], target, add P, vertical, before bool) bool {
	if n == nil {
		return false
	}
	if n.IsLeaf() {
		if n.Pane != target {
			return false
		}
		if before {
			n.A, n.B = Leaf(add), Leaf(target)
		} else {
			n.A, n.B = Leaf(target), Leaf(add)
		}
		var zero P
		n.Pane = zero
		n.Vertical = vertical
		n.Ratio = 0.5
		return true
	}
	return InsertAtSide(n.A, target, add, vertical, before) || InsertAtSide(n.B, target, add, vertical, before)
}

// RemoveAt deletes the leaf holding target, promoting its sibling.
func RemoveAt[P comparable](root **Node[P], target P) bool {
	node := *root
	if node == nil {
		return false
	}
	if node.IsLeaf() {
		if node.Pane == target {
			*root = nil
			return true
		}
		return false
	}
	if node.A.IsLeaf() && node.A.Pane == target {
		*root = node.B
		return true
	}
	if node.B.IsLeaf() && node.B.Pane == target {
		*root = node.A
		return true
	}
	return RemoveAt(&node.A, target) || RemoveAt(&node.B, target)
}

// Swap exchanges the leaves holding a and b. It reports false when either
// is missing or they are the same pane.
func Swap[P comparable](root *Node[P], a, b P) bool {
	if a == b {
		return false
	}
	var leafA, leafB *Node[P]
	var find func(n *Node[P])
	find = func(n *Node[P]) {
		if n == nil {
			return
		}
		if n.IsLeaf() {
			switch n.Pane {
			case a:
				leafA = n
			case b:
				leafB = n
			}
			return
		}
		find(n.A)
		find(n.B)
	}
	find(root)
	if leafA == nil || leafB == nil {
		return false
	}
	leafA.Pane, leafB.Pane = b, a
	return true
}

// Move takes the leaf holding pane out of the tree and splits target with
// it. vertical places them side by side; before puts pane left of or above
// target. The tree is unchanged when the move is impossible.
func Move[P comparable](root **Node[P], pane, target P, vertical, before bool) bool {
	if pane == target || !(*root).Contains(pane) || !(*root).Contains(target) {
		return false
	}
	if !RemoveAt(root, pane) {
		return false
	}
	return InsertAtSide(*root, target, pane, vertical, before)
}

// ParentSplit returns the split whose direct child is the leaf holding target.
func ParentSplit[P comparable](n *Node[P], target P) *Node[P] {
	if n == nil || n.IsLeaf() {
		return nil
	}
	if (n.A.IsLeaf() && n.A.Pane == target) || (n.B.IsLeaf() && n.B.Pane == target) {
		return n
	}
	if n.A.Contains(target) {
		return ParentSplit(n.A, target)
	}
	return ParentSplit(n.B, target)
}

// Equalize resets every split ratio below n to an even share.
func Equalize[P comparable](n *Node[P]) {
	if n == nil || n.IsLeaf() {
		return
	}
	n.Ratio = 0.5
	Equalize(n.A)
	Equalize(n.B)
}

// At follows path from n, 0 selecting A and 1 selecting B. Nil when the path
// leaves the tree.
func At[P comparable](n *Node[P], path []int) *Node[P] {
	for _, step := range path {
		if n == nil || n.IsLeaf() {
			return nil
		}
		switch step {
		case 0:
			n = n.A
		case 1:
			n = n.B
		default:
			return nil
		}
	}
	return n
}

// Complete verifies every pane appears exactly once in the tree and that
// the tree holds nothing else.
func Complete[P comparable](root *Node[P], panes []P) bool {
	seen := make(map[P]int)
	root.Walk(func(current P) { seen[current]++ })
	if len(seen) != len(panes) {
		return false
	}
	for _, current := range panes {
		if seen[current] != 1 {
			return false
		}
	}
	return true
}

// Fallback stacks all panes in side-by-side splits when the saved tree is
// missing or inconsistent with the pane list.
func Fallback[P comparable](panes []P) *Node[P] {
	if len(panes) == 0 {
		return nil
	}
	root := Leaf(panes[0])
	for _, current := range panes[1:] {
		InsertAt(root, panes[0], current, true)
	}
	return root
}

// ToState converts a tree to its saved form, referencing panes by their
// index. Panes missing from index (ephemeral ones) are dropped, and a split
// losing a child collapses to nil so restore falls back safely.
func ToState[P comparable](n *Node[P], index map[P]int) *state.Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if i, ok := index[n.Pane]; ok {
			return &state.Node{Pane: &i}
		}
		return nil
	}
	return &state.Node{
		Vertical: n.Vertical,
		Ratio:    n.Ratio,
		A:        ToState(n.A, index),
		B:        ToState(n.B, index),
	}
}

// FromState rebuilds a tree from its saved form. Nil when any index is out
// of range or a split is missing a child. Out-of-range ratios become 0.5.
func FromState[P comparable](n *state.Node, panes []P) *Node[P] {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		if *n.Pane < 0 || *n.Pane >= len(panes) {
			return nil
		}
		return Leaf(panes[*n.Pane])
	}
	a := FromState(n.A, panes)
	b := FromState(n.B, panes)
	if a == nil || b == nil {
		return nil
	}
	ratio := n.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.5
	}
	return &Node[P]{Vertical: n.Vertical, Ratio: ratio, A: a, B: b}
}

// Restore rebuilds a saved tree and falls back to the default layout when
// the saved one is missing or does not hold every pane exactly once.
func Restore[P comparable](n *state.Node, panes []P) *Node[P] {
	root := FromState(n, panes)
	if root == nil || !Complete(root, panes) {
		return Fallback(panes)
	}
	return root
}
