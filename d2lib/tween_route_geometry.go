package d2lib

import (
	"cmp"
	"github.com/d2lang/d2/lib/xsort"
	"iter"
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

type tweenRouteShapePair struct{ src, dst *d2target.Shape }

// tweenRouteCandidate transports the two held routes to the current ports. All
// samples depend only on the held boards and the current shape geometry. The
// caller checks the proposed path against the other objects in the frame.
func tweenRouteCandidate(from, to d2target.Connection, a, b, now tweenRouteShapePair, t float64) []*geo.Point {
	if !(t >= 0 && t <= 1) || from.IsCurve || to.IsCurve ||
		!tweenShapeTriplet(a.src, b.src, now.src) || !tweenShapeTriplet(a.dst, b.dst, now.dst) ||
		now.src == now.dst || now.src.ID != "" && now.src.ID == now.dst.ID {
		return nil
	}

	f, g := from.Route, to.Route
	fl, gl := tweenBendFractions(f), tweenBendFractions(g)
	if fl == nil || gl == nil {
		return nil
	}
	start := tweenBoundaryPort(a.src, b.src, now.src, f[0], g[0], t)
	end := tweenBoundaryPort(a.dst, b.dst, now.dst, f[len(f)-1], g[len(g)-1], t)
	if start == nil || end == nil || start.Equals(end) {
		return nil
	}
	// At a held board, retain every authored bend and its exact coordinates.
	for _, held := range []struct {
		at    bool
		pair  tweenRouteShapePair
		route []*geo.Point
	}{{t == 0, a, f}, {t == 1, b, g}} {
		if held.at && sameSlot(held.pair.src, now.src) && sameSlot(held.pair.dst, now.dst) {
			r := make([]*geo.Point, len(held.route))
			for i, p := range held.route {
				r[i] = p.Copy()
			}
			return r
		}
	}

	// A direct center-to-center link keeps facing its peer, including when
	// their relative ordering changes. Other straight links can have authored
	// off-center ports, so infer this only from both held layouts.
	if len(f) == 2 && len(g) == 2 {
		line := func(e *d2target.Connection, p tweenRouteShapePair) bool {
			l := gridEdgeLayout{edge: e, src: p.src, dst: p.dst, srcRow: -1, dstRow: -1}
			return l.hasStraightPorts()
		}
		if line(&from, a) && line(&to, b) {
			s, d := straightGridPorts(now.src, now.dst, -1, -1)
			if s == nil {
				return nil
			}
			// The rectangular router does not inset a rounded corner.
			return []*geo.Point{tweenOutlinePort(now.src, s), tweenOutlinePort(now.dst, d)}
		}
	}

	// When a direct link grows one elbow, its two face-normal approaches
	// can be carried continuously instead of swinging a marker sideways
	// against a face while its port travels toward the corner.
	if len(f)+len(g) == 5 {
		if r := tweenOpeningElbow(&from, &to, now, start, end, t); r != nil {
			return r
		}
	}

	// Compatible elbow sequences share vertices. If a bend appears or
	// disappears, register both paths by distance and keep the vertices of
	// both. Sampling at a fixed count can cut across a held corner.
	compatible := len(f) == len(g)
	if compatible {
		for i := 1; i < len(f); i++ {
			if (f[i-1].X == f[i].X) != (g[i-1].X == g[i].X) ||
				(f[i-1].Y == f[i].Y) != (g[i-1].Y == g[i].Y) {
				compatible = false
				break
			}
		}
	}
	var points []*geo.Point
	var fractions []float64
	var rails []*geo.Point
	if compatible {
		rails = f
		for i := range f {
			points = append(points, f[i].Interpolate(g[i], t))
			fractions = append(fractions, (1-t)*fl[i]+t*gl[i])
		}
	} else {
		points, fractions = tweenRegisteredBends(f, g, fl, gl, t)
	}

	// Each orthogonal run shares one rail. Shift that coordinate once for
	// the entire run, so a terminal elbow follows its port even when only
	// one page moves or resizes.
	tweenShiftRails(points, rails, fractions, start.X-points[0].X, end.X-points[len(points)-1].X, true)
	tweenShiftRails(points, rails, fractions, start.Y-points[0].Y, end.Y-points[len(points)-1].Y, false)
	points[0], points[len(points)-1] = start, end

	clean := points[:0]
	for _, p := range points {
		if n := len(clean); n == 0 || !p.Equals(clean[n-1]) {
			clean = append(clean, p)
		}
	}
	if !tweenRouteSimple(clean) {
		return nil
	}
	return clean
}

// A morph or a shifted terminal rail can fold across another leg. Keep the
// bounded search for short routes; long custom routes retain their native
// router's geometry.
func tweenRouteSimple(route []*geo.Point) bool {
	if len(route) < 2 || len(route) > maxTweenRoutePoints {
		return false
	}
	for i := 2; i < len(route); i++ {
		p, q, r := route[i-2], route[i-1], route[i]
		x, y, u, v := q.X-p.X, q.Y-p.Y, r.X-q.X, r.Y-q.Y
		if x*u+y*v < 0 && math.Abs(x*v-y*u) <= 1e-9*math.Hypot(x, y)*math.Hypot(u, v) {
			return false // Adjacent legs doubled back on each other.
		}
		for j := 1; j < i-1; j++ {
			if geo.IntersectionPoint(route[j-1], route[j], q, r) != nil {
				return false
			}
		}
	}
	return true
}

// tweenOpeningElbow opens a straight connection into an L (or closes it).
// Two facing ports share the available gap. As one port turns an adjacent
// corner its arm turns outward and both arms extend to the held elbow.
func tweenOpeningElbow(from, to *d2target.Connection, now tweenRouteShapePair, start, end *geo.Point, t float64) []*geo.Point {
	f, g := from.Route, to.Route
	for _, route := range [][]*geo.Point{f, g} {
		for i := 1; i < len(route); i++ {
			if route[i-1].X != route[i].X && route[i-1].Y != route[i].Y {
				return nil
			}
		}
		if len(route) == 3 && (route[0].X == route[1].X) == (route[1].X == route[2].X) {
			return nil
		}
	}
	normal := func(s *d2target.Shape, p, a, nextA, b, nextB *geo.Point) (geo.Point, bool) {
		if s.Type != "" && s.Type != d2target.ShapeRectangle && s.Type != d2target.ShapeSquare {
			return geo.Point{}, false
		}
		// Within an arrowhead's width of a corner, adjoining faces can
		// both support its approach. Away from the corner use the exact
		// face normal of the held route.
		span := min(12., float64(min(s.Width, s.Height))/2)
		direction := func(port, next *geo.Point) geo.Point {
			dx, dy := next.X-port.X, next.Y-port.Y
			switch {
			case dx == 0 && dy != 0:
				face := float64(s.Pos.Y)
				if dy > 0 {
					face += float64(s.Height)
				}
				return geo.Point{Y: math.Copysign(max(0, 1-math.Abs(p.Y-face)/span), dy)}
			case dy == 0 && dx != 0:
				face := float64(s.Pos.X)
				if dx > 0 {
					face += float64(s.Width)
				}
				return geo.Point{X: math.Copysign(max(0, 1-math.Abs(p.X-face)/span), dx)}
			}
			return geo.Point{}
		}
		x, y := direction(a, nextA), direction(b, nextB)
		if x.X*y.X < 0 || x.Y*y.Y < 0 {
			return geo.Point{}, false
		}
		dx, dy := x.X*(1-t)+y.X*t, x.Y*(1-t)+y.Y*t
		m := math.Hypot(dx, dy)
		if m == 0 {
			return geo.Point{}, false
		}
		return geo.Point{X: dx / m, Y: dy / m}, true
	}
	i, j := len(f)-1, len(g)-1
	ns, ok := normal(now.src, start, f[0], f[1], g[0], g[1])
	nd, valid := normal(now.dst, end, f[i], f[i-1], g[j], g[j-1])
	dx, dy := end.X-start.X, end.Y-start.Y
	srcSpan, dstSpan := dx*ns.X+dy*ns.Y, -dx*nd.X-dy*nd.Y
	cos := ns.X*nd.X + ns.Y*nd.Y
	if !ok || !valid || cos > 0 || srcSpan < 0 || dstSpan < 0 {
		return nil
	}
	part := 1 + cos/2
	// The two approaches cannot use more than the available span on
	// either axis. When they meet, the middle leg naturally vanishes.
	for _, axis := range [][3]float64{{dx, ns.X, nd.X}, {dy, ns.Y, nd.Y}} {
		gap, u, v := axis[0], axis[1], axis[2]
		if gap*u < -1e-7 || gap*v > 1e-7 {
			return nil
		}
		used := math.Abs(srcSpan*u - dstSpan*v)
		if used > 0 {
			part = min(part, math.Abs(gap)/used)
		}
	}
	srcReach, dstReach := srcSpan*part, dstSpan*part
	if cos == -1 {
		// A marker uses more of its terminal leg than a plain line end.
		// Share the space that remains after both insets have been paid.
		a, b := from.ArrowheadAdjustmentsForWidths(now.src.RenderedStrokeWidth(), now.dst.RenderedStrokeWidth())
		c, d := to.ArrowheadAdjustmentsForWidths(now.src.RenderedStrokeWidth(), now.dst.RenderedStrokeWidth())
		srcInset := (1-t)*math.Hypot(a.X, a.Y) + t*math.Hypot(c.X, c.Y)
		dstInset := (1-t)*math.Hypot(b.X, b.Y) + t*math.Hypot(d.X, d.Y)
		if srcSpan <= srcInset+dstInset {
			return []*geo.Point{start, end}
		}
		srcReach = (srcSpan + srcInset - dstInset) / 2
		dstReach = srcSpan - srcReach
	}
	a := geo.NewPoint(start.X+ns.X*srcReach, start.Y+ns.Y*srcReach)
	b := geo.NewPoint(end.X+nd.X*dstReach, end.Y+nd.Y*dstReach)
	r := []*geo.Point{start}
	for _, point := range []*geo.Point{a, b, end} {
		if math.Hypot(point.X-r[len(r)-1].X, point.Y-r[len(r)-1].Y) > 1e-7 {
			r = append(r, point)
		}
	}
	if tweenRouteSimple(r) {
		return r
	}
	return nil
}

func tweenShapeTriplet(a, b, now *d2target.Shape) bool {
	return tweenRoutableShape(a) && tweenRoutableShape(b) && tweenRoutableShape(now) && a.Type == b.Type && b.Type == now.Type
}

func tweenRoutableShape(s *d2target.Shape) bool {
	if s == nil || s.Width <= 0 || s.Height <= 0 || s.ThreeDee || s.Multiple {
		return false
	}
	switch s.Type {
	case "", d2target.ShapeRectangle, d2target.ShapeSquare, d2target.ShapeOval, d2target.ShapeCircle, d2target.ShapeDiamond, d2target.ShapeHexagon:
		return true
	}
	return false
}

func tweenBoundaryPort(a, b, now *d2target.Shape, p, q *geo.Point, t float64) *geo.Point {
	angle := func(s *d2target.Shape, p *geo.Point) (float64, bool) {
		c := gridEdgeCenter(s)
		if p.Equals(&c) {
			return 0, false
		}
		border := tweenOutlinePort(s, p)
		// Nonrectangular layout intersections are rounded to pixels. A
		// custom or synthetic endpoint far from the outline has no port to
		// transport.
		return math.Atan2((p.Y-c.Y)/float64(s.Height), (p.X-c.X)/float64(s.Width)),
			math.Hypot(p.X-border.X, p.Y-border.Y) <= 1.5
	}
	before, ok := angle(a, p)
	after, valid := angle(b, q)
	if !ok || !valid {
		return nil
	}
	if sameSlot(a, now) && (t == 0 || p.Equals(q) && sameSlot(a, b)) {
		return p.Copy()
	}
	if t == 1 && sameSlot(b, now) {
		return q.Copy()
	}
	// The shorter angular sweep stays on the perimeter, including when
	// two rectangles attach on different sides of the same corner.
	dir := before + t*math.Remainder(after-before, 2*math.Pi)
	sin, cos := math.Sincos(dir)
	c := gridEdgeCenter(now)
	return tweenOutlinePort(now, geo.NewPoint(c.X+cos*float64(now.Width), c.Y+sin*float64(now.Height)))
}

func tweenOutlinePort(s *d2target.Shape, toward *geo.Point) *geo.Point {
	c := gridEdgeCenter(s)
	dx, dy := toward.X-c.X, toward.Y-c.Y
	p := gridEdgeBorder(s, dx, dy)
	if s.BorderRadius <= 0 || (s.Type != "" && s.Type != d2target.ShapeRectangle && s.Type != d2target.ShapeSquare) {
		return p
	}
	// A rectangle's router uses its bounding box. At a painted rounded
	// corner, intersect the same center ray with the corner ellipse.
	hw, hh := float64(s.Width)/2, float64(s.Height)/2
	rx, ry := min(float64(s.BorderRadius), hw), min(float64(s.BorderRadius), hh)
	cx, cy := hw-rx, hh-ry
	x, y := math.Abs(p.X-c.X), math.Abs(p.Y-c.Y)
	if x <= cx || y <= cy {
		return p
	}
	x, y, cx, cy = x/rx, y/ry, cx/rx, cy/ry
	aa := x*x + y*y
	bb := x*cx + y*cy
	scale := (bb + math.Sqrt(max(0, bb*bb-aa*(cx*cx+cy*cy-1)))) / aa
	return geo.NewPoint(c.X+(p.X-c.X)*scale, c.Y+(p.Y-c.Y)*scale)
}

// Normalized arc length at every vertex. Repeated or nonfinite vertices have
// no usable segment for interpolation or arrowhead placement.
func tweenBendFractions(route []*geo.Point) []float64 {
	if len(route) < 2 || !tweenFinitePoint(route[0]) {
		return nil
	}
	fractions := make([]float64, len(route))
	for i := 1; i < len(route); i++ {
		a, b := route[i-1], route[i]
		if !tweenFinitePoint(b) || a.Equals(b) {
			return nil
		}
		fractions[i] = fractions[i-1] + math.Hypot(b.X-a.X, b.Y-a.Y)
	}
	length := fractions[len(fractions)-1]
	if math.IsInf(length, 0) || !(length > 0) {
		return nil
	}
	for i := range fractions {
		fractions[i] /= length
		if i > 0 && fractions[i] <= fractions[i-1] {
			return nil
		}
	}
	return fractions
}

func tweenRegisteredBends(f, g []*geo.Point, fl, gl []float64, t float64) ([]*geo.Point, []float64) {
	points := []*geo.Point{f[0].Interpolate(g[0], t)}
	fractions := []float64{0}
	for i, j := 0, 0; i+1 < len(f) && j+1 < len(g); {
		u := min(fl[i+1], gl[j+1])
		a := f[i].Interpolate(f[i+1], (u-fl[i])/(fl[i+1]-fl[i]))
		b := g[j].Interpolate(g[j+1], (u-gl[j])/(gl[j+1]-gl[j]))
		points = append(points, a.Interpolate(b, t))
		fractions = append(fractions, u)
		if u == fl[i+1] {
			i++
		}
		if u == gl[j+1] {
			j++
		}
	}
	return points, fractions
}

func tweenShiftRails(points, rails []*geo.Point, fractions []float64, start, end float64, xAxis bool) {
	coord := func(p *geo.Point) *float64 {
		if xAxis {
			return &p.X
		}
		return &p.Y
	}
	for i := 0; i < len(points); {
		j := i
		for j+1 < len(rails) && *coord(rails[j+1]) == *coord(rails[i]) {
			j++
		}
		fraction := (fractions[i] + fractions[j]) / 2
		switch {
		case i == 0:
			fraction = 0
		case j == len(points)-1:
			fraction = 1
		}
		for k := i; k <= j; k++ {
			if i == 0 && j == len(points)-1 {
				fraction = fractions[k] // A single rail may tilt between two off-center ports.
			}
			*coord(points[k]) += start + (end-start)*fraction
		}
		i = j + 1
	}
}

func tweenFinitePoint(p *geo.Point) bool {
	return p != nil && !math.IsNaN(p.X) && !math.IsNaN(p.Y) && !math.IsInf(p.X, 0) && !math.IsInf(p.Y, 0)
}

// tweenPortRail shifts a terminal orthogonal run lengthwise along the node face.
// Interior bends outside that run retain their authored positions.
type tweenPortRail struct {
	first, last int
	xAxis       bool
	shifts      []float64
}

// tweenRoutePortAlternates yields nearby positions on the same two faces. Try
// single-port moves before paired moves. A caller can stop when its search
// budget is reached without constructing the remaining routes.
func tweenRoutePortAlternates(route []*geo.Point, now tweenRouteShapePair) iter.Seq[[]*geo.Point] {
	return func(yield func([]*geo.Point) bool) {
		if len(route) < 3 || len(route) > maxTweenRoutePoints {
			return
		}
		for i, p := range route {
			if !tweenFinitePoint(p) || i > 0 && math.Abs(p.X-route[i-1].X) > 1e-6 && math.Abs(p.Y-route[i-1].Y) > 1e-6 {
				return
			}
		}
		start := tweenFaceRail(route, now.src, 0, 1)
		end := tweenFaceRail(route, now.dst, len(route)-1, -1)
		emit := func(a, b float64) bool {
			if !start.canShift(route, a) || !end.canShift(route, b) {
				return true
			}
			out := make([]*geo.Point, len(route))
			for i, p := range route {
				out[i] = p.Copy()
			}
			start.apply(out, a)
			end.apply(out, b)
			return !tweenRouteSimple(out) || yield(out)
		}
		for i := 0; i < max(len(start.shifts), len(end.shifts)); i++ {
			if i < len(end.shifts) && !emit(0, end.shifts[i]) || i < len(start.shifts) && !emit(start.shifts[i], 0) {
				return
			}
		}
		// If both ends of a three-run dogleg move, their shared middle leg
		// must still point in the authored direction.
		for _, a := range start.shifts {
			for _, b := range end.shifts {
				if start.xAxis == end.xAxis && start.last+1 == end.first && !start.canShift(route, a-b) {
					continue
				}
				if !emit(a, b) {
					return
				}
			}
		}
	}
}

func tweenFaceRail(route []*geo.Point, s *d2target.Shape, first, step int) tweenPortRail {
	var rail tweenPortRail
	if s == nil || s.Width <= 0 || s.Height <= 0 || s.ThreeDee || s.Multiple {
		return rail
	}
	p, n := route[first], route[first+step]
	rail.xAxis = math.Abs(p.X-n.X) <= 1e-6
	along, across := p.Y, p.X
	length, faceLow, faceHigh := float64(s.Width), float64(s.Pos.Y), float64(s.Pos.Y+s.Height)
	base, direction := float64(s.Pos.X), n.Y-p.Y
	if !rail.xAxis {
		along, across = p.X, p.Y
		length, faceLow, faceHigh = float64(s.Height), float64(s.Pos.X), float64(s.Pos.X+s.Width)
		base, direction = float64(s.Pos.Y), n.X-p.X
	}
	// Search only a flat face, moving the port away from rounded corners.
	margin := min(float64(s.BorderRadius), length/2)
	switch s.Type {
	case "", d2target.ShapeRectangle, d2target.ShapeSquare:
	case d2target.ShapeHexagon:
		if !rail.xAxis {
			return rail
		}
		margin = length / 4
	default:
		return rail
	}
	if !(direction < 0 && math.Abs(along-faceLow) < 1e-6 || direction > 0 && math.Abs(along-faceHigh) < 1e-6) {
		return rail
	}
	last := first
	for next := first + step; next >= 0 && next < len(route); next += step {
		if math.Abs(rail.coord(route[next])-across) > 1e-6 {
			break
		}
		last = next
	}
	if last+step < 0 || last+step >= len(route) {
		return rail // A single straight run has no independent terminal elbow.
	}
	rail.first, rail.last = min(first, last), max(first, last)
	for k := 1; k < 8; k++ {
		at := length * float64(k) / 8
		shift := base + at - across
		if at >= margin && at <= length-margin && math.Abs(shift) > 1e-6 {
			rail.shifts = append(rail.shifts, shift)
		}
	}
	xsort.StableFunc(rail.shifts, func(a, b float64) int {
		return cmp.Compare(math.Abs(a), math.Abs(b))
	})
	return rail
}

func (r tweenPortRail) coord(p *geo.Point) float64 {
	if r.xAxis {
		return p.X
	}
	return p.Y
}

func (r tweenPortRail) canShift(route []*geo.Point, delta float64) bool {
	if delta == 0 {
		return true
	}
	i, j := r.last, r.last+1
	if r.first > 0 {
		i, j = r.first, r.first-1
	}
	leg := r.coord(route[j]) - r.coord(route[i])
	return leg*(leg-delta) > 0
}

func (r tweenPortRail) apply(route []*geo.Point, delta float64) {
	if delta == 0 {
		return
	}
	for i := r.first; i <= r.last; i++ {
		if r.xAxis {
			route[i].X += delta
		} else {
			route[i].Y += delta
		}
	}
}

// tweenRouteCornerRadius leaves a forward tangent between each arrow/stroke
// inset and the first/last rounded corner of a synthetic route.
func tweenRouteCornerRadius(edge *d2target.Connection, route []*geo.Point, now tweenRouteShapePair) float64 {
	if len(route) < 3 || edge.BorderRadius <= 0 {
		return edge.BorderRadius
	}
	c := *edge
	c.Route = route
	a, b := c.ArrowheadAdjustmentsForWidths(now.src.RenderedStrokeWidth(), now.dst.RenderedStrokeWidth())
	last := len(route) - 1
	free := func(p, q, adjust *geo.Point) float64 {
		return math.Hypot(q.X-p.X, q.Y-p.Y) - math.Hypot(adjust.X, adjust.Y)
	}
	return max(0, min(c.BorderRadius, free(route[0], route[1], a)/2, free(route[last], route[last-1], b)/2))
}
