package trees

import (
	"context"
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// TestPlacementTreesInDirectionOrder: an isolated tree whose roots point
// both ways gets one placement tree per direction. They were created in map
// order, so the layout of such trees varied from run to run, in the
// original too (a tree's orientation and the
// order of trees in their container). Now Outwards comes before Inwards.
func TestPlacementTreesInDirectionOrder(t *testing.T) {
	ctx := withTestLogger(context.Background(), t)
	var want string
	for run := 0; run < 40; run++ {
		g := layoutgraph.NewGraph()
		nodes := make([]*layoutgraph.Node, 7)
		for i := range nodes {
			nodes[i] = makeNode(i)
			nodes[i].TopLeft = geo.NewPoint(float64(200*i), 0)
			g.AddNodeUnchecked(nodes[i])
			g.Containers[nil] = append(g.Containers[nil], nodes[i])
		}
		directed := func(a, b int) {
			e := g.Connect(nodes[a], nodes[b])
			e.TargetArrowhead = layoutgraph.TriangleArrowhead
		}
		// Hub 0: leaves 1-3 point at it, it points at leaves 4-6.
		directed(1, 0)
		directed(2, 0)
		directed(3, 0)
		directed(0, 4)
		directed(0, 5)
		directed(0, 6)
		g.Trees = mustExtractTrees(t, ctx, g)
		guard, err := newWorkGuard(ctx, "PlaceTrees")
		if err != nil {
			t.Fatal(err)
		}
		placement, err := buildPlacementTrees(g, guard)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		for _, tree := range placement {
			got += "["
			for _, child := range tree.Children {
				got += fmt.Sprintf(" %d", child.Node.ID)
			}
			got += " ]"
		}
		if run == 0 {
			want = got
			if len(placement) < 2 {
				t.Fatalf("expected a placement tree per direction, got %s", got)
			}
			continue
		}
		if got != want {
			t.Fatalf("run %d: placement trees %s, first run %s", run, got, want)
		}
	}
	directions := sortedTreeDirections(map[TreeEdgeDirection][]*layoutgraph.Tree{Bidirectional: nil, Inwards: nil, Outwards: nil, "Other": nil})
	if fmt.Sprint(directions) != "[Outwards Inwards Bidirectional Other]" {
		t.Fatalf("direction order %v", directions)
	}
}
