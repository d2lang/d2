package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeDocument struct {
	shapeSquare
}

func (s shapeDocument) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.InsideTopLeft |
			1<<label.InsideTopCenter |
			1<<label.InsideTopRight |
			1<<label.InsideMiddleCenter
	case OK:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopCenter |
			1<<label.OutsideTopRight |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight
	case Unideal:
		return 1<<label.OutsideLeftTop |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightTop |
			1<<label.OutsideRightMiddle |
			1<<label.OutsideRightBottom |
			1<<label.InsideBottomCenter |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight
	case Bad:
		return 1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomCenter |
			1<<label.OutsideBottomRight
	}
	return 0
}

func (s shapeDocument) SnapPointPercentages() [][]*geo.RelativePoint {
	box := s.GetBox()
	width := box.Width
	height := box.Height
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, height*(16.3/18.925)),
		geo.NewPoint(width/6, height*(19.8/18.925)),
		geo.NewPoint(width/3, height*(19.8/18.925)),
		geo.NewPoint(width/2, height*(16.3/18.925)),
	}).At(0.5)
	bottomRightCurve := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width/2, height*(16.3/18.925)),
		geo.NewPoint(width*2/3, height*(12.8/18.925)),
		geo.NewPoint(width*5/6, height*(12.8/18.925)),
		geo.NewPoint(width, height*(16.3/18.925)),
	})
	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(0.25, 0),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(0, 0.25),
			geo.NewRelativePoint(0, 0.5),
			geo.NewRelativePoint(0, 0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
			geo.NewRelativePoint(bottomRightCurve.At(0).X/width, bottomRightCurve.At(0).Y/height),
			geo.NewRelativePoint(bottomRightCurve.At(0.5).X/width, bottomRightCurve.At(0.5).Y/height),
		},
		// Right
		{
			geo.NewRelativePoint(1, 0.25),
			geo.NewRelativePoint(1, 0.5),
			geo.NewRelativePoint(1, 0.75),
		},
	}
}
