package routing

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestOVGCandidateBudgetDoesNotSelectDifferentRoutes(t *testing.T) {
	graph, edge := newRoutingTestGraph(400, 0)
	obstacle := layoutgraph.NewNode(3, 80, 140)
	obstacle.TopLeft = geo.NewPoint(160, -50)
	graph.AddNode(obstacle)
	for i := range 16 {
		node := layoutgraph.NewNode(layoutgraph.EntityID(i+4), 40, 40)
		node.TopLeft = geo.NewPoint(float64(550+i*90), float64(250+i*100))
		graph.AddNode(node)
		graph.Connect(edge.To, node)
	}
	ctx := t.Context()
	limits := defaultOVGBuildLimits()

	guard, err := newOVGBuildGuard(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	ovg, err := buildOVGFromGraphWithGuard(graph, nil, guard)
	if err != nil {
		t.Fatal(err)
	}
	router, err := newOVGEdgeRouter(ctx, Default, ovg, graph, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, _, err := router.search(ctx, edge)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) < 4 || nodes[0] != ovg.Centers[edge.From] || nodes[len(nodes)-1] != ovg.Centers[edge.To] {
		t.Fatalf("incomplete obstacle detour: %d nodes", len(nodes))
	}
	for i := 1; i < len(nodes); i++ {
		from, to := nodes[i-1].Point, nodes[i].Point
		if obstacle.PassesThrough(from, to) {
			t.Fatalf("segment from %v to %v crosses the obstacle", from, to)
		}
	}
	// Tightening the budget to the exact number of grid candidates keeps
	// every route point. With one fewer candidate, report the resource limit.
	limits.intersectionCandidates = guard.candidates
	tight, err := buildOVGFromGraphWithLimits(ctx, graph, nil, limits)
	if err != nil {
		t.Fatal(err)
	}
	tightRouter, err := newOVGEdgeRouter(ctx, Default, tight, graph, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tightNodes, _, err := tightRouter.search(ctx, edge)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != len(tightNodes) {
		t.Fatalf("route has %d points with the tight budget, want %d", len(tightNodes), len(nodes))
	}
	for i, n := range nodes {
		if *n.Point != *tightNodes[i].Point {
			t.Fatalf("point %d is %v with the tight budget, want %v", i, tightNodes[i].Point, n.Point)
		}
	}
	limits.intersectionCandidates = guard.candidates - 1
	_, err = buildOVGFromGraphWithLimits(ctx, graph, nil, limits)
	requireOVGResourceError(t, err, "intersection candidate count")
}

func TestHierarchyRoutingScopesFollowSiblingEdges(t *testing.T) {
	g := layoutgraph.NewGraph()
	h := layoutgraph.NewHierarchy()
	node := func(id layoutgraph.EntityID, parent *layoutgraph.Node, level int) *layoutgraph.Node {
		n := g.AddNode(layoutgraph.NewNode(id, 40, 40))
		n.Container = parent
		n.Hierarchy = h
		h.Levels()[n] = level
		return n
	}
	left, right := node(1, nil, 0), node(2, nil, 2)
	a, b := node(3, left, 1), node(4, left, 4)
	c, d := node(5, right, 1), node(6, right, 4)
	g.Connect(a, b)
	g.Connect(b, c)
	scopes, err := siblingRoutingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if scopes[a] == nil || scopes[a] != scopes[b] || scopes[c] != nil || scopes[d] != nil || scopes[left] != nil || scopes[right] != nil {
		t.Fatal("only the two linked siblings should have local hierarchy lanes")
	}
	g.Connect(c, d)
	scopes, err = siblingRoutingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if scopes[a] == nil || scopes[c] == nil || scopes[a] != scopes[b] || scopes[c] != scopes[d] || scopes[a] == scopes[c] {
		t.Fatal("links in different containers should receive separate local lanes")
	}
}
