package routing

import (
	"math"
	"math/bits"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

type portAxes struct {
	x, y map[float64]struct{}
}

func newPortAxes() *portAxes {
	return &portAxes{make(map[float64]struct{}), make(map[float64]struct{})}
}

func (axes *portAxes) add(p geo.Point) {
	axes.x[p.X] = struct{}{}
	axes.y[p.Y] = struct{}{}
}

// Dense routing crosses every x port coordinate with every y port coordinate.
// Hierarchies also build their own grids before merging into the shared OVG.
// Hierarchy grids retain enough of these candidates to make heap work per
// connection expensive. A single grid prunes many more intersections, but
// graphs with at least one connection per node also have expensive searches.
func preferSparseOVG(g *layoutgraph.Graph, nearby []*layoutgraph.Node, guard *ovgBuildGuard) (bool, error) {
	if len(g.Edges) == 0 {
		return false, nil
	}
	excluded, err := nodesWithoutRoutingPorts(g, guard)
	if err != nil {
		return false, err
	}
	all := newPortAxes()
	grids := []*portAxes{all}
	hierarchies := make(map[*layoutgraph.Hierarchy]*portAxes)
	for group, nodes := range [...][]*layoutgraph.Node{g.Nodes, nearby} {
		for _, node := range nodes {
			if err := guard.step(); err != nil {
				return false, err
			}
			if _, skip := excluded[node]; skip {
				continue
			}
			var local *portAxes
			if group == 0 && node.Hierarchy != nil {
				local = hierarchies[node.Hierarchy]
				if local == nil {
					local = newPortAxes()
					hierarchies[node.Hierarchy] = local
					grids = append(grids, local)
				}
			}
			for _, side := range node.SnapPointPercentages() {
				for _, point := range side {
					if err := guard.step(); err != nil {
						return false, err
					}
					port := portPosition(node, point)
					all.add(port)
					if local != nil {
						local.add(port)
					}
				}
			}
		}
	}
	var candidates uint64
	remaining := guard.limits.intersectionCandidates - guard.candidates
	for _, grid := range grids {
		if err := guard.step(); err != nil {
			return false, err
		}
		x, y := uint64(len(grid.x)), uint64(len(grid.y))
		if y > 0 && x > (remaining-candidates)/y {
			return true, nil
		}
		candidates += x * y
	}
	if len(hierarchies) == 0 && len(g.Edges) < len(g.Nodes) {
		return false, nil
	}
	heapLevels := uint64(bits.Len64(candidates))
	return heapLevels > 0 && candidates > maxRouteSearchWorkUnits/uint64(len(g.Edges))/heapLevels, nil
}

// A sparse OVG retains the normal ports, obstacle checks and route costs. Its
// extra vertices are local to an obstacle or a connection, so disconnected
// pairs of port axes do not populate a diagram-wide grid.
func (ovg *OVG) addSparseIntersections(g *layoutgraph.Graph, guard *ovgBuildGuard) error {
	type portRay struct {
		start geo.Point
		side  geo.Orientation
	}
	var rays []portRay
	boxes := make([]clearanceBox, 0, len(ovg.NodesInsideBoundingBox))
	outer := clearanceBox{left: math.Inf(1), top: math.Inf(1), right: math.Inf(-1), bottom: math.Inf(-1)}
	for _, node := range ovg.NodesInsideBoundingBox {
		if err := guard.step(); err != nil {
			return err
		}
		box := clearanceBox{
			left: node.TopLeft.X - ovgPadding, top: node.TopLeft.Y - ovgPadding,
			right: node.TopLeft.X + node.Width + ovgPadding, bottom: node.TopLeft.Y + node.Height + ovgPadding,
		}
		boxes = append(boxes, box)
		outer.left = min(outer.left, box.left-ovgPadding)
		outer.top = min(outer.top, box.top-ovgPadding)
		outer.right = max(outer.right, box.right+ovgPadding)
		outer.bottom = max(outer.bottom, box.bottom+ovgPadding)
		corners := box.corners()
		if err := ovg.addSparsePoints(guard, corners[:]...); err != nil {
			return err
		}
		for _, port := range ovg.Ports[node] {
			if err := guard.step(); err != nil {
				return err
			}
			for _, side := range [...]geo.Orientation{geo.Top, geo.Left, geo.Bottom, geo.Right} {
				if port.hasPortDirection(node, side) {
					outward := box.project(*port.Point, side)
					rays = append(rays, portRay{start: outward, side: side})
					if err := ovg.addSparsePoints(guard, outward); err != nil {
						return err
					}
				}
			}
		}
	}
	if len(boxes) == 0 {
		return guard.check()
	}
	corners := outer.corners()
	if err := ovg.addSparsePoints(guard, corners[:]...); err != nil {
		return err
	}

	// Cast four rays from each clearance corner to the nearest clearance
	// boundary. These junctions connect neighboring obstacle outlines and let
	// a route leave a nested container or detour past an intervening shape.
	for _, box := range boxes {
		for _, corner := range box.corners() {
			nearest := outer
			for _, obstacle := range boxes {
				if err := guard.step(); err != nil {
					return err
				}
				nearest.clipRays(corner, obstacle)
			}
			if err := ovg.addSparsePoints(guard,
				geo.Point{X: nearest.left, Y: corner.Y}, geo.Point{X: nearest.right, Y: corner.Y},
				geo.Point{X: corner.X, Y: nearest.top}, geo.Point{X: corner.X, Y: nearest.bottom}); err != nil {
				return err
			}
		}
	}

	// Connect a port's outward ray to the next clearance boundary as well.
	for _, ray := range rays {
		nearest := outer
		for _, box := range boxes {
			if err := guard.step(); err != nil {
				return err
			}
			nearest.clipRays(ray.start, box)
		}
		if err := ovg.addSparsePoints(guard, nearest.project(ray.start, ray.side)); err != nil {
			return err
		}
	}

	// Offer both single-bend routes when the endpoint ports face that bend.
	for _, edge := range g.Edges {
		if err := guard.step(); err != nil {
			return err
		}
		if edge.IsLoop() {
			continue
		}
		for _, source := range ovg.Ports[edge.From] {
			for _, target := range ovg.Ports[edge.To] {
				if err := guard.step(); err != nil {
					return err
				}
				if source.X == target.X || source.Y == target.Y {
					continue
				}
				horizontal, vertical := geo.Right, geo.Bottom
				if source.X > target.X {
					horizontal = geo.Left
				}
				if source.Y > target.Y {
					vertical = geo.Top
				}
				if source.hasPortDirection(edge.From, horizontal) && target.hasPortDirection(edge.To, vertical.GetOpposite()) {
					if err := ovg.addSparsePoints(guard, geo.Point{X: target.X, Y: source.Y}); err != nil {
						return err
					}
				}
				if target.hasPortDirection(edge.To, horizontal.GetOpposite()) && source.hasPortDirection(edge.From, vertical) {
					if err := ovg.addSparsePoints(guard, geo.Point{X: source.X, Y: target.Y}); err != nil {
						return err
					}
				}
			}
		}
	}
	return guard.check()
}

func (ovg *OVG) addSparsePoints(guard *ovgBuildGuard, points ...geo.Point) error {
	if err := guard.reserveCandidates(uint64(len(points))); err != nil {
		return err
	}
	for i := range points {
		if _, err := guard.addPoint(ovg, &points[i]); err != nil {
			return err
		}
	}
	return nil
}

type clearanceBox struct{ left, top, right, bottom float64 }

func (b clearanceBox) corners() [4]geo.Point {
	return [4]geo.Point{
		{X: b.left, Y: b.top}, {X: b.left, Y: b.bottom},
		{X: b.right, Y: b.top}, {X: b.right, Y: b.bottom},
	}
}

func (b clearanceBox) project(p geo.Point, side geo.Orientation) geo.Point {
	switch side {
	case geo.Left:
		p.X = b.left
	case geo.Right:
		p.X = b.right
	case geo.Top:
		p.Y = b.top
	case geo.Bottom:
		p.Y = b.bottom
	}
	return p
}

func (b *clearanceBox) clipRays(p geo.Point, obstacle clearanceBox) {
	if obstacle.top <= p.Y && p.Y <= obstacle.bottom {
		for _, x := range [...]float64{obstacle.left, obstacle.right} {
			if x < p.X {
				b.left = max(b.left, x)
			}
			if x > p.X {
				b.right = min(b.right, x)
			}
		}
	}
	if obstacle.left <= p.X && p.X <= obstacle.right {
		for _, y := range [...]float64{obstacle.top, obstacle.bottom} {
			if y < p.Y {
				b.top = max(b.top, y)
			}
			if y > p.Y {
				b.bottom = min(b.bottom, y)
			}
		}
	}
}
