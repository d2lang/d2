package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeOval struct {
	shapeSquare
}

func (s shapeOval) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideMiddleCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.InsideBottomCenter |
			1<<label.InsideTopCenter |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight
	case Unideal:
		return 1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle
	case Bad:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom |
			1<<label.InsideTopLeft |
			1<<label.InsideTopRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight
	}
	return 0
}

func (s shapeOval) SnapPointPercentages() [][]*geo.RelativePoint {
	return ovalSnapPoints
}

// ovalSnapPoints is shared by every call: callers only read it.
var ovalSnapPoints = [][]*geo.RelativePoint{
	// Top
	{
		geo.NewRelativePoint(0.25, 0.066),
		geo.NewRelativePoint(0.5, 0),
		geo.NewRelativePoint(0.75, 0.066),
	},
	// Left
	{
		geo.NewRelativePoint(0.066, 0.25),
		geo.NewRelativePoint(0, 0.5),
		geo.NewRelativePoint(0.066, 0.75),
	},
	// Bottom
	{
		geo.NewRelativePoint(0.25, 0.934),
		geo.NewRelativePoint(0.5, 1),
		geo.NewRelativePoint(0.75, 0.934),
	},
	// Right
	{
		geo.NewRelativePoint(0.934, 0.25),
		geo.NewRelativePoint(1, 0.5),
		geo.NewRelativePoint(0.934, 0.75),
	},
}
