package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeParallelogram struct {
	shapeSquare
}

func (s shapeParallelogram) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.InsideTopLeft |
			1<<label.InsideMiddleCenter |
			1<<label.InsideBottomRight |
			1<<label.OutsideTopRight |
			1<<label.OutsideBottomLeft
	case OK:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideTopCenter |
			1<<label.InsideBottomCenter |
			1<<label.OutsideBottomCenter
	case Unideal:
		return 1<<label.OutsideLeftMiddle |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightTop |
			1<<label.OutsideRightMiddle |
			1<<label.InsideTopRight |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight |
			1<<label.InsideBottomLeft
	case Bad:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightBottom |
			1<<label.OutsideBottomRight
	}
	return 0
}

func (s shapeParallelogram) SnapPointPercentages() [][]*geo.RelativePoint {
	width := s.GetBox().Width
	wedgeWidth := 26.0
	if width < wedgeWidth {
		wedgeWidth = width / 2.0
	}

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.25, 0),
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.5, 0),
			geo.NewRelativePoint(wedgeWidth/width+((width-wedgeWidth)/width)*0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint((wedgeWidth/2.0+wedgeWidth)/2.0/width, 0.25),
			geo.NewRelativePoint(wedgeWidth/2.0/width, 0.5),
			geo.NewRelativePoint(wedgeWidth/2.0/2.0/width, 0.75),
		},
		// Bottom
		{
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.25, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.5, 1),
			geo.NewRelativePoint(((width-wedgeWidth)/width)*0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint((width-wedgeWidth)/width+(wedgeWidth/2.0+wedgeWidth)/2.0/width, 0.25),
			geo.NewRelativePoint((width-wedgeWidth)/width+wedgeWidth/2.0/width, 0.5),
			geo.NewRelativePoint((width-wedgeWidth)/width+wedgeWidth/2.0/2.0/width, 0.75),
		},
	}
}
