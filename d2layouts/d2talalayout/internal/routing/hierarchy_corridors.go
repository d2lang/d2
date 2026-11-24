package routing

import (
	"context"
	"errors"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// RefineHierarchyCorridors shortens short orthogonal routes on a completed
// hierarchy. It keeps node positions and terminal faces fixed. The candidate
// and all obstacles must fit the bounded shortcut stage's geometry inventory.
func RefineHierarchyCorridors(ctx context.Context, g *layoutgraph.Graph) error {
	if ctx != nil && g != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		if shortcutInputTooLarge(g) {
			return ctx.Err()
		}
		eligible := false
		for _, e := range g.Edges {
			if e != nil && e.From != nil && e.To != nil && hierarchyCorridorEdge(e) {
				eligible = true
				break
			}
		}
		if !eligible {
			return ctx.Err()
		}
	}
	err := runAtomicRouteStage(ctx, "RefineHierarchyCorridors", g, nil, maxRouteStageWorkUnits, func(guard *routeWorkGuard) error {
		return refineHierarchyCorridors(g, guard)
	})
	if errors.Is(err, errRouteStageWorkLimit) {
		return nil
	}
	return err
}

func hierarchyCorridorEdge(e *layoutgraph.Edge) bool {
	return (len(e.Points) == 3 || len(e.Points) == 4) &&
		freeFanEdge(e) && !e.IsCurve && e.From.Container == e.To.Container
}

func refineHierarchyCorridors(g *layoutgraph.Graph, guard *routeWorkGuard) error {
	env, err := hierarchyRouteObstacles(g, guard)
	if err != nil || env == nil {
		return err
	}
	for _, e := range g.Edges {
		if err := guard.step(); err != nil {
			return err
		}
		_, fromTree := g.NodeToTree[e.From]
		_, toTree := g.NodeToTree[e.To]
		if !hierarchyCorridorEdge(e) || fromTree || toTree {
			continue
		}
		var candidates [][]*geo.Point
		switch {
		case len(e.Points) == 3:
			p, err := shortenSharedExit(e, guard)
			if err != nil {
				return err
			}
			if p != nil {
				candidates = append(candidates, p)
			}
		case isEdgeUShaped(e):
			p, err := contractExteriorLane(g, e, guard)
			if err != nil {
				return err
			}
			if p != nil {
				candidates = append(candidates, p)
			}
		case isEdgeSShaped(e):
			candidates = straightHierarchyLanes(e)
		}
		for _, points := range candidates {
			if !shortcutOrthogonal(points) || shortcutLength(points) >= shortcutLength(e.Points)-shortcutEpsilon {
				continue
			}
			old := e.Points
			if !sameRouteDirection(old[0], old[1], points[0], points[1]) ||
				!sameRouteDirection(old[len(old)-2], old[len(old)-1], points[len(points)-2], points[len(points)-1]) {
				continue
			}
			clear, err := shortcutGeometrySafe(g, e, points, env.labels, false, guard)
			if err != nil {
				return err
			}
			if !clear {
				continue
			}
			clear, err = clearHierarchyCorridor(env, e, points)
			if err != nil {
				return err
			}
			if clear {
				e.Points = points
				break
			}
		}
	}
	return nil
}

func atCorridorCoordinate(p *geo.Point, xAxis bool, coordinate float64) *geo.Point {
	if xAxis {
		return geo.NewPoint(coordinate, p.Y)
	}
	return geo.NewPoint(p.X, coordinate)
}

func corridorSideRange(n *layoutgraph.Node, xAxis bool) (float64, float64) {
	if xAxis {
		return n.TopLeft.X, n.TopLeft.X + n.Width
	}
	return n.TopLeft.Y, n.TopLeft.Y + n.Height
}

// Automatic ports may slide along a flat face. Row ports, clipped outlines
// and layered drawing styles have different attachment geometry.
func corridorSlidingPort(n *layoutgraph.Node, p *geo.Point, xAxis bool, coordinate float64) bool {
	if !automaticFanFace(n) {
		return false
	}
	lo, hi := corridorSideRange(n, xAxis)
	near, far := corridorSideRange(n, !xAxis)
	face := channelCoordinate(p, !xAxis)
	// Half a standard 16px attachment lane leaves room for a default arrowhead.
	return (face == near || face == far) && coordinate >= lo+fanMarkerPitch/2 && coordinate <= hi-fanMarkerPitch/2
}

// The two terminal runs of a dogleg already point in the same direction.
// Try retaining one existing port, then the middle of the two facing edges'
// common interval if neither existing port projects to an available lane.
func straightHierarchyLanes(e *layoutgraph.Edge) [][]*geo.Point {
	p, q := e.Points[0], e.Points[3]
	xAxis := p.X == e.Points[1].X
	a, b := corridorSideRange(e.From, xAxis)
	c, d := corridorSideRange(e.To, xAxis)
	lo, hi := math.Max(a, c), math.Min(b, d)
	if lo >= hi {
		return nil
	}
	var result [][]*geo.Point
	for _, lane := range []float64{channelCoordinate(p, xAxis), channelCoordinate(q, xAxis), math.Round((lo + hi) / 2)} {
		if corridorSlidingPort(e.From, p, xAxis, lane) && corridorSlidingPort(e.To, q, xAxis, lane) {
			result = append(result, []*geo.Point{atCorridorCoordinate(p, xAxis, lane), atCorridorCoordinate(q, xAxis, lane)})
		}
	}
	return result
}

// When a perimeter run has no wall on its outer side, the final channel
// nudge leaves it fixed. Move it inward only as far as the outermost box
// under that run, with the ordinary 40px balancing clearance.
func contractExteriorLane(g *layoutgraph.Graph, e *layoutgraph.Edge, guard *routeWorkGuard) ([]*geo.Point, error) {
	p := e.Points
	xAxis := p[0].X != p[1].X
	sign := math.Copysign(1, channelCoordinate(p[1], xAxis)-channelCoordinate(p[0], xAxis))
	u, v := channelCoordinate(p[1], !xAxis), channelCoordinate(p[2], !xAxis)
	outer := math.Inf(-1)
	for _, n := range g.Nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		lo, hi := corridorSideRange(n, !xAxis)
		if lo > math.Max(u, v) || hi < math.Min(u, v) {
			continue
		}
		lo, hi = corridorSideRange(n, xAxis)
		outer = math.Max(outer, math.Max(sign*lo, sign*hi))
	}
	lane := sign * (outer + segmentSpacingBuffer)
	if math.IsInf(lane, 0) || sign*(channelCoordinate(p[1], xAxis)-lane) <= shortcutEpsilon {
		return nil, nil
	}
	return []*geo.Point{p[0], atCorridorCoordinate(p[1], xAxis, lane), atCorridorCoordinate(p[2], xAxis, lane), p[3]}, nil
}

// Separate a shared, unmarked departure when two arrows to distinct nodes
// share a substantial stem and one turns earlier toward an available lane.
// A multiway fan has a different ordering problem and is handled as a group.
func shortenSharedExit(e *layoutgraph.Edge, guard *routeWorkGuard) ([]*geo.Point, error) {
	p := e.Points
	if e.SourceArrowhead != layoutgraph.NoArrowhead || shortcutLength(p[:2]) < segmentSpacingBuffer {
		return nil, nil
	}
	var sharing *layoutgraph.Edge
	for _, other := range e.From.Edges {
		if err := guard.step(); err != nil {
			return nil, err
		}
		if other == e {
			continue
		}
		if other.To == e.From && *other.TargetPort() == *p[0] {
			return nil, nil
		}
		if other.From != e.From || *other.SourcePort() != *p[0] {
			continue
		}
		if sharing != nil || other.To == e.To || other.IsLoop() || other.SourceArrowhead != layoutgraph.NoArrowhead ||
			!sameRouteDirection(p[0], p[1], other.Points[0], other.Points[1]) || shortcutLength(other.Points[:2]) <= shortcutLength(p[:2]) {
			return nil, nil
		}
		sharing = other
	}
	if sharing == nil {
		return nil, nil
	}
	xAxis := p[0].X == p[1].X
	// Keep a full segment-spacing buffer between the now-distinct shafts.
	lane := channelCoordinate(p[0], xAxis) + math.Copysign(segmentSpacingBuffer, channelCoordinate(p[2], xAxis)-channelCoordinate(p[1], xAxis))
	if !corridorSlidingPort(e.From, p[0], xAxis, lane) {
		return nil, nil
	}
	return []*geo.Point{atCorridorCoordinate(p[0], xAxis, lane), atCorridorCoordinate(p[1], xAxis, lane), p[2]}, nil
}

// Candidate centerlines already satisfy the wall/spacing checks. Also test the
// painted shaft and any moved arrowhead against nearby ink. Crossed corridors
// are deliberately left in place, even when the same crossing could be kept.
func clearHierarchyCorridor(env *fanObstacles, e *layoutgraph.Edge, points []*geo.Point) (bool, error) {
	ignore := map[*layoutgraph.Edge]bool{e: true}
	proposal := *e
	proposal.Points = points
	for _, endpoint := range []*layoutgraph.Node{e.From, e.To} {
		old, _ := fanEndpoint(e, endpoint)
		tip, next := fanEndpoint(&proposal, endpoint)
		if *tip == *old {
			continue
		}
		marker, supported := hierarchyMarkerBox(e, endpoint, tip, next)
		if !supported {
			return false, nil
		}
		if marker != nil {
			clear, err := env.clear(marker, endpoint, nil, ignore)
			if err != nil || !clear {
				return clear, err
			}
		}
	}
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		// The supported 2px stroke occupies 1px on either side of its axis.
		strip := geo.NewBox(geo.NewPoint(math.Min(a.X, b.X)-1, math.Min(a.Y, b.Y)-1), math.Abs(b.X-a.X)+2, math.Abs(b.Y-a.Y)+2)
		clear, err := env.clear(strip, e.From, e.To, ignore)
		if err != nil || !clear {
			return clear, err
		}
		if corner := fanCornerBounds(points, i); corner != nil {
			clear, err = env.clear(corner, e.From, e.To, ignore)
			if err != nil || !clear {
				return clear, err
			}
		}
	}
	return true, nil
}
