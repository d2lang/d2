package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

type shapeOval struct {
	shapeSquare
}

func (s shapeOval) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return ovalLabelPreferencesGood
	case OK:
		return ovalLabelPreferencesOK
	case Unideal:
		return ovalLabelPreferencesUnideal
	case Bad:
		return ovalLabelPreferencesBad
	}
	return ovalLabelPreferencesFallback
}

func (s shapeOval) SnapPointPercentages() [][]*geo.RelativePoint {
	return ovalSnapPoints
}

var ovalLabelPreferencesGood = map[label.Position]struct{}{
	label.OutsideTopCenter:    {},
	label.InsideMiddleCenter:  {},
	label.OutsideBottomCenter: {},
}

var ovalLabelPreferencesOK = map[label.Position]struct{}{
	label.InsideBottomCenter: {},
	label.InsideTopCenter:    {},
	label.InsideMiddleLeft:   {},
	label.InsideMiddleRight:  {},
}

var ovalLabelPreferencesUnideal = map[label.Position]struct{}{
	label.OutsideLeftMiddle:  {},
	label.OutsideRightMiddle: {},
}

var ovalLabelPreferencesBad = map[label.Position]struct{}{
	label.OutsideTopLeft:     {},
	label.OutsideTopRight:    {},
	label.OutsideLeftTop:     {},
	label.OutsideRightTop:    {},
	label.OutsideBottomLeft:  {},
	label.OutsideBottomRight: {},
	label.OutsideLeftBottom:  {},
	label.OutsideRightBottom: {},
	label.InsideTopLeft:      {},
	label.InsideTopRight:     {},
	label.InsideBottomLeft:   {},
	label.InsideBottomRight:  {},
}

var ovalLabelPreferencesFallback = map[label.Position]struct{}{}

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
