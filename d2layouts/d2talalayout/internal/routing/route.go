package routing

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

type Route struct {
	GEdge    *layoutgraph.Edge
	OVGNodes []*OVGNode
	FromPort geo.Point
	ToPort   geo.Point
}

func (r *Route) createSegmentEndpoints() []*geo.Point {
	points := make([]*geo.Point, 0)

	points = append(points, r.OVGNodes[1].Point.Copy())
	for i := 2; i < len(r.OVGNodes)-2; i++ {
		lastPoint := r.OVGNodes[i-1].Point
		currentPoint := r.OVGNodes[i].Point
		nextPoint := r.OVGNodes[i+1].Point
		if (currentPoint.X == lastPoint.X) && (currentPoint.X != nextPoint.X) {
			points = append(points, currentPoint.Copy())
		} else if (currentPoint.Y == lastPoint.Y) && (currentPoint.Y != nextPoint.Y) {
			points = append(points, currentPoint.Copy())
		}
	}
	points = append(points, r.OVGNodes[len(r.OVGNodes)-2].Point.Copy())
	return points
}

// The entire route is colinear
func (r *Route) isEntireColinear(from, to *OVGNode) bool {
	if from.Y == to.Y {
		y := from.Y
		minX, maxX := from.X, to.X
		if maxX < minX {
			minX, maxX = maxX, minX
		}
		for i := 1; i < len(r.OVGNodes)-2; i++ {
			rFrom, rTo := r.OVGNodes[i], r.OVGNodes[i+1]
			if rFrom.Y != y || rTo.Y != y {
				return false
			}
			rMinX, rMaxX := rFrom.X, rTo.X
			if rMaxX < rMinX {
				rMinX, rMaxX = rMaxX, rMinX
			}
			if rMinX < minX || rMaxX > maxX {
				return false
			}
		}
		return true
	}

	if from.X == to.X {
		x := from.X
		minY, maxY := from.Y, to.Y
		if maxY < minY {
			minY, maxY = maxY, minY
		}
		for i := 1; i < len(r.OVGNodes)-2; i++ {
			rFrom, rTo := r.OVGNodes[i], r.OVGNodes[i+1]
			if rFrom.X != x || rTo.X != x {
				return false
			}
			rMinY, rMaxY := rFrom.Y, rTo.Y
			if rMaxY < rMinY {
				rMinY, rMaxY = rMaxY, rMinY
			}
			if rMinY < minY || rMaxY > maxY {
				return false
			}
		}
		return true
	}

	return false
}

// A route can swap edges with another route if:
// - The two endpoints are the same nodes
// - No other edges are using the ports
//   - TODO can iterate on this to allow some instances
//
// - The two endpoints is not diagonal
// - The ports that the two routes are connected to on each node are on the same side
// TODO consider min distance for labels
// Not self-route
func (r *Route) canSwapEdgesGuarded(r2 *Route, allRoutes []*Route, guard workBudget) (bool, error) {
	// A SQL row is part of the authored endpoint, even when both routes
	// use the same table face.
	if r.GEdge.HasTableColumn() || r2.GEdge.HasTableColumn() {
		return false, nil
	}
	if r.GEdge.From == r.GEdge.To {
		return false, nil
	}
	if r2.GEdge.From == r2.GEdge.To {
		return false, nil
	}
	if !((r.GEdge.From == r2.GEdge.From && r.GEdge.To == r2.GEdge.To) ||
		(r.GEdge.From == r2.GEdge.To && r.GEdge.To == r2.GEdge.From)) {
		return false, nil
	}

	if r.GEdge.From.Orientation(r.GEdge.To).IsDiagonal() {
		return false, nil
	}

	if r.GEdge.From == r2.GEdge.From && r.GEdge.To == r2.GEdge.To {
		if r.GEdge.From.Orientation(r.GEdge.To).IsVertical() {
			if r.FromPort.Y != r2.FromPort.Y {
				return false, nil
			}
			if r.ToPort.Y != r2.ToPort.Y {
				return false, nil
			}
		}
		if r.GEdge.From.Orientation(r.GEdge.To).IsHorizontal() {
			if r.FromPort.X != r2.FromPort.X {
				return false, nil
			}
			if r.ToPort.X != r2.ToPort.X {
				return false, nil
			}
		}
	}

	for _, r3 := range allRoutes {
		if guard != nil {
			if err := guard.step(); err != nil {
				return false, err
			}
		}
		if r == r3 || r2 == r3 {
			continue
		}
		if r.FromPort == r3.FromPort || r.FromPort == r3.ToPort {
			return false, nil
		}
		if r.ToPort == r3.FromPort || r.ToPort == r3.ToPort {
			return false, nil
		}
		if r2.FromPort == r3.FromPort || r2.FromPort == r3.ToPort {
			return false, nil
		}
		if r2.ToPort == r3.FromPort || r2.ToPort == r3.ToPort {
			return false, nil
		}
	}

	return true, nil
}
