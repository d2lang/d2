package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapeCircle struct {
	shapeSquare
}

func (s shapeCircle) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return circleLabelPreferencesGood
	case OK:
		return circleLabelPreferencesOK
	case Unideal:
		return circleLabelPreferencesUnideal
	case Bad:
		return circleLabelPreferencesBad
	}
	return circleLabelPreferencesFallback
}

var circleLabelPreferencesGood = map[label.Position]struct{}{
	label.OutsideTopCenter:    {},
	label.InsideMiddleCenter:  {},
	label.OutsideBottomCenter: {},
}

var circleLabelPreferencesOK = map[label.Position]struct{}{
	label.InsideBottomCenter: {},
	label.InsideTopCenter:    {},
	label.InsideMiddleLeft:   {},
	label.InsideMiddleRight:  {},
}

var circleLabelPreferencesUnideal = map[label.Position]struct{}{
	label.InsideTopLeft:      {},
	label.InsideTopRight:     {},
	label.InsideBottomLeft:   {},
	label.InsideBottomRight:  {},
	label.OutsideLeftMiddle:  {},
	label.OutsideRightMiddle: {},
}

var circleLabelPreferencesBad = map[label.Position]struct{}{
	label.OutsideTopLeft:     {},
	label.OutsideTopRight:    {},
	label.OutsideLeftTop:     {},
	label.OutsideRightTop:    {},
	label.OutsideBottomLeft:  {},
	label.OutsideBottomRight: {},
	label.OutsideLeftBottom:  {},
	label.OutsideRightBottom: {},
}

var circleLabelPreferencesFallback = map[label.Position]struct{}{}
