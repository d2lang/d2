package routing

import (
	"github.com/d2lang/d2/lib/geo"
)

// The route search looks up, per relaxed OVG edge, the routes through the
// edge's endpoints (keyed by point) and whether the edge is near, crossed by
// or overlapped by a route (keyed by edge). These lookups dominated the search
// as map accesses. The structures below answer the same questions from
// slices: edges by their position in the OVG's Edges, points by a class shared
// by all OVG nodes at the same point (the same equality a geo.Point map key
// uses). Anything without a position or class falls back to the maps, so
// every answer, and the order of every route list, is the map's.

// ovgEdgeSlot is edge's position in edges, or -1 when it is not there (edges
// built outside OVG.Connect, or added after edges was taken).
func ovgEdgeSlot(edges []*OVGEdge, edge *OVGEdge) int {
	if i := int(edge.index); i < len(edges) && edges[i] == edge {
		return i
	}
	return -1
}

// ovgPointClasses numbers the distinct points of an OVG's nodes. Two nodes
// share a class exactly when their points are equal map keys.
type ovgPointClasses struct {
	ovg       *OVG
	nodes     []*OVGNode
	nodeClass []int32
	classOf   map[geo.Point]int32
}

func newOVGPointClasses(ovg *OVG) *ovgPointClasses {
	classes := &ovgPointClasses{
		ovg:       ovg,
		nodes:     ovg.Nodes,
		nodeClass: make([]int32, len(ovg.Nodes)),
		classOf:   make(map[geo.Point]int32, len(ovg.Nodes)),
	}
	for i, node := range ovg.Nodes {
		class, ok := classes.classOf[*node.Point]
		if !ok {
			class = int32(len(classes.classOf))
			classes.classOf[*node.Point] = class
		}
		classes.nodeClass[i] = class
	}
	return classes
}

// pointClassesFor returns the point classes of ovg, shared by the routers of
// one OVG when shared is not nil. They are read-only once built.
func pointClassesFor(ovg *OVG, shared *ovgRouterShared) *ovgPointClasses {
	if shared != nil && shared.classes != nil && shared.classes.ovg == ovg && len(shared.classes.nodes) == len(ovg.Nodes) {
		return shared.classes
	}
	classes := newOVGPointClasses(ovg)
	if shared != nil {
		shared.classes = classes
	}
	return classes
}

// classOfPoint is the class of point, or -1 when no OVG node is there.
func (classes *ovgPointClasses) classOfPoint(point geo.Point) int32 {
	if classes == nil {
		return -1
	}
	if class, ok := classes.classOf[point]; ok {
		return class
	}
	return -1
}

// classOfNode is classOfPoint(*node.Point) without the map when node is the
// OVG node at its Index.
func (classes *ovgPointClasses) classOfNode(node *OVGNode) int32 {
	if classes == nil {
		return -1
	}
	if i := node.Index; i >= 0 && i < len(classes.nodes) && classes.nodes[i] == node {
		return classes.nodeClass[i]
	}
	return classes.classOfPoint(*node.Point)
}

// routesAtClass returns the routes recorded at a point class (nil if none).
func (router *ovgEdgeRouter) routesAtClass(class int32) []*Route {
	if slot := router.classSlot[class]; slot != 0 {
		return router.classRoutes[slot-1]
	}
	return nil
}

// routesAtNode is the former pointToRoute[*node.Point].
func (router *ovgEdgeRouter) routesAtNode(node *OVGNode) []*Route {
	if class := router.classes.classOfNode(node); class >= 0 {
		return router.routesAtClass(class)
	}
	return router.pointToRoute[*node.Point]
}

// addRouteAtPoint is the former pointToRoute[point] = append(..., route).
func (router *ovgEdgeRouter) addRouteAtPoint(point geo.Point, route *Route) {
	class := router.classes.classOfPoint(point)
	if class < 0 {
		if router.pointToRoute == nil {
			router.pointToRoute = make(map[geo.Point][]*Route)
		}
		router.pointToRoute[point] = append(router.pointToRoute[point], route)
		return
	}
	slot := router.classSlot[class]
	if slot == 0 {
		router.classRoutes = append(router.classRoutes, nil)
		slot = int32(len(router.classRoutes))
		router.classSlot[class] = slot
	}
	router.classRoutes[slot-1] = append(router.classRoutes[slot-1], route)
}

const (
	routeEdgeNearby     uint8 = 1
	routeEdgeOverlapped uint8 = 2
)

func (router *ovgEdgeRouter) edgeSlot(edge *OVGEdge) int {
	if router.crossings == nil {
		return -1
	}
	return ovgEdgeSlot(router.crossings.edges, edge)
}

// markNearbyEdge is the former hasNearbyEdge[edge] = struct{}{}.
func (router *ovgEdgeRouter) markNearbyEdge(edge *OVGEdge) {
	if slot := router.edgeSlot(edge); slot >= 0 {
		router.edgeFlags[slot] |= routeEdgeNearby
		return
	}
	if router.hasNearbyEdge == nil {
		router.hasNearbyEdge = make(map[*OVGEdge]struct{})
	}
	router.hasNearbyEdge[edge] = struct{}{}
}

// isNearbyEdge is the former hasNearbyEdge[edge] membership test.
func (router *ovgEdgeRouter) isNearbyEdge(edge *OVGEdge) bool {
	if slot := router.edgeSlot(edge); slot >= 0 {
		return router.edgeFlags[slot]&routeEdgeNearby != 0
	}
	_, ok := router.hasNearbyEdge[edge]
	return ok
}

// addOverlappingRoute appends route to overlappingRoutes[edge] and flags the
// edge so lookups of unflagged edges can skip the map.
func (router *ovgEdgeRouter) addOverlappingRoute(edge *OVGEdge, route *Route) {
	if router.overlappingRoutes == nil {
		router.overlappingRoutes = make(map[*OVGEdge][]*Route)
	}
	router.overlappingRoutes[edge] = append(router.overlappingRoutes[edge], route)
	if slot := router.edgeSlot(edge); slot >= 0 {
		router.edgeFlags[slot] |= routeEdgeOverlapped
	}
}

// overlappingRoutesOf is the former overlappingRoutes[edge].
func (router *ovgEdgeRouter) overlappingRoutesOf(edge *OVGEdge) []*Route {
	if slot := router.edgeSlot(edge); slot >= 0 && router.edgeFlags[slot]&routeEdgeOverlapped == 0 {
		return nil
	}
	return router.overlappingRoutes[edge]
}
