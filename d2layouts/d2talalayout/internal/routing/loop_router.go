package routing

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// makeLoopRoute makes a `Route` based on the edge points
func makeLoopRoute(e *layoutgraph.Edge, points []*geo.Point, ovg *OVG) *Route {
	var nodes []*OVGNode
	nodes = append(nodes, ovg.Centers[e.From])
	for _, p := range points {
		nodes = append(nodes, NewOVGNode(p))
	}
	nodes = append(nodes, ovg.Centers[e.To])
	return &Route{
		GEdge:    e,
		FromPort: *points[0],
		ToPort:   *points[len(points)-1],
		OVGNodes: nodes,
	}
}
