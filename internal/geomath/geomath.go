// Package geomath holds the arithmetic of lib/geo's segment intersection on
// plain coordinates, so that lib/shape and the layout code can intersect
// segments without allocating points or slices. The expressions are
// lib/geo's, unchanged, so every result has the same bits.
//
// geo.ComputeIntersections (Bézier curves) deliberately stays as it is: a
// restructured copy of it (arrays instead of slices) computed the same bits
// in normal builds but not in race-instrumented ones on arm64, where FMA
// contraction is decided per function by the compiler.
package geomath

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

// SegmentIntersection is geo.IntersectionPoint on coordinates: the point where
// the segments u0-u1 and v0-v1 intersect, and whether they do.
//
// The expressions are geo.IntersectionPoint's and so are the fused operations
// the compiler makes of them, so every result that is not NaN has the same
// bits: IEEE addition and multiplication are commutative except for which
// NaN operand a NaN result carries. That choice follows the operand order of
// the compiled instructions, which the compiler picks per function and picks
// differently here (on arm64 for s*udx, s*udy and udx*uvdy; on amd64 for
// s*udy and both additions). The result is NaN exactly when a NaN reached it,
// and the comparisons that decide whether there is an intersection do not
// depend on NaN payloads, so a NaN result is computed again by
// geo.IntersectionPoint itself, which gives the original's bits.
func SegmentIntersection(u0x, u0y, u1x, u1y, v0x, v0y, v1x, v1y float64) (x, y float64, ok bool) {
	// s*udx - t*vdx = uvdx
	// s*udy - t*vdy = uvdy
	udx := u1x - u0x
	vdx := v1x - v0x
	uvdx := v0x - u0x
	udy := u1y - u0y
	vdy := v1y - v0y
	uvdy := v0y - u0y

	denom := (udy*vdx - udx*vdy)
	if denom == 0 {
		// lines are parallel
		return 0, 0, false
	}
	// Cramer's rule
	s := (vdx*uvdy - vdy*uvdx) / denom
	t := (udx*uvdy - udy*uvdx) / denom

	// lines don't intersect within segments
	if s < 0 || s > 1 || t < 0 || t > 1 {
		return 0, 0, false
	}

	// use s parameter to get point along u
	x, y = u0x+math.Round(s*udx), u0y+math.Round(s*udy)
	if math.IsNaN(x) || math.IsNaN(y) {
		return nanIntersection(u0x, u0y, u1x, u1y, v0x, v0y, v1x, v1y)
	}
	return x, y, true
}

// nanIntersection returns geo.IntersectionPoint's result for an intersection
// whose point SegmentIntersection found to be NaN.
func nanIntersection(u0x, u0y, u1x, u1y, v0x, v0y, v1x, v1y float64) (x, y float64, ok bool) {
	p := geo.IntersectionPoint(&geo.Point{X: u0x, Y: u0y}, &geo.Point{X: u1x, Y: u1y}, &geo.Point{X: v0x, Y: v0y}, &geo.Point{X: v1x, Y: v1y})
	if p == nil {
		// Not reached: both take the same branches on the same values.
		return 0, 0, false
	}
	return p.X, p.Y, true
}

// FirstBoxIntersection is the first point of geo.Box.Intersections(s) for the
// box with top-left corner (x, y), width w and height h and the segment s
// from (sx, sy) to (ex, ey): the sides top, right, bottom and left, from the
// top-left corner clockwise, each tested as IntersectionPoint(s.Start, s.End,
// side start, side end) is.
func FirstBoxIntersection(x, y, w, h, sx, sy, ex, ey float64) (px, py float64, ok bool) {
	trX, trY := x+w, y
	brX, brY := trX, trY+h
	blX, blY := x, brY
	if px, py, ok = SegmentIntersection(sx, sy, ex, ey, x, y, trX, trY); ok {
		return px, py, ok
	}
	if px, py, ok = SegmentIntersection(sx, sy, ex, ey, trX, trY, brX, brY); ok {
		return px, py, ok
	}
	if px, py, ok = SegmentIntersection(sx, sy, ex, ey, brX, brY, blX, blY); ok {
		return px, py, ok
	}
	return SegmentIntersection(sx, sy, ex, ey, blX, blY, x, y)
}
