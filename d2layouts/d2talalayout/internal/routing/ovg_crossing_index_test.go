package routing

import (
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestCrossingIndexMatchesRouteSegments(t *testing.T) {
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
