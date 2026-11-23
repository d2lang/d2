package d2target

import (
	"math"
	"sort"
	"strings"

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
}

type crossingSegment struct {
	edge, start            int
	minX, maxX, minY, maxY float64
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
func ConnectionCrossings(connections []Connection, shapes map[string]Shape) [][]ConnectionGap {
	gaps := make([][]ConnectionGap, len(connections))
	if len(connections) < 2 {
		return gaps
	}
	routes := make([]crossingRoute, len(connections))
	var segments []crossingSegment
	var extentX, extentY, spanX, spanY float64
	var minX, maxX, minY, maxY float64
	for i, c := range connections {
		if c.Opacity == 0 || c.StrokeWidth == 0 || c.Stroke == "" || strings.EqualFold(c.Stroke, "transparent") || strings.EqualFold(c.Stroke, "none") {
			continue
		}
		route := &routes[i]
		src, dst := c.ArrowheadAdjustments(shapes)
		for cmd := range c.PathCommands(src, dst) {
			if cmd.Kind == 'C' || cmd.Kind == 'S' {
				route.points = flattenCrossingCubic(route.points, route.points[len(route.points)-1], cmd.Control1, cmd.Control2, cmd.End, 0)
			} else if len(route.points) == 0 || route.points[len(route.points)-1] != cmd.End {
				route.points = append(route.points, cmd.End)
			}
			if len(segments)+len(route.points)-1 > maxCrossingSegments {
				return gaps
			}
		}
		route.srcMarker, _ = c.SrcArrow.Dimensions(float64(c.StrokeWidth))
		route.dstMarker, _ = c.DstArrow.Dimensions(float64(c.StrokeWidth))
		for j := 1; j < len(route.points); j++ {
			a, b := route.points[j-1], route.points[j]
			s := crossingSegment{i, j - 1, min(a.X, b.X), max(a.X, b.X), min(a.Y, b.Y), max(a.Y, b.Y)}
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
			radius := 2 + 2*float64(max(connections[lower].StrokeWidth, connections[upper].StrokeWidth))
			if ra.touchesEndpoint(*p, radius) || rb.touchesEndpoint(*p, radius) {
				continue
			}
			gaps[lower] = append(gaps[lower], ConnectionGap{Center: *p, Radius: radius})
			count++
		}
	}
	return gaps
}

func (r crossingRoute) touchesEndpoint(p geo.Point, radius float64) bool {
	src, dst := r.points[0], r.points[len(r.points)-1]
	return math.Hypot(src.X-p.X, src.Y-p.Y) <= radius+r.srcMarker || math.Hypot(dst.X-p.X, dst.Y-p.Y) <= radius+r.dstMarker
}

// The convex hull of a cubic lies within its control points. When each
// control is within a fraction of a pixel of the finite chord, that chord is
// sufficient for positioning a gap. Check the finite segment so collinear
// cubics that double back retain their extrema.
func flattenCrossingCubic(points []geo.Point, a, b, c, d geo.Point, depth int) []geo.Point {
	if depth >= 8 || (b.DistanceToLine(&a, &d) <= 0.125 && c.DistanceToLine(&a, &d) <= 0.125) {
		if points[len(points)-1] != d {
			points = append(points, d)
		}
		return points
	}
	mid := func(p, q geo.Point) geo.Point { return geo.Point{X: (p.X + q.X) / 2, Y: (p.Y + q.Y) / 2} }
	ab, bc, cd := mid(a, b), mid(b, c), mid(c, d)
	abc, bcd := mid(ab, bc), mid(bc, cd)
	m := mid(abc, bcd)
	points = flattenCrossingCubic(points, a, ab, abc, m, depth+1)
	return flattenCrossingCubic(points, m, bcd, cd, d, depth+1)
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
