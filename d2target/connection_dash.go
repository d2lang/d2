package d2target

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// A static dash pattern can leave a crossing entirely free of paint.
// The renderer draws round caps, so both painted intervals extend half a
// stroke width beyond their ends.
type crossingDashPattern struct {
	on, period, cap, slack float64
	unknownPhase           bool
}

func newCrossingDashPattern(c Connection, sketch bool) crossingDashPattern {
	if c.Animated || c.StrokeDash <= 0 {
		return crossingDashPattern{}
	}
	var slack float64
	if sketch {
		// Rough strokes restart the dash phase on each generated subpath.
		// Only a single straight route has an unambiguous nominal phase.
		if len(c.Route) != 2 {
			return crossingDashPattern{unknownPhase: true}
		}
		// Allow for endpoint jitter and the bow in very long hand-drawn lines.
		slack = 2 + .01*math.Hypot(c.Route[1].X-c.Route[0].X, c.Route[1].Y-c.Route[0].Y)
	}
	width := float64(c.StrokeWidth)
	on, off := svg.GetStrokeDashAttributes(width, c.StrokeDash)
	return crossingDashPattern{on: on, period: on + off, cap: width / 2, slack: slack}
}

func (d crossingDashPattern) emptyAtCrossing(over, under crossingSegment, p geo.Point, routes []crossingRoute, underWidth, radius float64) bool {
	fore := routes[over.edge].points[over.start : over.start+2]
	back := routes[under.edge].points[under.start : under.start+2]
	dist := func(a, b geo.Point) float64 { return math.Hypot(a.X-b.X, a.Y-b.Y) }
	along := dist(fore[0], p)
	phase := math.Mod(over.distance+along, d.period)
	// At a bend, clearing the entire mask disk of painted ink is sufficient.
	clearance := radius + d.cap + .5
	if min(along, dist(fore[1], p), dist(back[0], p), dist(back[1], p)) > clearance {
		// Away from bends, the distance from a dash end to the back
		// centerline is its along-route distance times sin(crossing angle).
		// A rounded dash cap can reach half a foreground stroke width
		// beyond that end; also include the back stroke and subpixel error.
		fx, fy := fore[1].X-fore[0].X, fore[1].Y-fore[0].Y
		bx, by := back[1].X-back[0].X, back[1].Y-back[0].Y
		sine := math.Abs(fx*by-fy*bx) / (math.Hypot(fx, fy) * math.Hypot(bx, by))
		clearance = min(clearance, (d.cap+underWidth/2+.5)/sine)
	}
	clearance += d.slack
	return phase-d.on > clearance && d.period-phase > clearance
}
