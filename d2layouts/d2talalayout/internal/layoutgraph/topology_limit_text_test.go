package layoutgraph

import (
	"context"
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestTopologyLimitErrorsDoNotNameTheReference: the topology preflights walk
// the graph's maps in map order, and their limit errors named the reference
// being visited when the count crossed the limit ("container key",
// "container child", "reserving container children capacity", ...), so the
// text varied from run to run. The texts
// are now fixed; whether the limit is crossed does not depend on the order.
func TestTopologyLimitErrorsDoNotNameTheReference(t *testing.T) {
	// Two containers: one whose children slice reserves nearly the whole
	// reference budget, one with a few children. In one map order the
	// reservation crosses the limit, in the other a child reference does.
	build := func() *Graph {
		g := NewGraph()
		reserving := NewNode(1, 100, 100)
		reserving.TopLeft = geo.NewPoint(0, 0)
		reserving.isContainer = true
		g.AddNewNodeToContainer(nil, reserving)
		child := NewNode(2, 10, 10)
		child.TopLeft = geo.NewPoint(10, 10)
		g.AddNewNodeToContainer(reserving, child)
		children := make([]*Node, 1, maxEngineTopologyReferences-40)
		children[0] = child
		g.Containers[reserving] = children
		many := NewNode(3, 400, 100)
		many.TopLeft = geo.NewPoint(200, 0)
		many.isContainer = true
		g.AddNewNodeToContainer(nil, many)
		for i := 0; i < 30; i++ {
			c := NewNode(EntityID(10+i), 5, 5)
			c.TopLeft = geo.NewPoint(float64(210+10*i), 10)
			g.AddNewNodeToContainer(many, c)
		}
		return g
	}
	want := fmt.Sprintf("TALA engine topology references exceed limit %d", maxEngineTopologyReferences)
	for run := 0; run < 20; run++ {
		err := validateEngineGraph(context.Background(), "test", build())
		if err == nil || err.Error() != want {
			t.Fatalf("run %d: error %v, want %q", run, err, want)
		}
	}

	// CombineSubgraphs: a node's edge slice reserving the budget.
	g := NewGraph()
	a := NewNode(1, 10, 10)
	a.TopLeft = geo.NewPoint(0, 0)
	b := NewNode(2, 10, 10)
	b.TopLeft = geo.NewPoint(100, 0)
	g.AddNewNodeToContainer(nil, a)
	g.AddNewNodeToContainer(nil, b)
	edge := g.Connect(a, b)
	edges := make([]*Edge, 1, maxEngineTopologyReferences+1)
	edges[0] = edge
	a.Edges = edges
	wantCombine := fmt.Sprintf("TALA CombineSubgraphs topology references exceed limit %d", maxEngineTopologyReferences)
	if err := validateCombineNodeTopology(context.Background(), []*Graph{g}); err == nil || err.Error() != wantCombine {
		t.Fatalf("CombineSubgraphs: error %v, want %q", err, wantCombine)
	}
}
