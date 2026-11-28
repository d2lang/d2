package d2target

import (
	"cmp"
	"math"
	"math/bits"
	"slices"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/xsort"
)

const connectionCircleSize = 10

// MovingIconFootprint describes the artwork that a primary caption must clear.
// A stopped stream keeps its footprint while its last icons finish traveling.
type MovingIconFootprint uint8

const (
	noMovingIcon MovingIconFootprint = iota
	movingCircle
	movingImage
)

func (c *Connection) MovingIconFootprint() MovingIconFootprint {
	if c.IconAnimation == nil {
		return noMovingIcon
	}
	if c.Icon != nil {
		return movingImage
	}
	return movingCircle
}

// LabelTopLeft measures a candidate caption at the same position for layout and
// for painting. Inline captions retain their intentional gap in the connection.
// A layout work guard can stop the measurement by returning false from charge.
func (icon MovingIconFootprint) LabelTopLeft(c *Connection, pos label.Position, fraction, width, height float64, charge func(int64) bool) *geo.Point {
	topLeft, segment := pos.GetPointOnRoute(c.Route, c.RenderedStrokeWidth(), fraction, width, height)
	if icon == noMovingIcon || topLeft == nil || pos.IsOnEdge() || segment < 0 {
		return topLeft
	}

	a, b := c.Route[segment], c.Route[segment+1]
	nx, ny := geo.GetUnitNormalVector(a.X, a.Y, b.X, b.Y)
	switch pos {
	case label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideTopRight, label.UnlockedTop:
		nx, ny = -nx, -ny
	}
	// The primary wire sets the side and minimum normal clearance. The text
	// mask extends two units past either end of the measured caption.
	cx, cy := topLeft.X+width/2, topLeft.Y+height/2
	extent := func(x, y float64) float64 {
		radius := float64(connectionCircleSize) / 2
		if icon == movingImage {
			radius = float64(DEFAULT_ICON_SIZE) / 2 * (math.Abs(x) + math.Abs(y))
		}
		return (width/2+2)*math.Abs(x) + height/2*math.Abs(y) + max(radius, c.RenderedStrokeWidth()/2) + label.PADDING
	}
	normalGap := extent(nx, ny) - ((cx-a.X)*nx + (cy-a.Y)*ny)
	if normalGap > 0 {
		topLeft.X += nx * normalGap
		topLeft.Y += ny * normalGap
	}
	if len(c.Route) == 2 {
		return topLeft
	}

	// At an elbow the next leg may still cover the caption. Choose the shortest
	// slide along the wire or farther out on the chosen side. For example, a
	// caption above a horizontal run often needs to slide just past its elbow.
	center := geo.Point{X: topLeft.X + width/2, Y: topLeft.Y + height/2}
	src, dst := c.arrowheadAdjustments(c.IconLabelInsets)
	var path []geo.Point
	for point := range c.iconMotionPoints(src, dst) {
		if charge != nil && !charge(1) {
			return nil
		}
		if len(path) == 0 || path[len(path)-1] != point {
			path = append(path, point)
		}
	}
	direction := geo.Point{X: nx, Y: ny}
	distance, ok := firstIconClearance(path, center, direction, extent, charge)
	if !ok {
		return nil
	}
	if distance > 0 {
		for _, tangent := range [...]geo.Point{{X: -ny, Y: nx}, {X: ny, Y: -nx}} {
			slide, ok := firstIconClearance(path, center, tangent, extent, charge)
			if !ok {
				return nil
			}
			if slide < distance {
				distance, direction = slide, tangent
			}
		}
		topLeft.X += direction.X * distance
		topLeft.Y += direction.Y * distance
	}
	return topLeft
}

// WithIconLabelInsets resolves derived geometry for standalone render targets
// and for frames whose endpoint strokes changed after layout. The original
// board and its connections remain usable by concurrent renders.
func (d *Diagram) WithIconLabelInsets() *Diagram {
	result := d
	var widths map[string]float64
	for i := range d.Connections {
		c := &d.Connections[i]
		if c.IconAnimation == nil || c.Label == "" || len(c.Route) <= 2 || label.FromString(c.LabelPosition).IsOnEdge() {
			continue
		}
		if widths == nil {
			widths = d.shapeStrokeWidths()
		}
		insets := c.EndpointInsets(widths[c.Src], widths[c.Dst])
		if c.IconLabelInsets != insets {
			if result == d {
				copy := *d
				copy.Connections = slices.Clone(d.Connections)
				result = &copy
			}
			result.Connections[i].IconLabelInsets = insets
		}
	}
	return result
}

// Moving the caption along a ray gives one forbidden interval for each painted
// segment. Return the end of the connected interval containing the starting
// position, so separated legs on the far side of a loop do not displace it.
func firstIconClearance(path []geo.Point, center, direction geo.Point, extent func(float64, float64) float64, charge func(int64) bool) (float64, bool) {
	type interval struct{ start, end float64 }
	var later []interval
	var shift float64
	for i := 1; i < len(path); i++ {
		// Batch the guard checks without letting a long scan run uncancelled.
		if charge != nil && (i-1)%64 == 0 && !charge(int64(min(64, len(path)-i))) {
			return 0, false
		}
		p, q := path[i-1], path[i]
		lo, hi := 0., math.Inf(1)
		// The separating axes of an upright rectangle and a line segment are X,
		// Y and the segment normal. Extending each projection by the moving
		// artwork's support also clears its diagonal corners.
		clip := func(x, y float64) bool {
			from := (p.X-center.X)*x + (p.Y-center.Y)*y
			to := (q.X-center.X)*x + (q.Y-center.Y)*y
			r := extent(x, y)
			left, right := min(from, to)-r, max(from, to)+r
			speed := direction.X*x + direction.Y*y
			if math.Abs(speed) < 1e-12 {
				return left < -1e-9 && right > 1e-9
			}
			t0, t1 := left/speed, right/speed
			lo, hi = max(lo, min(t0, t1)), min(hi, max(t0, t1))
			return lo < hi
		}
		if !clip(1, 0) || !clip(0, 1) {
			continue
		}
		nx, ny := geo.GetUnitNormalVector(p.X, p.Y, q.X, q.Y)
		if !clip(nx, ny) {
			continue
		}
		if lo <= shift {
			shift = max(shift, hi)
		} else {
			later = append(later, interval{lo, hi})
		}
	}
	if shift > 0 && len(later) > 0 {
		if charge != nil && !charge(int64(len(later))*int64(bits.Len(uint(len(later))))) {
			return 0, false
		}
		xsort.Func(later, func(a, b interval) int { return cmp.Compare(a.start, b.start) })
		for _, blocked := range later {
			if blocked.start > shift {
				break
			}
			shift = max(shift, blocked.end)
		}
	}
	return shift, true
}
