package routing

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// In an isolated two-rank fan, one-bend routes leaving the same side can
// share a port. An adjacent-pair swap can miss crossings when an edge on
// a shared port sits between the two that cross. Reassign the existing
// port slots by bend distance in one sort, keeping both endpoint boxes and
// each destination port fixed.
func orderHierarchyFanPorts(n *layoutgraph.Node, side geo.Orientation, edges []*layoutgraph.Edge, guard *routeWorkGuard) (bool, error) {
	if n.Hierarchy == nil || len(edges) < 2 || len(n.Graph.Edges) != len(n.Edges) || len(n.Graph.Nodes) != len(n.Edges)+1 {
		return false, nil
	}
	if err := guard.add(uint64(len(n.Edges) + len(edges))); err != nil {
		return false, err
	}

	// Use the coordinate along this side of the hub: y for a port on the
	// left or right, x for a port on the top or bottom.
	along := func(point *geo.Point) float64 {
		if side.IsVertical() {
			return point.X
		}
		return point.Y
	}
	extent := func(node *layoutgraph.Node) float64 {
		if side.IsVertical() {
			return node.Width
		}
		return node.Height
	}
	lo, hi := along(n.TopLeft), along(n.TopLeft)+extent(n)
	positive := false
	for i, edge := range n.Edges {
		leaf := n.Adjacent(edge)
		if len(leaf.Edges) != 1 || leaf.Hierarchy != n.Hierarchy || edge.IsCurve || edge.HasTableColumn() ||
			edge.Label != nil || edge.SourceArrowheadLabel != nil || edge.TargetArrowheadLabel != nil ||
			edge.ArrowheadTo(n) != n.Edges[0].ArrowheadTo(n) {
			return false, nil
		}
		// Only adjust the strip occupied by the hub. Every other box is
		// beyond the same end of this strip, so the changed routes stay clear.
		ahead := along(leaf.TopLeft) > hi
		behind := along(leaf.TopLeft)+extent(leaf) < lo
		if !ahead && !behind || i > 0 && ahead != positive {
			return false, nil
		}
		positive = ahead
	}
	ports := make([]float64, len(edges))
	var face float64
	for i, edge := range edges {
		if len(edge.Points) != 3 {
			return false, nil
		}
		p, bend, end := pointsAt(edge, n)
		// Curved boundaries can have exits at different distances from
		// the center. A common face keeps every reassigned port on the hub.
		normal := p.X
		if side.IsVertical() {
			normal = p.Y
		}
		if i > 0 && normal != face {
			return false, nil
		}
		face = normal
		if along(bend) != along(p) || (along(end) > along(p)) != positive || along(end) == along(p) ||
			(side.IsVertical() && end.Y != bend.Y) || (!side.IsVertical() && end.X != bend.X) {
			return false, nil
		}
		ports[i] = along(p)
	}
	// The caller sorted the ports in increasing side coordinate. A route
	// which extends farther must leave farther away from the target rank.
	distance := func(edge *layoutgraph.Edge) float64 {
		p, bend, _ := pointsAt(edge, n)
		return math.Abs(bend.X-p.X) + math.Abs(bend.Y-p.Y)
	}
	if err := stableSortRouteValues(edges, func(a, b *layoutgraph.Edge) bool {
		if positive {
			return distance(a) > distance(b)
		}
		return distance(a) < distance(b)
	}, guard); err != nil {
		return false, err
	}
	changed := false
	for i, edge := range edges {
		p, bend, _ := pointsAt(edge, n)
		changed = changed || along(p) != ports[i]
		if side.IsVertical() {
			p.X, bend.X = ports[i], ports[i]
		} else {
			p.Y, bend.Y = ports[i], ports[i]
		}
	}
	return changed, nil
}
