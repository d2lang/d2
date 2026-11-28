package d2lib

import (
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

type tweenRouteShapePair struct{ src, dst *d2target.Shape }

// Reattach one layout route to the moving boxes. An orthogonal route keeps
// the orientation and direction of every run; a different lane is a switch.
func transportTweenRoute(from, to tweenRouteSample) []*geo.Point {
	f := from.edge.Route
	a, now := from.nodes, to.nodes
	if from.edge.IsCurve || !tweenRoutableShape(a.src) || !tweenRoutableShape(a.dst) ||
		!tweenRoutableShape(now.src) || !tweenRoutableShape(now.dst) ||
		a.src.Type != now.src.Type || a.dst.Type != now.dst.Type || !validTweenRoute(f) {
		return nil
	}
	if sameTweenOutline(a.src, now.src) && sameTweenOutline(a.dst, now.dst) {
		return f
	}
	fractions := tweenBendFractions(f)
	if fractions == nil {
		return nil
	}
	port := func(old, current *d2target.Shape, point *geo.Point) *geo.Point {
		p := tweenOutlinePort(old, point)
		if math.Hypot(p.X-point.X, p.Y-point.Y) > 1.5 {
			return nil
		}
		if sameTweenOutline(old, current) {
			return point.Copy()
		}
		x := (point.X - float64(old.Pos.X)) * float64(current.Width) / float64(old.Width)
		y := (point.Y - float64(old.Pos.Y)) * float64(current.Height) / float64(old.Height)
		return tweenOutlinePort(current, geo.NewPoint(float64(current.Pos.X)+x, float64(current.Pos.Y)+y))
	}
	last := len(f) - 1
	start, end := port(a.src, now.src, f[0]), port(a.dst, now.dst, f[last])
	if start == nil || end == nil || start.Equals(end) {
		return nil
	}
	if len(f) == 2 {
		if axisX, axisY := f[0].X == f[1].X, f[0].Y == f[1].Y; axisX || axisY {
			if !alignTweenPorts(a, now, start, end, axisX) {
				return nil
			}
		} else {
			layout := gridEdgeLayout{edge: from.edge, src: a.src, dst: a.dst, srcRow: -1, dstRow: -1}
			if layout.hasStraightPorts() {
				s, d := straightGridPorts(now.src, now.dst, -1, -1)
				if s == nil {
					return nil
				}
				return []*geo.Point{tweenOutlinePort(now.src, s), tweenOutlinePort(now.dst, d)}
			}
		}
	}
	r := make([]*geo.Point, len(f))
	for i, p := range f {
		r[i] = p.Copy()
	}
	tweenShiftRails(r, f, fractions, start.X-f[0].X, end.X-f[last].X, true)
	tweenShiftRails(r, f, fractions, start.Y-f[0].Y, end.Y-f[last].Y, false)
	r[0], r[last] = start, end
	for i := 1; i < len(r); i++ {
		oldX, oldY := f[i].X-f[i-1].X, f[i].Y-f[i-1].Y
		x, y := r[i].X-r[i-1].X, r[i].Y-r[i-1].Y
		if oldX == 0 && (math.Abs(x) > 1e-7 || y*oldY <= 0) || oldY == 0 && (math.Abs(y) > 1e-7 || x*oldX <= 0) {
			// A moving box can cover the elbow. The surviving run ends on
			// its near face in the same frame the old approach disappears.
			if len(f) == 3 && (f[0].X == f[1].X && f[1].Y == f[2].Y || f[0].Y == f[1].Y && f[1].X == f[2].X) {
				if tip := tweenFlatFaceAlong(start, f[0], f[1], now.dst); tip != nil {
					return []*geo.Point{start, tip}
				}
				if tip := tweenFlatFaceAlong(end, f[2], f[1], now.src); tip != nil {
					return []*geo.Point{tip, end}
				}
			}
			return nil
		}
	}
	if !tweenRouteSimple(r) {
		return nil
	}
	return r
}

func sameTweenOutline(a, b *d2target.Shape) bool {
	return sameSlot(a, b) && a.BorderRadius == b.BorderRadius
}

func tweenFlatFaceAlong(start, old, next *geo.Point, target *d2target.Shape) *geo.Point {
	if target.Type != "" && target.Type != d2target.ShapeRectangle && target.Type != d2target.ShapeSquare {
		return nil
	}
	x, y := float64(target.Pos.X), float64(target.Pos.Y)
	w, h := float64(target.Width), float64(target.Height)
	if old.X == next.X {
		margin := min(float64(target.BorderRadius), w/2)
		if start.X < x+margin || start.X > x+w-margin {
			return nil
		}
		if next.Y < old.Y {
			y += h
		}
		if (y-start.Y)*(next.Y-old.Y) > 0 {
			return geo.NewPoint(start.X, y)
		}
	} else {
		margin := min(float64(target.BorderRadius), h/2)
		if start.Y < y+margin || start.Y > y+h-margin {
			return nil
		}
		if next.X < old.X {
			x += w
		}
		if (x-start.X)*(next.X-old.X) > 0 {
			return geo.NewPoint(x, start.Y)
		}
	}
	return nil
}

// A straight orthogonal connection has just one lane. When its boxes move
// sideways, slide the less stationary port only as far as their facing flat
// borders allow. Disjoint faces require a different router-produced path.
func alignTweenPorts(from, now tweenRouteShapePair, a, b *geo.Point, vertical bool) bool {
	coord := func(p *geo.Point) *float64 {
		if vertical {
			return &p.X
		}
		return &p.Y
	}
	if math.Abs(*coord(a)-*coord(b)) <= 1e-7 {
		return true
	}
	span := func(s *d2target.Shape, port *geo.Point) (float64, float64, bool) {
		if s.Type != "" && s.Type != d2target.ShapeRectangle && s.Type != d2target.ShapeSquare {
			return 0, 0, false
		}
		base, size, face, depth, n := s.Pos.X, s.Width, s.Pos.Y, s.Height, port.Y
		if !vertical {
			base, size, face, depth, n = s.Pos.Y, s.Height, s.Pos.X, s.Width, port.X
		}
		r := min(float64(s.BorderRadius), float64(size)/2)
		onFace := math.Abs(n-float64(face)) < 1e-7 || math.Abs(n-float64(face+depth)) < 1e-7
		return float64(base) + r, float64(base+size) - r, onFace
	}
	loA, hiA, okA := span(now.src, a)
	loB, hiB, okB := span(now.dst, b)
	lo, hi := max(loA, loB), min(hiA, hiB)
	if !okA || !okB || lo > hi {
		return false
	}
	at := (*coord(a) + *coord(b)) / 2
	if sameSlot(from.src, now.src) {
		at = *coord(a)
	} else if sameSlot(from.dst, now.dst) {
		at = *coord(b)
	}
	at = max(lo, min(hi, at))
	*coord(a), *coord(b) = at, at
	return true
}

// Reject a transported path that folds back or intersects itself.
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
	available := min(free(route[0], route[1], a), free(route[last], route[last-1], b))
	if available > c.BorderRadius {
		return c.BorderRadius
	}
	return max(0, available/2)
}
