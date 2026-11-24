package hierarchy

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

func TestCycleBreakingPreservesBridgeBetweenCycles(t *testing.T) {
	// Two cyclic components joined only by a -> c. Three copies of c -> d
	// previously made c the global heuristic's first choice, reversing a -> c.
	g, nodes := newDirectedGraph(4, [][2]int{
		{0, 1}, {1, 0}, {2, 3}, {3, 2}, {2, 3}, {2, 3}, {0, 2},
	})
	feedback := mustFindCycleEdges(t, t.Context(), g)
	if len(feedback) != 2 {
		t.Fatalf("selected %d feedback edges for two disjoint cycles, want 2", len(feedback))
	}
	if _, reversed := feedback[g.Edges[6]]; reversed {
		t.Fatal("bridge between distinct strongly connected components was reversed")
	}
	levels := preparedRankLevels(t, g, nodes)
	if levels[0] >= levels[2] {
		t.Fatalf("bridge a -> c points backward in prepared ranks %v", levels)
	}
}

func TestNeutralEdgesPreserveDirectedPrecedence(t *testing.T) {
	for _, arrows := range []layoutgraph.Arrowhead{layoutgraph.NoArrowhead, layoutgraph.TriangleArrowhead} {
		// d -> a -> {b,c} can be preserved regardless of how the neutral
		// links b--c and b--d are declared.
		g, nodes := newDirectedGraph(4, [][2]int{{0, 1}, {0, 2}, {3, 0}, {1, 2}, {1, 3}})
		for _, edge := range g.Edges[3:] {
			edge.SourceArrowhead, edge.TargetArrowhead = arrows, arrows
		}
		first := preparedRankLevels(t, g, nodes)
		if first[3] >= first[0] || first[0] >= first[1] || first[0] >= first[2] {
			t.Fatalf("authored arrows point backward with neutral arrowheads %q: levels %v", arrows, first)
		}
		for _, edge := range g.Edges[3:] {
			edge.From, edge.To = edge.To, edge.From
		}
		slices.Reverse(g.Edges)
		if second := preparedRankLevels(t, g, nodes); !slices.Equal(first, second) {
			t.Fatalf("redeclaring neutral edges changed levels from %v to %v", first, second)
		}
	}
}

func TestPreparedRanksIndependentOfEdgeOrder(t *testing.T) {
	// This DAG has equal-cost rank assignments on four and five levels.
	// Its nodes and edges keep their identities when the slices are reordered.
	g, nodes := newDirectedGraph(7, [][2]int{
		{0, 1}, {0, 4}, {0, 5}, {0, 6}, {1, 5}, {2, 3}, {2, 5}, {3, 4}, {5, 6},
	})
	first := preparedRankLevels(t, g, nodes)
	original := slices.Clone(g.Edges)
	for i, source := range []int{4, 2, 7, 6, 1, 3, 0, 5, 8} {
		g.Edges[i] = original[source]
	}
	slices.Reverse(g.Nodes)
	for _, n := range g.Nodes {
		slices.Reverse(n.Edges)
	}
	if second := preparedRankLevels(t, g, nodes); !slices.Equal(first, second) {
		t.Fatalf("same DAG changed ranks from %v to %v", first, second)
	}
}

func preparedRankLevels(t *testing.T, g *layoutgraph.Graph, nodes []*layoutgraph.Node) []int {
	t.Helper()
	dag := mustMakeSimpleDAG(t, t.Context(), g)
	ranks, err := rankDAG(t.Context(), dag)
	if err != nil {
		t.Fatal(err)
	}
	levels := mapDAGToGraphLevels(g, ranks.nodeToLevel)
	result := make([]int, len(nodes))
	for i, n := range nodes {
		result[i] = levels[n]
	}
	return result
}
