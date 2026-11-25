package shape

import "github.com/d2lang/d2/lib/geo"

// At this height on the person outline the head is at least as wide as
// the space between the shoulders. The crown above it narrows to a point.
const personCrownFraction = 8. / 77.4

// GridContentBox is the rectangle available for a grid and its padding.
// A cell with grid-gap: 0 can occupy the entire content rectangle.
func GridContentBox(s Shape) *geo.Box {
	inner := s.GetInnerBox()
	if s.GetType() == PERSON_TYPE {
		crown := personCrownFraction * s.GetBox().Height
		inner.TopLeft.Y += crown
		inner.Height -= crown
	}
	return inner
}

// GridDimensionsToFit reserves the same interior used to position children.
func GridDimensionsToFit(s Shape, width, height, paddingX, paddingY float64) (float64, float64) {
	if s.GetType() == PERSON_TYPE {
		height = (height + paddingY) / (1 - personCrownFraction)
		paddingY = 0
	}
	return s.GetDimensionsToFit(width, height, paddingX, paddingY)
}

// ReserveGridPadding accounts for corners on shapes where labels normally
// supply their own padding. Explicit small grid gaps can expose these corners.
func ReserveGridPadding(shapeType string, contentHeight float64, padding *geo.Spacing) {
	switch shapeType {
	case PAGE_TYPE:
		// Short pages already exclude the fold from their inner width.
		// On taller pages either a top or a right inset clears the fold.
		if contentHeight+padding.Top+padding.Bottom >= 3*pageCornerHeight && padding.Top < pageCornerHeight {
			padding.Right = max(padding.Right, pageCornerWidth)
		}
	case CLOUD_TYPE:
		// The ideal content rectangle touches the outline. The origins and
		// extents of the cloud and its children round independently on export.
		const roundingRoom = 2
		padding.Top, padding.Bottom = max(roundingRoom, padding.Top), max(roundingRoom, padding.Bottom)
		padding.Left, padding.Right = max(roundingRoom, padding.Left), max(roundingRoom, padding.Right)
	}
}
