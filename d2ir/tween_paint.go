package d2ir

import (
	"image/color"
	"math"
	"slices"
	"strconv"
	"strings"

	d2color "github.com/d2lang/d2/lib/color"
)

func newTweenPaint(start, end string) (tweenValue, bool) {
	if start == end {
		return nil, false
	}
	fromGradient, toGradient := d2color.IsGradient(start), d2color.IsGradient(end)
	if fromGradient || toGradient {
		if !fromGradient || !toGradient {
			return nil, false
		}
		return newTweenGradient(start, end)
	}
	// Solid D2 paints accept named colors and three- or six-digit hex.
	if !d2color.ValidColor(start) || !d2color.ValidColor(end) {
		return nil, false
	}
	from, okFrom := d2color.ParseRGBA(start)
	to, okTo := d2color.ParseRGBA(end)
	if !okFrom || !okTo || from.A != 255 || to.A != 255 || from == to {
		return nil, false
	}
	return tweenColor{start: start, end: end, from: from, to: to}, true
}

type tweenColor struct {
	start, end string
	from, to   color.NRGBA
}

func (c tweenColor) at(fraction float64) string {
	switch fraction {
	case 0:
		return c.start
	case 1:
		return c.end
	}
	return d2color.InterpolateHex(c.from, c.to, fraction)
}

type tweenGradient struct {
	start, end      string
	kind, direction string
	from, to        []tweenStop
}

type tweenStop struct {
	color  color.NRGBA
	offset float64
}

func newTweenGradient(start, end string) (tweenValue, bool) {
	from, err := d2color.ParseGradient(start)
	if err != nil {
		return nil, false
	}
	to, err := d2color.ParseGradient(end)
	if err != nil || len(from.ColorStops) != len(to.ColorStops) || !sameGradientGeometry(from, to) {
		return nil, false
	}
	first, okFrom := resolveTweenStops(from)
	last, okTo := resolveTweenStops(to)
	if !okFrom || !okTo || slices.Equal(first, last) {
		return nil, false
	}
	return tweenGradient{start: start, end: end, kind: to.Type, direction: to.Direction, from: first, to: last}, true
}

func resolveTweenStops(gradient d2color.Gradient) ([]tweenStop, bool) {
	stops := make([]tweenStop, len(gradient.ColorStops))
	previous := 0.0
	for i, stop := range gradient.ColorStops {
		paint, ok := d2color.ParseRGBA(stop.Color)
		offset, err := stop.Offset(i, len(stops), previous)
		if !ok || err != nil {
			return nil, false
		}
		stops[i] = tweenStop{color: paint, offset: offset}
		previous = offset
	}
	return stops, true
}

func sameGradientGeometry(from, to d2color.Gradient) bool {
	if from.Type != to.Type {
		return false
	}
	if from.Type == "radial" {
		// Both SVG and raster use the same centered radial gradient for all
		// three supported spellings (implicit, circle, ellipse).
		return true
	}
	ax, ay, bx, by, err := d2color.LinearGradientVector(from.Direction)
	if err != nil {
		return false
	}
	cx, cy, dx, dy, err := d2color.LinearGradientVector(to.Direction)
	if err != nil {
		return false
	}
	// Translating the line perpendicular to its direction leaves the paint
	// unchanged (e.g. the implicit downward gradient and "to bottom").
	vx, vy := bx-ax, by-ay
	const epsilon = 1e-12
	// A numeric diagonal and a corner keyword coincide only in a square.
	// The target paint box is not known until layout.
	if math.Abs(vx*vy) > epsilon && strings.HasSuffix(from.Direction, "deg") != strings.HasSuffix(to.Direction, "deg") {
		return false
	}
	return math.Abs(vx-(dx-cx)) < epsilon && math.Abs(vy-(dy-cy)) < epsilon &&
		math.Abs(vx*(ax-cx)+vy*(ay-cy)) < epsilon
}

func (g tweenGradient) at(fraction float64) string {
	switch fraction {
	case 0:
		return g.start
	case 1:
		return g.end
	}
	var buf strings.Builder
	buf.WriteString(g.kind)
	buf.WriteString("-gradient(")
	if g.direction != "" {
		buf.WriteString(g.direction)
		buf.WriteString(", ")
	}
	for i, a := range g.from {
		if i != 0 {
			buf.WriteString(", ")
		}
		b := g.to[i]
		buf.WriteString(d2color.InterpolateHex(a.color, b.color, fraction))
		buf.WriteByte(' ')
		offset := a.offset + (b.offset-a.offset)*fraction
		buf.WriteString(strconv.FormatFloat(offset*100, 'g', 15, 64))
		buf.WriteByte('%')
	}
	buf.WriteByte(')')
	return buf.String()
}
