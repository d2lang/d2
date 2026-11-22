package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeStoredData struct {
	shapeSquare
}

func (s shapeStoredData) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideTopCenter |
			1<<label.InsideMiddleCenter |
			1<<label.InsideBottomCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.OutsideTopRight |
			1<<label.OutsideBottomRight |
			1<<label.InsideTopRight |
			1<<label.InsideBottomRight |
			1<<label.InsideMiddleRight |
			1<<label.InsideTopLeft |
			1<<label.InsideBottomLeft
	case Unideal:
		return 1<<label.InsideMiddleLeft |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideTopLeft |
			1<<label.OutsideBottomLeft
	case Bad:
		return 1<<label.OutsideRightMiddle |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom
	}
	return 0
}

func (s shapeStoredData) SnapPointPercentages() [][]*geo.RelativePoint {
	box := s.GetBox()
	width := box.Width
	height := box.Height
	wedgeWidth := 15.0
	if width < wedgeWidth {
		wedgeWidth = width / 2
	}
	controlPointsMultiplier := 0.27

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(wedgeWidth, 0.0),
		geo.NewPoint(wedgeWidth-(wedgeWidth*controlPointsMultiplier), 0.0),
		geo.NewPoint(0.0, height*controlPointsMultiplier),
		geo.NewPoint(0.0, height/2.0),
	}).At(0.5)

	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(wedgeWidth, height),
		geo.NewPoint(wedgeWidth-(wedgeWidth*controlPointsMultiplier), height),
		geo.NewPoint(0.0, height-height*controlPointsMultiplier),
		geo.NewPoint(0.0, height/2.0),
	}).At(0.5)

	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, 0),
		geo.NewPoint(width-(wedgeWidth*controlPointsMultiplier), 0),
		geo.NewPoint(width-wedgeWidth, height*controlPointsMultiplier),
		geo.NewPoint(width-wedgeWidth, height/2.0),
	}).At(0.5)

	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width-wedgeWidth, height/2.0),
		geo.NewPoint(width-wedgeWidth, height-height*controlPointsMultiplier),
		geo.NewPoint(width-wedgeWidth*controlPointsMultiplier, height),
		geo.NewPoint(width, height),
	}).At(0.5)

	sideWidthPercentage := (width - wedgeWidth) / width
	sideWidthStartPercentage := 1 - sideWidthPercentage

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(sideWidthStartPercentage+(0.25*sideWidthPercentage), 0),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.5*sideWidthPercentage), 0),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.75*sideWidthPercentage), 0),
		},
		// Left
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.0, 0.5),
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
		},
		// Bottom
		{
			geo.NewRelativePoint(sideWidthStartPercentage+(0.25*sideWidthPercentage), 1),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.5*sideWidthPercentage), 1),
			geo.NewRelativePoint(sideWidthStartPercentage+(0.75*sideWidthPercentage), 1),
		},
		// Right
		{
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
			geo.NewRelativePoint((width-wedgeWidth)/width, 0.5),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
	}
}
