package packing

import (
	"context"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestCombineSubgraphsValidatesNodeOwnedTopologyAcrossSharedNilMaps(t *testing.T) {
	master, first, second := &layoutgraph.Graph{}, &layoutgraph.Graph{}, &layoutgraph.Graph{}
	firstNode := layoutgraph.NewNode(1, 1, 1)
	firstNode.Graph, first.Nodes = first, []*layoutgraph.Node{firstNode}
	secondNode := layoutgraph.NewNode(2, 1, 1)
	secondNode.Graph = second
	secondNode.Nears[nil] = struct{}{}
	second.Nodes = []*layoutgraph.Node{secondNode}
	combined, err := CombineSubgraphs(context.Background(), master, []*layoutgraph.Graph{first, second}, nil)
	if combined != nil || err == nil || !strings.Contains(err.Error(), "nil near node") {
		t.Fatalf("combined=%v err=%v, want nil near-node rejection", combined, err)
	}
}

func TestCombineSubgraphsMidLoopCancellationRestoresExactGeometry(t *testing.T) {
	first := layoutgraph.NewGraph()
	firstNode := layoutgraph.NewNode(1, 1000, 1000)
	firstNode.TopLeft = geo.NewPoint(100, 100)
	first.AddNodeUnchecked(firstNode)
	firstEdge := first.Connect(firstNode, firstNode)
	firstEdge.Points = []*geo.Point{geo.NewPoint(100, 100), geo.NewPoint(150, 175)}
	second := layoutgraph.NewGraph()
	for i := 0; i < 80; i++ {
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+2), 1, 1)
		node.TopLeft = geo.NewPoint(0, 0)
		second.AddNodeUnchecked(node)
	}
	originalTopLeft, originalValue, originalGraph := firstNode.TopLeft, *firstNode.TopLeft, firstNode.Graph
	route := captureExactRouteTest(firstEdge)
	ctx := &packingMutationProbe{Context: context.Background(), node: firstNode, position: originalValue, route: route}
	combined, err := CombineSubgraphs(ctx, layoutgraph.NewGraph(), []*layoutgraph.Graph{first, second}, nil)
	if combined != nil {
		t.Fatalf("combined=%p, want nil", combined)
	}
	requirePackingCanceledAt(t, err, "CombineSubgraphs")
	if !ctx.observed {
		t.Fatal("CombineSubgraphs did not reach a post-mutation cancellation check")
	}
	if firstNode.TopLeft != originalTopLeft || *firstNode.TopLeft != originalValue || firstNode.Graph != originalGraph {
		t.Fatal("node rollback was not exact")
	}
	route.assertRestored(t)
	for _, node := range second.Nodes {
		if node.Graph != second {
			t.Fatalf("second node graph changed")
		}
	}
}

func TestCombineNearRespectsFixedAndAncestorObstacles(t *testing.T) {
	fixed, moving := layoutgraph.NewGraph(), layoutgraph.NewGraph()
	addFixed := func(id int64, x, y float64) *layoutgraph.Node {
		node := fixed.AddNode(layoutgraph.NewNode(id, 40, 40))
		node.FixedTopLeft = geo.NewPoint(x, y)
		node.TopLeft = node.FixedTopLeft.Copy()
		return node
	}
	target := addFixed(1, 200, 200)
	addFixed(2, 260, 200)
	addFixed(3, 200, 260)
	note := moving.AddNode(layoutgraph.NewNode(4, 40, 40))
	note.TopLeft = geo.NewPoint(0, 0)
	note.AddNear(target)
	above := geo.Box{TopLeft: geo.NewPoint(200, 140), Width: 40, Height: 40}
	if _, err := CombineSubgraphs(t.Context(), layoutgraph.NewGraph(), []*layoutgraph.Graph{fixed, moving}, []geo.Box{above}); err != nil {
		t.Fatal(err)
	}
	if got, want := *note.TopLeft, (geo.Point{X: 140, Y: 200}); got != want {
		t.Fatalf("placed note at %v, want %v on target's only free side", got, want)
	}
	for _, node := range fixed.Nodes {
		if *node.TopLeft != *node.FixedTopLeft {
			t.Errorf("fixed node %d moved to %v", node.ID, *node.TopLeft)
		}
	}
}
