package d2target

import (
	"math"
	"sort"
	"strings"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// The marks in this file do more than surround one target: a highlighter
// runs over a label, an arrow joins a note to what it is about, and a lasso
// loops round several shapes at once.

// shapeLabelBox returns where the shape's own label is drawn, when the label
// is a line of text a mark can sit on.
func shapeLabelBox(s *Shape) (*geo.Box, bool) {
	if s.Label == "" || s.LabelWidth <= 0 || s.Type == ShapeClass || s.Type == ShapeSQLTable || s.Type == ShapeCode {
		return nil, false
	}
	// Resolve the label the way the renderers do.
	position := label.FromString(s.LabelPosition)
	geometry := s.Geometry()
	labelBox := geometry.GetInnerBox()
	if position.IsOutside() || position.IsBorder() {
		labelBox = s.OuterLabelBox(geometry.GetBox())
	}
	tl := s.LabelTopLeft(position, labelBox)
	return geo.NewBox(geo.NewPoint(tl.X, tl.Y), float64(s.LabelWidth), float64(s.LabelHeight)), true
}

func connectionLabelBox(c *Connection) *geo.Box {
	if c.Label == "" || c.LabelWidth <= 0 || c.LabelHeight <= 0 {
		return nil
	}
	tl := c.GetLabelTopLeft()
	if tl == nil {
		return nil
	}
	return geo.NewBox(tl, float64(c.LabelWidth), float64(c.LabelHeight))
}

func growBox(b *geo.Box, by float64) *geo.Box {
	return geo.NewBox(geo.NewPoint(b.TopLeft.X-by, b.TopLeft.Y-by), b.Width+2*by, b.Height+2*by)
}

func unionBox(a, b *geo.Box) *geo.Box {
	x1, y1 := math.Min(a.TopLeft.X, b.TopLeft.X), math.Min(a.TopLeft.Y, b.TopLeft.Y)
	x2 := math.Max(a.TopLeft.X+a.Width, b.TopLeft.X+b.Width)
	y2 := math.Max(a.TopLeft.Y+a.Height, b.TopLeft.Y+b.Height)
	return geo.NewBox(geo.NewPoint(x1, y1), x2-x1, y2-y1)
}

// nearestOnBox returns the point of the box that is closest to p.
func nearestOnBox(b *geo.Box, p geo.Point) geo.Point {
	return geo.Point{
		X: math.Max(b.TopLeft.X, math.Min(b.TopLeft.X+b.Width, p.X)),
		Y: math.Max(b.TopLeft.Y, math.Min(b.TopLeft.Y+b.Height, p.Y)),
	}
}

// shapeHighlightBand is what a highlighter covers on a shape: its label, one
// line at a time, or a band across its middle when it has no line of text.
func shapeHighlightBand(s *Shape, box *geo.Box) (band *geo.Box, lines int) {
	if labelBox, ok := shapeLabelBox(s); ok {
		lines = 1
		if s.Language == "" {
			lines += strings.Count(s.Label, "\n")
		}
		return labelBox, lines
	}
	height := math.Min(24, box.Height/2)
	return geo.NewBox(geo.NewPoint(box.TopLeft.X+6, box.TopLeft.Y+box.Height/2-height/2), math.Max(0, box.Width-12), height), 1
}

// highlightStrokes drags a highlighter across the band, one pass per line of
// text. It returns the passes and how wide the highlighter is.
func highlightStrokes(band *geo.Box, lines int, seed float64) (strokes [][]geo.Point, width float64) {
	lines = max(1, lines)
	lineHeight := band.Height / float64(lines)
	width = math.Max(12, lineHeight)
	x1, x2 := band.TopLeft.X-5, band.TopLeft.X+band.Width+5
	span := x2 - x1
	steps := int(math.Max(4, span/14))
	for i := 0; i < lines; i++ {
		y := band.TopLeft.Y + lineHeight*(float64(i)+0.5)
		// A highlighter is dragged, not ruled: each pass tilts and wavers a little.
		tilt := float64(1.1 * math.Sin(seed+1.9*float64(i)))
		pts := make([]geo.Point, 0, steps+1)
		for k := 0; k <= steps; k++ {
			t := float64(k) / float64(steps)
			waver := float64(0.7 * math.Sin(math.Pi*t) * math.Sin(2.3*t+seed+float64(i)))
			pts = append(pts, geo.Point{X: x1 + float64(span*t), Y: y - tilt + float64(2*tilt*t) + waver})
		}
		strokes = append(strokes, simplify(pts))
	}
	return strokes, width
}

// arrowStrokes draws from the note to what it is about: a shaft that bows a
// little, then the two barbs of its head, each drawn back from the tip.
func arrowStrokes(note, aim *geo.Box, seed float64) [][]geo.Point {
	center := geo.Point{X: aim.TopLeft.X + aim.Width/2, Y: aim.TopLeft.Y + aim.Height/2}
	from := nearestOnBox(growBox(note, 4), center)
	to := nearestOnBox(growBox(aim, 6), from)
	dx, dy := to.X-from.X, to.Y-from.Y
	length := math.Hypot(dx, dy)
	if length < 8 {
		return nil
	}
	bow := math.Min(12, length*0.18)
	if math.Sin(seed) < 0 {
		bow = -bow
	}
	cx := from.X + dx/2 - float64(dy/length*bow)
	cy := from.Y + dy/2 + float64(dx/length*bow)
	const steps = 14
	shaft := make([]geo.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps
		a, b, c := (1-t)*(1-t), 2*t*(1-t), t*t
		shaft = append(shaft, geo.Point{
			X: float64(a*from.X) + float64(b*cx) + float64(c*to.X),
			Y: float64(a*from.Y) + float64(b*cy) + float64(c*to.Y),
		})
	}
	shaft = simplify(shaft)
	// The head follows the direction the shaft arrives in.
	tip := shaft[len(shaft)-1]
	tx, ty := to.X-cx, to.Y-cy
	tl := math.Hypot(tx, ty)
	tx, ty = tx/tl, ty/tl
	const (
		barb   = 11.
		spread = 0.5
	)
	head := func(angle float64) []geo.Point {
		sin, cos := math.Sin(angle), math.Cos(angle)
		bx := float64(tx*cos) - float64(ty*sin)
		by := float64(tx*sin) + float64(ty*cos)
		return []geo.Point{tip, {X: snapTenth(tip.X - float64(barb*bx)), Y: snapTenth(tip.Y - float64(barb*by))}}
	}
	return [][]geo.Point{shaft, head(spread), head(-spread)}
}

// convexHull returns the corners of the smallest convex outline round the
// points, clockwise on screen from the leftmost. The points are whole
// numbers, so every comparison here is exact.
func convexHull(pts []geo.Point) []geo.Point {
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].X != pts[j].X {
			return pts[i].X < pts[j].X
		}
		return pts[i].Y < pts[j].Y
	})
	cross := func(o, a, b geo.Point) float64 {
		return float64((a.X-o.X)*(b.Y-o.Y)) - float64((a.Y-o.Y)*(b.X-o.X))
	}
	hull := make([]geo.Point, 0, len(pts)+1)
	for _, p := range pts {
		for len(hull) >= 2 && cross(hull[len(hull)-2], hull[len(hull)-1], p) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, p)
	}
	lower := len(hull) + 1
	for i := len(pts) - 2; i >= 0; i-- {
		for len(hull) >= lower && cross(hull[len(hull)-2], hull[len(hull)-1], pts[i]) <= 0 {
			hull = hull[:len(hull)-1]
		}
		hull = append(hull, pts[i])
	}
	return hull[:len(hull)-1]
}

// segmentCrossesBox reports whether the segment from a to b passes through
// the inside of the box. Running along its edge does not count.
func segmentCrossesBox(a, b geo.Point, box *geo.Box) bool {
	_, _, crosses := clipSegment(a, b, box)
	return crosses
}

// clipSegment returns the part of the segment from a to b that is inside the
// box, as fractions of its length.
func clipSegment(a, b geo.Point, box *geo.Box) (t0, t1 float64, crosses bool) {
	x1, y1 := box.TopLeft.X, box.TopLeft.Y
	x2, y2 := x1+box.Width, y1+box.Height
	t0, t1 = 0., 1.
	dx, dy := b.X-a.X, b.Y-a.Y
	// Clip the segment against each side in turn (Liang-Barsky).
	for _, side := range [4][2]float64{{-dx, a.X - x1}, {dx, x2 - a.X}, {-dy, a.Y - y1}, {dy, y2 - a.Y}} {
		p, q := side[0], side[1]
		if p == 0 {
			if q <= 0 {
				return 0, 0, false
			}
			continue
		}
		t := q / p
		if p < 0 {
			t0 = math.Max(t0, t)
		} else {
			t1 = math.Min(t1, t)
		}
	}
	return t0, t1, t0 < t1
}

// lassoOutline is the outline a lasso follows: the smallest convex one that
// holds every box, bent inward round any other box that pokes into it from
// the side. A box that sits right among the held ones is left inside, since
// no loop could leave it out.
func lassoOutline(held, others []*geo.Box, pad float64) []geo.Point {
	const (
		// clear is how far the outline stays from a box it goes round.
		clear = 8.
		// maxDent is how far in the outline will bend to leave a box out.
		maxDent = 90.
	)
	corners := make([]geo.Point, 0, 4*len(held))
	padded := make([]*geo.Box, len(held))
	for i, b := range held {
		padded[i] = growBox(b, pad)
		x1, y1 := b.TopLeft.X-pad, b.TopLeft.Y-pad
		x2, y2 := b.TopLeft.X+b.Width+pad, b.TopLeft.Y+b.Height+pad
		corners = append(corners, geo.Point{X: x1, Y: y1}, geo.Point{X: x2, Y: y1}, geo.Point{X: x2, Y: y2}, geo.Point{X: x1, Y: y2})
	}
	outline := convexHull(corners)
	// The outline runs clockwise on screen, so the inside is where this is positive.
	inward := func(a, b, c geo.Point) float64 {
		return float64((b.X-a.X)*(c.Y-a.Y)) - float64((b.Y-a.Y)*(c.X-a.X))
	}
	for pass := 0; pass < 4 && len(outline) >= 3; pass++ {
		bent := false
		for _, other := range others {
			box := growBox(other, clear)
			// Touching the outline at a corner it already bends round is not poking in.
			inner := growBox(box, -0.5)
			edge, depth := -1, math.Inf(1)
			for i, a := range outline {
				b := outline[(i+1)%len(outline)]
				if !segmentCrossesBox(a, b, inner) {
					continue
				}
				length := math.Hypot(b.X-a.X, b.Y-a.Y)
				deepest := 0.
				for _, c := range boxCorners(box) {
					deepest = math.Max(deepest, inward(a, b, c)/length)
				}
				if deepest > 0 && deepest < depth {
					edge, depth = i, deepest
				}
			}
			if edge < 0 || depth > maxDent {
				continue
			}
			a, b := outline[edge], outline[(edge+1)%len(outline)]
			var round []geo.Point
			for _, c := range boxCorners(box) {
				if inward(a, b, c) > 0 {
					round = append(round, c)
				}
			}
			along := func(c geo.Point) float64 {
				return float64((c.X-a.X)*(b.X-a.X)) + float64((c.Y-a.Y)*(b.Y-a.Y))
			}
			sort.Slice(round, func(i, j int) bool { return along(round[i]) < along(round[j]) })
			// Leave the edge some way before it meets the box and rejoin it
			// some way after, so the way round is a bend in the edge and not
			// a wedge cut from one end of it to the other.
			length := math.Hypot(b.X-a.X, b.Y-a.Y)
			enter, exit, _ := clipSegment(a, b, box)
			ramp := math.Max(48, 2*depth)
			on := func(distance float64) geo.Point {
				return geo.Point{X: snapTenth(a.X + float64((b.X-a.X)/length*distance)), Y: snapTenth(a.Y + float64((b.Y-a.Y)/length*distance))}
			}
			path := []geo.Point{a}
			if leave := enter*length - ramp; leave > 1 {
				path = append(path, on(leave))
			}
			path = append(path, round...)
			if rejoin := exit*length + ramp; rejoin < length-1 {
				path = append(path, on(rejoin))
			}
			path = append(path, b)
			// Going round it must not cut into anything the lasso holds.
			ok := true
			for i := 0; i+1 < len(path) && ok; i++ {
				for _, h := range padded {
					if segmentCrossesBox(path[i], path[i+1], growBox(h, -1)) {
						ok = false
						break
					}
				}
			}
			if !ok {
				continue
			}
			next := make([]geo.Point, 0, len(outline)+len(path))
			next = append(next, outline[:edge+1]...)
			next = append(next, path[1:len(path)-1]...)
			next = append(next, outline[edge+1:]...)
			outline, bent = next, true
		}
		if !bent {
			break
		}
	}
	return outline
}

func boxCorners(b *geo.Box) [4]geo.Point {
	x1, y1 := b.TopLeft.X, b.TopLeft.Y
	x2, y2 := x1+b.Width, y1+b.Height
	return [4]geo.Point{{X: x1, Y: y1}, {X: x2, Y: y1}, {X: x2, Y: y2}, {X: x1, Y: y2}}
}

// lassoStroke loops once round all of the held boxes the way a hand rings a
// group: the tightest loop that holds them, its corners rounded off, running
// a little past its own start. It bends in to leave out another box that
// pokes into it.
func lassoStroke(held, others []*geo.Box, seed float64) []geo.Point {
	const (
		pad     = 12.
		radius  = 30.
		spacing = 9.
		overlap = 0.06
		// perCorner is the number of points a rounded corner is drawn with.
		perCorner = 7
	)
	hull := lassoOutline(held, others, pad)
	n := len(hull)
	if n < 3 {
		return nil
	}
	// Start at the corner nearest the top left, where a hand starts a loop.
	first := 0
	for i, v := range hull {
		if v.X+v.Y < hull[first].X+hull[first].Y {
			first = i
		}
	}
	ring := make([]geo.Point, 0, n*perCorner)
	for k := 0; k < n; k++ {
		i := (first + k) % n
		prev, v, next := hull[(i+n-1)%n], hull[i], hull[(i+1)%n]
		in, out := math.Hypot(v.X-prev.X, v.Y-prev.Y), math.Hypot(next.X-v.X, next.Y-v.Y)
		r := math.Min(radius, math.Min(in, out)/2)
		ax, ay := v.X+float64((prev.X-v.X)/in*r), v.Y+float64((prev.Y-v.Y)/in*r)
		bx, by := v.X+float64((next.X-v.X)/out*r), v.Y+float64((next.Y-v.Y)/out*r)
		for j := 0; j < perCorner; j++ {
			t := float64(j) / (perCorner - 1)
			p, q, s := (1-t)*(1-t), 2*t*(1-t), t*t
			ring = append(ring, geo.Point{
				X: float64(p*ax) + float64(q*v.X) + float64(s*bx),
				Y: float64(p*ay) + float64(q*v.Y) + float64(s*by),
			})
		}
	}
	lengths := make([]float64, len(ring))
	total, cx, cy := 0., 0., 0.
	for i, p := range ring {
		q := ring[(i+1)%len(ring)]
		lengths[i] = math.Hypot(q.X-p.X, q.Y-p.Y)
		total += lengths[i]
		cx += p.X
		cy += p.Y
	}
	cx /= float64(len(ring))
	cy /= float64(len(ring))
	at := func(s float64) geo.Point {
		s = math.Mod(s, total)
		for i, l := range lengths {
			if s <= l || i == len(lengths)-1 {
				p, q := ring[i], ring[(i+1)%len(ring)]
				t := 0.
				if l > 0 {
					t = math.Min(1, s/l)
				}
				return geo.Point{X: p.X + float64((q.X-p.X)*t), Y: p.Y + float64((q.Y-p.Y)*t)}
			}
			s -= l
		}
		return ring[0]
	}
	// Begin partway round the first corner, at a place that differs a little
	// from target to target.
	start := float64(radius * (0.5 + 0.2*math.Sin(seed)))
	steps := int(math.Max(24, total*(1+overlap)/spacing))
	pts := make([]geo.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		u := float64(i) / float64(steps)
		p := at(start + float64(u*(1+overlap)*total))
		dx, dy := p.X-cx, p.Y-cy
		d := math.Hypot(dx, dy)
		if d == 0 {
			d = 1
		}
		// The loop drifts outward as it goes, so its end passes outside its
		// start, and the hand wavers on the way.
		off := (u-0.5)*6 + float64(1.2*math.Sin(u*19+seed))
		pts = append(pts, geo.Point{X: p.X + float64(dx/d*off), Y: p.Y + float64(dy/d*off)})
	}
	return simplify(pts)
}
