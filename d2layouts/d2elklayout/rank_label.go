package d2elklayout

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// ELK returns the label rectangle, while D2 represents an edge label by
// its distance along the final, shape-clipped route and the side it is on.
func rankLabelPosition(route geo.Route, l *ELKLabel) (label.Position, float64) {
	centerX, centerY := l.X+l.Width/2, l.Y+l.Height/2
	length := route.Length()
	best, bestFraction := label.UnlockedMiddle, 0.5
	bestError, traveled := math.Inf(1), 0.0
	for i := 1; i < len(route); i++ {
		a, b := route[i-1], route[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		segment := math.Hypot(dx, dy)
		if segment == 0 {
			continue
		}
		projection := ((centerX-a.X)*dx + (centerY-a.Y)*dy) / segment
		segmentFraction := max(0, min(1, projection/segment))
		fraction := (traveled + segment*segmentFraction) / length
		for _, position := range [...]label.Position{label.UnlockedMiddle, label.UnlockedTop, label.UnlockedBottom} {
			pt, _ := position.GetPointOnRoute(route[i-1:i+1], 2, segmentFraction, l.Width, l.Height)
			if d := math.Hypot(pt.X-l.X, pt.Y-l.Y); d < bestError {
				best, bestFraction, bestError = position, fraction, d
			}
		}
		traveled += segment
	}
	return best, bestFraction
}
