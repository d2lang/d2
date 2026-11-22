package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapeImage struct {
	shapeSquare
}

func (s shapeImage) LabelPositionPreferences(preferenceSet LabelTier) map[label.Position]struct{} {
	switch preferenceSet {
	case Good:
		return imageLabelPreferencesGood
	case OK:
		return imageLabelPreferencesOK
	case Unideal:
		return imageLabelPreferencesUnideal
	case Bad:
		return imageLabelPreferencesBad
	}
	return imageLabelPreferencesFallback
}

var imageLabelPreferencesGood = map[label.Position]struct{}{
	label.OutsideBottomCenter: {},
	label.OutsideTopCenter:    {},
}

var imageLabelPreferencesOK = map[label.Position]struct{}{
	label.OutsideLeftMiddle:  {},
	label.OutsideRightMiddle: {},
}

var imageLabelPreferencesUnideal = map[label.Position]struct{}{
	label.OutsideTopLeft:     {},
	label.OutsideTopRight:    {},
	label.OutsideBottomLeft:  {},
	label.OutsideBottomRight: {},
	label.OutsideLeftTop:     {},
	label.OutsideRightTop:    {},
	label.OutsideLeftBottom:  {},
	label.OutsideRightBottom: {},
}

var imageLabelPreferencesBad = map[label.Position]struct{}{}

var imageLabelPreferencesFallback = map[label.Position]struct{}{}
