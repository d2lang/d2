package routing

import (
	"slices"
	"sort"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// The stepped OVG construction: one guard call per elementary operation, as
// TALA built OVGs before the counted loops of ovg_fastpath.go replaced it,
// with the helpers only it uses. The tests use it as the reference those
// loops must match: the same nodes and edges in the same order, the same
// units charged and the same failures.

// addIntersectionCandidatesStepped is the candidate loop of addIntersections
// with a guard call per operation.
func (ovg *OVG) addIntersectionCandidatesStepped(xOrder, yOrder []float64, portIndex *ovgPortIndex, proximityIndex *ovgPointProximityIndex, guard *ovgBuildGuard) error {
	// TODO don't have cross section of port nodes from container to descendants
	// Cross section of all the port nodes
	for _, px := range xOrder {
		if err := guard.check(); err != nil {
			return err
		}
		for _, py := range yOrder {
			if err := guard.check(); err != nil {
				return err
			}
			// Can't be too close to any port or node
			tooClose, err := portIndex.tooClose(px, py, layoutgraph.MinPortClearance, guard)
			if err != nil {
				return err
			}
			if tooClose {
				continue
			}
			// Charge candidate inspection before doing any geometry, but keep the
			// candidate on the stack until it survives both rejection filters.
			if err := guard.step(); err != nil {
				return err
			}
			candidatePoint := geo.Point{X: px, Y: py}
			nearNode, err := proximityIndex.pointNear(px, py, guard)
			if err != nil {
				return err
			}
			if nearNode {
				continue
			}
			candidate := OVGNode{Point: &candidatePoint}
			unobstructed, err := candidate.hasUnobstructedLineToPorts(ovg, portIndex, 2, guard)
			if err != nil {
				return err
			}
			if unobstructed {
				n := NewOVGNode(geo.NewPoint(px, py))
				if _, err := guard.addNode(ovg, n); err != nil {
					return err
				}
			}
		}
	}
	return guard.check()
}

// addEdgeFillPointsStepped is the stepped edge loop of addEdgesNodes.
func (ovg *OVG) addEdgeFillPointsStepped(g *layoutgraph.Graph, hierarchies map[*layoutgraph.Node]*layoutgraph.Hierarchy, portIndex *ovgPortIndex, guard *ovgBuildGuard) error {
	for _, edge := range g.Edges {
		if err := guard.step(); err != nil {
			return err
		}
		if edge.IsLoop() {
			continue
		}
		if group := hierarchies[edge.From]; group != nil && group == hierarchies[edge.To] {
			// The hierarchy already supplies lanes between these endpoints.
			continue
		}
		fillPoints, err := ovg.perimeterPoints(edge.From, edge.To, guard)
		if err != nil {
			return err
		}
		halfwayPoints, err := ovg.halfwayPoints(edge.From, edge.To, guard)
		if err != nil {
			return err
		}
		fillPoints = append(fillPoints, halfwayPoints...)
		for _, fillPoint := range fillPoints {
			if err := guard.step(); err != nil {
				return err
			}
			if err := guard.step(); err != nil {
				return err
			}
			nearNode, err := guard.pointNearGraphNode(g, fillPoint)
			if err != nil {
				return err
			}
			if nearNode {
				continue
			}
			candidate := OVGNode{Point: fillPoint}
			unobstructed, err := candidate.hasUnobstructedLineToPorts(ovg, portIndex, 1, guard)
			if err != nil {
				return err
			}
			if unobstructed {
				fillPointNode := NewOVGNode(fillPoint)
				if _, err := guard.addNode(ovg, fillPointNode); err != nil {
					return err
				}
			}
		}
	}
	return guard.check()
}

// addPortConnectionNodesAtBoundariesStepped is addPortConnectionNodesAtBoundaries
// with a guard call per operation.
func (ovg *OVG) addPortConnectionNodesAtBoundariesStepped(g *layoutgraph.Graph, tl, br *geo.Point, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	// Don't count containers for children obstruction
	// Pad boundaries with points to allow edges to reach around
	for i := 0.0; i < extraInterestingPointLayers; i++ {
		if err := guard.step(); err != nil {
			return err
		}
		for node, ports := range ovg.Ports {
			if err := guard.step(); err != nil {
				return err
			}
			added := false
			seenPorts := make(map[*OVGNode]struct{}, len(ports))
			for _, portNode := range ports {
				if err := guard.step(); err != nil {
					return err
				}
				if _, ok := seenPorts[portNode]; ok {
					continue
				}
				seenPorts[portNode] = struct{}{}
				portDirections, _ := portNode.portDirectionsFor(node)
				var directionErr error
				portDirections.any(func(portDirection geo.Orientation) bool {
					if err := guard.step(); err != nil {
						directionErr = err
						return true
					}
					topPoint := geo.NewPoint(portNode.Point.X, tl.Y-(ovgPadding*(i+1)))
					bottomPoint := geo.NewPoint(portNode.Point.X, br.Y+(ovgPadding*(i+1)))
					leftPoint := geo.NewPoint(tl.X-(ovgPadding*(i+1)), portNode.Point.Y)
					rightPoint := geo.NewPoint(br.X+(ovgPadding*(i+1)), portNode.Point.Y)

					for _, boundaryPoint := range []*geo.Point{topPoint, bottomPoint, leftPoint, rightPoint} {
						if err := guard.step(); err != nil {
							directionErr = err
							return true
						}
						passesThroughARectangle := false
						for _, rectangle := range ovg.NodesInsideBoundingBox {
							if err := guard.step(); err != nil {
								directionErr = err
								return true
							}
							passesThrough, err := guard.passesThroughAllowingPorts(rectangle, portNode.Point, boundaryPoint, portDirection, ovg.Ports[rectangle])
							if err != nil {
								directionErr = err
								return true
							}
							if passesThrough {
								// We make exception for container
								isRectangleNodeContainer := false
								if rectangle.IsContainer() {
									descendant, err := guard.isDescendantOf(node, rectangle)
									if err != nil {
										directionErr = err
										return true
									}
									if descendant {
										isRectangleNodeContainer = true
									}
								}
								if !isRectangleNodeContainer {
									passesThroughARectangle = true
									break
								}
							}
						}
						if passesThroughARectangle {
							continue
						}
						nearNode, err := guard.pointNearGraphNode(g, boundaryPoint)
						if err != nil {
							directionErr = err
							return true
						}
						if !nearNode {
							if _, err := guard.addPoint(ovg, boundaryPoint); err != nil {
								directionErr = err
								return true
							}
							added = true
						}
					}
					return false
				})
				if directionErr != nil {
					return directionErr
				}
			}
			if !added {
				if err := ovg.createPortsConnectionsToBoundaries(node, ports, tl, br, guard); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// connectNodesStepped is connectNodes with the stepped line connector.
func (ovg *OVG) connectNodesStepped(g *layoutgraph.Graph, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	if uint64(len(ovg.Edges)) > guard.edges {
		if err := guard.reserveEdges(uint64(len(ovg.Edges)) - guard.edges); err != nil {
			return err
		}
	}
	// Create edges
	scratch := ovgLineScratchPool.Get()
	defer scratch.release()
	if cap(scratch.eligible) < len(ovg.Nodes) {
		scratch.eligible = make([]*OVGNode, 0, len(ovg.Nodes))
	}
	eligible := scratch.eligible[:0]
	for _, node := range ovg.Nodes {
		if err := guard.step(); err != nil {
			scratch.eligible = eligible
			return err
		}
		// Tunnels can only connect to each other (and to their node centers)
		if node.IsTunnel {
			continue
		}

		// sequence steps should only connect from certain sides
		restricted, err := guard.isRestrictedSequencePort(node)
		if err != nil {
			scratch.eligible = eligible
			return err
		}
		if restricted {
			continue
		}

		eligible = append(eligible, node)
	}
	scratch.eligible = eligible
	eligibleNodeCount := uint64(len(eligible))
	horizontal, vertical := scratch.groupLinesStepped(eligible)

	// sortLines charges what ordering a line map's keys charged: one step
	// per key, then the sort reservation.
	sortLines := func(lines *ovgLineGroups) error {
		for range lines.positions {
			if err := guard.step(); err != nil {
				return err
			}
		}
		return guard.reserveSortWork(len(lines.positions))
	}

	// Each eligible node appears in one horizontal and one vertical sweep.
	// The two sweeps therefore add at most (n-h) + (n-v) edges.
	// *---*---*---*---*
	// |   |   |   |   |
	// *---*---*---*---*
	// |   |   |   |   |
	// *---*---*---*---*
	// |   |   |   |   |
	// *---*---*---*---*
	h := uint64(len(horizontal.positions))
	v := uint64(len(vertical.positions))
	edgeCapacity, err := checkedOVGEdgeCapacity(uint64(len(ovg.Edges)), eligibleNodeCount, h, v, guard.limits.edges, maxIntAsUint64())
	if err != nil {
		return err
	}
	// we need to copy existing edges (e.g., we already connect the nodes when adding tunnels)
	edges := make([]*OVGEdge, len(ovg.Edges), edgeCapacity)
	copy(edges, ovg.Edges)
	ovg.Edges = edges
	boundingNodes := layoutgraph.Nodes(ovg.NodesInsideBoundingBox)
	fixedOverlaps, err := ovg.fixedOverlapsForBuild(g, boundingNodes, guard)
	if err != nil {
		return err
	}
	if err := sortLines(horizontal); err != nil {
		return err
	}
	for i, y := range horizontal.positions {
		if err := guard.step(); err != nil {
			return err
		}
		if err := ovg.connectNodesOnSameLineStepped(horizontal.line(i), y, true, fixedOverlaps, guard); err != nil {
			return err
		}
	}

	if err := sortLines(vertical); err != nil {
		return err
	}
	for i, x := range vertical.positions {
		if err := guard.step(); err != nil {
			return err
		}
		if err := ovg.connectNodesOnSameLineStepped(vertical.line(i), x, false, fixedOverlaps, guard); err != nil {
			return err
		}
	}
	return nil
}

// connectNodesOnSameLineStepped connects nodes on the same horizontal/vertical line.
// The nodes MUST be on the same line, so they are sorted and the connection is just connecting to the next
// node in the list.
// See connectNodes for more details around the algorithm
func (ovg *OVG) connectNodesOnSameLineStepped(nodes []*OVGNode, linePosition float64, isHorizontal bool, fixedOverlaps map[*layoutgraph.Node]struct{}, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}

	var intersectCandidates []*layoutgraph.Node
	for _, node := range ovg.NodesInsideBoundingBox {
		if err := guard.step(); err != nil {
			return err
		}
		if _, in := fixedOverlaps[node]; in {
			continue
		}
		if isHorizontal && node.TopLeft.Y <= linePosition && node.TopLeft.Y+node.Height >= linePosition {
			intersectCandidates = append(intersectCandidates, node)
		} else if !isHorizontal && node.TopLeft.X <= linePosition && node.TopLeft.X+node.Width >= linePosition {
			intersectCandidates = append(intersectCandidates, node)
		}
	}
	if err := guard.reserveSortWork(len(intersectCandidates)); err != nil {
		return err
	}

	sort.Slice(intersectCandidates, func(i, j int) bool {
		if isHorizontal {
			return intersectCandidates[i].TopLeft.X < intersectCandidates[j].TopLeft.X
		}
		return intersectCandidates[i].TopLeft.Y < intersectCandidates[j].TopLeft.Y
	})

	if err := guard.reserveSortWork(len(nodes)); err != nil {
		return err
	}
	sort.Slice(nodes, func(i, j int) bool {
		if isHorizontal {
			return nodes[i].X < nodes[j].X
		}
		return nodes[i].Y < nodes[j].Y
	})

	startIntersectionSearchAt := 0
	for i := 0; i < len(nodes)-1; i++ {
		if err := guard.step(); err != nil {
			return err
		}
		node := nodes[i]
		closest := nodes[i+1]
		misdirected, err := ovg.isMisdirectedPortPair(node, closest, isHorizontal, guard)
		if err != nil {
			return err
		}
		if misdirected {
			continue
		}

		shouldConnect := true
		for j := startIntersectionSearchAt; j < len(intersectCandidates); j++ {
			if err := guard.step(); err != nil {
				return err
			}
			rectangle := intersectCandidates[j]
			if (isHorizontal && rectangle.TopLeft.X > closest.X) || (!isHorizontal && rectangle.TopLeft.Y > closest.Y) {
				// here, rectangle is after closest to the right or to the bottom
				// since intersectCandidates is sorted, no other nodes will intersect, so we can skip the test
				break
			}

			passesThrough := func(from, to *OVGNode) (bool, error) {
				passes := true
				var directionErr error
				from.portDirectionsForObstacle(rectangle).any(func(direction geo.Orientation) bool {
					passesThrough, err := guard.passesThroughAllowingPorts(rectangle, from.Point, to.Point, direction, ovg.Ports[rectangle])
					if err != nil {
						directionErr = err
						return true
					}
					if !passesThrough {
						passes = false
						return true
					}
					return false
				})
				return passes, directionErr
			}
			c1, err := passesThrough(node, closest)
			if err != nil {
				return err
			}
			c2, err := passesThrough(closest, node)
			if err != nil {
				return err
			}
			if c1 && c2 {
				if rectangle.Sequence != nil {
					// sequences will have top and bottom ports overlapped by previous sequence nodes, but this overlap shouldn't prevent a connection with that port
					if !isHorizontal {
						if rectangle.TopLeft.Y == closest.Y || rectangle.TopLeft.Y+rectangle.Height == node.Y {
							continue
						}
					}
				}
				// Passing through a container is okay ...
				isContainerException := false
				if rectangle.IsContainer() {
					// ... if at least one of the nodes is within the container
					if rectangle.ContainsPointOnBox(node.Point) || rectangle.ContainsPointOnBox(closest.Point) {
						isContainerException = true
					}
				}

				if !isContainerException {
					shouldConnect = false
					break
				}
			} else {
				// this node didn't intersect with the current OVG, so we don't need to
				// check any other nodes prior to it for the next OVG nodes
				// (the loop is on top of sorted nodes)
				startIntersectionSearchAt = j
			}
		}

		if shouldConnect {
			if _, err := guard.connect(ovg, node, closest); err != nil {
				return err
			}
		}
	}
	return nil
}

// Note: assumes first node is left(IsHorizontal=true) or above(IsHorizontal=false) second
func (ovg *OVG) isMisdirectedPortPair(first, second *OVGNode, isHorizontal bool, guard *ovgBuildGuard) (bool, error) {
	if err := guard.check(); err != nil {
		return false, err
	}
	firstOwners := first.portOwners()
	secondOwners := second.portOwners()
	if len(firstOwners) == 0 || len(secondOwners) == 0 {
		return false, guard.check()
	}

	// A shared point can represent ports of several touching nodes. Reject the
	// connection only if every possible owner pair is misdirected; one valid
	// interpretation is enough to make the OVG segment usable.
	for firstOwner, firstMetadata := range firstOwners {
		if err := guard.step(); err != nil {
			return false, err
		}
		for secondOwner, secondMetadata := range secondOwners {
			if err := guard.step(); err != nil {
				return false, err
			}
			if firstOwner == secondOwner {
				return false, nil
			}

			// Since first is left/above second, at least one pair of roles must
			// face each other (not the other way around).
			correctDirections := firstMetadata.directions.has(geo.Bottom) && secondMetadata.directions.has(geo.Top)
			if isHorizontal {
				correctDirections = firstMetadata.directions.has(geo.Right) && secondMetadata.directions.has(geo.Left)
			}
			// ports must face each other unless it is a descendant and an ancestor
			firstBelowSecond, err := guard.isDescendantOf(firstOwner, secondOwner)
			if err != nil {
				return false, err
			}
			secondBelowFirst, err := guard.isDescendantOf(secondOwner, firstOwner)
			if err != nil {
				return false, err
			}
			if correctDirections || firstBelowSecond || secondBelowFirst {
				return false, nil
			}
		}
	}
	return true, guard.check()
}

// groupLinesStepped is the map grouping groupLines replaces.
func (scratch *ovgLineScratch) groupLinesStepped(eligible []*OVGNode) (*ovgLineGroups, *ovgLineGroups) {
	horizontal := map[float64][]*OVGNode{}
	vertical := map[float64][]*OVGNode{}
	for _, node := range eligible {
		horizontal[node.Y] = append(horizontal[node.Y], node)
		vertical[node.X] = append(vertical[node.X], node)
	}
	return linesFromMap(&scratch.horizontal, horizontal), linesFromMap(&scratch.vertical, vertical)
}

// linesFromMap is the map path: keys in slices.Sort order, each line the
// map's slice (nil for a NaN key, whose lookup fails).
func linesFromMap(lines *ovgLineGroups, byCoordinate map[float64][]*OVGNode) *ovgLineGroups {
	positions := make([]float64, 0, len(byCoordinate))
	for coordinate := range byCoordinate {
		positions = append(positions, coordinate)
	}
	slices.Sort(positions)
	lines.positions = positions
	lines.starts = append(lines.starts[:0], 0)
	nodes := lines.nodes[:0]
	for _, position := range positions {
		nodes = append(nodes, byCoordinate[position]...)
		lines.starts = append(lines.starts, len(nodes))
	}
	lines.nodes = nodes
	return lines
}

// hasUnobstructedLineToPorts checks if an OVG node has unobstructed lines to non-container ports
// numNodes indicates how many ports belonging to different nodes it must have lines to
func (ovgNode *OVGNode) hasUnobstructedLineToPorts(ovg *OVG, portIndex *ovgPortIndex, numNodes int, guard *ovgBuildGuard) (bool, error) {
	if err := guard.check(); err != nil {
		return false, err
	}
	// Only care about ovg nodes which are port aligned
	distinctPortNodeCount := 0

	aligned := portIndex.alignedOwners(ovgNode.X, ovgNode.Y)
	hasAlignedPort := len(aligned.byX) > 0 || len(aligned.byY) > 0
	for {
		ownerIndex, ok, err := aligned.next(guard)
		if err != nil {
			return false, err
		}
		if !ok {
			break
		}
		gNode := portIndex.owners[ownerIndex]
		portNodes := ovg.Ports[gNode]
		for _, portNode := range portNodes {
			if err := guard.step(); err != nil {
				return false, err
			}
			if portNode.X != ovgNode.X && portNode.Y != ovgNode.Y {
				continue
			}
			// Port nodes which can make a straight line between it, its shape's edge, and the point, are NOT candidates for unobstructed path
			// e.g. this is bad
			// ┌──────┐
			// │      │
			// x      │
			// ├──────┘
			// │
			// │
			// x
			//
			// this is fine
			//        ┌──────┐
			//        │      │
			// x──────x      │
			//        └──────┘
			if portNode.X == ovgNode.X && ((portNode.X == gNode.TopLeft.X) || (portNode.X == gNode.TopLeft.X+gNode.Width)) {
				continue
			}
			if portNode.Y == ovgNode.Y && ((portNode.Y == gNode.TopLeft.Y) || (portNode.Y == gNode.TopLeft.Y+gNode.Height)) {
				continue
			}

			// Remove nodes which are obstructed from a port by another node
			hasDirectLineToThisPort := true
			blockers := portIndex.horizontalBlockers[portNode.Y]
			if portNode.X == ovgNode.X {
				blockers = portIndex.verticalBlockers[portNode.X]
			}
			for _, otherGNode := range blockers {
				if err := guard.step(); err != nil {
					return false, err
				}
				if gNode == otherGNode {
					continue
				}
				if gNode.IsInvisible {
					continue
				}
				// don't count nodes of the same sequence as blocking each other
				if gNode.Sequence != nil && otherGNode.Sequence != nil && gNode.Sequence == otherGNode.Sequence {
					continue
				}
				if otherGNode.PassesThrough(portNode.Point, ovgNode.Point) {
					hasDirectLineToThisPort = false
					break
				}
			}

			if hasDirectLineToThisPort {
				distinctPortNodeCount++
				if distinctPortNodeCount == numNodes {
					return true, nil
				}
				break
			}
		}
	}

	return !hasAlignedPort, guard.check()
}

func (index *ovgPointProximityIndex) pointNear(x, y float64, guard *ovgBuildGuard) (bool, error) {
	if err := guard.step(); err != nil {
		return false, err
	}
	point := geo.Point{X: x, Y: y}
	for _, node := range index.byX[x] {
		if err := guard.step(); err != nil {
			return false, err
		}
		if node.IsPointNear(&point) {
			return true, nil
		}
	}
	return false, nil
}

func (guard *ovgBuildGuard) passesThroughAllowingPorts(node *layoutgraph.Node, p1, p2 *geo.Point, direction geo.Orientation, ports []*OVGNode) (bool, error) {
	if len(ports) == 0 {
		return node.PassesThrough(p1, p2), guard.check()
	}
	// A port can exempt a segment only when it points outwards in the supplied
	// direction. Most visibility nodes have no port owner (NONE), so do this
	// invariant direction check before scanning a shape's complete port list.
	allowsPort := false
	switch direction {
	case geo.Top:
		allowsPort = p1.X == p2.X && p1.Y > p2.Y
	case geo.Bottom:
		allowsPort = p1.X == p2.X && p1.Y < p2.Y
	case geo.Left:
		allowsPort = p1.Y == p2.Y && p1.X > p2.X
	case geo.Right:
		allowsPort = p1.Y == p2.Y && p1.X < p2.X
	}
	if allowsPort {
		for _, port := range ports {
			if err := guard.step(); err != nil {
				return false, err
			}
			if nonNilEquals(port.Point, p1) || nonNilEquals(port.Point, p2) {
				return false, nil
			}
		}
	}
	return node.PassesThrough(p1, p2), guard.check()
}

// Owner indexes are appended in ascending order during index construction.
// Merge the two axis lists so a query visits only aligned owners, once each,
// in the same order as a full owners scan. Duplicate ports and owners aligned
// on both axes do not count as distinct unobstructed graph nodes.
func (index *ovgPortIndex) alignedOwners(x, y float64) ovgAlignedOwners {
	return ovgAlignedOwners{byX: index.ownersByX[x], byY: index.ownersByY[y]}
}

type ovgAlignedOwners struct {
	byX, byY []int
}

func (owners *ovgAlignedOwners) next(guard *ovgBuildGuard) (int, bool, error) {
	if err := guard.step(); err != nil {
		return 0, false, err
	}
	if len(owners.byX) == 0 && len(owners.byY) == 0 {
		return 0, false, nil
	}
	var owner int
	if len(owners.byY) == 0 || len(owners.byX) > 0 && owners.byX[0] < owners.byY[0] {
		owner = owners.byX[0]
	} else {
		owner = owners.byY[0]
	}
	for len(owners.byX) > 0 && owners.byX[0] == owner {
		if err := guard.step(); err != nil {
			return 0, false, err
		}
		owners.byX = owners.byX[1:]
	}
	for len(owners.byY) > 0 && owners.byY[0] == owner {
		if err := guard.step(); err != nil {
			return 0, false, err
		}
		owners.byY = owners.byY[1:]
	}
	return owner, true, nil
}

func (guard *ovgBuildGuard) hasCoordinateWithin(sortedValues []float64, coordinate, distance float64) (bool, error) {
	low, high := 0, len(sortedValues)
	threshold := coordinate - distance
	for low < high {
		if err := guard.step(); err != nil {
			return false, err
		}
		mid := int(uint(low+high) >> 1)
		if sortedValues[mid] < threshold {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low < len(sortedValues) && sortedValues[low] <= coordinate+distance, guard.check()
}

func (index *ovgPortIndex) tooClose(x, y, distance float64, guard *ovgBuildGuard) (bool, error) {
	tooClose, err := guard.hasCoordinateWithin(index.xByY[y], x, distance)
	if err != nil || tooClose {
		return tooClose, err
	}
	return guard.hasCoordinateWithin(index.yByX[x], y, distance)
}
