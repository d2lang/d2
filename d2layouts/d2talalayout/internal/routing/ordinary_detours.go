package routing

import (
	"context"
	"errors"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// RefineOrdinaryDetours repairs unnecessary wall crossings on the selected
// layout. On flat undirected diagrams it can also take an outside lane when
// doing so removes multiple independent crossings without adding bends.
func RefineOrdinaryDetours(ctx context.Context, g *layoutgraph.Graph) error {
	if ctx != nil && g != nil {
		if shortcutInputTooLarge(g) || !slices.ContainsFunc(g.Edges, detourEdge) {
			return ctx.Err()
		}
	}
	err := runAtomicRouteStage(ctx, "RefineOrdinaryDetours", g, nil, maxRouteStageWorkUnits, func(guard *routeWorkGuard) error {
		env, err := hierarchyRouteObstacles(g, guard)
		if err != nil || env == nil {
			return err
		}
		for _, e := range g.Edges {
			if !detourEdge(e) || g.NodeToTree[e.From] != nil || g.NodeToTree[e.To] != nil {
				continue
			}
			if e.From.Container != nil || e.To.Container != nil {
				err = env.repairBoundaryDetour(e)
			} else {
				err = env.repairCrossedDogleg(e)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, errRouteStageWorkLimit) {
		return ctx.Err()
	}
	return err
}

func detourEdge(e *layoutgraph.Edge) bool {
	return e != nil && e.From != nil && e.To != nil && !e.IsInvisible && !e.IsCurve && !e.IsLoop() &&
		e.From.Hierarchy == nil && e.To.Hierarchy == nil && !e.HasTableColumn() &&
		e.Label == nil && e.SourceArrowheadLabel == nil && e.TargetArrowheadLabel == nil &&
		len(e.Points) >= 4 && len(e.Points) <= 6 && (e.From.Container != nil || e.To.Container != nil ||
		(len(e.Points) == 4 && !e.HasArrowTo(e.From) && !e.HasArrowTo(e.To)))
}

// Count strict transitions through a rectangular wall. A corner contact or a
// bend exactly on the wall is ambiguous and is left to the main router.
func wallTransitions(points []*geo.Point, n *layoutgraph.Node) (int, bool) {
	count := 0
	for i := 1; i < len(points); i++ {
		a, b := points[i-1], points[i]
		horizontal := a.Y == b.Y
		lo, hi := corridorSideRange(n, horizontal)
		near, far := corridorSideRange(n, !horizontal)
		track := channelCoordinate(a, !horizontal)
		start, end := channelCoordinate(a, horizontal), channelCoordinate(b, horizontal)
		start, end = min(start, end), max(start, end)
		if track < near || track > far || end < lo || start > hi {
			continue
		}
		if track == near || track == far || start == lo || end == lo || start == hi || end == hi {
			return 0, false
		}
		if start < lo && lo < end {
			count++
		}
		if start < hi && hi < end {
			count++
		}
	}
	return count, true
}

func (env fanObstacles) repairBoundaryDetour(e *layoutgraph.Edge) error {
	// Wall scores own endpoint ancestors. The ordinary paint checks still
	// include their captions and every unrelated container and child body.
	var ancestors, obstacles layoutgraph.Nodes
	for _, n := range env.g.Nodes {
		if err := env.guard.add(uint64(1 + 2*len(env.g.Nodes))); err != nil {
			return err
		}
		if n != e.From && n != e.To && n.IsContainer() && (e.From.IsDescendantOf(n) || e.To.IsDescendantOf(n)) {
			if n.IsInvisible || !n.IsRectangular() {
				return nil
			}
			ancestors = append(ancestors, n)
		} else {
			obstacles = append(obstacles, n)
		}
	}
	old := make([]int, len(ancestors))
	for i, n := range ancestors {
		var ok bool
		if old[i], ok = wallTransitions(e.Points, n); !ok {
			return nil
		}
	}
	paint := env
	paint.g = &layoutgraph.Graph{Nodes: obstacles, Edges: env.g.Edges}
	for i, n := range ancestors {
		if old[i] < 2 {
			continue
		}
		for j := 1; j+2 < len(e.Points); j++ {
			p, q := e.Points[j], e.Points[j+1]
			xAxis := p.X == q.X
			lo, hi := corridorSideRange(n, xAxis)
			lane := channelCoordinate(p, xAxis)
			if lo <= lane && lane <= hi {
				continue
			}
			if err := env.guard.add(uint64(1 + len(obstacles) + len(env.labels))); err != nil {
				return err
			}
			bands := paint.detourBands(p, q, n, xAxis)
			for k := 1; k < len(bands); k++ {
				if err := env.guard.add(uint64(1 + len(ancestors)*len(e.Points))); err != nil {
					return err
				}
				if math.Abs(bands[k]-bands[k-1]) < fanMarkerPitch {
					continue
				}
				mid := math.Round((bands[k] + bands[k-1]) / 2)
				candidate := slices.Clone(e.Points)
				candidate[j], candidate[j+1] = atCorridorCoordinate(p, xAxis, mid), atCorridorCoordinate(q, xAxis, mid)
				if !shortcutOrthogonal(candidate) || shortcutLength(candidate) >= shortcutLength(e.Points) {
					continue
				}
				improved := true
				for at, ancestor := range ancestors {
					crossings, ok := wallTransitions(candidate, ancestor)
					if !ok || crossings > old[at] || (at == i && crossings >= old[at]) {
						improved = false
						break
					}
				}
				if !improved {
					continue
				}
				clear, err := paint.detourClear(e, candidate, true)
				if err != nil {
					return err
				}
				if clear {
					e.Points = candidate
					return nil
				}
			}
		}
	}
	return nil
}

// Bound each candidate lane by the container wall and the projected node and
// label boxes. The geometric checks reject occupied bands as well as corners
// and terminal approaches that would collide outside the projected segment.
func (env fanObstacles) detourBands(p, q *geo.Point, n *layoutgraph.Node, xAxis bool) []float64 {
	lo, hi := corridorSideRange(n, xAxis)
	bands := []float64{lo, hi}
	a, b := channelCoordinate(p, !xAxis), channelCoordinate(q, !xAxis)
	addBox := func(box geo.Box) {
		l, r := boxInterval(box, !xAxis)
		if min(a, b) <= r && l <= max(a, b) {
			l, r = boxInterval(box, xAxis)
			bands = append(bands, min(hi, max(lo, l)), min(hi, max(lo, r)))
		}
	}
	for _, other := range env.g.Nodes {
		addBox(other.Box)
	}
	for _, box := range env.labels {
		addBox(box)
	}
	slices.Sort(bands)
	bands = slices.Compact(bands)
	if channelCoordinate(p, xAxis) > hi {
		slices.Reverse(bands)
	}
	return bands
}

func (env fanObstacles) detourClear(e *layoutgraph.Edge, candidate []*geo.Point, fixed bool) (bool, error) {
	proposed := *e
	proposed.Points = candidate
	for _, endpoint := range []*layoutgraph.Node{e.From, e.To} {
		if !e.HasArrowTo(endpoint) {
			continue
		}
		tip, next := fanEndpoint(&proposed, endpoint)
		old, second := fanEndpoint(e, endpoint)
		if math.Hypot(tip.X-next.X, tip.Y-next.Y) < min(layoutgraph.MinArrowheadClearance, math.Hypot(old.X-second.X, old.Y-second.Y)) {
			return false, nil
		}
	}
	for i := 1; i < len(candidate); i++ {
		for j := i + 2; j < len(candidate); j++ {
			if shortcutSegmentsMeet(candidate[i-1], candidate[i], candidate[j-1], candidate[j]) {
				return false, nil
			}
		}
	}
	clear, err := changedRouteIsClear(env.g, &proposed, e.Points, fixed, env.guard)
	if err != nil || !clear {
		return clear, err
	}
	return clearHierarchyCorridor(&env, e, candidate)
}

func ordinaryRectangle(n *layoutgraph.Node) bool {
	return n.TopLeft != nil && n.Hierarchy == nil && !n.IsInvisible && !n.Is3D && !n.IsMultiple &&
		(n.ShapeType() == "" || n.ShapeType() == shape.SQUARE_TYPE || n.ShapeType() == shape.REAL_SQUARE_TYPE)
}

func (env fanObstacles) repairCrossedDogleg(e *layoutgraph.Edge) error {
	if len(e.Points) != 4 || shortcutBends(e.Points) != 2 || e.HasArrowTo(e.From) || e.HasArrowTo(e.To) || e.From.IsContainer() || e.To.IsContainer() || !ordinaryRectangle(e.From) || !ordinaryRectangle(e.To) {
		return nil
	}
	// Require two different obstructing wires. Touching a bend or sharing a
	// terminal is not evidence that an exterior detour is worthwhile.
	crossed := 0
	for _, other := range env.g.Edges {
		if err := env.guard.add(uint64(len(e.Points) * len(other.Points))); err != nil {
			return err
		}
		if other.IsInvisible || other == e || other.From == e.From || other.To == e.From || other.From == e.To || other.To == e.To {
			continue
		}
		if crossOnStraightRuns(e.Points, other.Points) {
			crossed++
		}
	}
	if crossed < 2 {
		return nil
	}

	var best []*geo.Point
	// Limit the extra wire to two channel spacings and one third of this route.
	oldLength := shortcutLength(e.Points)
	bestLength := oldLength + min(2*segmentSpacingBuffer, oldLength/3)
	for _, face := range []geo.Orientation{geo.Left, geo.Right, geo.Top, geo.Bottom} {
		xAxis := face.IsHorizontal()
		lo, hi := corridorSideRange(e.From, xAxis)
		for _, n := range env.g.Nodes {
			l, r := corridorSideRange(n, xAxis)
			lo, hi = min(lo, l), max(hi, r)
		}
		lane := hi + segmentSpacingBuffer/2
		if face == geo.Left || face == geo.Top {
			lane = lo - segmentSpacingBuffer/2
		}
		from, to := e.From.PortsByOrientation(face), e.To.PortsByOrientation(face)
		for i := range from {
			for j := range to {
				p, q := &from[i], &to[j]
				points := []*geo.Point{p, atCorridorCoordinate(p, xAxis, lane), atCorridorCoordinate(q, xAxis, lane), q}
				length := shortcutLength(points)
				if !shortcutOrthogonal(points) || length >= bestLength {
					continue
				}
				clear, err := env.detourClear(e, points, false)
				if err != nil {
					return err
				}
				if clear {
					best, bestLength = points, length
				}
			}
		}
	}
	if best != nil {
		e.Points = best
	}
	return nil
}

// The SVG rounds a bend and can join two nearby turns with one cubic.
// A crossing on the discarded corner of the routing polyline does not
// justify a longer exterior route. Require a straight run on both wires.
func crossOnStraightRuns(first, second []*geo.Point) bool {
	const clearance = 2 * fanCornerRadius
	for i := 1; i < len(first); i++ {
		for j := 1; j < len(second); j++ {
			a, b, c, d := first[i-1], first[i], second[j-1], second[j]
			if (a.X == b.X) == (c.X == d.X) {
				continue
			}
			if a.X == b.X {
				a, b, c, d = c, d, a, b
			}
			if min(a.X, b.X)+clearance < c.X && c.X < max(a.X, b.X)-clearance &&
				min(c.Y, d.Y)+clearance < a.Y && a.Y < max(c.Y, d.Y)-clearance {
				return true
			}
		}
	}
	return false
}
