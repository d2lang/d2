// Package animation provides timing curves for D2 animation samples.
package animation

// EaseInOut returns the CSS ease-in-out curve for movement between resting positions.
func EaseInOut(t float64) float64 {
	// The curve is symmetric. Keep its midpoint exact for integer geometry.
	if t == 0.5 {
		return 0.5
	}
	return CubicBezier(t, 0.42, 0, 0.58, 1)
}

// CubicBezier returns the progress at normalized time x for a CSS cubic-bezier
// timing function. The curve runs from (0, 0) to (1, 1), with x1 and x2 in [0, 1].
func CubicBezier(x, x1, y1, x2, y2 float64) float64 {
	if x <= 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	// Time is the curve's x coordinate. Invert it to find the parameter at
	// which to evaluate y. Bisection also handles curves with a flat tangent.
	low, high := 0., 1.
	var t float64
	for range 30 {
		t = (low + high) / 2
		estimate := coordinate(x1, x2, t)
		if estimate == x {
			break
		}
		if estimate < x {
			low = t
		} else {
			high = t
		}
	}
	return coordinate(y1, y2, t)
}

func coordinate(p1, p2, t float64) float64 {
	u := 1 - t
	return 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t
}
