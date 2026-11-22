package routing

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// The slot and class lookups must answer exactly as the maps they replace:
// the same route lists in the same order per point (with coincident nodes,
// -0/+0 and points of no OVG node), and the same edge memberships (with
// edges outside the OVG's list).
func TestRouteSearchIndexMatchesMaps(t *testing.T) {
	random := rand.New(rand.NewSource(17))
	for trial := range 100 {
		ovg := NewOVG(nil)
		coordinate := func() float64 {
			switch random.Intn(8) {
			case 0:
				return math.Copysign(0, -1)
			case 1:
				return 0
			default:
				return float64(random.Intn(6) * 10)
			}
		}
		for range 5 + random.Intn(30) {
			node := NewOVGNode(geo.NewPoint(coordinate(), coordinate()))
			if random.Intn(5) == 0 {
				ovg.AddNodeUnchecked(node) // may coincide with an earlier node
			} else {
				ovg.AddNode(node)
			}
		}
		for i, node := range ovg.Nodes {
			node.Index = i
		}
		for range 2 * len(ovg.Nodes) {
			a := ovg.Nodes[random.Intn(len(ovg.Nodes))]
			b := ovg.Nodes[random.Intn(len(ovg.Nodes))]
			ovg.Connect(a, b)
		}
		foreign := []*OVGEdge{NewOVGEdge(ovg.Nodes[0], ovg.Nodes[len(ovg.Nodes)-1])}
		if len(ovg.Edges) > 0 {
			// Same index as an OVG edge, but not that edge.
			impostor := NewOVGEdge(ovg.Edges[0].From, ovg.Edges[0].To)
			impostor.index = ovg.Edges[0].index
			foreign = append(foreign, impostor)
		}
		// A node outside the OVG whose Index points at an OVG node.
		stray := NewOVGNode(geo.NewPoint(coordinate(), coordinate()))

		router := &ovgEdgeRouter{
			crossings: newCrossingIndexState(ovg, nil, nil),
			classes:   newOVGPointClasses(ovg),
		}
		router.classSlot = make([]int32, len(router.classes.classOf))
		router.edgeFlags = make([]uint8, len(router.crossings.edges))

		pointRoutes := map[geo.Point][]*Route{}
		nearby := map[*OVGEdge]struct{}{}
		overlapping := map[*OVGEdge][]*Route{}
		crossed := map[*OVGEdge]struct{}{}
		allEdges := append(slices.Clone(ovg.Edges), foreign...)
		pickEdge := func() *OVGEdge { return allEdges[random.Intn(len(allEdges))] }
		for range 200 {
			route := &Route{}
			switch random.Intn(4) {
			case 0:
				point := *ovg.Nodes[random.Intn(len(ovg.Nodes))].Point
				if random.Intn(4) == 0 {
					point = geo.Point{X: coordinate() + 5, Y: coordinate()}
				}
				router.addRouteAtPoint(point, route)
				pointRoutes[point] = append(pointRoutes[point], route)
			case 1:
				edge := pickEdge()
				router.markNearbyEdge(edge)
				nearby[edge] = struct{}{}
			case 2:
				edge := pickEdge()
				router.addOverlappingRoute(edge, route)
				overlapping[edge] = append(overlapping[edge], route)
			case 3:
				edge := pickEdge()
				router.crossings.markCrossed(edge)
				crossed[edge] = struct{}{}
			}
		}
		for _, node := range append(slices.Clone(ovg.Nodes), stray) {
			if got, want := router.routesAtNode(node), pointRoutes[*node.Point]; !slices.Equal(got, want) {
				t.Fatalf("trial %d node %v: %d routes, want %d", trial, *node.Point, len(got), len(want))
			}
		}
		for range 50 {
			point := geo.Point{X: coordinate() + float64(random.Intn(2)*5), Y: coordinate()}
			if got, want := router.routesAtPoint(point), pointRoutes[point]; !slices.Equal(got, want) {
				t.Fatalf("trial %d point %v: %d routes, want %d", trial, point, len(got), len(want))
			}
		}
		if router.hasPointRoutes() != (len(pointRoutes) > 0) {
			t.Fatalf("trial %d: hasPointRoutes %v", trial, router.hasPointRoutes())
		}
		for _, edge := range allEdges {
			_, wantNearby := nearby[edge]
			_, wantCrossed := crossed[edge]
			if router.isNearbyEdge(edge) != wantNearby || router.crossings.isCrossed(edge) != wantCrossed ||
				!slices.Equal(router.overlappingRoutesOf(edge), overlapping[edge]) {
				t.Fatalf("trial %d: edge memberships differ from the maps", trial)
			}
		}
		if router.crossings.crossedCount() != len(crossed) {
			t.Fatalf("trial %d: crossed count %d, want %d", trial, router.crossings.crossedCount(), len(crossed))
		}
	}
}
