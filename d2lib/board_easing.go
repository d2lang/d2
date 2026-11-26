package d2lib

import (
	"math"

	"github.com/d2lang/d2/d2target"
)

// boardGlide accelerates a group from rest and spends the last two thirds
// of the motion time slowing down. Its velocity is proportional to t²(1-t)⁴.
// The integral stays on the collision-checked path and stops at the target.
func boardGlide(t float64) float64 {
	remaining := 1 - t
	tail := remaining * remaining * remaining * remaining * remaining
	return max(0, 1-tail*(1+5*t+15*t*t))
}

// A stroke starts from rest, peaks a third of the way through, then eases
// into its port. Its derivative is 12*t*(1-t)^2 and is always nonnegative.
func boardStroke(t float64) float64 {
	if t > .5 {
		r := 1 - t
		return 1 - r*r*r*(1+3*t)
	}
	return t * t * (6 + t*(3*t-8))
}

// Slow the pen locally at a corner. This displacement vanishes with zero
// slope at the ends of the neighborhood and retains positive forward speed.
func boardCorner(distance, corner, radius float64) float64 {
	if radius <= 0 || math.Abs(distance-corner) >= radius {
		return distance
	}
	x := (distance - corner) / radius
	taper := 1 - x*x
	return distance - .48*radius*x*taper*taper
}

// A rotated arrowhead stays within its diagonal of the port.
func boardArrowReach(e *d2target.Connection) float64 {
	stroke := e.RenderedStrokeWidth()
	aw, ah := e.SrcArrow.Dimensions(stroke)
	bw, bh := e.DstArrow.Dimensions(stroke)
	return max(math.Hypot(aw, ah), math.Hypot(bw, bh))
}
