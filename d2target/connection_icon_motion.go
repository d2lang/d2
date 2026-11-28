package d2target

import (
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/lib/geo"
)

// IconMotionSpeed is measured in diagram coordinates per second, before export scaling.
const IconMotionSpeed = 180.0

const MaxMovingIconsPerConnection = 64

// IconMotionPath follows the painted centerline in the connection's travel direction.
// Distances are cumulative arc lengths, so equal time intervals cover equal distances.
type IconMotionPath struct {
	Points    []geo.Point
	Distances []float64
}

func (c *Connection) IconMotionPath(srcWidth, dstWidth float64) IconMotionPath {
	var p IconMotionPath
	if len(c.Route) < 2 {
		return p
	}
	src, dst := c.ArrowheadAdjustmentsForWidths(srcWidth, dstWidth)
	for cmd := range c.PathCommands(src, dst) {
		switch cmd.Kind {
		case 'C', 'S':
			p.flatten(p.Points[len(p.Points)-1], cmd.Control1, cmd.Control2, cmd.End, 0)
		default:
			p.appendPoint(cmd.End)
		}
	}
	if c.SrcArrow != NoArrowhead && c.DstArrow == NoArrowhead {
		length := p.Length()
		slices.Reverse(p.Points)
		slices.Reverse(p.Distances)
		for i := range p.Distances {
			p.Distances[i] = length - p.Distances[i]
		}
	}
	return p
}

func (p *IconMotionPath) appendPoint(end geo.Point) {
	length := 0.
	if len(p.Points) != 0 {
		prev := p.Points[len(p.Points)-1]
		dist := math.Hypot(end.X-prev.X, end.Y-prev.Y)
		if dist == 0 {
			return
		}
		length = p.Length() + dist
	}
	p.Points = append(p.Points, end)
	p.Distances = append(p.Distances, length)
}

func (p *IconMotionPath) flatten(a, b, c, d geo.Point, depth int) {
	// Subdivide until the control polygon and chord agree to subpixel precision.
	// Limit the perpendicular deviation too: a long, shallow bend can have
	// almost the same length as its chord while missing it by several pixels.
	chord := math.Hypot(a.X-d.X, a.Y-d.Y)
	polygon := math.Hypot(a.X-b.X, a.Y-b.Y) + math.Hypot(b.X-c.X, b.Y-c.Y) + math.Hypot(c.X-d.X, c.Y-d.Y)
	flat := func(q geo.Point) bool {
		return math.Abs((q.X-a.X)*(d.Y-a.Y)-(q.Y-a.Y)*(d.X-a.X)) <= .2*chord
	}
	if depth == 8 || polygon-chord <= .04 && flat(b) && flat(c) {
		p.appendPoint(d)
		return
	}
	mid := func(x, y geo.Point) geo.Point { return geo.Point{X: (x.X + y.X) / 2, Y: (x.Y + y.Y) / 2} }
	ab, bc, cd := mid(a, b), mid(b, c), mid(c, d)
	abc, bcd := mid(ab, bc), mid(bc, cd)
	center := mid(abc, bcd)
	p.flatten(a, ab, abc, center, depth+1)
	p.flatten(center, bcd, cd, d, depth+1)
}

func (p IconMotionPath) Length() float64 {
	if len(p.Distances) == 0 {
		return 0
	}
	return p.Distances[len(p.Distances)-1]
}

func (p IconMotionPath) PointAt(distance float64) geo.Point {
	if len(p.Points) == 0 {
		return geo.Point{}
	}
	i := sort.SearchFloat64s(p.Distances, distance)
	if i == 0 {
		return p.Points[0]
	}
	if i == len(p.Points) {
		return p.Points[i-1]
	}
	frac := (distance - p.Distances[i-1]) / (p.Distances[i] - p.Distances[i-1])
	return *p.Points[i-1].Interpolate(&p.Points[i], frac)
}

// LaunchPeriod gives the time in seconds between visible departures. The
// one-icon loop launches its next copy when the previous copy lands.
func (p IconMotionPath) LaunchPeriod(animation *ConnectionIconAnimation) float64 {
	transit := p.Length() / IconMotionSpeed
	if transit <= 0 || animation == nil {
		return math.Inf(1)
	}
	if animation.Rate == nil {
		return transit
	}
	if *animation.Rate <= 0 {
		return math.Inf(1)
	}
	period := min(math.MaxFloat64, 1 / *animation.Rate)
	// For an overcrowded route, draw every nth launch on the original clock.
	stride := math.Max(1, math.Ceil(transit/(period*MaxMovingIconsPerConnection)))
	if math.IsInf(stride, 1) {
		// Adjacent integral strides are indistinguishable at this magnitude.
		return transit / MaxMovingIconsPerConnection
	}
	return stride * period
}

// ConnectionIconSchedule describes a fixed picture on a repeating multiboard
// SVG clock. Existing copies finish once; the remaining copies launch at First
// and repeat with Period (positive infinity means no additional launches).
type ConnectionIconSchedule struct {
	// FixedLayer also allows a clicked board to keep running indefinitely.
	FixedLayer                  bool
	StartMS, DurationMS, FilmMS int
	Transit, First, Period      float64   // seconds
	Carried                     []float64 // fractions already traveled at the beginning of this picture
}

func (s ConnectionIconSchedule) valid() bool {
	if s.StartMS < 0 || s.DurationMS <= 0 || s.DurationMS > s.FilmMS || s.StartMS > s.FilmMS-s.DurationMS ||
		!finiteRenderNumber(s.Transit) || s.Transit <= 0 || !(s.Period > 0) || !(s.First >= 0) ||
		len(s.Carried) > MaxMovingIconsPerConnection {
		return false
	}
	for _, progress := range s.Carried {
		if !(progress >= 0 && progress < 1) {
			return false
		}
	}
	return true
}
