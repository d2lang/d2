package routing

import (
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestSparseOVGRoutesAroundObstacleWithinCandidateBudget(t *testing.T) {
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
	limits.intersectionCandidates = 2000
	guard, err := newOVGBuildGuard(ctx, limits)
	if err != nil {
		t.Fatal(err)
	}
	sparse, err := preferSparseOVG(graph, nil, guard)
	if err != nil || !sparse {
		t.Fatalf("sparse=%v, err=%v", sparse, err)
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
	limits.intersectionCandidates = guard.candidates - 1
	_, err = buildOVGFromGraphWithLimits(ctx, graph, nil, limits)
	requireOVGResourceError(t, err, "intersection candidate count")
}

func TestSparseCrossingIndexMatchesRouteSegments(t *testing.T) {
	ovg := NewOVG(nil)
	node := func(x, y float64) *OVGNode { return NewOVGNode(geo.NewPoint(x, y)) }
	for i := range 12 {
		axis := float64(i) + 0.5
		ovg.Connect(node(0, axis), node(12, axis))
		ovg.Connect(node(axis, 12), node(axis, 0))
		ovg.Connect(node(axis, axis), node(axis, axis+3))
	}
	guard, err := newRouteSearchWorkGuard(t.Context(), Default, maxRouteSearchWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	index, err := newOVGCrossingIndex(ovg, guard)
	if err != nil {
		t.Fatal(err)
	}
	set := newOvgEdgeSet()
	for _, segment := range []*OVGEdge{
		NewOVGEdge(node(2.5, 6.5), node(8.5, 6.5)),
		NewOVGEdge(node(5.5, 12), node(5.5, 4.5)),
		NewOVGEdge(node(2.5, 3.5), node(2.5, 5.5)),
		NewOVGEdge(node(12, 8.5), node(6.5, 8.5)),
		NewOVGEdge(node(0, 0), node(12, 12)),
	} {
		set.add(segment)
		if err := index.add(segment, guard); err != nil {
			t.Fatal(err)
		}
		for _, edge := range ovg.Edges {
			_, marked := index.crossed[edge]
			if want := set.intersectsWith(edge); marked != want {
				t.Fatalf("after adding %v to %v, edge %v to %v: marked %v, want %v",
					segment.From.Point, segment.To.Point, edge.From.Point, edge.To.Point, marked, want)
			}
		}
	}
}
