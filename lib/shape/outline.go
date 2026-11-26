package shape

import (
	"github.com/d2lang/d2/internal/geomath"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// pathBuilder is the part of *svg.SvgPathContext the shapes draw their
// outlines with. Each outline is drawn by one function that is generic over
// the builder, so the SVG path (*svg.SvgPathContext) and the border tracer
// (*borderTracer) receive the same commands with the same arguments.
type pathBuilder interface {
	Absolute(x, y float64) *geo.Point
	StartAt(p *geo.Point)
	L(isLowerCase bool, x, y float64)
	C(isLowerCase bool, x1, y1, x2, y2, x3, y3 float64)
	H(isLowerCase bool, x float64)
	V(isLowerCase bool, y float64)
	Z()
}

// borderOutline is implemented by the shapes of this package: traceBorder
// draws the outline whose Path is the shape's Perimeter into t, or reports
// false if the shape has no such outline.
type borderOutline interface {
	traceBorder(t *borderTracer) bool
}

// borderTracer is the loop of TraceToShapeBorder over Perimeter() as a
// pathBuilder: it intersects each piece of the outline with the segment
// (seg0, seg1) as the outline is drawn and keeps the intersection closest to
// ref, without building the path. Its points are the ones svg.SvgPathContext
// computes (it calls the same Relative and Absolute), each piece is
// intersected with the arithmetic its Intersections method uses (Segment:
// geo.IntersectionPoint's, i.e. geomath.SegmentIntersection; BezierCurve:
// geo.ComputeIntersections) and the pieces come in the order of Perimeter(),
// so the closest point and its distance are the same. Points are kept as
// values.
type borderTracer struct {
	ctx     svg.SvgPathContext // TopLeft, ScaleX and ScaleY
	topLeft geo.Point
	start   geo.Point
	current geo.Point

	seg0, seg1 geo.Point
	ref        geo.Point
	closestD   float64
	closest    geo.Point
	found      bool
}

// newPath starts an outline, as svg.NewSVGPathContext does.
func (t *borderTracer) newPath(tl *geo.Point, sx, sy float64) *borderTracer {
	t.topLeft = *tl
	t.ctx = svg.SvgPathContext{TopLeft: &t.topLeft, ScaleX: sx, ScaleY: sy}
	return t
}

func (t *borderTracer) Absolute(x, y float64) *geo.Point {
	return t.ctx.Absolute(x, y)
}

func (t *borderTracer) StartAt(p *geo.Point) {
	t.start = *p
	t.current = *p
}

func (t *borderTracer) point(isLowerCase bool, x, y float64) geo.Point {
	if isLowerCase {
		return *t.ctx.Relative(&t.current, x, y)
	}
	return *t.ctx.Absolute(x, y)
}

// lineTo intersects the segment from the current point to end.
func (t *borderTracer) lineTo(end geo.Point) {
	start := t.current
	if x, y, ok := geomath.SegmentIntersection(start.X, start.Y, end.X, end.Y, t.seg0.X, t.seg0.Y, t.seg1.X, t.seg1.Y); ok {
		t.consider(x, y)
	}
	t.current = end
}

func (t *borderTracer) consider(x, y float64) {
	d := geo.EuclideanDistance(t.ref.X, t.ref.Y, x, y)
	if d < t.closestD {
		t.closestD = d
		t.closest = geo.Point{X: x, Y: y}
		t.found = true
	}
}

func (t *borderTracer) L(isLowerCase bool, x, y float64) {
	t.lineTo(t.point(isLowerCase, x, y))
}

func (t *borderTracer) C(isLowerCase bool, x1, y1, x2, y2, x3, y3 float64) {
	p0 := t.current
	p1 := t.point(isLowerCase, x1, y1)
	p2 := t.point(isLowerCase, x2, y2)
	p3 := t.point(isLowerCase, x3, y3)
	for _, p := range geo.ComputeIntersections(
		[]float64{p0.X, p1.X, p2.X, p3.X},
		[]float64{p0.Y, p1.Y, p2.Y, p3.Y},
		[]float64{t.seg0.X, t.seg1.X},
		[]float64{t.seg0.Y, t.seg1.Y},
	) {
		t.consider(p.X, p.Y)
	}
	t.current = p3
}

func (t *borderTracer) H(isLowerCase bool, x float64) {
	var end geo.Point
	if isLowerCase {
		end = *t.ctx.Relative(&t.current, x, 0)
	} else {
		end = *t.ctx.Absolute(x, 0)
		end.Y = t.current.Y
	}
	t.lineTo(end)
}

func (t *borderTracer) V(isLowerCase bool, y float64) {
	var end geo.Point
	if isLowerCase {
		end = *t.ctx.Relative(&t.current, 0, y)
	} else {
		end = *t.ctx.Absolute(0, y)
		end.X = t.current.X
	}
	t.lineTo(end)
}

func (t *borderTracer) Z() {
	t.lineTo(t.start)
}
