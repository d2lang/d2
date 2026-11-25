package routing

import (
	"context"
	"errors"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/trees"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

func TestTreePathAlignment(t *testing.T) {
	g := layoutgraph.NewGraph()
	n0 := g.AddNode(layoutgraph.NewNode(3607948159, 191, 111))
	n0.TopLeft = geo.NewPoint(1000, 1000)
	n1 := g.AddNode(layoutgraph.NewNode(873436672, 158, 126))
	n1.TopLeft = geo.NewPoint(919, 1211)
	n2 := g.AddNode(layoutgraph.NewNode(2541194078, 130, 126))
	n2.TopLeft = geo.NewPoint(1127, 1211)
	n2.SetShape(shape.STORED_DATA_TYPE)
	n3 := g.AddNode(layoutgraph.NewNode(4207512678, 158, 126))
	n3.TopLeft = geo.NewPoint(919, 1437)
	n4 := g.AddNode(layoutgraph.NewNode(974102386, 100, 100))
	n4.TopLeft = geo.NewPoint(1142, 1450)
	e1 := g.Connect(n0, n1)
	e1.TargetArrowhead = layoutgraph.TriangleArrowhead
	e2 := g.Connect(n0, n2)
	e2.TargetArrowhead = layoutgraph.TriangleArrowhead
	e3 := g.Connect(n1, n3)
	e3.TargetArrowhead = layoutgraph.TriangleArrowhead
	e4 := g.Connect(n2, n4)
	e4.TargetArrowhead = layoutgraph.TriangleArrowhead

	ctx := withTestLogger(context.Background(), t)
	for _, node := range g.Nodes {
		g.AddNodeToContainer(nil, node)
	}
	if err := trees.Preprocess(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := trees.Place(ctx, g, nil); err != nil {
		t.Fatal(err)
	}

	ovg, err := buildOVGFromGraph(context.Background(), g, nil)
	if err != nil {
		t.Fatal(err)
	}
	isTreeEdge := g.TreeEdgeMap()
	g.AddIsolatedTreeEdges(isTreeEdge)

	tree := g.NodeToTree[n4]
	route, err := routeSentinelEdge(tree, ovg.Ports, ovg.Centers, nil)
	if err != nil {
		t.Fatalf("Error routing tree edge %v", err)
	}

	if len(ovg.Ports[n4]) != 13 {
		t.Fatalf("Expected 13 port nodes, got %v", len(ovg.Ports[n3]))
	}
	for i := 1; i < len(route.OVGNodes)-3; i++ {
		if route.OVGNodes[i].X != route.OVGNodes[i+1].X {
			t.Fatalf("Expected tree route to be a vertical line, but there is a bend at %d", i)
		}
	}
}

func TestAnnotatedTreeKeepsReadyRoutesWhenAlternateSearchExhausts(t *testing.T) {
	ctx := withTestLogger(t.Context(), t)
	g := layoutgraph.NewGraph()
	hub := g.AddNode(layoutgraph.NewNode(1, 100, 100))
	hub.TopLeft = geo.NewPoint(180, 0)
	for i := range 3 {
		leaf := g.AddNode(layoutgraph.NewNode(layoutgraph.EntityID(i+2), 80, 60))
		leaf.TopLeft = geo.NewPoint(float64(i*180), 200)
		edge := g.Connect(hub, leaf)
		edge.TargetArrowhead = layoutgraph.TriangleArrowhead
		edge.SourceArrowheadLabel = &layoutgraph.Label{Text: "Status", Width: 42, Height: 16}
	}
	for _, node := range g.Nodes {
		g.AddNodeToContainer(nil, node)
	}
	if err := trees.Preprocess(ctx, g); err != nil {
		t.Fatal(err)
	}
	if err := trees.Place(ctx, g, nil); err != nil {
		t.Fatal(err)
	}

	// Each alternate exhausts its individual search budget, but the
	// original tree routes have already been prepared within that budget.
	const searchLimit = 5_000
	stage, err := newRouteWorkGuard(ctx, "EdgeRouting", maxEdgeRoutingStageWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := routeEdgesWithSearchWorkLimit(contextWithRouteAggregateWork(ctx, stage), g, nil, searchLimit); err != nil {
		t.Fatal(err)
	}
	for _, edge := range g.Edges {
		if len(edge.Points) < 2 {
			t.Fatalf("lost tree edge %v", edge)
		}
		edge.Points = nil
	}

	// The same attempt must still fail atomically when the aggregate can't
	// pay for the work already performed by these optional searches.
	limited, err := newRouteWorkGuard(ctx, "EdgeRouting", stage.used-1)
	if err != nil {
		t.Fatal(err)
	}
	_, err = routeEdgesWithSearchWorkLimit(contextWithRouteAggregateWork(ctx, limited), g, nil, searchLimit)
	if !errors.Is(err, errRouteStageWorkLimit) {
		t.Fatalf("aggregate used %d (prior %d): got %v", limited.used, stage.used, err)
	}
	for _, edge := range g.Edges {
		if edge.Points != nil {
			t.Fatalf("committed a route after aggregate exhaustion: %v", edge)
		}
	}
}
