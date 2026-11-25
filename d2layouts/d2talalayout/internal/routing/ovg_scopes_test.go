package routing

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
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

func TestHierarchyRoutingLanes(t *testing.T) {
	g := layoutgraph.NewGraph()
	h := layoutgraph.NewHierarchy()
	node := func(id layoutgraph.EntityID, parent *layoutgraph.Node, level int) *layoutgraph.Node {
		n := g.AddNode(layoutgraph.NewNode(id, 40, 40))
		n.TopLeft = geo.NewPoint(0, 0)
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
	scopes, err := routingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if scopes[n] != h {
			t.Fatalf("compact hierarchy lost its original lanes for node %d", n.ID)
		}
	}

	// Spread the same nodes out far enough that the full grid would be
	// expensive. Only the sibling pair connected within its container needs
	// shared lanes. The cross-container edge still gets ordinary waypoints.
	d.TopLeft = geo.NewPoint(50_000, 50_000)
	scopes, err = routingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if scopes[a] == nil || scopes[a] != scopes[b] || scopes[c] != nil || scopes[d] != nil || scopes[left] != nil || scopes[right] != nil {
		t.Fatal("only the two linked siblings should have local hierarchy lanes")
	}
	g.Connect(c, d)
	scopes, err = routingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	if scopes[a] == nil || scopes[c] == nil || scopes[a] != scopes[b] || scopes[c] != scopes[d] || scopes[a] == scopes[c] {
		t.Fatal("links in different containers should receive separate local lanes")
	}
	for _, n := range g.Nodes {
		n.Container = nil
	}
	scopes, err = routingHierarchies(g, newBackgroundOVGBuildGuardForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if scopes[n] != h {
			t.Fatalf("a single container lost its hierarchy lanes for node %d", n.ID)
		}
	}
}

func TestHierarchyLanesUseOuterRankDirection(t *testing.T) {
	for _, flow := range []geo.Orientation{geo.Right, geo.Left, geo.Top, geo.Bottom} {
		t.Run(flow.ToString(), func(t *testing.T) {
			g := layoutgraph.NewGraph()
			g.Directions[nil] = flow
			h := layoutgraph.NewHierarchy()
			add := func(id layoutgraph.EntityID, parent *layoutgraph.Node, level int, x, y, size float64) *layoutgraph.Node {
				n := layoutgraph.NewNode(id, size, size)
				n.TopLeft = geo.NewPoint(x, y)
				g.AddNewNodeToContainer(parent, n)
				h.Levels()[n] = level
				n.Hierarchy = h
				return n
			}
			startX, startY, boxX, boxY := 0., 0., 0., 238.
			if flow.IsHorizontal() {
				boxX, boxY = boxY, boxX
			}
			if flow == geo.Left || flow == geo.Top {
				startX, boxX = boxX, startX
				startY, boxY = boxY, startY
			}
			start := add(2, nil, 0, startX, startY, 40)
			block := add(3, nil, 1, boxX, boxY, 40)
			// The descendant sorts before both rank nodes. Its container direction
			// is unset, and a nested table must not turn vertical ranks horizontal.
			child := add(1, block, 1, boxX+10, boxY+10, 20)
			if !flow.IsHorizontal() {
				child.SetShape(shape.TABLE_TYPE)
				child.SetNumColumns(1)
			}
			g.Connect(start, child)
			ovg, err := newOVGForHierarchy(g, h, newBackgroundOVGBuildGuardForTest(t))
			if err != nil {
				t.Fatal(err)
			}
			// With one edge between ranks, the router adds a lane halfway across
			// the gap, aligned with the port at the center of the start node's side.
			want := geo.Point{X: 20, Y: 139}
			if flow.IsHorizontal() {
				want.X, want.Y = want.Y, want.X
			}
			for _, n := range ovg.Nodes {
				if *n.Point == want {
					return
				}
			}
			t.Fatalf("missing between-rank lane waypoint at %v", want)
		})
	}
}
