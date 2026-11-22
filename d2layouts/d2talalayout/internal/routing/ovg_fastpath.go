package routing

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// OVG construction charges one guard step per elementary operation, which
// makes the guard calls themselves a large part of the build. The fast paths
// in this file evaluate the pure predicates of one candidate without calling
// the guard, count the steps the stepped code charges for that candidate, and
// charge them with stepN before any mutation. They run only for contexts with
// a Done channel, where a poll has no effect other than noticing a
// cancellation; synthetic contexts without Done keep the stepped code and its
// per-operation Err polling.

// stepN charges n units exactly as n consecutive step calls would: the same
// work count, the same stage-aggregate charges (pending while it has room,
// added with the boundary step otherwise), a reserveWork(1) at every unit
// that step would send there (limit, polling stride, aggregate room), and on
// failure the same error with the same counts left behind.
func (guard *ovgBuildGuard) stepN(n uint64) error {
	for n > 0 {
		if guard.done != nil && guard.work < guard.limits.work {
			// Units that take step's fast path: within the work limit, below
			// the next multiple of the polling stride, and (with a stage
			// aggregate) within its remaining room.
			k := n
			if remaining := guard.limits.work - guard.work; k > remaining {
				k = remaining
			}
			if toStride := routeStageContextCheckStride - 1 - guard.work%routeStageContextCheckStride; k > toStride {
				k = toStride
			}
			if guard.aggregate != nil {
				if guard.stage != nil && guard.pending == 0 {
					guard.syncStageRoom() // a batch starts: see ovgBuildGuard.stage
				}
				switch {
				case guard.stage == nil || guard.pending >= guard.room:
					k = 0
				case k > guard.room-guard.pending:
					k = guard.room - guard.pending
				}
			}
			if k > 0 {
				guard.work += k
				if guard.aggregate != nil {
					guard.pending += k
				}
				n -= k
				continue
			}
		}
		if err := guard.step(); err != nil {
			return err
		}
		n--
	}
	return nil
}

// coordinateWithinCounted is hasCoordinateWithin without the guard: the same
// lower-bound search and result, plus the number of steps it charges (one per
// iteration).
func coordinateWithinCounted(sortedValues []float64, coordinate, distance float64) (bool, uint64) {
	low, high := 0, len(sortedValues)
	threshold := coordinate - distance
	var units uint64
	for low < high {
		units++
		mid := int(uint(low+high) >> 1)
		if sortedValues[mid] < threshold {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low < len(sortedValues) && sortedValues[low] <= coordinate+distance, units
}

// ovgSegmentBox is the box segmentIntersectsBox (Node.PassesThrough) derives
// from a node, so that its bounding-box rejection, which decides most tests,
// runs without the call. A node without a position or with a NaN boundary
// never intersects a segment.
type ovgSegmentBox struct {
	left, right, top, bottom float64
	never                    bool
}

func newOVGSegmentBox(node *layoutgraph.Node) ovgSegmentBox {
	if node.TopLeft == nil {
		return ovgSegmentBox{never: true}
	}
	left, right := node.TopLeft.X, node.TopLeft.X+node.Width
	if left > right {
		left, right = right, left
	}
	top, bottom := node.TopLeft.Y, node.TopLeft.Y+node.Height
	if top > bottom {
		top, bottom = bottom, top
	}
	if left != left || right != right || top != top || bottom != bottom {
		return ovgSegmentBox{never: true}
	}
	return ovgSegmentBox{left: left, right: right, top: top, bottom: bottom}
}

func newOVGSegmentBoxes(nodes []*layoutgraph.Node) []ovgSegmentBox {
	boxes := make([]ovgSegmentBox, len(nodes))
	for i, node := range nodes {
		boxes[i] = newOVGSegmentBox(node)
	}
	return boxes
}

// passesThrough is node.PassesThrough(p1, p2) for the node the box was made
// from (non-nil points): false when the box rejects the segment exactly as
// segmentIntersectsBox's first tests do, else the full test.
func (box *ovgSegmentBox) passesThrough(node *layoutgraph.Node, p1, p2 *geo.Point) bool {
	if box.never {
		return false
	}
	if (p1.X < box.left && p2.X < box.left) || (p1.X > box.right && p2.X > box.right) ||
		(p1.Y < box.top && p2.Y < box.top) || (p1.Y > box.bottom && p2.Y > box.bottom) {
		return false
	}
	return node.PassesThrough(p1, p2)
}

type ovgPortViewPort struct {
	point     *geo.Point
	x, y      float64
	hBlockers []*layoutgraph.Node
	vBlockers []*layoutgraph.Node
	hBoxes    []ovgSegmentBox
	vBoxes    []ovgSegmentBox
}

type ovgPortViewOwner struct {
	node      *layoutgraph.Node
	left      float64
	right     float64
	top       float64
	bottom    float64
	invisible bool
	sequence  *layoutgraph.Sequence
	ports     []ovgPortViewPort
}

// ovgPortView resolves, once per index, the map lookups that
// hasUnobstructedLineToPorts makes per candidate: each owner's ports (in
// ovg.Ports order) with the blockers of both port axes, and the owner fields
// the predicate reads. Valid while neither the ports nor the graph geometry
// change, which holds during one intersection or edge-node sweep.
type ovgPortView struct {
	owners []ovgPortViewOwner
}

func newOVGPortView(ovg *OVG, index *ovgPortIndex) *ovgPortView {
	view := &ovgPortView{owners: make([]ovgPortViewOwner, len(index.owners))}
	total := 0
	for _, owner := range index.owners {
		total += len(ovg.Ports[owner])
	}
	portStore := make([]ovgPortViewPort, 0, total)
	// Blocker lists are shared by all ports on a line; so are their boxes.
	hBoxes := make(map[float64][]ovgSegmentBox)
	vBoxes := make(map[float64][]ovgSegmentBox)
	boxesFor := func(cache map[float64][]ovgSegmentBox, coordinate float64, blockers []*layoutgraph.Node) []ovgSegmentBox {
		if boxes, ok := cache[coordinate]; ok {
			return boxes
		}
		boxes := newOVGSegmentBoxes(blockers)
		cache[coordinate] = boxes
		return boxes
	}
	for i, owner := range index.owners {
		ports := ovg.Ports[owner]
		start := len(portStore)
		for _, port := range ports {
			hBlockers := index.horizontalBlockers[port.Y]
			vBlockers := index.verticalBlockers[port.X]
			portStore = append(portStore, ovgPortViewPort{
				point:     port.Point,
				x:         port.X,
				y:         port.Y,
				hBlockers: hBlockers,
				vBlockers: vBlockers,
				hBoxes:    boxesFor(hBoxes, port.Y, hBlockers),
				vBoxes:    boxesFor(vBoxes, port.X, vBlockers),
			})
		}
		view.owners[i] = ovgPortViewOwner{
			node:      owner,
			left:      owner.TopLeft.X,
			right:     owner.TopLeft.X + owner.Width,
			top:       owner.TopLeft.Y,
			bottom:    owner.TopLeft.Y + owner.Height,
			invisible: owner.IsInvisible,
			sequence:  owner.Sequence,
			ports:     portStore[start:len(portStore):len(portStore)],
		}
	}
	return view
}

// unobstructedCounted is hasUnobstructedLineToPorts for a point whose aligned
// owner lists are byX and byY, without the guard: the same owner merge, port
// order, filters, blocker order and early exits, plus the number of steps the
// guarded version charges. Its polls (check) are not counted; they charge
// nothing.
func (view *ovgPortView) unobstructedCounted(point *geo.Point, byX, byY []int, numNodes int) (bool, uint64) {
	var units uint64
	distinctPortNodeCount := 0
	hasAlignedPort := len(byX) > 0 || len(byY) > 0
	px, py := point.X, point.Y
	for {
		// ovgAlignedOwners.next
		units++
		if len(byX) == 0 && len(byY) == 0 {
			break
		}
		var ownerIndex int
		if len(byY) == 0 || len(byX) > 0 && byX[0] < byY[0] {
			ownerIndex = byX[0]
		} else {
			ownerIndex = byY[0]
		}
		for len(byX) > 0 && byX[0] == ownerIndex {
			units++
			byX = byX[1:]
		}
		for len(byY) > 0 && byY[0] == ownerIndex {
			units++
			byY = byY[1:]
		}

		owner := &view.owners[ownerIndex]
		for k := range owner.ports {
			port := &owner.ports[k]
			units++
			if port.x != px && port.y != py {
				continue
			}
			if port.x == px && ((port.x == owner.left) || (port.x == owner.right)) {
				continue
			}
			if port.y == py && ((port.y == owner.top) || (port.y == owner.bottom)) {
				continue
			}
			hasDirectLineToThisPort := true
			blockers, boxes := port.hBlockers, port.hBoxes
			if port.x == px {
				blockers, boxes = port.vBlockers, port.vBoxes
			}
			for b, otherGNode := range blockers {
				units++
				if owner.node == otherGNode {
					continue
				}
				if owner.invisible {
					continue
				}
				if owner.sequence != nil && otherGNode.Sequence != nil && owner.sequence == otherGNode.Sequence {
					continue
				}
				if boxes[b].passesThrough(otherGNode, port.point, point) {
					hasDirectLineToThisPort = false
					break
				}
			}
			if hasDirectLineToThisPort {
				distinctPortNodeCount++
				if distinctPortNodeCount == numNodes {
					return true, units
				}
				break
			}
		}
	}
	return !hasAlignedPort, units
}

// ovgCandidateRow and ovgCandidateColumn hold the port-index entries of one
// intersection row (y) and column (x).
type ovgCandidateRow struct {
	xs     []float64
	owners []int
}

type ovgCandidateColumn struct {
	x         float64
	ys        []float64
	owners    []int
	nearNodes []*layoutgraph.Node
}

// evaluateIntersectionCandidate evaluates tooClose, pointNear and
// hasUnobstructedLineToPorts for the candidate (column.x, py) in the stepped
// order with the same early exits, without the guard: it returns whether the
// candidate is added and the steps the stepped loop charges for it before
// adding it. It only reads the indexes, the port view and the graph.
func evaluateIntersectionCandidate(column *ovgCandidateColumn, row *ovgCandidateRow, py float64, view *ovgPortView) (bool, uint64) {
	px := column.x
	// portIndex.tooClose
	tooClose, units := coordinateWithinCounted(row.xs, px, layoutgraph.MinPortClearance)
	if !tooClose {
		var more uint64
		tooClose, more = coordinateWithinCounted(column.ys, py, layoutgraph.MinPortClearance)
		units += more
	}
	if tooClose {
		return false, units
	}
	// The candidate inspection step, then proximityIndex.pointNear.
	units += 2
	candidatePoint := geo.Point{X: px, Y: py}
	for _, node := range column.nearNodes {
		units++
		if node.IsPointNear(&candidatePoint) {
			return false, units
		}
	}
	unobstructed, more := view.unobstructedCounted(&candidatePoint, column.owners, row.owners, 2)
	return unobstructed, units + more
}

// ovgParallelCandidateThreshold is the number of intersection candidates from
// which their evaluation is spread over goroutines (see
// addIntersectionCandidatesCounted). Tests lower it.
var ovgParallelCandidateThreshold = 20_000

// addIntersectionCandidatesCounted is the candidate loop of addIntersections
// for contexts with a Done channel. Per candidate it evaluates tooClose,
// pointNear and hasUnobstructedLineToPorts in the same order with the same
// early exits, charges the steps they charge with one stepN (before the
// candidate is added, which is the first mutation), and adds the node through
// the same guard.addNode.
//
// The evaluations are pure (they read the port index, the port view and the
// graph, none of which the loop changes), so for large sweeps they run first,
// column by column on several goroutines, into a table; the charges and
// additions then run on this goroutine in candidate order, exactly as in the
// sequential loop. A panic in an evaluation discards the table and reruns the
// sequential loop, which panics where the stepped loop would.
func (ovg *OVG) addIntersectionCandidatesCounted(xOrder, yOrder []float64, portIndex *ovgPortIndex, proximityIndex *ovgPointProximityIndex, guard *ovgBuildGuard) error {
	view := newOVGPortView(ovg, portIndex)
	rows := make([]ovgCandidateRow, len(yOrder))
	for j, py := range yOrder {
		rows[j] = ovgCandidateRow{xs: portIndex.xByY[py], owners: portIndex.ownersByY[py]}
	}
	columns := make([]ovgCandidateColumn, len(xOrder))
	for i, px := range xOrder {
		columns[i] = ovgCandidateColumn{
			x:         px,
			ys:        portIndex.yByX[px],
			owners:    portIndex.ownersByX[px],
			nearNodes: proximityIndex.byX[px],
		}
	}
	workers := min(runtime.GOMAXPROCS(0), 8, len(xOrder))
	if workers > 1 && len(xOrder)*len(yOrder) >= ovgParallelCandidateThreshold {
		if results, ok := evaluateIntersectionCandidatesParallel(columns, rows, yOrder, view, workers); ok {
			ovg.reserveNodes(countAddedCandidates(results))
			for i := range columns {
				for j, py := range yOrder {
					result := results[i*len(yOrder)+j]
					if err := guard.stepN(result >> 1); err != nil {
						return err
					}
					if result&1 != 0 {
						if _, err := guard.addNode(ovg, ovg.newSlabNode(columns[i].x, py)); err != nil {
							return err
						}
					}
				}
			}
			return guard.check()
		}
	}
	for i := range columns {
		column := &columns[i]
		for j, py := range yOrder {
			add, units := evaluateIntersectionCandidate(column, &rows[j], py, view)
			if err := guard.stepN(units); err != nil {
				return err
			}
			if add {
				if _, err := guard.addNode(ovg, ovg.newSlabNode(column.x, py)); err != nil {
					return err
				}
			}
		}
	}
	return guard.check()
}

// countAddedCandidates is how many candidates of an evaluated table are
// added (low bit set).
func countAddedCandidates(results []uint64) int {
	added := 0
	for _, result := range results {
		added += int(result & 1)
	}
	return added
}

// reserveNodes makes room for added more nodes in ovg.Nodes and
// OccupiedPoints, so adding them grows neither. Only capacity changes:
// OccupiedPoints is only ever looked up (never iterated or measured), and the
// copy keeps every entry with its stored key.
func (ovg *OVG) reserveNodes(added int) {
	if added <= 0 {
		return
	}
	ovg.Nodes = slices.Grow(ovg.Nodes, added)
	grown := make(map[geo.Point]*OVGNode, len(ovg.OccupiedPoints)+added)
	for point, node := range ovg.OccupiedPoints {
		grown[point] = node
	}
	ovg.OccupiedPoints = grown
}

// evaluateIntersectionCandidatesParallel evaluates every candidate into a
// table indexed column-major (units<<1 | added), with workers goroutines
// claiming columns. ok is false if an evaluation panicked.
func evaluateIntersectionCandidatesParallel(columns []ovgCandidateColumn, rows []ovgCandidateRow, yOrder []float64, view *ovgPortView, workers int) (results []uint64, ok bool) {
	results = make([]uint64, len(columns)*len(yOrder))
	var next atomic.Int64
	var panicked atomic.Bool
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			defer func() {
				if recover() != nil {
					panicked.Store(true)
				}
			}()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(columns) || panicked.Load() {
					return
				}
				column := &columns[i]
				out := results[i*len(yOrder) : (i+1)*len(yOrder)]
				for j, py := range yOrder {
					add, units := evaluateIntersectionCandidate(column, &rows[j], py, view)
					out[j] = units << 1
					if add {
						out[j] |= 1
					}
				}
			}
		})
	}
	wg.Wait()
	return results, !panicked.Load()
}

// isDescendantOfCounted is ovgBuildGuard.isDescendantOf without the guard,
// plus its step count.
func isDescendantOfCounted(descendant, ancestor *layoutgraph.Node) (bool, uint64) {
	var units uint64
	for current := descendant; ; {
		units++
		if ancestor == current {
			return true, units
		}
		if current == nil {
			return false, units
		}
		switch {
		case current.Container != nil:
			current = current.Container
		case current.Cluster != nil:
			current = current.Cluster.Vessel
		case current.Sequence != nil:
			current = current.Sequence.Vessel
		default:
			return ancestor == nil, units
		}
	}
}

// ovgNearScan answers ovgBuildGuard.pointNearGraphNode without the guard,
// with its step count. A point outside the union of the near boxes of all
// non-container nodes is near none of them, so the scan would visit every
// node: the answer is false and the count len(nodes) without visiting them.
// Boxes that cannot contain any point (NaN or inverted) stay out of the
// union; a node without a position disables the shortcut so the scan fails
// where the stepped scan does.
type ovgNearScan struct {
	nodes                  []*layoutgraph.Node
	bounded                bool
	minX, maxX, minY, maxY float64
}

func newOVGNearScan(nodes []*layoutgraph.Node) ovgNearScan {
	scan := ovgNearScan{nodes: nodes, bounded: true}
	first := true
	for _, node := range nodes {
		if node.IsContainer() {
			continue
		}
		if node.TopLeft == nil {
			scan.bounded = false
			return scan
		}
		left := node.TopLeft.X - layoutgraph.MinRouteNodeClearance
		right := node.TopLeft.X + node.Width + layoutgraph.MinRouteNodeClearance
		top := node.TopLeft.Y - layoutgraph.MinRouteNodeClearance
		bottom := node.TopLeft.Y + node.Height + layoutgraph.MinRouteNodeClearance
		if !(left <= right && top <= bottom) {
			continue
		}
		if first {
			scan.minX, scan.maxX, scan.minY, scan.maxY = left, right, top, bottom
			first = false
			continue
		}
		if left < scan.minX {
			scan.minX = left
		}
		if right > scan.maxX {
			scan.maxX = right
		}
		if top < scan.minY {
			scan.minY = top
		}
		if bottom > scan.maxY {
			scan.maxY = bottom
		}
	}
	if first {
		// No node can be near any point.
		scan.minX, scan.maxX, scan.minY, scan.maxY = 1, 0, 1, 0
	}
	return scan
}

func (scan *ovgNearScan) nearCounted(point *geo.Point) (bool, uint64) {
	if scan.bounded && !(scan.minX <= point.X && point.X <= scan.maxX && scan.minY <= point.Y && point.Y <= scan.maxY) {
		return false, uint64(len(scan.nodes))
	}
	var units uint64
	for _, node := range scan.nodes {
		units++
		// It is valid for visibility nodes to lie on container boundaries.
		if node.IsContainer() {
			continue
		}
		if node.IsPointNear(point) {
			return true, units
		}
	}
	return false, units
}

// rayBlockedCounted is the rectangle scan of addPortConnectionNodesAtBoundaries
// for the segment p1 (a port of owner) to p2 without the guard: the same
// rectangles in order, passesThroughAllowingPorts' port exemption and its
// per-port steps, the container exception with isDescendantOf's steps, and the
// first blocking rectangle ends the scan. allowsPort depends only on the
// direction and the segment, so it is computed once.
func rayBlockedCounted(owner *layoutgraph.Node, p1, p2 *geo.Point, direction geo.Orientation, rects []*layoutgraph.Node, rectPorts [][]*OVGNode, rectBoxes []ovgSegmentBox) (bool, uint64) {
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
	var units uint64
	for k, rectangle := range rects {
		units++
		exempt := false
		if ports := rectPorts[k]; len(ports) > 0 && allowsPort {
			for _, port := range ports {
				units++
				if nonNilEquals(port.Point, p1) || nonNilEquals(port.Point, p2) {
					exempt = true
					break
				}
			}
		}
		if exempt || !rectBoxes[k].passesThrough(rectangle, p1, p2) {
			continue
		}
		// We make exception for container
		if rectangle.IsContainer() {
			descendant, more := isDescendantOfCounted(owner, rectangle)
			units += more
			if descendant {
				continue
			}
		}
		return true, units
	}
	return false, units
}

// addPortConnectionNodesAtBoundariesCounted is addPortConnectionNodesAtBoundaries
// for contexts with a Done channel: the same layers, owners (map order, as
// before), ports, directions and boundary points; per boundary point the pure
// rectangle scan and near check are evaluated without the guard and their
// steps charged with one stepN before the point is added.
func (ovg *OVG) addPortConnectionNodesAtBoundariesCounted(g *layoutgraph.Graph, tl, br *geo.Point, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	rects := ovg.NodesInsideBoundingBox
	rectPorts := make([][]*OVGNode, len(rects))
	for k, rectangle := range rects {
		rectPorts[k] = ovg.Ports[rectangle]
	}
	rectBoxes := newOVGSegmentBoxes(rects)
	near := newOVGNearScan(g.Nodes)
	var seenPorts []*OVGNode
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
			seenPorts = seenPorts[:0]
			for _, portNode := range ports {
				if err := guard.step(); err != nil {
					return err
				}
				if slices.Contains(seenPorts, portNode) {
					continue
				}
				seenPorts = append(seenPorts, portNode)
				portDirections, _ := portNode.portDirectionsFor(node)
				// portDirectionSet.any: NONE for an empty set, else every
				// member in increasing order.
				for d := geo.TopLeft; d <= geo.NONE; d++ {
					if portDirections == 0 {
						if d != geo.NONE {
							continue
						}
					} else if !portDirections.has(d) {
						continue
					}
					portDirection := d
					if err := guard.step(); err != nil {
						return err
					}
					boundaryPoints := [4]geo.Point{
						{X: portNode.Point.X, Y: tl.Y - (ovgPadding * (i + 1))},
						{X: portNode.Point.X, Y: br.Y + (ovgPadding * (i + 1))},
						{X: tl.X - (ovgPadding * (i + 1)), Y: portNode.Point.Y},
						{X: br.X + (ovgPadding * (i + 1)), Y: portNode.Point.Y},
					}
					for b := range boundaryPoints {
						boundaryPoint := &boundaryPoints[b]
						blocked, units := rayBlockedCounted(node, portNode.Point, boundaryPoint, portDirection, rects, rectPorts, rectBoxes)
						units++ // the boundary point's own step
						if blocked {
							if err := guard.stepN(units); err != nil {
								return err
							}
							continue
						}
						nearNode, more := near.nearCounted(boundaryPoint)
						if err := guard.stepN(units + more); err != nil {
							return err
						}
						if !nearNode {
							if _, err := guard.addPoint(ovg, geo.NewPoint(boundaryPoint.X, boundaryPoint.Y)); err != nil {
								return err
							}
							added = true
						}
					}
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

// passesThroughAllowingPortsCounted is ovgBuildGuard.passesThroughAllowingPorts
// without the guard, plus its step count (one per port compared); box is the
// node's segment box.
func passesThroughAllowingPortsCounted(node *layoutgraph.Node, box *ovgSegmentBox, p1, p2 *geo.Point, direction geo.Orientation, ports []*OVGNode) (bool, uint64) {
	if len(ports) == 0 {
		return box.passesThrough(node, p1, p2), 0
	}
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
	var units uint64
	if allowsPort {
		for _, port := range ports {
			units++
			if nonNilEquals(port.Point, p1) || nonNilEquals(port.Point, p2) {
				return false, units
			}
		}
	}
	return box.passesThrough(node, p1, p2), units
}

// segmentPassesThroughCounted is connectNodesOnSameLine's passesThrough
// closure: whether the segment from -> to passes through rectangle for every
// direction in which from is a port of it (portDirectionSet.any order, NONE
// for none), stopping at the first that does not; plus its step count.
func segmentPassesThroughCounted(from, to *OVGNode, rectangle *layoutgraph.Node, box *ovgSegmentBox, ports []*OVGNode) (bool, uint64) {
	directions := from.portDirectionsForObstacle(rectangle)
	var units uint64
	if directions == 0 {
		passes, more := passesThroughAllowingPortsCounted(rectangle, box, from.Point, to.Point, geo.NONE, ports)
		return passes, more
	}
	for direction := geo.TopLeft; direction <= geo.NONE; direction++ {
		if !directions.has(direction) {
			continue
		}
		passes, more := passesThroughAllowingPortsCounted(rectangle, box, from.Point, to.Point, direction, ports)
		units += more
		if !passes {
			return false, units
		}
	}
	return true, units
}

// isMisdirectedPortPairCounted is OVG.isMisdirectedPortPair without the guard,
// plus its step count. Like the stepped version it iterates the owner maps,
// so the count of a pair with several owners depends on map order, as before.
func isMisdirectedPortPairCounted(first, second *OVGNode, isHorizontal bool) (bool, uint64) {
	firstOwners := first.portOwners()
	secondOwners := second.portOwners()
	if len(firstOwners) == 0 || len(secondOwners) == 0 {
		return false, 0
	}
	var units uint64
	for firstOwner, firstMetadata := range firstOwners {
		units++
		for secondOwner, secondMetadata := range secondOwners {
			units++
			if firstOwner == secondOwner {
				return false, units
			}
			correctDirections := firstMetadata.directions.has(geo.Bottom) && secondMetadata.directions.has(geo.Top)
			if isHorizontal {
				correctDirections = firstMetadata.directions.has(geo.Right) && secondMetadata.directions.has(geo.Left)
			}
			firstBelowSecond, more := isDescendantOfCounted(firstOwner, secondOwner)
			units += more
			secondBelowFirst, more := isDescendantOfCounted(secondOwner, firstOwner)
			units += more
			if correctDirections || firstBelowSecond || secondBelowFirst {
				return false, units
			}
		}
	}
	return true, units
}

type ovgLineRect struct {
	node  *layoutgraph.Node
	ports []*OVGNode
	box   ovgSegmentBox
}

// ovgLineView holds, for one connectNodes call, the bounding-box nodes with
// their port lists and whether they are fixed overlaps, so each line scans
// them without map lookups.
type ovgLineView struct {
	rects      []ovgLineRect
	fixed      []bool
	candidates []ovgLineRect
}

func newOVGLineView(ovg *OVG, fixedOverlaps map[*layoutgraph.Node]struct{}) *ovgLineView {
	view := &ovgLineView{
		rects: make([]ovgLineRect, len(ovg.NodesInsideBoundingBox)),
		fixed: make([]bool, len(ovg.NodesInsideBoundingBox)),
	}
	for k, node := range ovg.NodesInsideBoundingBox {
		view.rects[k] = ovgLineRect{node: node, ports: ovg.Ports[node], box: newOVGSegmentBox(node)}
		_, view.fixed[k] = fixedOverlaps[node]
	}
	return view
}

// connectNodesOnSameLineCounted is connectNodesOnSameLine for contexts with a
// Done channel: the same candidate rectangles (graph order, then the same
// sort.Slice call, which yields the same permutation for the same keys), the
// same node sort, and per pair the misdirection test and rectangle scan
// evaluated without the guard, their steps charged with one stepN before the
// pair is connected.
func (ovg *OVG) connectNodesOnSameLineCounted(nodes []*OVGNode, linePosition float64, isHorizontal bool, view *ovgLineView, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	candidates := view.candidates[:0]
	for k := range view.rects {
		if view.fixed[k] {
			continue
		}
		node := view.rects[k].node
		if isHorizontal && node.TopLeft.Y <= linePosition && node.TopLeft.Y+node.Height >= linePosition {
			candidates = append(candidates, view.rects[k])
		} else if !isHorizontal && node.TopLeft.X <= linePosition && node.TopLeft.X+node.Width >= linePosition {
			candidates = append(candidates, view.rects[k])
		}
	}
	view.candidates = candidates
	if err := guard.stepN(uint64(len(view.rects))); err != nil {
		return err
	}
	if err := guard.reserveSortWork(len(candidates)); err != nil {
		return err
	}
	sortSliceExact(candidates, func(a, b ovgLineRect) bool {
		if isHorizontal {
			return a.node.TopLeft.X < b.node.TopLeft.X
		}
		return a.node.TopLeft.Y < b.node.TopLeft.Y
	})

	if err := guard.reserveSortWork(len(nodes)); err != nil {
		return err
	}
	sortSliceExact(nodes, func(a, b *OVGNode) bool {
		if isHorizontal {
			return a.X < b.X
		}
		return a.Y < b.Y
	})

	startIntersectionSearchAt := 0
	for i := 0; i < len(nodes)-1; i++ {
		node := nodes[i]
		closest := nodes[i+1]
		misdirected, units := isMisdirectedPortPairCounted(node, closest, isHorizontal)
		units++ // the pair's own step
		if misdirected {
			if err := guard.stepN(units); err != nil {
				return err
			}
			continue
		}

		shouldConnect := true
		for j := startIntersectionSearchAt; j < len(candidates); j++ {
			units++
			rectangle := candidates[j].node
			if (isHorizontal && rectangle.TopLeft.X > closest.X) || (!isHorizontal && rectangle.TopLeft.Y > closest.Y) {
				// here, rectangle is after closest to the right or to the bottom
				// since intersectCandidates is sorted, no other nodes will intersect, so we can skip the test
				break
			}
			c1, more := segmentPassesThroughCounted(node, closest, rectangle, &candidates[j].box, candidates[j].ports)
			units += more
			c2, more := segmentPassesThroughCounted(closest, node, rectangle, &candidates[j].box, candidates[j].ports)
			units += more
			if c1 && c2 {
				if rectangle.Sequence != nil {
					// sequences will have top and bottom ports overlapped by previous sequence nodes, but this overlap shouldn't prevent a connection with that port
					if !isHorizontal {
						if rectangle.TopLeft.Y == closest.Y || rectangle.TopLeft.Y+rectangle.Height == node.Y {
							continue
						}
					}
				}
				// Passing through a container is okay if at least one of the nodes is within the container
				if rectangle.IsContainer() && (rectangle.ContainsPointOnBox(node.Point) || rectangle.ContainsPointOnBox(closest.Point)) {
					continue
				}
				shouldConnect = false
				break
			}
			// this node didn't intersect with the current OVG, so we don't need to
			// check any other nodes prior to it for the next OVG nodes
			// (the loop is on top of sorted nodes)
			startIntersectionSearchAt = j
		}
		if err := guard.stepN(units); err != nil {
			return err
		}

		if shouldConnect {
			if _, err := guard.connect(ovg, node, closest); err != nil {
				return err
			}
		}
	}
	return nil
}
