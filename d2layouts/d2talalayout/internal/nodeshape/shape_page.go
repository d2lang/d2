package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapePage struct {
	shapeSquare
}

func (s shapePage) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.InsideMiddleCenter |
			1<<label.InsideBottomCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.InsideTopLeft |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight |
			1<<label.OutsideTopLeft |
			1<<label.OutsideTopCenter |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight
	case Unideal:
		return 1<<label.OutsideTopRight |
			1<<label.InsideTopCenter |
			1<<label.OutsideRightBottom |
			1<<label.OutsideRightMiddle |
			1<<label.OutsideLeftTop |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideLeftBottom
	case Bad:
		return 1<<label.InsideTopRight |
			1<<label.OutsideRightTop
	}
	return 0
}
