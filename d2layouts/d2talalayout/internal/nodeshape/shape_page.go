package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapePage struct {
	shapeSquare
}

func (s shapePage) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return pageLabelPreferencesGood
	case OK:
		return pageLabelPreferencesOK
	case Unideal:
		return pageLabelPreferencesUnideal
	case Bad:
		return pageLabelPreferencesBad
	}
	return pageLabelPreferencesFallback
}

var pageLabelPreferencesGood = map[label.Position]struct{}{
	label.InsideMiddleCenter:  {},
	label.InsideBottomCenter:  {},
	label.OutsideBottomCenter: {},
}

var pageLabelPreferencesOK = map[label.Position]struct{}{
	label.InsideTopLeft:      {},
	label.InsideMiddleLeft:   {},
	label.InsideMiddleRight:  {},
	label.InsideBottomLeft:   {},
	label.InsideBottomRight:  {},
	label.OutsideTopLeft:     {},
	label.OutsideTopCenter:   {},
	label.OutsideBottomLeft:  {},
	label.OutsideBottomRight: {},
}

var pageLabelPreferencesUnideal = map[label.Position]struct{}{
	label.OutsideTopRight:    {},
	label.InsideTopCenter:    {},
	label.OutsideRightBottom: {},
	label.OutsideRightMiddle: {},
	label.OutsideLeftTop:     {},
	label.OutsideLeftMiddle:  {},
	label.OutsideLeftBottom:  {},
}

var pageLabelPreferencesBad = map[label.Position]struct{}{
	label.InsideTopRight:  {},
	label.OutsideRightTop: {},
}

var pageLabelPreferencesFallback = map[label.Position]struct{}{}
