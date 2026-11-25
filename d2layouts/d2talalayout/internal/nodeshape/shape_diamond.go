package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

type shapeDiamond struct {
	shape.Shape
}

func (s shapeDiamond) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1 << label.InsideMiddleCenter
	case OK:
		return 1<<label.OutsideTopCenter |
			1<<label.OutsideBottomCenter |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight |
			1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle |
			1<<label.InsideBottomCenter |
			1<<label.InsideTopCenter
	case Unideal:
		return 1<<label.InsideTopLeft |
			1<<label.InsideTopRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight
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

func (s shapeDiamond) SnapPointPercentages() [][]*geo.RelativePoint {
	return diamondSnapPoints
}

func (s shapeDiamond) PortIndices(orientation geo.Orientation) []int {
	switch orientation {
	case geo.Top:
		return []int{0}
	case geo.Left:
		return []int{1}
	case geo.Bottom:
		return []int{2}
	case geo.Right:
		return []int{3}
	case geo.TopLeft:
		return []int{0, 1}
	case geo.TopRight:
		return []int{0, 3}
	case geo.BottomLeft:
		return []int{2, 1}
	case geo.BottomRight:
		return []int{2, 3}
	default:
		return []int{}
	}
}

func (s shapeDiamond) MirroredPortIndices() map[int]int {
	return map[int]int{
		// Top and bottom
		0: 2,
		2: 0,
		// Left and right
		1: 3,
		3: 1,
	}
}

func (s shapeDiamond) CenterPortIndices() []int {
	return []int{0, 1, 2, 3}
}

func (s shapeDiamond) CenterPortIndex(o geo.Orientation) int {
	centerPorts := s.CenterPortIndices()
	var centerPortsIndex int
	switch o {
	case geo.Top:
		centerPortsIndex = 0
	case geo.Left:
		centerPortsIndex = 1
	case geo.Bottom:
		centerPortsIndex = 2
	case geo.Right:
		centerPortsIndex = 3
	default:
		return -1
	}

	return centerPorts[centerPortsIndex]
}

// diamondSnapPoints is shared by every call: callers only read it.
var diamondSnapPoints = [][]*geo.RelativePoint{
	// Top
	{
		geo.NewRelativePoint(0.5, 0),
	},
	// Left
	{
		geo.NewRelativePoint(0, 0.5),
	},
	// Bottom
	{
		geo.NewRelativePoint(0.5, 1),
	},
	// Right
	{
		geo.NewRelativePoint(1, 0.5),
	},
}
