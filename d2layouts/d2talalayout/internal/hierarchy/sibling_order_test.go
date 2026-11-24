package hierarchy

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

func TestEquivalentBranchTieBreakKeepsConstrainedSlots(t *testing.T) {
	g := layoutgraph.NewGraph()
	newMember := func(id int64, level int) *placementNode {
		n := layoutgraph.NewNode(id, 96, 52)
		g.AddNewNodeToContainer(nil, n)
		return newPlacementNode(level, n)
	}
	connect := func(from, to *placementNode) *layoutgraph.Edge {
		to.connect(from)
		return g.Connect(from.graphNode, to.graphNode)
	}
	parent := newMember(1, 0)
	first := newMember(9, 1)
	labeled := newMember(8, 1)
	through := newMember(7, 1)
	last := newMember(6, 1)
	sink := newMember(2, 2)
	connect(parent, first)
	connect(parent, last)
	connect(parent, through)
	connect(through, sink)
	connect(parent, labeled).Label = &layoutgraph.Label{Text: "status", Width: 60, Height: 22}

	levels := map[int][]*placementNode{0: {parent}, 1: {last, through, labeled, first}, 2: {sink}}
	orderEquivalentBranches(g, levels)
	if !slices.Equal(levels[1], []*placementNode{first, through, labeled, last}) {
		t.Fatal("expected only the two unlabeled leaves with identical neighbors to exchange slots")
	}
}
