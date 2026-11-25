package loops

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/nodeshape"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

const sharedPortLoopGap = 30.0

// EnableSeparateCorners marks loop hosts in this trial's private graph so
// placement and routing can use the same alternative corner assignment.
func EnableSeparateCorners(ctx context.Context, g *layoutgraph.Graph) error {
	for _, edge := range g.Edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		if edge.IsLoop() {
			edge.From.SeparateLoopCorners = true
		}
	}
	return nil
}

// Equivalent loops can occupy the four corner pairs in rotation. Further
// loops nest around the previous route at that corner. Read the current edges
// since grouping can temporarily redirect an edge during placement.
func separateCorners(edges []*layoutgraph.Edge) bool {
	var first *layoutgraph.Edge
	count := 0
	for _, e := range edges {
		if !e.IsLoop() {
			continue
		}
		if first == nil {
			first = e
		}
		count++
		if first.SourceArrowhead != e.SourceArrowhead || first.TargetArrowhead != e.TargetArrowhead {
			return false
		}
	}
	return count >= 2
}

// ComputeOffsets refreshes loop extents for every graph node.
func ComputeOffsets(g *layoutgraph.Graph) {
	for _, node := range g.Nodes {
		UpdateOffsets(node)
	}
}

// UpdateOffsets refreshes the space reserved around node for self-edge routes.
func UpdateOffsets(node *layoutgraph.Node) {
	if !slices.ContainsFunc(node.Edges, (*layoutgraph.Edge).IsLoop) {
		// The last loop may have been removed or reconnected.
		node.LoopOffsets = nil
		return
	}
	// need to set node TopLeft because the node position is required to get the ports
	// which are required for routing
	tlIsNil := node.TopLeft == nil
	if tlIsNil {
		node.TopLeft = geo.NewPoint(0, 0)
	}
	computeOffsets(node)
	if tlIsNil {
		node.TopLeft = nil
	}
}

// computeLoopOffsets precomputes loops and their contribution to node bounds
// for later delta and bounding-box calculations.
func computeOffsets(node *layoutgraph.Node) {
	node.LoopOffsets = make(map[geo.Orientation]float64)

	tl, br := node.BoundingBox(nil)
	loopTL, loopBR := node.BoundingBox(nil)
	for _, edge := range routeLoops(node, nil) {
		if !edge.IsLoop() {
			continue
		}
		eTL, eBR := edge.BoundingBoxValues()
		loopTL.X = math.Min(loopTL.X, eTL.X)
		loopTL.Y = math.Min(loopTL.Y, eTL.Y)
		loopBR.X = math.Max(loopBR.X, eBR.X)
		loopBR.Y = math.Max(loopBR.Y, eBR.Y)
		// need to reset because if these are set based on `0, 0`, graph bounding box breaks during placement
		// as nodes move around during placement and these edge points would stay at `0, 0`
		// the graph would reach a bad state
		edge.Points = nil
	}

	if nonNilEquals(tl, loopTL) && nonNilEquals(br, loopBR) {
		return
	}

	node.LoopOffsets[geo.Left] = math.Round(math.Abs(tl.X - loopTL.X))
	node.LoopOffsets[geo.Right] = math.Round(math.Abs(br.X - loopBR.X))
	node.LoopOffsets[geo.Top] = math.Round(math.Abs(tl.Y - loopTL.Y))
	node.LoopOffsets[geo.Bottom] = math.Round(math.Abs(br.Y - loopBR.Y))

	node.LoopOffsets[geo.TopLeft] = math.Max(node.LoopOffsets[geo.Top], node.LoopOffsets[geo.Left])
	node.LoopOffsets[geo.TopRight] = math.Max(node.LoopOffsets[geo.Top], node.LoopOffsets[geo.Right])
	node.LoopOffsets[geo.BottomLeft] = math.Max(node.LoopOffsets[geo.Bottom], node.LoopOffsets[geo.Left])
	node.LoopOffsets[geo.BottomRight] = math.Max(node.LoopOffsets[geo.Bottom], node.LoopOffsets[geo.Right])
}

// loopPorts works as sort of context for a given ports pair while routing loops
type loopPorts struct {
	verticalOffset     float64
	horizontalOffset   float64
	hasSourceArrowhead *bool
	hasTargetArrowhead *bool
	sourceArrowhead    layoutgraph.Arrowhead
	targetArrowhead    layoutgraph.Arrowhead
	from               *geo.Point
	fromDirection      geo.Orientation
	to                 *geo.Point
	toDirection        geo.Orientation
}

func newLoopPorts(from, to *geo.Point, fromDirection, toDirection geo.Orientation) *loopPorts {
	return &loopPorts{
		verticalOffset:     0.,
		horizontalOffset:   0.,
		hasSourceArrowhead: nil,
		hasTargetArrowhead: nil,
		from:               from,
		fromDirection:      fromDirection,
		to:                 to,
		toDirection:        toDirection,
	}
}

// Route computes every self-edge route for node.
func Route(n *layoutgraph.Node) []*layoutgraph.Edge {
	return routeLoops(n, nil)
}

// routeLoops routes loops in a rule based approach and sets the proper label position
// edges are routed based on their arrowheads and label size
// there are 4 possible positions for loops, one at each corner
func routeLoops(n *layoutgraph.Node, selected map[*layoutgraph.Edge]struct{}) []*layoutgraph.Edge {
	var availablePorts []*loopPorts

	split := n.SeparateLoopCorners && separateCorners(n.Edges)
	var routedEdges []*layoutgraph.Edge
	ordered := edgesInOrder(n.Edges)
	var rowRoutes map[*layoutgraph.Edge][]*geo.Point
	if nodeshape.NumColumns(n.Shape) > 0 {
		rowRoutes = tableRowRoutes(ordered, selected)
	}
	loopIndex := 0
	for _, e := range ordered {
		if !e.IsLoop() {
			continue
		}

		_, moving := selected[e]
		moving = moving || selected == nil
		if moving {
			routedEdges = append(routedEdges, e)
			if e.Label != nil {
				e.Label.Position = label.OutsideTopCenter
			}
		}
		loopIndex++
		if IsTableRowLoop(e) {
			if moving {
				e.Points = rowRoutes[e]
			}
			continue
		}
		if availablePorts == nil {
			availablePorts = makeLoopPorts(n)
		}
		portsPair := findLoopPortsForEdge(e, availablePorts)
		if split {
			// Start on opposite corners to separate both incoming and outgoing ports.
			order := [...]int{0, 3, 1, 2}
			portsPair = availablePorts[order[(loopIndex-1)%len(order)]]
		}
		if moving {
			e.Points = routeLoop(portsPair)
		}
		portsPair.verticalOffset, portsPair.horizontalOffset = computeEdgeOffset(e)
		portsPair.hasSourceArrowhead = new(e.HasSourceArrow())
		portsPair.hasTargetArrowhead = new(e.HasTargetArrow())
		portsPair.sourceArrowhead = e.SourceArrowhead
		portsPair.targetArrowhead = e.TargetArrowhead
	}
	return routedEdges
}

// RouteSelected plans chosen loops together and keeps unselected routes fixed.
func RouteSelected(n *layoutgraph.Node, selected map[*layoutgraph.Edge]struct{}) []*layoutgraph.Edge {
	return routeLoops(n, selected)
}

func tableRowRoutes(ordered []*layoutgraph.Edge, selected map[*layoutgraph.Edge]struct{}) map[*layoutgraph.Edge][]*geo.Point {
	type band struct{ near, far float64 }
	var occupied []band
	if selected != nil {
		for _, e := range ordered {
			if _, moving := selected[e]; moving || !e.IsLoop() {
				continue
			}
			// A frozen path may have been supplied by another router. Reserve
			// the actual depths of its external parallel runs on any face.
			n := e.From
			near, far := math.Inf(1), math.Inf(-1)
			for i := 1; i < len(e.Points); i++ {
				p, q := e.Points[i-1], e.Points[i]
				var depth float64
				switch {
				case p.X == q.X:
					depth = max(n.TopLeft.X-p.X, p.X-n.TopLeft.X-n.Width)
				case p.Y == q.Y:
					depth = max(n.TopLeft.Y-p.Y, p.Y-n.TopLeft.Y-n.Height)
				}
				if depth > 0 {
					near = min(near, depth)
					far = max(far, depth)
				}
			}
			if near <= far {
				occupied = append(occupied, band{near, far + tableRowLaneWidth(e)})
			}
		}
		slices.SortFunc(occupied, func(a, b band) int { return cmp.Compare(a.near, b.near) })
	}
	var routes map[*layoutgraph.Edge][]*geo.Point
	depth, nextFree := sharedPortLoopGap, sharedPortLoopGap
	for _, e := range ordered {
		if !IsTableRowLoop(e) {
			continue
		}
		width := tableRowLaneWidth(e)
		if _, moving := selected[e]; selected == nil || moving {
			lane := max(depth, nextFree)
			for len(occupied) > 0 && lane+width > occupied[0].near {
				lane = max(lane, occupied[0].far)
				occupied = occupied[1:]
			}
			if routes == nil {
				routes = make(map[*layoutgraph.Edge][]*geo.Point)
			}
			routes[e] = tableRowRoute(e, lane)
			nextFree = lane + width
		}
		depth += width
	}
	return routes
}

func tableRowLaneWidth(e *layoutgraph.Edge) float64 {
	if e.Label != nil {
		return sharedPortLoopGap + e.Label.Width
	}
	return sharedPortLoopGap
}

// IsTableRowLoop reports whether both endpoints are the same table and at
// least one endpoint names a row.
func IsTableRowLoop(e *layoutgraph.Edge) bool {
	return e.IsLoop() && e.HasTableColumn() && nodeshape.NumColumns(e.From.Shape) > 0
}

// tableRowRoute follows a lane outside the table so a self-reference keeps
// each named row. A row-to-itself reference returns at the opposite face.
func tableRowRoute(e *layoutgraph.Edge, gap float64) []*geo.Point {
	n := e.From
	left := n.TopLeft.X - gap
	rowPort := func(side geo.Orientation, index *int) *geo.Point {
		p, _ := nodeshape.TableColumnPortValue(n.Shape, side, *index)
		return &p
	}
	if e.FromTableColumnIndex == nil || e.ToTableColumnIndex == nil {
		// Use a top port for the table-level end of a mixed reference.
		index := e.FromTableColumnIndex
		if index == nil {
			index = e.ToTableColumnIndex
		}
		row := rowPort(geo.Left, index)
		top := geo.NewPoint(n.TopLeft.X+math.Round(n.Width/4), n.TopLeft.Y)
		points := []*geo.Point{row, geo.NewPoint(left, row.Y), geo.NewPoint(left, top.Y-gap), geo.NewPoint(top.X, top.Y-gap), top}
		if e.FromTableColumnIndex == nil {
			slices.Reverse(points)
		}
		return points
	}
	from, to := rowPort(geo.Left, e.FromTableColumnIndex), rowPort(geo.Left, e.ToTableColumnIndex)
	if from.Y != to.Y {
		return []*geo.Point{from, geo.NewPoint(left, from.Y), geo.NewPoint(left, to.Y), to}
	}
	to = rowPort(geo.Right, e.ToTableColumnIndex)
	across := n.TopLeft.Y - gap
	if from.Y > n.Center().Y {
		across = n.TopLeft.Y + n.Height + gap
	}
	right := to.X + gap
	return []*geo.Point{from, geo.NewPoint(left, from.Y), geo.NewPoint(left, across), geo.NewPoint(right, across), geo.NewPoint(right, to.Y), to}
}

// edgesInOrder sorts edges based on their arrowheads and label sizes
// sorting by arrowhead is required so that, if a given `diamond` node has all edge types (->, --, <->)
// the arrowheads will match the ports as expected (see `self_edges_all_shapes` test case)
// then, sorts by label so that the smallest ones will use the routes closes to the shape, avoiding empty space
func edgesInOrder(edges []*layoutgraph.Edge) []*layoutgraph.Edge {
	sortedEdges := make([]*layoutgraph.Edge, len(edges))
	copy(sortedEdges, edges)
	arrowheadScore := func(e *layoutgraph.Edge) int {
		// ->
		if e.HasSourceArrow() != e.HasTargetArrow() {
			return 0
		} else if e.HasSourceArrow() {
			return 1
		} else {
			return 2
		}
	}
	slices.SortStableFunc(sortedEdges, func(a, b *layoutgraph.Edge) int {
		// 1st ->
		// 2nd <->
		// 3rd --
		if a.HasSourceArrow() != b.HasSourceArrow() || a.HasTargetArrow() != b.HasTargetArrow() {
			aScore := arrowheadScore(a)
			bScore := arrowheadScore(b)
			switch {
			case aScore < bScore:
				return -1
			case bScore < aScore:
				return 1
			default:
				return 0
			}
		}

		// if arrowheads are equal, sort by label area
		aArea := 0.
		bArea := 0.
		if a.Label != nil {
			aArea = a.Label.Width * a.Label.Height
		}
		if b.Label != nil {
			bArea = b.Label.Width * b.Label.Height
		}
		switch {
		case aArea < bArea:
			return -1
		case bArea < aArea:
			return 1
		default:
			return 0
		}
	})
	return sortedEdges
}

// findLoopPortsForEdge finds the ports that are using the same arrowheads in `availablePorts`
func findLoopPortsForEdge(e *layoutgraph.Edge, availablePorts []*loopPorts) *loopPorts {
	// first find a matching set if it exists
	for _, ports := range availablePorts {
		if ports.hasSourceArrowhead != nil && ports.hasTargetArrowhead != nil {
			if ports.sourceArrowhead == e.SourceArrowhead && ports.targetArrowhead == e.TargetArrowhead {
				return ports
			}
		}
	}
	// look for an open pair
	for _, ports := range availablePorts {
		if ports.hasSourceArrowhead == nil && ports.hasTargetArrowhead == nil {
			return ports
		}
	}
	// find a pair that have arrows at the same end
	for _, ports := range availablePorts {
		if ports.hasSourceArrowhead != nil && ports.hasTargetArrowhead != nil &&
			*ports.hasSourceArrowhead == e.HasSourceArrow() && *ports.hasTargetArrowhead == e.HasTargetArrow() {
			return ports
		}
	}
	return nil
}

// makeLoopPorts makes the 4 possible port pairs, one at each corner, using the closest ports for each combination
// closest ports because this avoids crossings with other routes
// the ports order is related to edgesInOrder and the `shape: diamond` condition (see test case)
// Four pairs are required because the declaration orientation makes source-only
// and target-only arrows distinct categories in addition to -- and <->.
func makeLoopPorts(n *layoutgraph.Node) []*loopPorts {
	ports := n.Ports()
	topIndices := n.PortIndices(geo.Top)
	rightIndices := n.PortIndices(geo.Right)
	bottomIndices := n.PortIndices(geo.Bottom)
	leftIndices := n.PortIndices(geo.Left)

	trTop, trRight := closestPorts(ports, topIndices, rightIndices)
	blBottom, blLeft := closestPorts(ports, bottomIndices, leftIndices)
	ltLeft, ltTop := closestPorts(ports, leftIndices, topIndices)
	brRight, brBottom := closestPorts(ports, rightIndices, bottomIndices)

	return []*loopPorts{
		newLoopPorts(ltLeft, ltTop, geo.Left, geo.Top),
		newLoopPorts(trTop, trRight, geo.Top, geo.Right),
		newLoopPorts(blBottom, blLeft, geo.Bottom, geo.Left),
		newLoopPorts(brRight, brBottom, geo.Right, geo.Bottom),
	}
}

func closestPorts(ports []geo.Point, sideA, sideB []int) (*geo.Point, *geo.Point) {
	var closestA, closestB geo.Point
	minDistance := math.Inf(1)

	for _, indexA := range sideA {
		for _, indexB := range sideB {
			pa := ports[indexA]
			pb := ports[indexB]
			d := geo.EuclideanDistance(pa.X, pa.Y, pb.X, pb.Y)
			if d < minDistance {
				minDistance = d
				closestA = pa
				closestB = pb
			}
		}
	}

	return &closestA, &closestB
}

// routeLoop creates the route shown below with ports `*`, bend points and intersection (`#`)
// . #───────────#
// . │           │
// . │      ┌────*───┐
// . │      │        │
// . #──────*        │
// .        │  node  │
// .        │        │
// .        └────────┘
func routeLoop(ports *loopPorts) []*geo.Point {
	p1 := makeBendPoint(ports.from, ports.fromDirection, ports.verticalOffset, ports.horizontalOffset)
	p2 := makeBendPoint(ports.to, ports.toDirection, ports.verticalOffset, ports.horizontalOffset)
	intersection := makeIntersectionPoint(p1, p2, ports.fromDirection)

	return []*geo.Point{
		ports.from,
		p1,
		intersection,
		p2,
		ports.to,
	}
}

func makeBendPoint(port *geo.Point, direction geo.Orientation, verticalOffset, horizontalOffset float64) *geo.Point {
	switch direction {
	case geo.Top:
		return geo.NewPoint(port.X, port.Y-verticalOffset-sharedPortLoopGap)
	case geo.Bottom:
		return geo.NewPoint(port.X, port.Y+verticalOffset+sharedPortLoopGap)
	case geo.Left:
		return geo.NewPoint(port.X-horizontalOffset-sharedPortLoopGap, port.Y)
	case geo.Right:
		return geo.NewPoint(port.X+horizontalOffset+sharedPortLoopGap, port.Y)
	}
	return nil
}

func makeIntersectionPoint(from, to *geo.Point, fromDirection geo.Orientation) *geo.Point {
	if !fromDirection.IsVertical() {
		return geo.NewPoint(from.X, to.Y)
	}
	return geo.NewPoint(to.X, from.Y)
}

// computeEdgeOffset computes the offset to place the next loop considering the current `edge` and its label
func computeEdgeOffset(edge *layoutgraph.Edge) (float64, float64) {
	var verticalOffset float64
	var horizontalOffset float64
	points := edge.Points
	if edge.Points[0].X == edge.Points[1].X {
		verticalOffset = math.Abs(points[0].Y - points[1].Y)
		horizontalOffset = math.Abs(points[len(points)-1].X - points[len(points)-2].X)
	} else {
		verticalOffset = math.Abs(points[len(points)-1].Y - points[len(points)-2].Y)
		horizontalOffset = math.Abs(points[0].X - points[1].X)
	}

	if edge.Label != nil {
		verticalOffset += edge.Label.Height
		horizontalOffset += edge.Label.Width
	}
	return verticalOffset, horizontalOffset
}
