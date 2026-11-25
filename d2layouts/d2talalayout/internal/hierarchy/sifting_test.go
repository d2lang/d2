package hierarchy

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

func newSiftingTestGraph() map[int][]*placementNode {
	g := layoutgraph.NewGraph()
	a := g.AddNode(layoutgraph.NewNode(1, 10, 10))
	b := g.AddNode(layoutgraph.NewNode(2, 10, 10))
	c := g.AddNode(layoutgraph.NewNode(3, 10, 10))
	d := g.AddNode(layoutgraph.NewNode(4, 10, 10))
	e := g.AddNode(layoutgraph.NewNode(5, 10, 10))
	f := g.AddNode(layoutgraph.NewNode(6, 10, 10))
	g.Connect(a, f)
	g.Connect(b, e)
	g.Connect(c, d)

	hierarchy := newHierarchyWithLevels(map[*layoutgraph.Node]int{
		a: 0,
		b: 0,
		c: 0,
		d: 1,
		e: 1,
		f: 1,
	})
	for node := range hierarchy.Levels() {
		node.Hierarchy = hierarchy
	}

	nodes := createPlacementNodes(g, g.Nodes, nil)
	connectPlacementNodes(g, nodes)
	byLevel := groupPlacementNodesByLevel(nodes)
	initializeRanks(byLevel)
	return byLevel
}

func TestSiftingDoesNotIncreaseCrossings(t *testing.T) {
	for _, improveIfEqual := range []bool{false, true} {
		byLevel := newSiftingTestGraph()
		queue := nodesInDescendingDegreeOrder(byLevel)
		nodeToSiblings := buildNodeToSiblings(queue, byLevel)
		sawCrossing := false
		var scratch crossingScratch

		for _, node := range queue {
			siblings := nodeToSiblings[node]
			segments := scratch.crossLevelSegments(siblings, true, true)
			before := scratch.countCrossings(segments)
			sawCrossing = sawCrossing || before > 0
			if err := sifting(node, siblings, byLevel, improveIfEqual, &scratch); err != nil {
				t.Fatal(err)
			}
			after := scratch.countCrossings(scratch.crossLevelSegments(siblings, true, true))
			if after > before {
				t.Fatalf("sifting node %s increased crossings from %d to %d", node.graphNode.DebugID(), before, after)
			}
		}
		if !sawCrossing {
			t.Fatal("sifting fixture did not exercise a crossing")
		}
	}
}

func TestSiftingRetainsShorterOrderAtEqualCrossings(t *testing.T) {
	graph := flowRuleGraph(7, [][2]int{
		{0, 2}, {0, 3}, {0, 4}, {1, 2}, {1, 4}, {2, 6}, {3, 5}, {3, 6}, {4, 5},
	})
	levels := []int{0, 0, 1, 1, 1, 2, 2}
	h := layoutgraph.NewHierarchy()
	for i, node := range graph.Nodes {
		node.Hierarchy = h
		h.Levels()[node] = levels[i]
	}
	nodes := createPlacementNodes(graph, graph.Nodes, nil)
	connectPlacementNodes(graph, nodes)
	rows := groupPlacementNodesByLevel(nodes)
	initializeRanks(rows)
	minimizeHierarchyCrossings(rows)
	var scratch crossingScratch
	score := func() (int64, float64) {
		var crossings int64
		var length float64
		for level := 0; level < len(rows)-1; level++ {
			segments := scratch.crossLevelSegments(rows[level], false, true)
			crossings += scratch.countCrossings(segments)
			length += addLengths(segments)
		}
		return crossings, length
	}
	beforeCrossings, beforeLength := score()
	if err := globalSifting(rows); err != nil {
		t.Fatal(err)
	}
	afterCrossings, afterLength := score()
	if beforeCrossings != 2 || afterCrossings != beforeCrossings || afterLength > beforeLength+crossingComparisonPrecision {
		t.Fatalf("score went from (%d crossings, %.5f) to (%d crossings, %.5f)", beforeCrossings, beforeLength, afterCrossings, afterLength)
	}
	t.Logf("sifting: (%d crossings, %.5f) to (%d crossings, %.5f)", beforeCrossings, beforeLength, afterCrossings, afterLength)
}
