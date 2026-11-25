package routing

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestRouteOpposingColinear(t *testing.T) {
	route := &Route{
		GEdge: &layoutgraph.Edge{
			TargetArrowhead: layoutgraph.TriangleArrowhead,
		},
		OVGNodes: []*OVGNode{
			NewOVGNode(geo.NewPoint(0, 0)),
			NewOVGNode(geo.NewPoint(100, 0)),
			NewOVGNode(geo.NewPoint(100, 100)),
			NewOVGNode(geo.NewPoint(200, 100)),
		},
	}

	guard, err := newRouteSearchWorkGuard(t.Context(), Default, MaxSearchWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	router := &ovgEdgeRouter{work: guard, routeDirections: make(map[*Route]routeDirectionIndex)}
	opposing := func(from, to *OVGNode) bool {
		t.Helper()
		got, err := router.hasOpposingSegment(route, from, to)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	// Test horizontal
	from, to := NewOVGNode(geo.NewPoint(0, 0)), NewOVGNode(geo.NewPoint(100, 0))
	assert.False(t, opposing(from, to))
	assert.True(t, opposing(to, from))

	// Test vertical
	from, to = NewOVGNode(geo.NewPoint(100, 0)), NewOVGNode(geo.NewPoint(100, 100))
	assert.False(t, opposing(from, to))
	assert.True(t, opposing(to, from))

	// Test touch
	from, to = NewOVGNode(geo.NewPoint(190, 0)), NewOVGNode(geo.NewPoint(100, 0))
	assert.False(t, opposing(from, to))
	assert.False(t, opposing(to, from))

	// Test partial
	from, to = NewOVGNode(geo.NewPoint(90, 0)), NewOVGNode(geo.NewPoint(120, 0))
	assert.False(t, opposing(from, to))
	assert.True(t, opposing(to, from))

	// Test non intersecting
	from, to = NewOVGNode(geo.NewPoint(101, 0)), NewOVGNode(geo.NewPoint(200, 0))
	assert.False(t, opposing(from, to))
	assert.False(t, opposing(to, from))

	// Test non colinear
	from, to = NewOVGNode(geo.NewPoint(0, 0)), NewOVGNode(geo.NewPoint(100, 100))
	assert.False(t, opposing(from, to))
	assert.False(t, opposing(to, from))
	from, to = NewOVGNode(geo.NewPoint(120, 100)), NewOVGNode(geo.NewPoint(300, 100))
	assert.False(t, opposing(from, to))
	assert.False(t, opposing(to, from))
}
