package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeCylinder struct {
	shapeSquare
}

func (s shapeCylinder) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideMiddleCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.InsideTopCenter |
			1<<label.InsideBottomCenter |
			1<<label.InsideMiddleLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideBottomRight |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle |
			1<<label.OutsideTopLeft |
			1<<label.OutsideBottomLeft |
			1<<label.InsideTopLeft |
			1<<label.InsideBottomLeft |
			1<<label.InsideMiddleRight |
			1<<label.InsideTopRight |
			1<<label.InsideBottomRight
	case Unideal:
		return 1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom
	}
	return 0
}

func (s shapeCylinder) SnapPointPercentages() [][]*geo.RelativePoint {
	box := s.GetBox()
	width := box.Width
	height := box.Height
	arcDepth := 24.0
	controlPointsMultiplier := 0.45

	if height < arcDepth*2 {
		arcDepth = height / 2.0
	}

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, arcDepth),
		geo.NewPoint(0, 0),
		geo.NewPoint(width*controlPointsMultiplier, 0),
		geo.NewPoint(width/2.0, 0),
	}).At(0.5)
	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2.0, 0),
		geo.NewPoint(width-(width*controlPointsMultiplier), 0),
		geo.NewPoint(width, 0),
		geo.NewPoint(width, arcDepth),
	}).At(0.5)
	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, height-arcDepth),
		geo.NewPoint(width, height),
		geo.NewPoint(width-width*controlPointsMultiplier, height),
		geo.NewPoint(width/2.0, height),
	}).At(0.5)
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2.0, height),
		geo.NewPoint(width*controlPointsMultiplier, height),
		geo.NewPoint(0, height),
		geo.NewPoint(0, height-arcDepth),
	}).At(0.5)

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
		},
		// Left
		{
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.25),
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.5),
			geo.NewRelativePoint(0, arcDepth/height+((height-arcDepth*2)/height)*0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
			geo.NewRelativePoint(0.5, 1),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
		// Right
		{
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.25),
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.5),
			geo.NewRelativePoint(1, arcDepth/height+((height-arcDepth*2)/height)*0.75),
		},
	}
}
