package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

type shapeCloud struct {
	shape.Shape
}

func (s shapeCloud) LabelPositionPreferences(preferenceSet LabelTier) LabelSet {
	switch preferenceSet {
	case Good:
		return 1<<label.InsideMiddleCenter |
			1<<label.InsideBottomCenter |
			1<<label.OutsideBottomCenter |
			1<<label.InsideTopCenter
	case OK:
		return 1<<label.OutsideTopCenter |
			1<<label.OutsideBottomLeft |
			1<<label.OutsideBottomRight |
			1<<label.InsideTopLeft |
			1<<label.InsideTopRight |
			1<<label.InsideBottomLeft |
			1<<label.InsideBottomRight |
			1<<label.InsideMiddleLeft |
			1<<label.InsideMiddleRight
	case Unideal:
		return 1<<label.OutsideLeftMiddle |
			1<<label.OutsideRightMiddle |
			1<<label.OutsideLeftBottom |
			1<<label.OutsideRightBottom
	case Bad:
		return 1<<label.OutsideTopLeft |
			1<<label.OutsideTopRight |
			1<<label.OutsideLeftTop |
			1<<label.OutsideRightTop
	}
	return 0
}

func (s shapeCloud) SnapPointPercentages() [][]*geo.RelativePoint {
	return cloudSnapPoints
}

func (s shapeCloud) PortIndices(orientation geo.Orientation) []int {
	switch orientation {
	case geo.Top:
		return []int{0, 1, 2}
	case geo.Left:
		return []int{3, 4}
	case geo.Bottom:
		return []int{5, 6, 7}
	case geo.Right:
		return []int{8, 9}
	case geo.TopLeft:
		return []int{0, 1, 2, 3, 4}
	case geo.TopRight:
		return []int{0, 1, 2, 8, 9}
	case geo.BottomLeft:
		return []int{5, 6, 7, 3, 4}
	case geo.BottomRight:
		return []int{5, 6, 7, 8, 9}
	default:
		return []int{}
	}
}

func (s shapeCloud) MirroredPortIndices() map[int]int {
	// Left and right
	return map[int]int{
		3: 8,
		8: 3,
	}
}

func (s shapeCloud) CenterPortIndices() []int {
	return []int{1, 3, 6, 8}
}

func (s shapeCloud) CenterPortIndex(o geo.Orientation) int {
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

// cloudSnapPoints is shared by every call: callers only read it.
var cloudSnapPoints = [][]*geo.RelativePoint{
	// Top
	{
		geo.NewRelativePoint(0.16, 0.368),
		geo.NewRelativePoint(0.378, 0.155),
		geo.NewRelativePoint(0.815, 0.328),
	},
	// Left
	{
		geo.NewRelativePoint(0, 0.7),
		geo.NewRelativePoint(0.066, 0.935),
	},
	// Bottom
	{
		geo.NewRelativePoint(0.25, 1),
		geo.NewRelativePoint(0.5, 1),
		geo.NewRelativePoint(0.75, 1),
	},
	// Right
	{
		geo.NewRelativePoint(1, 0.7),
		geo.NewRelativePoint(0.95, 0.89),
	},
}
