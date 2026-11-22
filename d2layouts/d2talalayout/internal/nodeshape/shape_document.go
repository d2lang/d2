package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeDocument struct {
	shapeSquare
}

func (s shapeDocument) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return documentLabelPreferencesGood
	case OK:
		return documentLabelPreferencesOK
	case Unideal:
		return documentLabelPreferencesUnideal
	case Bad:
		return documentLabelPreferencesBad
	}
	return documentLabelPreferencesFallback
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

var documentLabelPreferencesGood = map[label.Position]struct{}{
	label.InsideTopLeft:      {},
	label.InsideTopCenter:    {},
	label.InsideTopRight:     {},
	label.InsideMiddleCenter: {},
}

var documentLabelPreferencesOK = map[label.Position]struct{}{
	label.OutsideTopLeft:    {},
	label.OutsideTopCenter:  {},
	label.OutsideTopRight:   {},
	label.InsideMiddleLeft:  {},
	label.InsideMiddleRight: {},
}

var documentLabelPreferencesUnideal = map[label.Position]struct{}{
	label.OutsideLeftTop:     {},
	label.OutsideLeftMiddle:  {},
	label.OutsideLeftBottom:  {},
	label.OutsideRightTop:    {},
	label.OutsideRightMiddle: {},
	label.OutsideRightBottom: {},
	label.InsideBottomCenter: {},
	label.InsideBottomLeft:   {},
	label.InsideBottomRight:  {},
}

var documentLabelPreferencesBad = map[label.Position]struct{}{
	label.OutsideBottomLeft:   {},
	label.OutsideBottomCenter: {},
	label.OutsideBottomRight:  {},
}

var documentLabelPreferencesFallback = map[label.Position]struct{}{}
