package routing

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func isNonSharedCrossing(edge, otherEdge *layoutgraph.Edge, edgeSegment, otherSegment int) bool {
	if !nonParallelIntersection(
		edge.Points[edgeSegment],
		edge.Points[edgeSegment+1],
		otherEdge.Points[otherSegment],
		otherEdge.Points[otherSegment+1],
	) {
		return false
	}

	// Skip intersections at the exact end of the segment to avoid counting
	// bends off shared edges.
	segmentStartX := edge.Points[edgeSegment].X
	segmentStartY := edge.Points[edgeSegment].Y
	if segmentStartX == edge.Points[edgeSegment+1].X &&
		(segmentStartX == otherEdge.Points[otherSegment].X || segmentStartX == otherEdge.Points[otherSegment+1].X) {
		return false
	}
	if segmentStartY == edge.Points[edgeSegment+1].Y &&
		(segmentStartY == otherEdge.Points[otherSegment].Y || segmentStartY == otherEdge.Points[otherSegment+1].Y) {
		return false
	}
	return true
}

func equalSigns(first, second float64) bool {
	return first > 0 && second > 0 || first == 0 && second == 0 || first < 0 && second < 0
}

// nonParallelIntersection reports whether segments ab and cd intersect at an
// angle. Collinear overlap is intentionally not a routing crossing.
func nonParallelIntersection(a, b, c, d *geo.Point) bool {
	abcOrientation := orientation(a, b, c)
	abdOrientation := orientation(a, b, d)
	if equalSigns(abcOrientation, abdOrientation) {
		return false
	}

	cdaOrientation := orientation(c, d, a)
	cdbOrientation := orientation(c, d, b)
	return !equalSigns(cdaOrientation, cdbOrientation)
}
