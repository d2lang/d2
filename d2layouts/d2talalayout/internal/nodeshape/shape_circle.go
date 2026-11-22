package nodeshape

import (
	"github.com/d2lang/d2/lib/label"
)

type shapeCircle struct {
	shapeSquare
}

func (s shapeCircle) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.OutsideTopCenter |
			1<<label.InsideMiddleCenter |
			1<<label.OutsideBottomCenter
	case OK:
		return 1<<label.InsideBottomCenter |
			1<<label.InsideTopCenter |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight
	case Unideal:
		return 1<<label.InsideTopLeft |
			1<<label.InsideTopRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle
	case Bad:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom
	}
	return 0
}
