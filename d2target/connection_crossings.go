package d2target

import (
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
)

// ConnectionGap is a transparent opening in an individual connection stroke.
type ConnectionGap struct {
	Center geo.Point
	Radius float64
}

type crossingRoute struct {
	points               []geo.Point
	srcMarker, dstMarker float64
	dash                 crossingDashPattern
	folds                bool
}

type crossingSegment struct {
	edge, start            int
	distance               float64 // Centerline length up to the start of this segment.
	minX, maxX, minY, maxY float64
}

func (s crossingSegment) distanceTo(p geo.Point, routes []crossingRoute) float64 {
	start := routes[s.edge].points[s.start]
	return s.distance + math.Hypot(p.X-start.X, p.Y-start.Y)
}

type locatedGap struct {
	index     int
	distance  float64
	maxRadius float64
	minRadius float64 // Smallest opening that contains a hand-drawn intersection.
}

// Bound the extra work for unusually large or coincident routes. If a board
// exceeds a limit, render its original paths rather than a partial set of gaps.
const (
	maxCrossingSegments    = 128 * 1024
	maxCrossingComparisons = 4 * 1024 * 1024
	maxCrossingGaps        = 64 * 1024
)

// ConnectionCrossings returns one list for each input connection in original
// diagram order. A higher z-index passes in front; ties use stable paint order.
func ConnectionCrossings(connections []Connection, shapes map[string]Shape, sketch bool, strokeVisible func(string) bool) [][]ConnectionGap {
	gaps := make([][]ConnectionGap, len(connections))
	if len(connections) < 2 {
		return gaps
	}
	if strokeVisible == nil {
		strokeVisible = color.PaintVisibleThroughout
	}
	routes := make([]crossingRoute, len(connections))
	locations := make([][]locatedGap, len(connections))
	var segments []crossingSegment
	var extentX, extentY, spanX, spanY float64
	var minX, maxX, minY, maxY float64
	for i, c := range connections {
		if c.Opacity == 0 || c.RenderedStrokeWidth() == 0 || !strokeVisible(c.Stroke) {
			continue
		}
		route := &routes[i]
		route.dash = newCrossingDashPattern(c, sketch)
		var dashSlack *float64
		if route.dash.period != 0 {
			dashSlack = &route.dash.slack
		}
		src, dst := c.ArrowheadAdjustments(shapes)
		for cmd := range c.PathCommands(src, dst) {
			if cmd.Kind == 'C' || cmd.Kind == 'S' {
				route.points = flattenCrossingCubic(route.points, route.points[len(route.points)-1], cmd.Control1, cmd.Control2, cmd.End, 0, dashSlack)
			} else if len(route.points) == 0 || route.points[len(route.points)-1] != cmd.End {
				route.points = append(route.points, cmd.End)
			}
			if len(segments)+len(route.points)-1 > maxCrossingSegments {
				return gaps
			}
		}
		route.srcMarker, _ = c.SrcArrow.Dimensions(c.RenderedStrokeWidth())
		route.dstMarker, _ = c.DstArrow.Dimensions(c.RenderedStrokeWidth())
		var distance float64
		first, last := route.points[0], route.points[len(route.points)-1]
		netX, netY := last.X-first.X, last.Y-first.Y
		for j := 1; j < len(route.points); j++ {
			a, b := route.points[j-1], route.points[j]
			s := crossingSegment{edge: i, start: j - 1, distance: distance, minX: min(a.X, b.X), maxX: max(a.X, b.X), minY: min(a.Y, b.Y), maxY: max(a.Y, b.Y)}
			dx, dy := b.X-a.X, b.Y-a.Y
			distance += math.Hypot(dx, dy)
			if (dx != 0 && dx*netX <= 0) || (dy != 0 && dy*netY <= 0) {
				route.folds = true
			}
			if len(segments) == 0 {
				minX, maxX, minY, maxY = s.minX, s.maxX, s.minY, s.maxY
			}
			minX, maxX = min(minX, s.minX), max(maxX, s.maxX)
			minY, maxY = min(minY, s.minY), max(maxY, s.maxY)
			spanX += s.maxX - s.minX
			spanY += s.maxY - s.minY
			segments = append(segments, s)
		}
	}
	// Sweep along the axis on which segment bounds cover the smaller
	// fraction of the drawing. This also handles stacks of long parallel lines.
	extentX, extentY = maxX-minX, maxY-minY
	if spanX*extentY > spanY*extentX {
		for i := range segments {
			s := &segments[i]
			s.minX, s.minY = s.minY, s.minX
			s.maxX, s.maxY = s.maxY, s.maxX
		}
	}
	sort.SliceStable(segments, func(i, j int) bool { return segments[i].minX < segments[j].minX })
	comparisons, count := 0, 0
	for i, a := range segments {
		for _, b := range segments[i+1:] {
			if b.minX > a.maxX {
				break
			}
			comparisons++
			if comparisons > maxCrossingComparisons || count >= maxCrossingGaps {
				clear(gaps)
				return gaps
			}
			if a.edge == b.edge || a.maxY < b.minY || b.maxY < a.minY {
				continue
			}
			lower, upper := a.edge, b.edge
			if connections[lower].ZIndex > connections[upper].ZIndex || (connections[lower].ZIndex == connections[upper].ZIndex && lower > upper) {
				lower, upper = upper, lower
			}
			ra, rb := routes[a.edge], routes[b.edge]
			p := properCrossing(ra.points, a.start, rb.points, b.start)
			if p == nil {
				continue
			}
			radius := 2 + 2*max(connections[lower].RenderedStrokeWidth(), connections[upper].RenderedStrokeWidth())
			if ra.touchesEndpoint(*p, radius) || rb.touchesEndpoint(*p, radius) {
				continue
			}
			underSegment, overSegment := a, b
			if lower == b.edge {
				underSegment, overSegment = b, a
			}
			if route := routes[lower]; route.folds {
				// A mask applies to this entire edge, including any nearby,
				// unrelated fold. Account for the bounded route scan.
				comparisons += len(route.points)
				if comparisons > maxCrossingComparisons {
					clear(gaps)
					return gaps
				}
				radius = route.clearOfOtherLegs(underSegment.start, *p, radius, connections[lower].RenderedStrokeWidth())
				if radius <= connections[lower].RenderedStrokeWidth()/2 {
					continue
				}
			}
			var minRadius float64
			if sketch {
				minRadius = minimumSketchGapRadius(connections[lower], connections[upper], underSegment, overSegment, routes)
				if radius < minRadius {
					continue
				}
			}
			if dash := routes[upper].dash; dash.unknownPhase || dash.period != 0 && dash.emptyAtCrossing(overSegment, underSegment, *p, routes, connections[lower].RenderedStrokeWidth(), radius) {
				continue
			}
			locations[lower] = append(locations[lower], locatedGap{index: len(gaps[lower]), distance: underSegment.distanceTo(*p, routes), maxRadius: radius, minRadius: minRadius})
			gaps[lower] = append(gaps[lower], ConnectionGap{Center: *p, Radius: radius})
			count++
		}
	}
	for edge, onRoute := range locations {
		if len(onRoute) > 1 {
			gaps[edge] = spaceCrossingGaps(gaps[edge], onRoute, connections[edge].RenderedStrokeWidth())
		}
	}
	return gaps
}

// Space neighboring openings on the back route so at least one stroke width
// remains visible between them. Mask elements keep their stable discovery order.
func spaceCrossingGaps(gaps []ConnectionGap, locations []locatedGap, strokeWidth float64) []ConnectionGap {
	sort.SliceStable(locations, func(i, j int) bool { return locations[i].distance < locations[j].distance })
	unique := locations[:0]
	for _, g := range locations {
		// Several foreground edges can meet the back edge at the same point.
		// They need one opening sized for the widest of those strokes.
		if n := len(unique); n > 0 && g.distance-unique[n-1].distance < 1e-6 {
			prev := &unique[n-1]
			prev.maxRadius = max(prev.maxRadius, g.maxRadius)
			prev.minRadius = max(prev.minRadius, g.minRadius)
			gaps[prev.index].Radius = prev.maxRadius
			gaps[g.index].Radius = 0
			continue
		}
		unique = append(unique, g)
	}
	for i := 1; i < len(unique); i++ {
		a, b := &unique[i-1], &unique[i]
		left, right := gaps[a.index], gaps[b.index]
		room := max(0, math.Hypot(left.Center.X-right.Center.X, left.Center.Y-right.Center.Y)-strokeWidth)
		// When one opening is narrow, the other can use the remaining room.
		a.maxRadius = min(a.maxRadius, room-min(room/2, right.Radius))
		b.maxRadius = min(b.maxRadius, room-min(room/2, left.Radius))
	}
	for _, g := range unique {
		gaps[g.index].Radius = g.maxRadius
		if g.maxRadius < g.minRadius {
			gaps[g.index].Radius = 0
		}
	}
	// A circle that cannot cut across the back stroke would leave a notch.
	return slices.DeleteFunc(gaps, func(g ConnectionGap) bool { return g.Radius <= strokeWidth/2 })
}

// A route monotone in both coordinates passes through a centered disk only
// once. For a folding route, limit the radius against any separate visits
// after following the crossing's own passage to the boundary on each side.
func (r crossingRoute) clearOfOtherLegs(segment int, center geo.Point, radius, width float64) float64 {
	const flatness = 0.125
	margin := width/2 + flatness
	inside := func(p geo.Point) bool { return math.Hypot(p.X-center.X, p.Y-center.Y) <= radius+margin }
	lo, hi := segment, segment+1
	for lo > 0 && inside(r.points[lo]) {
		lo--
	}
	for hi < len(r.points)-1 && inside(r.points[hi]) {
		hi++
	}
	for i := 1; i < len(r.points); i++ {
		if i > lo && i <= hi {
			continue
		}
		radius = min(radius, center.DistanceToLine(&r.points[i-1], &r.points[i])-margin)
	}
	return radius
}

func (r crossingRoute) touchesEndpoint(p geo.Point, radius float64) bool {
	src, dst := r.points[0], r.points[len(r.points)-1]
	return math.Hypot(src.X-p.X, src.Y-p.Y) <= radius+r.srcMarker || math.Hypot(dst.X-p.X, dst.Y-p.Y) <= radius+r.dstMarker
}

// The convex hull of a cubic lies within its control points. When each
// control is within a fraction of a pixel of the finite chord, that chord is
// sufficient for positioning a gap. Check the finite segment so collinear
// cubics that double back retain their extrema.
func flattenCrossingCubic(points []geo.Point, a, b, c, d geo.Point, depth int, dashSlack *float64) []geo.Point {
	if depth >= 8 || (b.DistanceToLine(&a, &d) <= 0.125 && c.DistanceToLine(&a, &d) <= 0.125) {
		if dashSlack != nil {
			// Chord and control-polygon lengths bound the true arc length.
			// Charge the full uncertainty before comparing a crossing's dash phase.
			polygon := math.Hypot(a.X-b.X, a.Y-b.Y) + math.Hypot(b.X-c.X, b.Y-c.Y) + math.Hypot(c.X-d.X, c.Y-d.Y)
			*dashSlack += max(0, polygon-math.Hypot(a.X-d.X, a.Y-d.Y))
		}
		if points[len(points)-1] != d {
			points = append(points, d)
		}
		return points
	}
	mid := func(p, q geo.Point) geo.Point { return geo.Point{X: (p.X + q.X) / 2, Y: (p.Y + q.Y) / 2} }
	ab, bc, cd := mid(a, b), mid(b, c), mid(c, d)
	abc, bcd := mid(ab, bc), mid(bc, cd)
	m := mid(abc, bcd)
	points = flattenCrossingCubic(points, a, ab, abc, m, depth+1, dashSlack)
	return flattenCrossingCubic(points, m, bcd, cd, d, depth+1, dashSlack)
}

func properCrossing(a []geo.Point, i int, b []geo.Point, j int) *geo.Point {
	// Keep subpixel coordinates: flattened curve vertices and adjacent segments
	// must agree about whether the intersection falls on a vertex.
	ax, ay := a[i+1].X-a[i].X, a[i+1].Y-a[i].Y
	bx, by := b[j+1].X-b[j].X, b[j+1].Y-b[j].Y
	denom := ax*by - ay*bx
	if denom == 0 {
		return nil
	}
	dx, dy := b[j].X-a[i].X, b[j].Y-a[i].Y
	t, u := (dx*by-dy*bx)/denom, (dx*ay-dy*ax)/denom
	// Half-open segments give a vertex to its outgoing segment exactly once.
	const epsilon = 1e-10
	if t < -epsilon || t >= 1-epsilon || u < -epsilon || u >= 1-epsilon {
		return nil
	}
	p := &geo.Point{X: a[i].X + t*ax, Y: a[i].Y + t*ay}
	atA, atB := t <= epsilon, u <= epsilon
	if !atA && !atB {
		return p
	}
	if (atA && i == 0) || (atB && j == 0) {
		return nil
	}
	prevA, prevB := a[i], b[j]
	if atA {
		prevA = a[i-1]
	}
	if atB {
		prevB = b[j-1]
	}
	// At a bend, a mere touch also intersects a segment. The four incident
	// rays alternate around the point only for a true crossing.
	angle := func(q geo.Point) float64 { return math.Atan2(q.Y-p.Y, q.X-p.X) }
	base := angle(prevA)
	relative := func(q geo.Point) float64 { return math.Mod(angle(q)-base+2*math.Pi, 2*math.Pi) }
	endA, firstB, lastB := relative(a[i+1]), relative(prevB), relative(b[j+1])
	const angularEpsilon = 1e-9
	for _, v := range []float64{firstB, lastB} {
		if v < angularEpsilon || 2*math.Pi-v < angularEpsilon || math.Abs(endA-v) < angularEpsilon {
			return nil
		}
	}
	if (firstB < endA) == (lastB < endA) {
		return nil
	}
	return p
}
