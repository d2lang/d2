package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapeImage struct {
	shapeSquare
}

func (s shapeImage) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideBottomCenter |
			1<<label.OutsideTopCenter
	case OK:
		return 1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle
	case Unideal:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom
	}
	return 0
}
