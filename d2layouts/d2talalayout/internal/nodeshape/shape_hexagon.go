package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeHexagon struct {
	shapeSquare
}

func (s shapeHexagon) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideMiddleCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight |
			1<<label.InsideTopCenter |
			1<<label.InsideBottomCenter
	case Unideal:
		return 1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle |
			1<<label.InsideTopLeft |
			1<<label.InsideTopRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight
	case Bad:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideLeftTop |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightTop |
			1<<label.OutsideRightBottom |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight
	}
	return 0
}

func (s shapeHexagon) SnapPointPercentages() [][]*geo.RelativePoint {
	return hexagonSnapPoints
}

// hexagonSnapPoints is shared by every call: callers only read it.
var hexagonSnapPoints = [][]*geo.RelativePoint{
	// Top
	{
		geo.NewRelativePoint(0.25, 0),
		geo.NewRelativePoint(0.5, 0),
		geo.NewRelativePoint(0.75, 0),
	},
	// Left
	{
		geo.NewRelativePoint(0.125, 0.25),
		geo.NewRelativePoint(0, 0.5),
		geo.NewRelativePoint(0.125, 0.75),
	},
	// Bottom
	{
		geo.NewRelativePoint(0.25, 1),
		geo.NewRelativePoint(0.5, 1),
		geo.NewRelativePoint(0.75, 1),
	},
	// Right
	{
		geo.NewRelativePoint(0.875, 0.25),
		geo.NewRelativePoint(1, 0.5),
		geo.NewRelativePoint(0.875, 0.75),
	},
}
