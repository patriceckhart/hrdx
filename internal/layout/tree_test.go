package layout

import (
	"testing"

	"github.com/patriceckhart/hrdx/internal/state"
)

type handle struct{ id int }

func panes(n int) []*handle {
	out := make([]*handle, n)
	for i := range out {
		out[i] = &handle{i}
	}
	return out
}

func TestInsertRemovePromotesSibling(t *testing.T) {
	p := panes(3)
	root := Leaf(p[0])
	if !InsertAt(root, p[0], p[1], true) || !InsertAt(root, p[1], p[2], false) {
		t.Fatal("insert failed")
	}
	root.B.Ratio = 0.3
	if !Complete(root, p) {
		t.Fatal("tree incomplete after inserts")
	}
	if !RemoveAt(&root, p[0]) {
		t.Fatal("remove failed")
	}
	if root.IsLeaf() || root.Vertical || root.Ratio != 0.3 {
		t.Fatalf("sibling not promoted intact: %+v", root)
	}
	if !Complete(root, p[1:]) {
		t.Fatal("tree incomplete after remove")
	}
	if RemoveAt(&root, p[0]) {
		t.Fatal("removed a pane twice")
	}
}

func TestInsertBeforeAndParent(t *testing.T) {
	p := panes(2)
	root := Leaf(p[0])
	InsertAtSide(root, p[0], p[1], true, true)
	if root.A.Pane != p[1] || root.B.Pane != p[0] {
		t.Fatal("before insert misordered")
	}
	if ParentSplit(root, p[0]) != root {
		t.Fatal("parent not found")
	}
}

func TestSwapAndMove(t *testing.T) {
	p := panes(3)
	root := Leaf(p[0])
	InsertAt(root, p[0], p[1], true)
	InsertAt(root, p[1], p[2], false)
	if !Swap(root, p[0], p[2]) || root.A.Pane != p[2] || root.B.B.Pane != p[0] {
		t.Fatal("swap failed")
	}
	if Swap(root, p[0], p[0]) || Swap(root, p[0], &handle{9}) {
		t.Fatal("invalid swap accepted")
	}
	// Move p[2] (left) below p[1]: the right split is promoted to the root,
	// then p[1] splits into p[1] above p[2].
	if !Move(&root, p[2], p[1], false, false) {
		t.Fatal("move failed")
	}
	if !Complete(root, p) {
		t.Fatal("tree after move incomplete")
	}
	parent := ParentSplit(root, p[2])
	if parent == nil || parent.Vertical || parent.A.Pane != p[1] || parent.B.Pane != p[2] {
		t.Fatal("moved pane not below target")
	}
	if Move(&root, p[1], p[1], true, false) {
		t.Fatal("move onto itself accepted")
	}
	// Moving the only other leaf next to a sibling keeps every pane once.
	two := Leaf(p[0])
	InsertAt(two, p[0], p[1], true)
	if !Move(&two, p[0], p[1], false, true) || !Complete(two, p[:2]) || two.Vertical || two.A.Pane != p[0] {
		t.Fatal("two-pane move wrong")
	}
}

func TestEqualizeAndAt(t *testing.T) {
	p := panes(3)
	root := Leaf(p[0])
	InsertAt(root, p[0], p[1], true)
	InsertAt(root, p[1], p[2], false)
	root.Ratio, root.B.Ratio = 0.2, 0.8
	Equalize(root)
	if root.Ratio != 0.5 || root.B.Ratio != 0.5 {
		t.Fatal("equalize missed a split")
	}
	if At(root, []int{1}) != root.B || At(root, []int{1, 1}).Pane != p[2] {
		t.Fatal("path lookup wrong")
	}
	if At(root, []int{0, 0}) != nil || At(root, []int{2}) != nil {
		t.Fatal("invalid path accepted")
	}
}

func TestStateRoundTripAndRestoreFallback(t *testing.T) {
	p := panes(3)
	root := Leaf(p[0])
	InsertAt(root, p[0], p[1], true)
	InsertAt(root, p[1], p[2], false)
	index := map[*handle]int{p[0]: 0, p[1]: 1, p[2]: 2}
	saved := ToState(root, index)
	back := Restore(saved, p)
	if !Complete(back, p) || !back.Vertical || back.B.Vertical {
		t.Fatal("round trip changed the tree")
	}

	bad := 7
	for name, node := range map[string]*state.Node{
		"nil":          nil,
		"out of range": {Pane: &bad},
		"missing pane": ToState(Leaf(p[0]), index),
		"half split":   {Vertical: true, A: &state.Node{Pane: new(int)}},
	} {
		got := Restore(node, p)
		if !Complete(got, p) {
			t.Fatalf("%s: fallback incomplete", name)
		}
	}
	if Restore[*handle](nil, nil) != nil {
		t.Fatal("empty pane list should give nil")
	}
}

func TestToStateDropsEphemeral(t *testing.T) {
	p := panes(2)
	root := Leaf(p[0])
	InsertAt(root, p[0], p[1], true)
	saved := ToState(root, map[*handle]int{p[0]: 0})
	if saved == nil || saved.B != nil {
		t.Fatalf("ephemeral pane persisted: %+v", saved)
	}
}

func TestFromStateClampsRatio(t *testing.T) {
	zero, one := 0, 1
	n := FromState(&state.Node{Ratio: 1.5, A: &state.Node{Pane: &zero}, B: &state.Node{Pane: &one}}, panes(2))
	if n.Ratio != 0.5 {
		t.Fatalf("ratio = %v", n.Ratio)
	}
}
