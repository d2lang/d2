package routing

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// Routers sharing one OVG's grid lines must see the lines a router building
// its own would, and be charged exactly the units building them charges.
func TestSharedCrossingIndexMatchesOwnIndex(t *testing.T) {
	for _, size := range []int{0, 1, 2, 5, 12} {
		ovg := NewOVG(nil)
		node := func(x, y float64) *OVGNode { return NewOVGNode(geo.NewPoint(x, y)) }
		for i := range size {
			axis := float64(i) + 0.5
			ovg.Connect(node(0, axis), node(12, axis))
			ovg.Connect(node(axis, 12), node(axis, 0))
			ovg.Connect(node(axis, axis), node(axis, axis+3))
			ovg.Connect(node(axis+1, axis), node(axis+2, axis))
		}
		newGuard := func() *routeSearchWorkGuard {
			guard, err := newRouteSearchWorkGuard(t.Context(), Default, maxRouteSearchWorkUnits)
			if err != nil {
				t.Fatal(err)
			}
			return guard
		}
		ownGuard := newGuard()
		own, err := newOVGCrossingIndex(ovg, ownGuard)
		if err != nil {
			t.Fatal(err)
		}
		var shared ovgRouterShared
		for router := range 3 {
			guard := newGuard()
			index, err := newSharedOVGCrossingIndex(ovg, &shared, guard)
			if err != nil {
				t.Fatal(err)
			}
			if guard.used != ownGuard.used {
				t.Fatalf("size %d router %d: charged %d units, building charges %d", size, router, guard.used, ownGuard.used)
			}
			if !reflect.DeepEqual(index.horizontal, own.horizontal) || !reflect.DeepEqual(index.vertical, own.vertical) {
				t.Fatalf("size %d router %d: grid lines differ", size, router)
			}
			if index.crossedCount() != 0 {
				t.Fatalf("size %d router %d: crossing set is not empty", size, router)
			}
		}
	}
}

// Label hit lists are cached per OVG edge index; an edge outside the OVG's
// list falls back to a map. Both must equal a direct scan.
func TestRouteLabelBoxesHitsMatchScan(t *testing.T) {
	ovg := NewOVG(nil)
	node := func(x, y float64) *OVGNode { return NewOVGNode(geo.NewPoint(x, y)) }
	for i := range 10 {
		axis := float64(i) * 10
		ovg.Connect(node(-5, axis), node(105, axis))
		ovg.Connect(node(axis, -5), node(axis, 105))
	}
	extra := NewOVGEdge(node(0, 0), node(100, 100))
	boxes := &routeLabelBoxes{edges: ovg.Edges, spans: make([]routeLabelHitSpan, len(ovg.Edges))}
	for i := range boxes.spans {
		boxes.spans[i].count = -1
	}
	for i := range 7 {
		box := geo.Box{TopLeft: geo.NewPoint(float64(i)*13, float64(i)*9), Width: 12, Height: 7}
		boxes.nodes = append(boxes.nodes, nil)
		boxes.boxes = append(boxes.boxes, box)
		boxes.bounds = append(boxes.bounds, routeLabelBoxBounds{
			left: box.TopLeft.X, right: box.TopLeft.X + box.Width,
			top: box.TopLeft.Y, bottom: box.TopLeft.Y + box.Height, valid: true,
		})
	}
	for round := range 2 {
		for _, edge := range append(append([]*OVGEdge{}, ovg.Edges...), extra) {
			var want []int32
			for i := range boxes.boxes {
				if segmentIntersectsBox(edge.From.Point, edge.To.Point, &boxes.boxes[i]) {
					want = append(want, int32(i))
				}
			}
			got := boxes.hitsFor(edge)
			if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
				t.Fatalf("round %d edge %v-%v: hits %v, want %v", round, edge.From.Point, edge.To.Point, got, want)
			}
		}
	}
}
