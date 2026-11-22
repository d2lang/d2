package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeQueue struct {
	shapeSquare
}

func (s shapeQueue) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return queueLabelPreferencesGood
	case OK:
		return queueLabelPreferencesOK
	case Unideal:
		return queueLabelPreferencesUnideal
	case Bad:
		return queueLabelPreferencesBad
	}
	return queueLabelPreferencesFallback
}

func (s shapeQueue) SnapPointPercentages() [][]*geo.RelativePoint {
	box := s.GetBox()
	width := box.Width
	height := box.Height
	arcDepth := 24.0
	controlPointsMultiplier := 0.45

	if width < arcDepth*2 {
		arcDepth = width / 2.0
	}

	topLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(arcDepth, 0),
		geo.NewPoint(0, 0),
		geo.NewPoint(0, height*controlPointsMultiplier),
		geo.NewPoint(0, height/2.0),
	}).At(0.5)
	topRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width-arcDepth, 0),
		geo.NewPoint(width, 0),
		geo.NewPoint(width, height*controlPointsMultiplier),
		geo.NewPoint(width, height/2.0),
	}).At(0.5)
	bottomRightMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(width, height/2.0),
		geo.NewPoint(width, height-height*controlPointsMultiplier),
		geo.NewPoint(width, height),
		geo.NewPoint(width-arcDepth, height),
	}).At(0.5)
	bottomLeftMidpoint := geo.NewBezierCurve([]*geo.Point{
		geo.NewPoint(0, height/2.0),
		geo.NewPoint(0, height-height*controlPointsMultiplier),
		geo.NewPoint(0, height),
		geo.NewPoint(arcDepth, height),
	}).At(0.5)

	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.25, 0),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.5, 0),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.75, 0),
		},
		// Left
		{
			geo.NewRelativePoint(topLeftMidpoint.X/width, topLeftMidpoint.Y/height),
			geo.NewRelativePoint(0, 0.5),
			geo.NewRelativePoint(bottomLeftMidpoint.X/width, bottomLeftMidpoint.Y/height),
		},
		// Bottom
		{
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.25, 1),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.5, 1),
			geo.NewRelativePoint(arcDepth/width+((width-arcDepth*2)/width)*0.75, 1),
		},
		// Right
		{
			geo.NewRelativePoint(topRightMidpoint.X/width, topRightMidpoint.Y/height),
			geo.NewRelativePoint(1, 0.5),
			geo.NewRelativePoint(bottomRightMidpoint.X/width, bottomRightMidpoint.Y/height),
		},
	}
}

var queueLabelPreferencesGood = map[label.Position]struct{}{
	label.OutsideTopCenter:    {},
	label.InsideTopCenter:     {},
	label.InsideMiddleCenter:  {},
	label.InsideBottomCenter:  {},
	label.OutsideBottomCenter: {},
}

var queueLabelPreferencesOK = map[label.Position]struct{}{
	label.InsideTopLeft:     {},
	label.InsideBottomLeft:  {},
	label.InsideMiddleRight: {},
}

var queueLabelPreferencesUnideal = map[label.Position]struct{}{
	label.InsideTopRight:     {},
	label.InsideBottomRight:  {},
	label.OutsideLeftMiddle:  {},
	label.OutsideRightMiddle: {},
	label.InsideMiddleLeft:   {},
}

var queueLabelPreferencesBad = map[label.Position]struct{}{
	label.OutsideTopLeft:     {},
	label.OutsideTopRight:    {},
	label.OutsideBottomLeft:  {},
	label.OutsideBottomRight: {},
	label.OutsideLeftTop:     {},
	label.OutsideRightTop:    {},
	label.OutsideLeftBottom:  {},
	label.OutsideRightBottom: {},
}

var queueLabelPreferencesFallback = map[label.Position]struct{}{}
