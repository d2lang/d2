package routing

import (
	"cmp"
	"fmt"
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/hierarchy"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// Keep the placement hierarchy's original lanes when their grid is affordable.
// Wide hierarchies spanning multiple containers can fill most of the drawing
// with extra intersections. In those hierarchies, only linked siblings need a
// shared set of lanes. Both kinds of lanes feed the same visibility graph.
func routingHierarchies(g *layoutgraph.Graph, guard *ovgBuildGuard) (map[*layoutgraph.Node]*layoutgraph.Hierarchy, error) {
	membership := make(map[*layoutgraph.Node]*layoutgraph.Hierarchy)
	whole := make(map[*layoutgraph.Hierarchy]bool)
	needsLocalLanes := false
	for _, node := range g.Nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		h := node.Hierarchy
		if h == nil {
			continue
		}
		keep, seen := whole[h]
		if !seen {
			var err error
			keep, err = keepHierarchyLanes(h, node.Container, len(g.Edges), guard)
			if err != nil {
				return nil, err
			}
			whole[h] = keep
			needsLocalLanes = needsLocalLanes || !keep
		}
		if keep {
			membership[node] = h
		}
	}
	if !needsLocalLanes {
		return membership, guard.check()
	}
	type scope struct {
		hierarchy *layoutgraph.Hierarchy
		container *layoutgraph.Node
	}
	groups := make(map[scope]*layoutgraph.Hierarchy)
	for _, edge := range g.Edges {
		if err := guard.step(); err != nil {
			return nil, err
		}
		from, to := edge.From, edge.To
		if edge.IsLoop() || from.Hierarchy == nil || whole[from.Hierarchy] || from.Hierarchy != to.Hierarchy || from.Container != to.Container {
			continue
		}
		key := scope{from.Hierarchy, from.Container}
		if groups[key] == nil {
			groups[key] = layoutgraph.NewHierarchy()
		}
	}
	for _, node := range g.Nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if group := groups[scope{node.Hierarchy, node.Container}]; group != nil {
			group.Levels()[node] = node.HierarchyLevel()
			membership[node] = group
		}
	}
	return membership, guard.check()
}

func keepHierarchyLanes(h *layoutgraph.Hierarchy, container *layoutgraph.Node, edgeCount int, guard *ovgBuildGuard) (bool, error) {
	left, top := math.Inf(1), math.Inf(1)
	right, bottom := math.Inf(-1), math.Inf(-1)
	oneContainer := true
	for n := range h.Levels() {
		if err := guard.step(); err != nil {
			return false, err
		}
		if n == nil || n.TopLeft == nil {
			return false, fmt.Errorf("TALA routing hierarchy contains a node without a position")
		}
		oneContainer = oneContainer && n.Container == container
		left, top = min(left, n.TopLeft.X), min(top, n.TopLeft.Y)
		right, bottom = max(right, n.TopLeft.X+n.Width), max(bottom, n.TopLeft.Y+n.Height)
	}
	if oneContainer {
		return true, nil
	}

	// Estimate the footprint at the spacing used by addHierarchyLevelNodes,
	// then one heap insertion per arrival direction (horizontal or vertical)
	// per point and arrow. The search guard charges the actual work.
	columns := 1 + math.Ceil((right-left)/ovgPadding)
	rows := 1 + math.Ceil((bottom-top)/ovgPadding)
	points := columns * rows
	heapLevels := 1 + math.Ceil(math.Log2(points))
	estimatedSearchWork := 2 * points * heapLevels * float64(edgeCount)
	return estimatedSearchWork <= float64(maxRouteSearchWorkUnits), nil
}

func newOVGForHierarchy(g *layoutgraph.Graph, h *layoutgraph.Hierarchy, guard *ovgBuildGuard) (*OVG, error) {
	if err := guard.check(); err != nil {
		return nil, err
	}
	// only for SQL Table hierarchies
	// ports must be added before we transpose because transpose can change the node size and then the ports positions
	// for example, take the node below with 2 top/bottom ports and 1 left/right port
	// initial        ->    after transpose      ->      ports added       |       expected
	// ┌──*─────*─┐             ┌─────┐                   ┌*───*┐                  ┌──*──┐
	// *          *             |     |                   |     |                  |     |
	// └──*─────*─┘             |     |                   |     |                  *     *
	//                          |     |                   *     *                  |     |
	//                          |     |                   |     |                  *     *
	//                          |     |                   |     |                  |     |
	//                          └─────┘                   └*───*┘                  └──*──┘
	// note how adding ports after transposing creates them in the wrong position
	// this happens because ports are just relative snap points on a given side
	// in this case, adding the ports first (as in `initial` above) and then transposing them later with the node results in `expected`
	levels := h.Levels()
	nodes := make([]*layoutgraph.Node, 0, len(levels))
	for n := range levels {
		if err := guard.step(); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if err := guard.reserveSortWork(len(nodes)); err != nil {
		return nil, err
	}
	xsort.Func(nodes, func(a, b *layoutgraph.Node) int {
		return cmp.Compare(a.ID, b.ID)
	})
	if err := guard.check(); err != nil {
		return nil, err
	}

	ovg := newBuildOVG(nodes, guard)
	if err := ovg.addPorts(g, guard); err != nil {
		return nil, err
	}

	// Descendants inherit the containing block's rank. Only the blocks that
	// were placed in ranks determine the flow, including the SQL table rule.
	var placementRoots []*layoutgraph.Node
	for _, node := range nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if _, nested := levels[node.Container]; !nested {
			placementRoots = append(placementRoots, node)
		}
	}
	// Mirror leftward and upward flows before building the routing graph.
	switch placementRoots[0].ContainerDirection() {
	case geo.Left:
		if err := guard.reserveHierarchyTransform(ovg, len(nodes)); err != nil {
			return nil, err
		}
		for _, node := range nodes {
			node.Mirror(true, false)
		}
		ovg.transformHierarchyOVGNodes(func(node *OVGNode) {
			node.X = -node.X
		}, func(direction geo.Orientation) geo.Orientation {
			return mirrorPortDirection(direction, true, false)
		})
		defer func() {
			for _, node := range nodes {
				node.Mirror(true, false)
			}
			ovg.transformHierarchyOVGNodes(func(node *OVGNode) {
				node.X = -node.X
			}, func(direction geo.Orientation) geo.Orientation {
				return mirrorPortDirection(direction, true, false)
			})
		}()
		if err := guard.check(); err != nil {
			return nil, err
		}
	case geo.Top:
		if err := guard.reserveHierarchyTransform(ovg, len(nodes)); err != nil {
			return nil, err
		}
		for _, node := range nodes {
			node.Mirror(false, true)
		}
		ovg.transformHierarchyOVGNodes(func(node *OVGNode) {
			node.Y = -node.Y
		}, func(direction geo.Orientation) geo.Orientation {
			return mirrorPortDirection(direction, false, true)
		})
		defer func() {
			for _, node := range nodes {
				node.Mirror(false, true)
			}
			ovg.transformHierarchyOVGNodes(func(node *OVGNode) {
				node.Y = -node.Y
			}, func(direction geo.Orientation) geo.Orientation {
				return mirrorPortDirection(direction, false, true)
			})
		}()
		if err := guard.check(); err != nil {
			return nil, err
		}
	}

	// the hierarchy must be Top to Bottom, Left to Right when creating the OVG
	// if `isHorizontalHierarchy`, the hierarchy is transposed to vertical, the OVG is created, and then transposed back
	if err := guard.reserveWork(uint64(len(nodes))); err != nil {
		return nil, err
	}
	isHorizontalHierarchy := hierarchy.IsHorizontal(placementRoots)
	if isHorizontalHierarchy {
		if err := guard.reserveHierarchyTransform(ovg, len(ovg.NodesInsideBoundingBox)); err != nil {
			return nil, err
		}
		ovg.transpose()
		defer func() {
			ovg.transpose()
		}()
		if err := guard.check(); err != nil {
			return nil, err
		}
	}

	if err := ovg.addNodesIntersections(g, guard); err != nil {
		return nil, err
	}
	if err := ovg.addHierarchyLevelNodes(g, h, guard); err != nil {
		return nil, err
	}
	tl, br, err := guard.ovgBoundingBox(ovg)
	if err != nil {
		return nil, err
	}
	if err := ovg.addNewBoundaryLayers(g, tl, br, guard); err != nil {
		return nil, err
	}
	if err := ovg.addCornerNodes(g, tl, br, guard); err != nil {
		return nil, err
	}
	if err := guard.check(); err != nil {
		return nil, err
	}

	return ovg, nil
}

func (ovg *OVG) addHierarchyLevelNodes(g *layoutgraph.Graph, hierarchy *layoutgraph.Hierarchy, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	levelToNodes := map[int][]*layoutgraph.Node{}
	for n, level := range hierarchy.Levels() {
		if err := guard.step(); err != nil {
			return err
		}
		levelToNodes[level] = append(levelToNodes[level], n)
	}
	// A container can own only some of the original placement levels.
	levels := make([]int, 0, len(levelToNodes))
	for level := range levelToNodes {
		if err := guard.step(); err != nil {
			return err
		}
		levels = append(levels, level)
	}
	if err := guard.reserveSortWork(len(levels)); err != nil {
		return err
	}
	xsort.Ordered(levels)
	rows := make([][]*layoutgraph.Node, 0, len(levels))
	for _, level := range levels {
		nodes := levelToNodes[level]
		if err := guard.reserveSortWork(len(nodes)); err != nil {
			return err
		}
		xsort.Func(nodes, func(a, b *layoutgraph.Node) int {
			if c := cmp.Compare(a.TopLeft.X, b.TopLeft.X); c != 0 {
				return c
			}
			return cmp.Compare(a.ID, b.ID)
		})
		rows = append(rows, nodes)
	}
	tl, br, err := guard.ovgBoundingBox(ovg)
	if err != nil {
		return err
	}
	// Align the bounds to ovgPadding so OVG nodes line up across levels.
	// Subtract one interval to leave at least one OVG node before the first node.
	tl.X -= ovgPadding
	tl.X = math.Floor(tl.X/ovgPadding) * ovgPadding
	width := math.Ceil((br.X-tl.X)/ovgPadding) * ovgPadding
	// Add one interval to leave at least one OVG node after the last node.
	br.X = tl.X + width + ovgPadding
	ys := make(map[float64]struct{})
	for level := 1; level < len(rows); level++ {
		if err := guard.step(); err != nil {
			return err
		}
		levelYs, err := ovg.addBoundaryNodesAboveLevelNodes(rows[level-1], rows[level], hierarchy.Levels(), tl.X, br.X, guard)
		if err != nil {
			return err
		}
		if err := ovg.addPortAlignedNodes(rows[level-1], rows[level], levelYs, guard); err != nil {
			return err
		}
		for _, y := range levelYs {
			ys[y] = struct{}{}
		}
	}

	xs, err := ovg.addLevelNodes(rows, tl.X, br.X, guard)
	if err != nil {
		return err
	}
	return ovg.addIntersections(g, xs, ys, guard)
}

// addBoundaryNodesAboveLevelNodes adds nodes between two levels at the graph boundaries.
// Given the graph below with OVG Nodes highlighted as `*`, this routine will
// add new OVG Nodes, highlighted as `#`, between levels at the left and right boundaries of the OVG.
// Note: boundary nodes are padded considering the "tl, br" change in `ovg.forHierarchicalGraph`
//
// .     ┌──*───────*───┐         ┌──*───────*───┐
// .     │              │         │              │
// .     *              *         *              *
// .     │              │         │              │
// .     *              *         *              *
// .     │              │         │              │
// .     └──*───────*───┘         └──*───────*───┘
// .
// . #                                               #
// .
// . #                                               #
// .
// .                  ┌──*───────*───┐
// .                  │              │
// .                  *              *
// .                  │  nodesBelow  │
// .                  *              *
// .                  │              │
// .                  └──*───────*───┘
func (ovg *OVG) addBoundaryNodesAboveLevelNodes(nodesAbove, nodesBelow []*layoutgraph.Node, levels map[*layoutgraph.Node]int, leftX, rightX float64, guard *ovgBuildGuard) ([]float64, error) {
	if err := guard.check(); err != nil {
		return nil, err
	}
	nodesBelowMinY := math.Inf(1)
	nEdgesAbove := 1.0
	for _, node := range nodesBelow {
		if err := guard.step(); err != nil {
			return nil, err
		}
		// top is the closest to 0, 0 in SVG space
		nodesBelowMinY = math.Min(nodesBelowMinY, node.TopLeft.Y)
		for _, e := range node.Edges {
			if err := guard.step(); err != nil {
				return nil, err
			}
			adj := node.Adjacent(e)
			level, local := levels[adj]
			if e.IsLoop() || !local {
				// hierarchical nodes can be connected to nodes outside the hierarchy
				continue
			}
			if level < levels[node] {
				nEdgesAbove++
			}
		}
	}

	nodesAboveMaxY := math.Inf(-1)
	for _, node := range nodesAbove {
		if err := guard.step(); err != nil {
			return nil, err
		}
		nodesAboveMaxY = math.Max(nodesAboveMaxY, node.TopLeft.Y+node.Height)
	}

	levelDistance := (nodesBelowMinY - nodesAboveMaxY)
	maxHorizontalLines := levelDistance / ovgPadding
	nHorizontalLines := math.Min(nEdgesAbove, maxHorizontalLines)
	pad := math.Ceil(levelDistance / nHorizontalLines)
	pad = math.Max(pad, ovgPadding)

	// these will form the horizontal lines above `nodesBelow`
	var ys []float64
	y := nodesBelowMinY
	for i := 0; i < int(nHorizontalLines)-1; i++ {
		if err := guard.step(); err != nil {
			return nil, err
		}
		y -= pad
		ys = append(ys, y)
		if _, err := guard.addPoint(ovg, geo.NewPoint(leftX, y)); err != nil {
			return nil, err
		}
		if _, err := guard.addPoint(ovg, geo.NewPoint(rightX, y)); err != nil {
			return nil, err
		}
	}
	return ys, guard.check()
}

// addPortAlignedNodes adds OVG Nodes aligned with ports (up/down) at the Y coordinates of the level (marked as `#` below)
// .      #       #                #       #
// .   ┌──*───────*───┐         ┌──*───────*───┐
// .   │              │         │              │
// .   *              *         *              *
// .   │              │         │              │
// .   *              *         *              *
// .   │              │         │              │
// .   └──*───────*───┘         └──*───────*───┘
func (ovg *OVG) addPortAlignedNodes(nodesAbove, nodesBelow []*layoutgraph.Node, ys []float64, guard *ovgBuildGuard) error {
	if err := guard.check(); err != nil {
		return err
	}
	for _, y := range ys {
		if err := guard.step(); err != nil {
			return err
		}
		for _, node := range nodesAbove {
			if err := guard.step(); err != nil {
				return err
			}
			ports, err := guard.portsByOrientation(ovg, node, geo.Bottom)
			if err != nil {
				return err
			}
			for _, port := range ports {
				if _, err := guard.addPoint(ovg, geo.NewPoint(port.X, y)); err != nil {
					return err
				}
			}
		}
		for _, node := range nodesBelow {
			if err := guard.step(); err != nil {
				return err
			}
			ports, err := guard.portsByOrientation(ovg, node, geo.Top)
			if err != nil {
				return err
			}
			for _, port := range ports {
				if _, err := guard.addPoint(ovg, geo.NewPoint(port.X, y)); err != nil {
					return err
				}
			}
		}
	}
	return guard.check()
}

// addLevelNodes creates port aligned nodes between ordered siblings.
// Returns the X coordinate of added nodes.
// .    ┌──*───────*───┐         ┌──*───────*───┐
// .    │              │         │              │
// . +  *              │    +    *              * +
// .    │              *    +    │              │
// . +  *              │    +    *              * +
// .    │              │         │              │
// .    └──*───────*───┘         └──*───────*───┘
func (ovg *OVG) addLevelNodes(levelToNodes [][]*layoutgraph.Node, minXBound, maxXBound float64, guard *ovgBuildGuard) (map[float64]struct{}, error) {
	if err := guard.check(); err != nil {
		return nil, err
	}
	xs := make(map[float64]struct{})
	for l := 0; l < len(levelToNodes); l++ {
		if err := guard.step(); err != nil {
			return nil, err
		}
		isLastNode := false
		ni := 0
		node := levelToNodes[l][ni]
		ports, err := guard.portsByOrientation(ovg, node, geo.Left)
		if err != nil {
			return nil, err
		}
		for x := minXBound; x <= maxXBound; x += ovgPadding {
			if err := guard.step(); err != nil {
				return nil, err
			}
			xs[x] = struct{}{}
			for _, port := range ports {
				if _, err := guard.addPoint(ovg, geo.NewPoint(x, port.Y)); err != nil {
					return nil, err
				}
			}
			if !isLastNode && x >= node.TopLeft.X-ovgPadding {
				// set `x` as the next X coordinate aligned with "common OVG nodes" (see the method docs)
				nextX := node.TopLeft.X + node.Width
				x = math.Ceil(nextX/ovgPadding) * ovgPadding
				// now use the ports on the right of this node
				ports, err = guard.portsByOrientation(ovg, node, geo.Right)
				if err != nil {
					return nil, err
				}
				isLastNode = ni == len(levelToNodes[l])-1
				if !isLastNode {
					// if there is other node to the right, use the left ports of this node too
					ni++
					node = levelToNodes[l][ni]
					leftPorts, err := guard.portsByOrientation(ovg, node, geo.Left)
					if err != nil {
						return nil, err
					}
					ports = append(ports, leftPorts...)
				}
			}
		}
	}
	return xs, guard.check()
}

func (ovg *OVG) transpose() {
	// Transpose each canonical OVG point and each owner's complete direction
	// set once. Ports may appear multiple times in Ports when rounded snap
	// points coincide, so iterating those slices would toggle shared points and
	// metadata more than once.
	ovg.transformHierarchyOVGNodes(func(node *OVGNode) {
		node.X, node.Y = node.Y, node.X
	}, transposePortDirection)
	for _, node := range ovg.NodesInsideBoundingBox {
		node.Transpose()
	}
}

func (ovg *OVG) transformHierarchyOVGNodes(transformPoint func(*OVGNode), transformDirection func(geo.Orientation) geo.Orientation) {
	for _, node := range ovg.Nodes {
		transformPoint(node)
		for owner, metadata := range node.portOwners() {
			node.setPortDirections(owner, metadata.directions.transformed(transformDirection))
		}
	}
	ovg.reindexOccupiedPoints()
}

func transposePortDirection(direction geo.Orientation) geo.Orientation {
	switch direction {
	case geo.Top:
		return geo.Left
	case geo.Bottom:
		return geo.Right
	case geo.Left:
		return geo.Top
	case geo.Right:
		return geo.Bottom
	case geo.TopRight:
		return geo.BottomLeft
	case geo.BottomLeft:
		return geo.TopRight
	default:
		return direction
	}
}

func mirrorPortDirection(direction geo.Orientation, x, y bool) geo.Orientation {
	if x {
		switch direction {
		case geo.Left:
			direction = geo.Right
		case geo.Right:
			direction = geo.Left
		case geo.TopLeft:
			direction = geo.TopRight
		case geo.TopRight:
			direction = geo.TopLeft
		case geo.BottomLeft:
			direction = geo.BottomRight
		case geo.BottomRight:
			direction = geo.BottomLeft
		}
	}
	if y {
		switch direction {
		case geo.Top:
			direction = geo.Bottom
		case geo.Bottom:
			direction = geo.Top
		case geo.TopLeft:
			direction = geo.BottomLeft
		case geo.TopRight:
			direction = geo.BottomRight
		case geo.BottomLeft:
			direction = geo.TopLeft
		case geo.BottomRight:
			direction = geo.TopRight
		}
	}
	return direction
}
