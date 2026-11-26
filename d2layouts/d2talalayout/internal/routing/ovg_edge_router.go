package routing

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/labeling"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/nodeshape"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// Map pools for reducing allocations in hot paths
var portMapPool = typedpool.New(func() map[geo.Point]bool {
	return make(map[geo.Point]bool, 16)
})

// borrowPortMap gets a map from the pool and clears it
func borrowPortMap() map[geo.Point]bool {
	m := portMapPool.Get()
	clear(m)
	return m
}

// returnPortMap returns a map to the pool
func returnPortMap(m map[geo.Point]bool) {
	// Don't return oversized maps to pool to prevent memory leaks
	if len(m) < 128 {
		portMapPool.Put(m)
	}
}

/*
	Keeps the state for OVG edge search with data structure to quickly check if a given

edge is already in some route or if it intersects with other routes.
*/
type ovgEdgeRouter struct {
	flavor           RouteGenerationFlavor
	graph            *layoutgraph.Graph
	ovg              *OVG
	edges            []*layoutgraph.Edge
	routes           []*Route
	routedEdges      []*layoutgraph.Edge
	positionedLabels []labeling.PositionedArrowheadLabel
	// Routes through each point: by point class (classSlot holds 1+ the
	// index into classRoutes, 0 for none) for points of OVG nodes, else in
	// pointToRoute. See route_search_index.go.
	classes           *ovgPointClasses
	classSlot         []int32
	classRoutes       [][]*Route
	pointToRoute      map[geo.Point][]*Route
	edgeFlags         []uint8
	turnCost          float64
	crossingCost      float64
	nonCenterPortCost float64
	edgeSet           *ovgEdgeSet
	crossings         *ovgCrossingIndex
	hasNearbyEdge     map[*OVGEdge]struct{}
	overlappingRoutes map[*OVGEdge][]*Route
	fixedOverlaps     map[*layoutgraph.Node]struct{}
	fixedOverlapsSet  bool
	// Search state, indexed by OVGNode.Index. The slices are carved from
	// scratch (pooled across routing calls, see route_search_scratch.go)
	// exactly where they used to be allocated, so their lengths, and every
	// decision and charge that depends on them, are unchanged.
	verticalHops      []*OVGNode
	horizontalHops    []*OVGNode
	nodeContext       []*searchNodeContext
	nodeContextValues []searchNodeContext
	touchedNodes      []int
	routeDirections   map[*Route]routeDirectionIndex
	scratch           *routeSearchScratch
	// Usually, edge routing occurs before label placement. When the router is used
	// independently, labels are already placed and the edges should move out of the way.
	considerNodeLabels bool
	labelBoxes         *routeLabelBoxes
	work               *routeSearchWorkGuard
}

func checkEdgeRoutingCanceled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("EdgeRouting: %w", err)
	}
	return nil
}

func isContextTermination(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type searchNodeContext struct {
	verticalDistance, horizontalDistance float64
	verticalEntry, horizontalEntry       *priorityQueueEntry
}

func (router *ovgEdgeRouter) bindWork(ctx context.Context) error {
	if router.work == nil {
		guard, err := newRouteSearchWorkGuard(ctx, router.flavor, maxRouteSearchWorkUnits)
		if err != nil {
			return err
		}
		router.work = guard
		return nil
	}
	return router.work.bind(ctx)
}

// newSharingOVGEdgeRouter consumes one flavor's guard and shares the OVG
// indexes with other flavors when shared is supplied.
func newSharingOVGEdgeRouter(
	guard *routeSearchWorkGuard,
	ovg *OVG,
	g *layoutgraph.Graph,
	existingRoutes []*Route,
	edges []*layoutgraph.Edge,
	shared *ovgRouterShared,
) (_ *ovgEdgeRouter, err error) {
	flavor := guard.flavor
	defer func() {
		if err != nil {
			if chargeErr := guard.finish(); chargeErr != nil {
				err = chargeErr
			}
		}
	}()
	if err := guard.add(uint64(len(edges))); err != nil {
		return nil, err
	}
	orderedEdges := make([]*layoutgraph.Edge, len(edges))
	copy(orderedEdges, edges)

	clusterNodes := map[*layoutgraph.Node]struct{}{}
	for _, c := range g.Clusters {
		if err := guard.step(); err != nil {
			return nil, err
		}
		for _, cn := range c.Nodes {
			if err := guard.step(); err != nil {
				return nil, err
			}
			clusterNodes[cn] = struct{}{}
		}
	}
	for _, edge := range orderedEdges {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if edge == nil || edge.From == nil || edge.To == nil {
			continue
		}
		if err := guard.reserveSum(uint64(len(edge.From.Edges)), uint64(len(edge.To.Edges))); err != nil {
			return nil, err
		}
	}
	if err := guard.reserveSort(len(orderedEdges)); err != nil {
		return nil, err
	}
	if err := guard.reserveSort(len(orderedEdges)); err != nil {
		return nil, err
	}
	sortEdges(flavor, orderedEdges, clusterNodes)

	router := &ovgEdgeRouter{
		flavor:            flavor,
		graph:             g,
		ovg:               ovg,
		routes:            make([]*Route, 0),
		routedEdges:       make([]*layoutgraph.Edge, 0),
		positionedLabels:  make([]labeling.PositionedArrowheadLabel, 0),
		edges:             orderedEdges,
		pointToRoute:      make(map[geo.Point][]*Route),
		turnCost:          g.TurnCost(),
		crossingCost:      g.CrossingCost(),
		nonCenterPortCost: g.NonCenterPortCost(),
		edgeSet:           newOvgEdgeSet(),
		hasNearbyEdge:     make(map[*OVGEdge]struct{}),
		overlappingRoutes: make(map[*OVGEdge][]*Route),
		work:              guard,
	}

	router.crossings, err = newSharedOVGCrossingIndex(ovg, shared, guard)
	if err != nil {
		return nil, err
	}
	router.edgeFlags = make([]uint8, len(router.crossings.edges))
	router.classes = pointClassesFor(ovg, shared)
	router.classSlot = make([]int32, len(router.classes.classOf))
	router.routeDirections = make(map[*Route]routeDirectionIndex)

	for _, route := range existingRoutes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if err := router.addRoute(route); err != nil {
			return nil, err
		}
	}

	if err := guard.check(); err != nil {
		return nil, err
	}
	return router, nil
}

func sortEdges(flavor RouteGenerationFlavor, edges []*layoutgraph.Edge, clusterNodes map[*layoutgraph.Node]struct{}) {
	switch flavor {
	case ShortestToLongest:
		xsort.StableFunc(edges, func(a, b *layoutgraph.Edge) int {
			aDistance := a.EuclideanDistance()
			bDistance := b.EuclideanDistance()
			switch {
			case aDistance < bDistance:
				return -1
			case bDistance < aDistance:
				return 1
			default:
				return 0
			}
		})
	case LongestToShortest:
		xsort.StableFunc(edges, func(a, b *layoutgraph.Edge) int {
			aDistance := a.EuclideanDistance()
			bDistance := b.EuclideanDistance()
			switch {
			case aDistance > bDistance:
				return -1
			case bDistance > aDistance:
				return 1
			default:
				return 0
			}
		})
	case TopDownLeftRight:
		xsort.StableFunc(edges, func(a, b *layoutgraph.Edge) int {
			// Ensure that edge.From is above edge.To for both edges.
			aFrom := a.From
			aTo := a.To
			if aFrom.TopLeft.Y > aTo.TopLeft.Y {
				aFrom, aTo = aTo, aFrom
			}
			bFrom := b.From
			bTo := b.To
			if bFrom.TopLeft.Y > bTo.TopLeft.Y {
				bFrom, bTo = bTo, bFrom
			}

			if aFrom.TopLeft.Y != bFrom.TopLeft.Y {
				switch {
				case aFrom.TopLeft.Y < bFrom.TopLeft.Y:
					return -1
				case bFrom.TopLeft.Y < aFrom.TopLeft.Y:
					return 1
				default:
					return 0
				}
			}
			// a and b start in the same row.
			if aFrom != bFrom {
				switch {
				case aFrom.TopLeft.X < bFrom.TopLeft.X:
					return -1
				case bFrom.TopLeft.X < aFrom.TopLeft.X:
					return 1
				default:
					return 0
				}
			}
			// Same from node, maybe different targets.
			if aTo.TopLeft.Y == bTo.TopLeft.Y {
				switch {
				case aTo.TopLeft.X < bTo.TopLeft.X:
					return -1
				case bTo.TopLeft.X < aTo.TopLeft.X:
					return 1
				default:
					return 0
				}
			}
			// Return the top-most target.
			switch {
			case aTo.TopLeft.Y < bTo.TopLeft.Y:
				return -1
			case bTo.TopLeft.Y < aTo.TopLeft.Y:
				return 1
			default:
				return 0
			}
		})
	}

	// Clusters go first, looks best
	xsort.StableFunc(edges, func(a, b *layoutgraph.Edge) int {
		connectedA := false
		connectedB := false

		if _, is := clusterNodes[a.From]; is {
			connectedA = true
		} else if _, is := clusterNodes[a.To]; is {
			connectedA = true
		}
		if _, is := clusterNodes[b.From]; is {
			connectedB = true
		} else if _, is := clusterNodes[b.To]; is {
			connectedB = true
		}

		switch {
		case connectedA && !connectedB:
			return -1
		case !connectedA && connectedB:
			return 1
		default:
			return 0
		}
	})
}

func (router *ovgEdgeRouter) routeLine(ctx context.Context, edge *layoutgraph.Edge) ([]*OVGNode, float64, error) {
	if err := router.bindWork(ctx); err != nil {
		return nil, 0, err
	}
	fromPort, toPort, cost, err := routeLineGuarded(router.graph, edge, router.routedEdges, router.routes, router.work)

	if err != nil {
		if errors.Is(err, layoutgraph.ErrInvalidCandidate) {
			fromCenter := router.ovg.Centers[edge.From]
			toCenter := router.ovg.Centers[edge.To]
			var fromPort, toPort *geo.Point
			segment := geo.NewSegment(fromCenter.Point, toCenter.Point)
			if intersections := edge.From.Intersections(*segment); len(intersections) > 0 {
				fromPort = intersections[0]
				segment.Start = fromPort
			}
			if intersections := edge.To.Intersections(*segment); len(intersections) > 0 {
				toPort = intersections[0]
			}
			// These only happen when there's overlap. It'll look terrible regardless, so just make it work
			if fromPort == nil {
				fromPort = fromCenter.Point
			}
			if toPort == nil {
				toPort = toCenter.Point
			}
			ovgNodes := []*OVGNode{fromCenter, NewOVGNode(fromPort), NewOVGNode(toPort), toCenter}
			return ovgNodes, geo.EuclideanDistance(fromCenter.X, fromCenter.Y, toCenter.X, toCenter.Y), nil
		}
		return nil, 0, err
	}
	ovgNodes := []*OVGNode{
		router.ovg.Centers[edge.From],
		router.ovg.OccupiedPoints[*fromPort],
		router.ovg.OccupiedPoints[*toPort],
		router.ovg.Centers[edge.To],
	}
	return ovgNodes, cost, nil
}

func (router *ovgEdgeRouter) generateRoutes(ctx context.Context, straightLineFallback bool) (resp GenerateRouteResponse) {
	resp.Flavor = router.flavor
	if err := router.bindWork(ctx); err != nil {
		resp.Err = err
		if router.work != nil {
			_ = router.work.finish()
		}
		return resp
	}
	resp.work = router.work
	defer func() {
		if err := router.work.finish(); err != nil {
			resp.Err = err
			resp.Routes = nil
		}
	}()

	totalDistance := 0.0
	for _, edge := range router.edges {
		if err := router.work.step(); err != nil {
			resp.Err = err
			return resp
		}
		var distance float64
		var ovgNodes []*OVGNode
		var err error

		ovgNodes, distance, err = router.slingshot(ctx, edge)
		if err != nil {
			if straightLineFallback && !errors.Is(err, errRouteStageWorkLimit) && !isContextTermination(err) {
				ovgNodes, distance, err = router.routeLine(ctx, edge)
			}
			if err != nil {
				resp.Err = err
				return resp
			}
		}
		if len(ovgNodes) <= 1 {
			ovgNodes, distance, err = router.search(ctx, edge)
			if isContextTermination(err) || errors.Is(err, errRouteStageWorkLimit) {
				resp.Err = err
				return resp
			}
			if err != nil {
				if straightLineFallback {
					ovgNodes, distance, err = router.routeLine(ctx, edge)
				}
				if err != nil {
					resp.Err = err
					return resp
				}
			}
			if len(ovgNodes) <= 1 {
				message := fmt.Sprintf("Route '%s' has size %v", edge.DebugID(), len(ovgNodes))
				resp.Err = invariant.New(message)
				return resp
			}
		}

		totalDistance += distance
		err = router.addRoute(&Route{
			GEdge:    edge,
			OVGNodes: ovgNodes,
			FromPort: *ovgNodes[1].Point,
			ToPort:   *ovgNodes[len(ovgNodes)-2].Point,
		})
		if err != nil {
			resp.Err = err
			return resp
		}
	}

	resp.Routes = router.routes
	resp.Distance = totalDistance
	return resp
}

// Among center ports, slightly favor going through center ports where the mirrored center port is occupied
// So if a node has a route going through its left center port, another route going through the right center port looks nicely symmetrical
func (router *ovgEdgeRouter) centerSymmetricalPorts(ctx context.Context, gSource, gTarget *layoutgraph.Node, sourcePortsUsed, targetPortsUsed map[geo.Point]bool) (map[*OVGNode]struct{}, error) {
	nodeIsCenterSymmetricalPort := make(map[*OVGNode]struct{})
	if err := router.work.reserveSum(uint64(len(router.ovg.Ports[gSource])), uint64(len(router.ovg.Ports[gTarget]))); err != nil {
		return nil, err
	}
	sourceMirroredPorts := gSource.MirroredPorts()
	targetMirroredPorts := gTarget.MirroredPorts()
	for _, port := range router.ovg.Ports[gSource] {
		if err := router.work.step(); err != nil {
			return nil, err
		}
		// If this is a port number that has a mirror
		if mirroredPort, is := sourceMirroredPorts[*port.Point]; is {
			// If the mirrored port is occupied, making this one desirable
			if _, used := sourcePortsUsed[mirroredPort]; used {
				nodeIsCenterSymmetricalPort[port] = struct{}{}
			}
		}
	}
	// Do the same thing for target ports
	for _, port := range router.ovg.Ports[gTarget] {
		if err := router.work.step(); err != nil {
			return nil, err
		}
		if mirroredPort, is := targetMirroredPorts[*port.Point]; is {
			if _, used := targetPortsUsed[mirroredPort]; used {
				nodeIsCenterSymmetricalPort[port] = struct{}{}
			}
		}
	}

	if err := checkEdgeRoutingCanceled(ctx); err != nil {
		return nil, err
	}
	return nodeIsCenterSymmetricalPort, nil
}

func (router *ovgEdgeRouter) usedPorts(
	ctx context.Context,
	gSource, gTarget *layoutgraph.Node,
	source, target *OVGNode,
	sourceClusterNodes, targetClusterNodes map[*layoutgraph.Node]bool,
	undesirableClusterArrangement bool,
) (map[geo.Point]bool, map[geo.Point]bool, map[geo.Point]bool, map[geo.Point]bool, map[geo.Point]bool, map[geo.Point]bool, error) {
	sourcePortsUsed := make(map[geo.Point]bool)
	targetPortsUsed := make(map[geo.Point]bool)
	duplicateSourcePortsUsed := make(map[geo.Point]bool)
	duplicateTargetPortsUsed := make(map[geo.Point]bool)
	sharedClusterSourcePortsUsed := make(map[geo.Point]bool)
	sharedClusterTargetPortsUsed := make(map[geo.Point]bool)

	for _, node := range []*OVGNode{source, target} {
		if err := router.work.step(); err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}
		// we might process the same route, but since it's just assigning to maps, it's fine
		for _, route := range router.routesAtNode(node) {
			if err := router.work.step(); err != nil {
				return nil, nil, nil, nil, nil, nil, err
			}
			if (route.GEdge.From != gSource) && (route.GEdge.To != gSource) && (route.GEdge.From != gTarget) && (route.GEdge.To != gTarget) {
				continue
			}

			// note ports of duplicate connections
			if (route.GEdge.From == gSource) && (route.GEdge.To == gTarget) {
				duplicateSourcePortsUsed[route.FromPort] = true
				duplicateTargetPortsUsed[route.ToPort] = true
			}

			// If we're going to or coming from a cluster, then we penalize going through port nodes which
			// are not shared by other nodes of the cluster, which has the effect of making cluster nodes share
			// routes
			// ClusterPortSharing Step 1.
			// we want the same port if a cluster route has the same source or target so record it here
			if !undesirableClusterArrangement {
				if route.GEdge.To == gTarget {
					if _, is := sourceClusterNodes[route.GEdge.From]; is {
						sharedClusterTargetPortsUsed[route.ToPort] = true
					}
				}
				if route.GEdge.From == gSource {
					if _, is := targetClusterNodes[route.GEdge.To]; is {
						sharedClusterSourcePortsUsed[route.FromPort] = true
					}
				}
			}

			for _, routeNode := range route.OVGNodes {
				if err := router.work.step(); err != nil {
					return nil, nil, nil, nil, nil, nil, err
				}
				for _, portNode := range router.ovg.Ports[gSource] {
					if err := router.work.step(); err != nil {
						return nil, nil, nil, nil, nil, nil, err
					}
					if routeNode == portNode {
						sourcePortsUsed[*portNode.Point] = true
						break
					}
				}
				for _, portNode := range router.ovg.Ports[gTarget] {
					if err := router.work.step(); err != nil {
						return nil, nil, nil, nil, nil, nil, err
					}
					if routeNode == portNode {
						targetPortsUsed[*portNode.Point] = true
						break
					}
				}
			}
		}
	}
	if err := checkEdgeRoutingCanceled(ctx); err != nil {
		return nil, nil, nil, nil, nil, nil, err
	}
	return sourcePortsUsed, targetPortsUsed, duplicateSourcePortsUsed, duplicateTargetPortsUsed, sharedClusterSourcePortsUsed, sharedClusterTargetPortsUsed, nil
}

// Most optimizations in the search function are related to reducing allocations by:
// 1. Reusing temporary slices for occupiedEdges, overlappingEdges, and nodeLabelBoxes
// 2. Using preallocated maps for fromFacingPorts, toFacingPorts, blockedSourcePorts, and blockedTargetPorts
// 3. Reusing routePoints when creating positioned arrowhead labels
// 4. Using explicit nil checks to avoid unnecessary allocations
// 5. Reducing heap allocations by reusing existing data structures
//
// These optimizations significantly reduce the number of allocations and memory usage
// in the hot path of the edge routing algorithm.

// Basically Dijkstra's shortest path with some other checks for overlap and alternative routes
func (router *ovgEdgeRouter) search(ctx context.Context, gEdge *layoutgraph.Edge) ([]*OVGNode, float64, error) {
	if err := router.bindWork(ctx); err != nil {
		return nil, 0, err
	}
	g := router.ovg
	originalGraph := router.graph
	gSource := gEdge.From
	gTarget := gEdge.To
	source := g.Centers[gSource]
	target := g.Centers[gTarget]
	var routePoints [2]*geo.Point

	// Get maps from pool to reduce allocations
	fromFacingPorts := borrowPortMap()
	defer returnPortMap(fromFacingPorts)
	toFacingPorts := borrowPortMap()
	defer returnPortMap(toFacingPorts)

	// Pre-allocate slices for temp storage with reasonable capacity
	occupiedEdges := make([]*layoutgraph.Edge, 0, 32)
	overlappingEdges := make([]*layoutgraph.Edge, 0, 32)

	// When node labels are obstacles, every labelled node other than the
	// edge's endpoints contributes one label box. The boxes and their
	// per-edge intersections are cached on the router (see routeLabelBoxes).
	nodeLabelBoxCount := 0
	if router.considerNodeLabels {
		if router.labelBoxes == nil {
			router.labelBoxes = newRouteLabelBoxes(originalGraph.Nodes, g.Edges)
		}
		for _, n := range originalGraph.Nodes {
			if err := router.work.step(); err != nil {
				return nil, 0, err
			}
			if n == gSource || n == gTarget {
				continue
			}
			if n.Label != nil && n.Label.Text != "" {
				nodeLabelBoxCount++
			}
		}
	}

	overlap := false
	// That's fine, could be overlapped
	fromOrientation := gSource.Orientation(gTarget)
	if fromOrientation == geo.NONE {
		overlap = true
	}
	toOrientation := gTarget.Orientation(gSource)
	if toOrientation == geo.NONE {
		overlap = true
	}

	quickRoute, distance, err := router.quickRoute(ctx, gEdge, overlap, fromOrientation)
	if err != nil {
		return nil, 0, err
	}
	if quickRoute != nil {
		return quickRoute, distance, nil
	}

	// Quick routes need no search scratch. For the next search, clear only
	// states reached by the previous edge on this visibility graph.
	nodeCount := len(g.Nodes)
	if len(router.nodeContext) != nodeCount {
		if err := router.work.reserveProduct(uint64(nodeCount), 4); err != nil {
			return nil, 0, err
		}
		router.carveSearchState(nodeCount)
	} else {
		if err := router.work.reserveProduct(uint64(len(router.touchedNodes)), 3); err != nil {
			return nil, 0, err
		}
		for _, i := range router.touchedNodes {
			router.verticalHops[i] = nil
			router.horizontalHops[i] = nil
			router.nodeContext[i] = nil
		}
	}
	router.touchedNodes = router.touchedNodes[:0]
	verticalHops, horizontalHops, nodeContext := router.verticalHops, router.horizontalHops, router.nodeContext

	var nonAncestors layoutgraph.Nodes
	if gEdge.SourceArrowheadLabel != nil || gEdge.TargetArrowheadLabel != nil {
		nonAncestors, err = filterEdgeAncestorsGuarded(gEdge, router.graph.Nodes, router.work)
		if err != nil {
			return nil, 0, err
		}
	}

	sourceContainer := gSource.Container
	targetContainer := gTarget.Container
	isSourceDescendantOfTarget, err := routeSearchIsDescendantOf(router.work, gSource, gTarget)
	if err != nil {
		return nil, 0, err
	}
	isTargetDescendantOfSource, err := routeSearchIsDescendantOf(router.work, gTarget, gSource)
	if err != nil {
		return nil, 0, err
	}

	// Fast access to find if a node is a cluster node (cluster nodes sharing routes is nice)
	sourceClusterNodes, targetClusterNodes, err := sourceAndTargetClusterNodesGuarded(originalGraph, gSource, gTarget, router.work)
	if err != nil {
		return nil, 0, err
	}
	isSourceCluster := len(sourceClusterNodes) > 0
	isTargetCluster := len(targetClusterNodes) > 0

	undesirableClusterArrangement := router.isUndesirableClusterArrangement(gSource, gTarget)

	// These ports are populated source/target is a cluster, and the orientation is nonambiguous (e.g. not diagonal)
	// When such a case arises, we want to prefer edges that make the connections face-to-face

	var allowSrcPortSharing, allowDstPortSharing bool
	preferFacingPorts := false
	nonFacingPortsCost := 0.0
	if (isSourceCluster || isTargetCluster) && !undesirableClusterArrangement {
		// use facing ports for properly arranged clusters
		preferFacingPorts = true
		nonFacingPortsCost = router.turnCost * 2
		allowSrcPortSharing = true
		allowDstPortSharing = true
		for _, port := range gSource.PortsByOrientation(fromOrientation.GetOpposite()) {
			if err := router.work.step(); err != nil {
				return nil, 0, err
			}
			fromFacingPorts[port] = true
		}
		for _, port := range gTarget.PortsByOrientation(toOrientation.GetOpposite()) {
			if err := router.work.step(); err != nil {
				return nil, 0, err
			}
			toFacingPorts[port] = true
		}
	}

	var sharedRouteCost float64 = basicallyInfinity
	blockedSourcePorts := borrowPortMap()
	defer returnPortMap(blockedSourcePorts)
	blockedTargetPorts := borrowPortMap()
	defer returnPortMap(blockedTargetPorts)

	if !gEdge.HasTableColumn() {
		if err := router.work.reserveProduct(uint64(len(router.ovg.Ports[gSource])), uint64(len(router.ovg.Ports[gTarget]))); err != nil {
			return nil, 0, err
		}
		// Copy overlapping ports into our pooled maps
		sourceOverlaps := gSource.OverlappingPorts(gTarget)
		maps.Copy(blockedSourcePorts, sourceOverlaps)
		targetOverlaps := gTarget.OverlappingPorts(gSource)
		maps.Copy(blockedTargetPorts, targetOverlaps)
	} else {
		sourcePorts, targetPorts, err := router.tablePorts(gEdge, gSource, gTarget, false)
		if err != nil {
			return nil, 0, err
		}
		// A column end may only use its column's ports, and may share them
		// with the routes of the other end's neighbours.
		switch {
		case gEdge.IsBetweenTableColumns():
			sharedRouteCost = basicallyInfinity / 10
			allowSrcPortSharing = true
			allowDstPortSharing = true
			if err := router.blockPortsOutside(gSource, sourcePorts, blockedSourcePorts); err != nil {
				return nil, 0, err
			}
			if err := router.blockPortsOutside(gTarget, targetPorts, blockedTargetPorts); err != nil {
				return nil, 0, err
			}
			if err := router.markAdjacentNodes(gSource, targetClusterNodes); err != nil {
				return nil, 0, err
			}
			if err := router.markAdjacentNodes(gTarget, sourceClusterNodes); err != nil {
				return nil, 0, err
			}
		case gEdge.FromTableColumnIndex != nil:
			allowSrcPortSharing = true
			if err := router.blockPortsOutside(gSource, sourcePorts, blockedSourcePorts); err != nil {
				return nil, 0, err
			}
			if err := router.markAdjacentNodes(gTarget, sourceClusterNodes); err != nil {
				return nil, 0, err
			}
		case gEdge.ToTableColumnIndex != nil:
			allowDstPortSharing = true
			if err := router.blockPortsOutside(gTarget, targetPorts, blockedTargetPorts); err != nil {
				return nil, 0, err
			}
			if err := router.markAdjacentNodes(gSource, targetClusterNodes); err != nil {
				return nil, 0, err
			}
		}
	}

	idealTurnAxes := idealTurnAxes(gSource, gTarget)

	sourcePortsUsed, targetPortsUsed, duplicateSourcePortsUsed, duplicateTargetPortsUsed, sharedClusterSourcePortsUsed, sharedClusterTargetPortsUsed, err := router.usedPorts(
		ctx, gSource, gTarget, source, target, sourceClusterNodes, targetClusterNodes, undesirableClusterArrangement,
	)
	if err != nil {
		return nil, 0, err
	}
	nodeIsCenterSymmetricalPort, err := router.centerSymmetricalPorts(ctx, gSource, gTarget, sourcePortsUsed, targetPortsUsed)
	if err != nil {
		return nil, 0, err
	}

	if !router.fixedOverlapsSet {
		router.fixedOverlaps, err = fixedOverlapsForRoute(layoutgraph.Nodes(originalGraph.Nodes), router.work)
		if err != nil {
			return nil, 0, err
		}
		router.fixedOverlapsSet = true
	}
	fixedOverlaps := router.fixedOverlaps

	// Create the initial source node context
	sourceContext := &router.nodeContextValues[source.Index]
	*sourceContext = searchNodeContext{
		verticalDistance:   math.Inf(1),
		horizontalDistance: math.Inf(1),
	}
	nodeContext[source.Index] = sourceContext
	router.touchedNodes = append(router.touchedNodes, source.Index)

	// Priority queue storage is reusable because a flavor routes its edges
	// sequentially.
	pq := &router.searchScratch().queue
	pq.reset()

	// Each node can be visited twice -- once from each direction. Seed the
	// vertical state first to make the equal-cost source preference explicit.
	if _, err := pq.push(0, source, false, router.work); err != nil {
		return nil, 0, err
	}
	if _, err := pq.push(0, source, true, router.work); err != nil {
		return nil, 0, err
	}
	// We've already pre-allocated these slices above

	for !pq.empty() {
		if err := router.work.step(); err != nil {
			return nil, 0, err
		}
		// Get the least distance node, regardless of direction
		leastDistanceEntry, err := pq.pop(router.work)
		if err != nil {
			if errors.Is(err, errRouteSearchWorkLimit) || isContextTermination(err) {
				return nil, 0, err
			}
			return nil, 0, invariant.New(
				fmt.Sprintf(
					"Path not found for '%s'. Error %v",
					gEdge.DebugID(),
					err,
				),
			)
		}
		isFromHorizontal := leastDistanceEntry.isHorizontal
		leastDistanceNode := leastDistanceEntry.node
		leastDistance := leastDistanceEntry.priority

		if leastDistanceNode == target {
			routeNodes, err := router.bestRoute(ctx, gSource, gTarget, source, target, nodeContext, verticalHops, horizontalHops)
			if err != nil {
				return nil, 0, err
			}
			return routeNodes, leastDistance, nil
		}

		var lastNode *OVGNode
		if isFromHorizontal {
			lastNode = horizontalHops[leastDistanceNode.Index]
		} else {
			lastNode = verticalHops[leastDistanceNode.Index]
		}

		// Reset occupiedEdges array for reuse
		occupiedEdges = occupiedEdges[:0]
		if (leastDistanceNode != source) && (leastDistanceNode != target) {
			pointRoutes := router.routesAtNode(leastDistanceNode)
			for _, route := range pointRoutes {
				if err := router.work.step(); err != nil {
					return nil, 0, err
				}
				occupiedEdges = append(occupiedEdges, route.GEdge)
			}
		}

		isOnNodeOfRoute := len(occupiedEdges) > 0
		areAllOccupiedRoutesShareable, err := edgeCanOverlapEdgesGuarded(gEdge, occupiedEdges, sourceClusterNodes, targetClusterNodes, router.work)
		if err != nil {
			return nil, 0, err
		}

		for _, e := range leastDistanceNode.Edges {
			if err := router.work.step(); err != nil {
				return nil, 0, err
			}
			adjacentNode := leastDistanceNode.Adjacent(e)
			if adjacentNode == lastNode {
				continue
			}

			// Every traversal cost is nonnegative. An arrival that already
			// costs at least as much as this neighbor's best arrival on the
			// same axis cannot improve it, even before checking penalties.
			if known := nodeContext[adjacentNode.Index]; known != nil {
				best := known.verticalDistance
				if adjacentNode.Y == leastDistanceNode.Y {
					best = known.horizontalDistance
				}
				if leastDistance >= best {
					continue
				}
			}

			// Prevent exiting source port node at weird angles
			if sourcePort, ok := leastDistanceNode.portMetadataFor(gSource); ok {
				validExit := sourcePort.directions.any(func(direction geo.Orientation) bool {
					// When overlapped, the requirements are relaxed a little.
					if overlap {
						switch direction {
						case geo.Top, geo.Bottom:
							return adjacentNode.Point.Y != leastDistanceNode.Point.Y
						case geo.Right, geo.Left:
							return adjacentNode.Point.X != leastDistanceNode.Point.X
						default:
							return true
						}
					}

					switch direction {
					case geo.Top:
						return adjacentNode.Point.Y < leastDistanceNode.Point.Y
					case geo.Bottom:
						return adjacentNode.Point.Y > leastDistanceNode.Point.Y
					case geo.Right:
						return adjacentNode.Point.X > leastDistanceNode.Point.X
					case geo.Left:
						return adjacentNode.Point.X < leastDistanceNode.Point.X
					default:
						return true
					}
				})
				if !validExit {
					continue
				}
			}

			if targetPort, ok := adjacentNode.portMetadataFor(gTarget); ok {
				if _, blocked := blockedTargetPorts[*adjacentNode.Point]; blocked {
					continue
				}

				// Prevent approaching target port node from weird angles. Coincident
				// rounded ports can represent several sides of the same node, so one
				// valid approach is enough.
				validApproach := targetPort.directions.any(func(direction geo.Orientation) bool {
					if overlap {
						switch direction {
						case geo.Top, geo.Bottom:
							return adjacentNode.Point.Y != leastDistanceNode.Point.Y
						case geo.Right, geo.Left:
							return adjacentNode.Point.X != leastDistanceNode.Point.X
						default:
							return true
						}
					}

					switch direction {
					case geo.Top:
						return adjacentNode.Point.Y > leastDistanceNode.Point.Y
					case geo.Bottom:
						return adjacentNode.Point.Y < leastDistanceNode.Point.Y
					case geo.Right:
						return adjacentNode.Point.X < leastDistanceNode.Point.X
					case geo.Left:
						return adjacentNode.Point.X > leastDistanceNode.Point.X
					default:
						return true
					}
				})
				if !validApproach {
					continue
				}
			}

			if leastDistanceNode != source && adjacentNode != target &&
				!adjacentNode.isPortOf(gSource) &&
				!adjacentNode.isPortOf(gTarget) {
				// Cannot go through a container unless the target or source is a descendant of that container (or is that container)
				if adjacentNode.Container != nil {
					sourceInside, err := routeSearchIsDescendantOf(router.work, sourceContainer, adjacentNode.Container)
					if err != nil {
						return nil, 0, err
					}
					targetInside, err := routeSearchIsDescendantOf(router.work, targetContainer, adjacentNode.Container)
					if err != nil {
						return nil, 0, err
					}
					if !sourceInside && !targetInside {
						if _, in := fixedOverlaps[adjacentNode.Container]; !in {
							continue
						}
					}
				}
			}

			// only use adjacent nodes that are vertically or horizontally aligned with leastDistanceNode (no diagonals), unless using source/target center
			if (adjacentNode != target) && (leastDistanceNode != source) {
				if (adjacentNode.Point.X != leastDistanceNode.Point.X) && (adjacentNode.Point.Y != leastDistanceNode.Point.Y) {
					continue
				}
			}

			if adjacentNode.isPortOf(gSource) {
				if _, blocked := blockedSourcePorts[*adjacentNode.Point]; blocked {
					continue
				}
			}

			var nextDistance float64
			if leastDistanceNode == source {
				nextDistance = 1
				// To avoid overlapping duplicate edges, avoid using the same source port as a duplicate edge
				if !allowSrcPortSharing && duplicateSourcePortsUsed[*adjacentNode.Point] {
					nextDistance = basicallyInfinity
				}
				// ClusterPortSharing Step 2a.
				// penalize not using the same source port if another node in the cluster already connects to that source
				if len(sharedClusterSourcePortsUsed) > 0 {
					if _, isShared := sharedClusterSourcePortsUsed[*adjacentNode.Point]; !isShared {
						// If it takes more than 2 turns to get there, not worth sharing
						nextDistance += (2 * router.turnCost)
					}
				}
				if _, is := fromFacingPorts[*adjacentNode.Point]; !is && preferFacingPorts {
					nextDistance += nonFacingPortsCost
				}

				// Looks nicer when arrows come out of center ports
				if !adjacentNode.isCenterPortOf(gSource) {
					nextDistance += router.nonCenterPortCost
				} else {
					// Looks even slightly nicer when it comes out a port that has an edge going through the symmetrical port
					if _, is := nodeIsCenterSymmetricalPort[adjacentNode]; !is {
						// Only prefer it all else being equal
						nextDistance += 1
					}
				}
			} else {
				nextDistance = e.Distance
			}

			// Cannot go outside a container if both the source and target are inside that container
			// but for nested going to parent, there may not be enough paths otherwise, so we allow that
			if sourceContainer != nil && sourceContainer == targetContainer && !(isSourceDescendantOfTarget || isTargetDescendantOfSource) {
				if adjacentNode.Container == nil || adjacentNode.Container != sourceContainer {
					// Note: penalty is if adjacent is outside the source container, and the edge from least distance to adjacent enters a different container
					if leastDistanceNode.Container != adjacentNode.Container {
						nextDistance += router.turnCost * 4
					}
				}
			}

			// Penalize routing between ports of the same node
			if leastDistanceNode.sharesPortOwner(adjacentNode) {
				nextDistance += router.turnCost * 4
			}

			// APPLY SPECIAL WEIGHTS --------------
			if adjacentNode == target {
				nextDistance = 1
				// To avoid overlapping duplicate edges, don't use the same target port as a duplicate edge
				if !allowDstPortSharing && duplicateTargetPortsUsed[*leastDistanceNode.Point] {
					nextDistance = basicallyInfinity
				}
				// ClusterPortSharing Step 2b.
				// penalize not using the same target port if another node in the cluster already connects to that target
				if len(sharedClusterTargetPortsUsed) > 0 {
					if _, isShared := sharedClusterTargetPortsUsed[*leastDistanceNode.Point]; !isShared {
						// If it takes more than 2 turns to get there, not worth sharing
						nextDistance += (2 * router.turnCost)
					}
				}
				if _, is := toFacingPorts[*leastDistanceNode.Point]; !is && preferFacingPorts {
					nextDistance += nonFacingPortsCost
				}

				if leastDistanceNode.isPortOf(gTarget) {
					if !leastDistanceNode.isCenterPortOf(gTarget) {
						nextDistance += router.nonCenterPortCost
					} else {
						if _, is := nodeIsCenterSymmetricalPort[leastDistanceNode]; !is {
							nextDistance += 1
						}
					}
				}
			} else if adjacentNode.IsNodeCenter {
				nextDistance = basicallyInfinity
			} else {
				// Reset overlappingEdges array for reuse
				overlappingEdges = overlappingEdges[:0]
				isProhibitedSharing := false

				if leastDistanceNode != source && leastDistanceNode != target {
					routes := router.overlappingRoutesOf(e)
					for _, route := range routes {
						if err := router.work.step(); err != nil {
							return nil, 0, err
						}
						overlappingEdges = append(overlappingEdges, route.GEdge)
					}
					canOverlap, err := edgeCanOverlapEdgesGuarded(gEdge, overlappingEdges, sourceClusterNodes, targetClusterNodes, router.work)
					if err != nil {
						return nil, 0, err
					}
					// Only a shareable edge needs the more expensive geometric checks.
					isProhibitedSharing = !canOverlap
					if canOverlap {
						isProhibitedSharing, err = router.routesBlockTraversal(routes, leastDistanceNode, adjacentNode, gEdge.IsDirected())
						if err != nil {
							return nil, 0, err
						}
					}
				}

				isAdjacentNodeOnRoute := false
				if !isProhibitedSharing && (leastDistanceNode != source) && (leastDistanceNode != target) {
					isAdjacentNodeOnRoute = len(router.routesAtNode(adjacentNode)) > 0
				}

				if !isProhibitedSharing && !isOnNodeOfRoute && !isAdjacentNodeOnRoute && (leastDistanceNode != source) {
					if router.isNearbyEdge(e) {
						isProhibitedSharing = true
					}
				}

				if isProhibitedSharing {
					nextDistance = sharedRouteCost
				} else {
					isCrossing := false
					// There's two cases of crossing:
					// 1. When an OVG node is at the intersection
					// 2. When there isn't

					// Case 1
					if isOnNodeOfRoute && !isAdjacentNodeOnRoute && !areAllOccupiedRoutesShareable {
						isCrossing = true
					}

					// Case 2
					if !isCrossing && (leastDistanceNode != source) {
						// If coming from source or going towards target, it'll intersect with other edges, and that's fine
						// Even if node is not on a route, still have to check for if it's crossing while not sharing a node.
						//       +
						//       |
						//       |
						// +-----------+
						//       |
						//       |
						//       +
						if err := router.work.step(); err != nil {
							return nil, 0, err
						}
						isCrossing = router.crossings.isCrossed(e)
					}

					if isCrossing {
						nextDistance += router.crossingCost
					}
				}
			}

			// Check intersection with positioned labels (avoiding allocation)
			for i := range router.positionedLabels {
				if err := router.work.step(); err != nil {
					return nil, 0, err
				}
				if segmentIntersectsBox(e.From.Point, e.To.Point, &router.positionedLabels[i].Box) {
					nextDistance += basicallyInfinity
					break
				}
			}

			// Check intersection with node label boxes: one step per box and one
			// turn cost per intersected box, added one at a time as before.
			if nodeLabelBoxCount > 0 {
				if err := router.work.stepN(nodeLabelBoxCount); err != nil {
					return nil, 0, err
				}
				for _, hit := range router.labelBoxes.hitsFor(e) {
					if n := router.labelBoxes.nodes[hit]; n != gSource && n != gTarget {
						nextDistance += router.turnCost
					}
				}
			}

			// Handle arrow labels
			if leastDistanceNode.isPortOf(gSource) {
				if l := gEdge.SourceArrowheadLabel; l != nil {
					routePoints[0] = leastDistanceNode.Point
					routePoints[1] = adjacentNode.Point
					al := labeling.PositionArrowheadLabel(gEdge, false, routePoints[:])
					labelCost, err := positionedArrowheadLabelCostGuarded(*al, nonAncestors, router.positionedLabels, router.routes, nil, router.work)
					if err != nil {
						return nil, 0, err
					}
					nextDistance += labelCost
				}
			}
			if adjacentNode.isPortOf(gTarget) {
				if l := gEdge.TargetArrowheadLabel; l != nil {
					routePoints[0] = leastDistanceNode.Point
					routePoints[1] = adjacentNode.Point
					al := labeling.PositionArrowheadLabel(gEdge, true, routePoints[:])
					labelCost, err := positionedArrowheadLabelCostGuarded(*al, nonAncestors, router.positionedLabels, router.routes, nil, router.work)
					if err != nil {
						return nil, 0, err
					}
					nextDistance += labelCost
				}
			}

			// Don't count coming from source or heading to target as turns
			if lastNode != nil && ((adjacentNode != target) && (leastDistanceNode != source)) && (lastNode != source) {
				turns := (!isFromHorizontal && (adjacentNode.Point.X != leastDistanceNode.Point.X)) ||
					(isFromHorizontal && (adjacentNode.Point.Y != leastDistanceNode.Point.Y))
				if turns {
					// Knock off a few points for turning at an ideal axis
					multiplier := 1.0
					for _, idealTurnAxis := range idealTurnAxes {
						if err := router.work.step(); err != nil {
							return nil, 0, err
						}
						if idealTurnAxis.isX {
							if math.Abs(adjacentNode.Point.X-idealTurnAxis.val) <= idealTurnAxisTolerance {
								multiplier = idealTurnMultiplier
								break
							}
						} else {
							if math.Abs(adjacentNode.Point.Y-idealTurnAxis.val) <= idealTurnAxisTolerance {
								multiplier = idealTurnMultiplier
								break
							}
						}
					}
					if multiplier != 1.0 {
						if gEdge.From.Cluster != nil && (gSource.Cluster.Arrangement == gSource.Cluster.DesiredArrangement) && len(gEdge.From.Cluster.Nodes)%2 == 0 {
							multiplier = idealTurnEvenClusterMultiplier
						}
					}
					// Turns right or left
					if !isFromHorizontal && (adjacentNode.Point.X != leastDistanceNode.Point.X) {
						nextDistance += (router.turnCost * multiplier)
						// Not allowed to turn too close to the target, looks ugly
						// but you gotta do what you gotta do for weird situations like child routing to container
						if !(isSourceDescendantOfTarget || isTargetDescendantOfSource) {
							if leastDistanceNode.distanceToBoundary(gTarget) <= turnEndpointClearance {
								nextDistance += router.turnCost
							}
							if leastDistanceNode.distanceToBoundary(gSource) <= turnEndpointClearance {
								nextDistance += router.turnCost
							}
						}
					}
					// Turns up or down
					if isFromHorizontal && (adjacentNode.Point.Y != leastDistanceNode.Point.Y) {
						nextDistance += (router.turnCost * multiplier)
						if !(isSourceDescendantOfTarget || isTargetDescendantOfSource) {
							if leastDistanceNode.distanceToBoundary(gTarget) <= turnEndpointClearance {
								nextDistance += router.turnCost
							}
							if leastDistanceNode.distanceToBoundary(gSource) <= turnEndpointClearance {
								nextDistance += router.turnCost
							}
						}
					}
				}
			}

			// All else being equal, penalize taking routes that are too close to nodes
			if leastDistanceNode != source && adjacentNode != target && len(adjacentNode.IsNearPort) > 0 {
				if _, is := adjacentNode.IsNearPort[gSource]; is {
					nextDistance += nodeProximityPenalty
				} else if _, is := adjacentNode.IsNearPort[gTarget]; is {
					nextDistance += nodeProximityPenalty
				}
			}

			maybeNewDistance := leastDistance + nextDistance

			// Create node context if it doesn't exist
			if nodeContext[adjacentNode.Index] == nil {
				adjacentContext := &router.nodeContextValues[adjacentNode.Index]
				*adjacentContext = searchNodeContext{
					verticalDistance:   math.Inf(1),
					horizontalDistance: math.Inf(1),
				}
				nodeContext[adjacentNode.Index] = adjacentContext
				router.touchedNodes = append(router.touchedNodes, adjacentNode.Index)
			}

			// Horizontal path
			if adjacentNode.Point.Y == leastDistanceNode.Point.Y {
				if maybeNewDistance < nodeContext[adjacentNode.Index].horizontalDistance {
					// If we already visited this node horizontally, enqueue the current hop
					// to potentially be revisited
					if horizontalHops[adjacentNode.Index] != nil {
						if _, err := pq.push(
							nodeContext[horizontalHops[adjacentNode.Index].Index].horizontalDistance,
							horizontalHops[adjacentNode.Index],
							true,
							router.work,
						); err != nil {
							return nil, 0, err
						}
					}

					// Create or update entry
					if nodeContext[adjacentNode.Index].horizontalEntry == nil {
						horizontalEntry, err := pq.push(maybeNewDistance, adjacentNode, true, router.work)
						if err != nil {
							return nil, 0, err
						}
						nodeContext[adjacentNode.Index].horizontalEntry = horizontalEntry
					} else {
						err = pq.decrease(nodeContext[adjacentNode.Index].horizontalEntry, maybeNewDistance, router.work)
						if err != nil {
							return nil, 0, err
						}
					}

					nodeContext[adjacentNode.Index].horizontalDistance = maybeNewDistance
					horizontalHops[adjacentNode.Index] = leastDistanceNode
				}
			} else { // Vertical path
				if maybeNewDistance < nodeContext[adjacentNode.Index].verticalDistance {
					// If we already visited this node vertically, enqueue the current hop
					// to potentially be revisited
					if verticalHops[adjacentNode.Index] != nil {
						if _, err := pq.push(
							nodeContext[verticalHops[adjacentNode.Index].Index].verticalDistance,
							verticalHops[adjacentNode.Index],
							false,
							router.work,
						); err != nil {
							return nil, 0, err
						}
					}

					// Create or update entry
					if nodeContext[adjacentNode.Index].verticalEntry == nil {
						verticalEntry, err := pq.push(maybeNewDistance, adjacentNode, false, router.work)
						if err != nil {
							return nil, 0, err
						}
						nodeContext[adjacentNode.Index].verticalEntry = verticalEntry
					} else {
						err = pq.decrease(nodeContext[adjacentNode.Index].verticalEntry, maybeNewDistance, router.work)
						if err != nil {
							return nil, 0, err
						}
					}

					nodeContext[adjacentNode.Index].verticalDistance = maybeNewDistance
					verticalHops[adjacentNode.Index] = leastDistanceNode
				}
			}
		}
	}

	return nil, 0, fmt.Errorf("path not found for '%s'. Queue is empty", gEdge.DebugID())
}

func (router *ovgEdgeRouter) routesBlockTraversal(routes []*Route, from, to *OVGNode, directed bool) (bool, error) {
	if !directed && !to.IsTunnel {
		return false, nil
	}
	for _, route := range routes {
		if err := router.work.step(); err != nil {
			return false, err
		}
		if directed && route.GEdge.IsDirected() {
			opposing, err := router.hasOpposingSegment(route, from, to)
			if err != nil || opposing {
				return opposing, err
			}
		}
		if to.IsTunnel {
			if err := router.work.add(uint64(len(route.OVGNodes))); err != nil {
				return false, err
			}
			if route.isEntireColinear(from, to) {
				return true, nil
			}
		}
	}
	return false, nil
}

// If two non-diagonal nodes are below (MinRouteNodeClearance+1) * 2 from each other and connected, there won't be a direct path, and it'll take a roundabout path which can block other paths.
// To alleviate this, we directly connect fromFacingPort to toFacingPort, no need to search
func (router *ovgEdgeRouter) quickRoute(ctx context.Context, gEdge *layoutgraph.Edge, overlap bool, fromOrientation geo.Orientation) ([]*OVGNode, float64, error) {
	if err := router.bindWork(ctx); err != nil {
		return nil, 0, err
	}
	if err := checkEdgeRoutingCanceled(ctx); err != nil {
		return nil, 0, err
	}
	g := router.ovg
	originalGraph := router.graph
	gSource := gEdge.From
	gTarget := gEdge.To
	source := g.Centers[gSource]
	target := g.Centers[gTarget]
	if !overlap && !fromOrientation.IsDiagonal() && gSource.DistanceTo(gTarget, true) < 2*(layoutgraph.MinRouteNodeClearance+1) && (gSource.DistanceTo(gTarget, true) > layoutgraph.MinArrowheadClearance) {
		edges := make([]*layoutgraph.Edge, 0)
		for _, route := range router.routes {
			if err := router.work.add(uint64(len(route.OVGNodes)) + 1); err != nil {
				return nil, 0, err
			}
			edge := layoutgraph.NewEdge(route.GEdge.From, route.GEdge.To)
			edge.Points = route.createSegmentEndpoints()
			edge.ReversedForLayout = route.GEdge.ReversedForLayout
			edge.Style.StrokeWidth = route.GEdge.Style.StrokeWidth
			edge.SourceArrowhead = route.GEdge.SourceArrowhead
			edge.TargetArrowhead = route.GEdge.TargetArrowhead
			edge.SourceArrowheadLabel = route.GEdge.SourceArrowheadLabel
			edge.TargetArrowheadLabel = route.GEdge.TargetArrowheadLabel
			edge.Label = route.GEdge.Label
			edges = append(edges, edge)
		}
		if err := checkEdgeRoutingCanceled(ctx); err != nil {
			return nil, 0, err
		}
		sourcePort, targetPort, lineCost, err := routeLineGuarded(originalGraph, gEdge, edges, nil, router.work)

		if err == nil {
			return []*OVGNode{
				source,
				NewOVGNode(sourcePort),
				NewOVGNode(targetPort),
				target,
			}, lineCost, nil
		}
		if isContextTermination(err) {
			return nil, 0, err
		}
	}
	return nil, 0, nil
}

func (router *ovgEdgeRouter) isUndesirableClusterArrangement(gSource, gTarget *layoutgraph.Node) bool {
	// If the cluster nodes are in a bad arrangement for the gSource->gTarget edge, e.g. c1 and c2 to t
	// then they should not be treated as clusters for the purposes of route searching
	// NOTE that this will only take effect if cluster optimizing is included as a prior stage
	//
	// ┌──────────┐
	// │    c1    │
	// │          │
	// └──────────┘
	//
	// ┌──────────┐
	// │   c2     │
	// │          │
	// └──────────┘
	//
	//    ┌───┐
	//    │   │
	//    │t  │
	//    └───┘
	if gSource.Cluster != nil {
		if !clusterHasDesirableArrangementTo(gSource.Cluster, gTarget) {
			return true
		}
	}
	if gTarget.Cluster != nil {
		if !clusterHasDesirableArrangementTo(gTarget.Cluster, gSource) {
			return true
		}
	}
	return false
}

func clusterHasDesirableArrangementTo(cluster *layoutgraph.Cluster, node *layoutgraph.Node) bool {
	bounds := cluster.Vessel
	if !cluster.IsActive() {
		// The vessel may be out of sync after cluster placement. Cluster nodes
		// remain arranged in visual order, so their first and last boxes define
		// the same routing extent used by the original implementation.
		topLeft := cluster.Nodes[0].TopLeft.Copy()
		last := cluster.Nodes[len(cluster.Nodes)-1]
		bottomRight := last.TopLeft.Copy()
		bottomRight.X += last.Width
		bottomRight.Y += last.Height
		bounds = &layoutgraph.Node{Box: geo.Box{
			TopLeft: topLeft,
			Width:   bottomRight.X - topLeft.X,
			Height:  bottomRight.Y - topLeft.Y,
		}}
	}
	switch bounds.Orientation(node) {
	case geo.Top, geo.Bottom:
		return cluster.Arrangement == layoutgraph.Row
	case geo.Left, geo.Right:
		return cluster.Arrangement == layoutgraph.Column
	default:
		return false
	}
}

// bestRoute finds the lowest-cost route to target, choosing between vertical
// and horizontal hops.
func (router *ovgEdgeRouter) bestRoute(
	ctx context.Context,
	gSource, gTarget *layoutgraph.Node,
	source, target *OVGNode,
	nodeContext []*searchNodeContext,
	verticalHops, horizontalHops []*OVGNode) ([]*OVGNode, error) {
	if err := router.bindWork(ctx); err != nil {
		return nil, err
	}
	sequence := make([]*OVGNode, 0)

	idealTurnAxes := idealTurnAxes(gSource, gTarget)

	curr := target
	var prev *OVGNode
	var next *OVGNode
	visited := make(map[*OVGNode]struct{})
	for curr != nil {
		if err := router.work.step(); err != nil {
			return nil, err
		}
		if _, ok := visited[curr]; ok {
			// Cycle detected, take corrective action
			break
		}
		visited[curr] = struct{}{}
		sequence = append(sequence, curr)

		var isHorizontal bool
		entry := nodeContext[curr.Index]
		verticalDistance := entry.verticalDistance
		horizontalDistance := entry.horizontalDistance
		if (verticalHops[curr.Index] != nil) && (horizontalHops[curr.Index] != nil) {
			if verticalDistance < horizontalDistance {
				next = verticalHops[curr.Index]
				isHorizontal = false
			} else {
				next = horizontalHops[curr.Index]
				isHorizontal = true
			}
		} else if verticalHops[curr.Index] != nil {
			next = verticalHops[curr.Index]
			isHorizontal = false
		} else {
			next = horizontalHops[curr.Index]
			isHorizontal = true
		}

		// Because the cost of a turn is baked into the node (node T) it turns into, and not the intersection (node I)
		// When another route comes in without a turn, it'll set lower weights from node T onwards, erasing the cost of turn,
		// But node I is not changed, and the best route would still be the one with the turn
		// So we reintroduce the cost to node I here. That way, we only turn if the cost of it PLUS the turn is better than the route without the turn
		// If next is source, we're at a port and disregard the turn
		if (prev != nil) && (next != nil) && (next != source) {
			// If the next one leads to a turn ...
			if ((prev.Point.X == curr.Point.X) && isHorizontal) ||
				((prev.Point.Y == curr.Point.Y) && !isHorizontal) {
				var alternativeDistance float64
				var alternative *OVGNode
				multiplier := 1.0

				for _, idealTurnAxis := range idealTurnAxes {
					if err := router.work.step(); err != nil {
						return nil, err
					}
					if idealTurnAxis.isX {
						if math.Abs(curr.Point.X-idealTurnAxis.val) <= idealTurnAxisTolerance {
							multiplier = idealTurnMultiplier
							break
						}
					} else {
						if math.Abs(curr.Point.Y-idealTurnAxis.val) <= idealTurnAxisTolerance {
							multiplier = idealTurnMultiplier
							break
						}
					}
				}
				if multiplier != 1.0 {
					if gSource.Cluster != nil && (gSource.Cluster.Arrangement == gSource.Cluster.DesiredArrangement) && len(gSource.Cluster.Nodes)%2 == 0 {
						multiplier = idealTurnEvenClusterMultiplier
					}
				}
				// And the margin is only by turn distance ...
				if isHorizontal && (geo.PrecisionCompare(horizontalDistance+router.turnCost*multiplier, verticalDistance, geo.PRECISION) > 0) {
					// Then we should use the other direction
					alternativeDistance = verticalDistance
					alternative = verticalHops[curr.Index]
				}
				if !isHorizontal && (geo.PrecisionCompare(verticalDistance+router.turnCost*multiplier, horizontalDistance, geo.PRECISION) > 0) {
					alternativeDistance = horizontalDistance
					alternative = horizontalHops[curr.Index]
				}
				// If alternative is basically infinity, it'll cycle
				if alternative != nil && alternativeDistance < basicallyInfinity {
					next = alternative
				}
			}
		}

		prev = curr
		curr = next
	}
	for left, right := 0, len(sequence)-1; left < right; left, right = left+1, right-1 {
		if err := router.work.step(); err != nil {
			return nil, err
		}
		sequence[left], sequence[right] = sequence[right], sequence[left]
	}
	return sequence, nil
}

/*
	OVG routes only overlap if both OVG nodes share the same route.

n1
|
|
n2----n3

As all routes are either vertical or horizontal, there is no overlap between n1 and n3 because they can't share a route.
In this case, checking for overlaps is just a check for shared routes of start and end (set intersection).

However, the example below, breaks the assumption above.
Assuming t1 and t2 are tunnel nodes they generate an overlap with n1-n2 that is not a simple
set intersection operation. In this case, we need to check for all vertical edges at the same X.

t1
|
|
n1
|
n2
|
|
t2
*/
func (router *ovgEdgeRouter) findOverlappingRoutes(start, end *OVGNode) ([]*Route, error) {
	var overlappingRoutes []*Route
	routeSet := make(map[*Route]struct{})

	// find all overlapping edges
	overlaps, err := router.edgeSet.overlappingEdgesGuarded(NewOVGEdge(start, end), router.work)
	if err != nil {
		return nil, err
	}
	for _, overlapping := range overlaps {
		if err := router.work.step(); err != nil {
			return nil, err
		}
		for _, rf := range router.routesAtNode(overlapping.From) {
			if err := router.work.step(); err != nil {
				return nil, err
			}
			for _, rt := range router.routesAtNode(overlapping.To) {
				if err := router.work.step(); err != nil {
					return nil, err
				}
				// find the route that contains the edge
				// only add the route if not seen before
				if _, exists := routeSet[rt]; !exists && rt == rf {
					routeSet[rt] = struct{}{}
					overlappingRoutes = append(overlappingRoutes, rt)
				}
			}
		}
	}
	return overlappingRoutes, nil
}

func (router *ovgEdgeRouter) addRoute(route *Route) error {
	dups := make(map[*OVGNode]struct{}, len(route.OVGNodes))
	routeNodes := route.OVGNodes
	if route.GEdge.IsLoop() {
		// The source and target centers are the same node for a loop.
		routeNodes = routeNodes[1:]
	}
	for i, n := range routeNodes {
		if err := router.work.step(); err != nil {
			return err
		}
		if _, ok := dups[n]; ok {
			return fmt.Errorf("found duplicate OVGNode (%+v) at index %d in route %s", n.Point, i, route.GEdge.DebugID())
		}
		dups[n] = struct{}{}
	}

	if label := route.GEdge.SourceArrowheadLabel; label != nil {
		if err := router.work.add(uint64(len(route.OVGNodes))); err != nil {
			return err
		}
		routePoints := route.createSegmentEndpoints()
		al := labeling.PositionArrowheadLabel(route.GEdge, false, routePoints)
		router.positionedLabels = append(router.positionedLabels, *al)
	}
	if label := route.GEdge.TargetArrowheadLabel; label != nil {
		if err := router.work.add(uint64(len(route.OVGNodes))); err != nil {
			return err
		}
		routePoints := route.createSegmentEndpoints()
		al := labeling.PositionArrowheadLabel(route.GEdge, true, routePoints)
		router.positionedLabels = append(router.positionedLabels, *al)
	}

	router.routes = append(router.routes, route)
	router.routedEdges = append(router.routedEdges, route.GEdge)
	for i := 0; i < len(route.OVGNodes); i++ {
		if err := router.work.step(); err != nil {
			return err
		}
		start := route.OVGNodes[i]
		router.addRouteAtPoint(*start.Point, route)

		if i > 0 && i < len(route.OVGNodes)-2 {
			// only keep edges of the OVG path, ignore edges from port to center
			end := route.OVGNodes[i+1]
			segment := NewOVGEdge(start, end)
			before := len(router.edgeSet.edges)
			if err := router.edgeSet.addGuarded(segment, router.work); err != nil {
				return err
			}
			if before < len(router.edgeSet.edges) {
				if err := router.crossings.add(segment, router.work); err != nil {
					return err
				}
			}
		}
	}

	if !route.GEdge.IsInvisible {
		for i := 1; i < len(route.OVGNodes)-2; i++ {
			if err := router.work.step(); err != nil {
				return err
			}
			routeNode := route.OVGNodes[i]
			nextRouteNode := route.OVGNodes[i+1]
			axis, a, b := routeNode.Y, routeNode.X, nextRouteNode.X
			lines := router.crossings.horizontal
			if routeNode.X == nextRouteNode.X {
				axis, a, b = routeNode.X, routeNode.Y, nextRouteNode.Y
				lines = router.crossings.vertical
			}
			// Keep the same fractional-coordinate proximity test at the range boundaries.
			first := math.Nextafter(axis-pathNodeProximityFloor, math.Inf(-1))
			last := math.Nextafter(axis+pathNodeProximityFloor, math.Inf(1))
			err := lines.visit(first, last, min(a, b), max(a, b), router.work, func(coordinate float64, interval gridInterval) {
				if math.Abs(coordinate-axis) > pathNodeProximityFloor {
					return
				}
				router.markNearbyEdge(interval.edge)
				if coordinate == axis {
					router.addOverlappingRoute(interval.edge, route)
				}
			})
			if err != nil {
				return err
			}
		}
	}
	return router.work.check()
}

// tablePorts finds the ports to connect the columns from source to target.
// By default, it returns ports on both sides of `source` and `target`, `facingPorts` forces it to return
// ports on facing sides, if they exists (for example, if source.Orientation(target) = geo.NONE, there are no facing ports)
func (router *ovgEdgeRouter) tablePorts(edge *layoutgraph.Edge, source, target *layoutgraph.Node, facingPorts bool) (sourcePorts, targetPorts map[*OVGNode]struct{}, err error) {
	getPorts := func(node *layoutgraph.Node, columnIndex int, orientations []geo.Orientation) (map[*OVGNode]struct{}, error) {
		ports := make(map[*OVGNode]struct{})
		for _, o := range orientations {
			// Port-index derivation scans the node's incident edges to account for
			// loop offsets before indexing the already-bounded port slice.
			if err := router.work.add(uint64(len(node.Edges)) + 1); err != nil {
				return nil, err
			}
			portIndex, isTableSide := nodeshape.TablePortIndex(node.Shape, o, columnIndex)
			if !isTableSide {
				portIndex = node.Shape.PortIndices(o)[columnIndex]
			}
			ports[router.ovg.Ports[node][portIndex]] = struct{}{}
		}
		return ports, nil
	}

	var sourceOrientations, targetOrientations []geo.Orientation
	if !facingPorts {
		sourceOrientations = []geo.Orientation{geo.Right, geo.Left}
		targetOrientations = []geo.Orientation{geo.Right, geo.Left}
	} else {
		switch source.Orientation(target) {
		case geo.Left, geo.TopLeft, geo.BottomLeft:
			sourceOrientations = []geo.Orientation{geo.Right}
			targetOrientations = []geo.Orientation{geo.Left}
		case geo.Right, geo.TopRight, geo.BottomRight:
			sourceOrientations = []geo.Orientation{geo.Left}
			targetOrientations = []geo.Orientation{geo.Right}
		}
	}

	if edge.FromTableColumnIndex != nil {
		sourcePorts, err = getPorts(source, *edge.FromTableColumnIndex, sourceOrientations)
		if err != nil {
			return nil, nil, err
		}
	}
	if edge.ToTableColumnIndex != nil {
		targetPorts, err = getPorts(target, *edge.ToTableColumnIndex, targetOrientations)
		if err != nil {
			return nil, nil, err
		}
	}
	return sourcePorts, targetPorts, nil
}

// blockPortsOutside blocks every port of node that allowed does not hold.
func (router *ovgEdgeRouter) blockPortsOutside(node *layoutgraph.Node, allowed map[*OVGNode]struct{}, blocked map[geo.Point]bool) error {
	for _, port := range router.ovg.Ports[node] {
		if err := router.work.step(); err != nil {
			return err
		}
		if _, exists := allowed[port]; !exists {
			blocked[*port.Point] = true
		}
	}
	return nil
}

// markAdjacentNodes adds every node adjacent to node to into.
func (router *ovgEdgeRouter) markAdjacentNodes(node *layoutgraph.Node, into map[*layoutgraph.Node]bool) error {
	for _, e := range node.Edges {
		if err := router.work.step(); err != nil {
			return err
		}
		into[node.Adjacent(e)] = true
	}
	return nil
}
