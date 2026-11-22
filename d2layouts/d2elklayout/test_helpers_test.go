package d2elklayout

import (
	"github.com/d2lang/d2/d2graph"
)

// newDescendantShifter indexes g on its own (Layout builds the same states
// as it walks the graph).
func newDescendantShifter(g *d2graph.Graph) *descendantShifter {
	objects := make(map[*d2graph.Object]*elkObject, len(g.Objects))
	var states slab[elkObject]
	var visits []elkVisit
	_, rootRange := walkObjects(g, objects, &states, visits)
	edges := make(map[*d2graph.Edge]*elkEdgeState, len(g.Edges))
	for _, edge := range g.Edges {
		if edges[edge] == nil {
			edges[edge] = &elkEdgeState{}
		}
	}
	var lists lists[*d2graph.Edge]
	collectIncidentEdges(g, objects, &lists)
	return newDescendantShifterWith(g, rootRange, objects, edges)
}

// deleteBends is a shim for ELK to delete unnecessary bends
// see https://github.com/terrastruct/d2/issues/1030
func deleteBends(g *d2graph.Graph, opts LayeredOptions, keepRoute map[*d2graph.Edge]bool) {
	deleteBendsWith(g, opts, keepRoute, nil, nil)
}

func newBendCollisionIndex(g *d2graph.Graph, opts LayeredOptions) *bendCollisionIndex {
	items := len(g.Objects)
	for _, edge := range g.Edges {
		items += max(0, len(edge.Route)-1)
	}
	return newBendCollisionIndexWith(g, opts, items <= linearIndexLimit, nil)
}
