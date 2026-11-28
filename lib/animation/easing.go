// Package animation provides timing curves for D2 animation samples.
package animation

// EaseInOut returns the fraction of a rest-to-rest movement completed at time t.
// Cubic acceleration covers 1/16 of the distance in the first quarter of the
// move. Mirroring the curve brings the object to rest at the other end.
func EaseInOut(t float64) float64 {
	t = min(1, max(0, t))
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := 1 - t
	return 1 - 4*u*u*u
}
