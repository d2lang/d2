package d2target

import "math"

// A rough-go straight line is painted as two separately jittered cubics.
// Bound the opening needed to contain the painted intersection.
func minimumSketchGapRadius(back, front Connection, under, over crossingSegment, routes []crossingRoute) float64 {
	if len(back.Route) != 2 || len(front.Route) != 2 || back.Animated && front.Animated {
		return 0
	}
	a := routes[under.edge].points
	b := routes[over.edge].points
	ax, ay := a[1].X-a[0].X, a[1].Y-a[0].Y
	bx, by := b[1].X-b[0].X, b[1].Y-b[0].Y
	al, bl := math.Hypot(ax, ay), math.Hypot(bx, by)
	ax, ay, bx, by = ax/al, ay/al, bx/bl, by/bl
	sine := math.Abs(ax*by - ay*bx)
	cosine := math.Abs(ax*bx + ay*by)
	// Each stroke lies in a strip around its centerline. The greatest
	// possible intersection displacement is the distance to a corner of
	// the parallelogram formed by those strips.
	x, y := sketchLineDeviation(al, back.Animated), sketchLineDeviation(bl, front.Animated)
	distance := math.Sqrt(x*x+y*y+2*x*y*cosine) / sine
	return distance + float64(back.StrokeWidth)/2
}

func sketchLineDeviation(length float64, animated bool) float64 {
	if animated {
		return 0 // Animated sketch routes use the original centerline.
	}
	// Our sketch renderer uses roughness .5 with rough-go's default
	// randomness offset 2 and bowing 1. Endpoint jitter is at most one
	// unit per axis. Both inner controls can bow by .005 * length; their
	// combined Bernstein weight peaks at .75. Rough-go reduces both
	// contributions to 40% above 500 units.
	bound := math.Sqrt2 + .75*.005*length
	if length > 500 {
		bound *= .4
	}
	return bound
}
