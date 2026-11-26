package nodeshape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

// NumColumns returns the number of row-specific side ports for a table shape.
// Non-table shapes do not carry this metadata.
func NumColumns(s *Shape) int {
	return s.numColumns
}

// SetNumColumns updates the row-specific side ports for a table shape.
// It is a no-op for every other shape.
func SetNumColumns(s *Shape, numColumns int) {
	if s.kind == Table {
		s.numColumns = numColumns
	}
}

// TablePortIndex returns the absolute snap-point index for a table column on
// the requested side. Non-table shapes and non-side orientations return
// ok=false so callers can use their generic port policy.
func TablePortIndex(s *Shape, orientation geo.Orientation, columnIndex int) (index int, ok bool) {
	if s.kind != Table {
		return 0, false
	}
	numColumns := s.numColumns
	if numColumns == 0 {
		numColumns = 1
	}
	if columnIndex < 0 || columnIndex >= numColumns {
		panic("table column port index out of range")
	}
	switch orientation {
	case geo.Left:
		return 3 + columnIndex, true
	case geo.Right:
		return 6 + numColumns + columnIndex, true
	default:
		return 0, false
	}
}

// TableColumnPortValue returns the table column's concrete side port without
// materializing all of the shape's snap points. Non-table shapes and non-side
// orientations return ok=false so callers can use their generic port policy.
func TableColumnPortValue(s *Shape, orientation geo.Orientation, columnIndex int) (port geo.Point, ok bool) {
	if s.kind != Table || (orientation != geo.Left && orientation != geo.Right) {
		return geo.Point{}, false
	}
	numColumns := s.numColumns
	if numColumns == 0 {
		if columnIndex != 0 {
			panic("table column port index out of range")
		}
	} else if columnIndex < 0 || columnIndex >= numColumns {
		panic("table column port index out of range")
	}

	yPercentage := 0.5
	if numColumns > 0 {
		rowHeightPercentage := 1. / (float64(numColumns) + 1.)
		percentage := rowHeightPercentage + rowHeightPercentage/2.
		for range columnIndex {
			percentage += rowHeightPercentage
		}
		percentage = math.Round(percentage*10_000) / 10_000
		yPercentage = geo.TruncateDecimals(percentage)
	}
	xPercentage := 0.0
	if orientation == geo.Right {
		xPercentage = 1.0
	}
	box := s.GetBox()
	return geo.Point{
		X: box.TopLeft.X + math.Round(box.Width*xPercentage),
		Y: box.TopLeft.Y + math.Round(box.Height*yPercentage),
	}, true
}

// tableSnapPoints returns the relative port positions of a table. Row ports exclude the
// table header and sit at each row's center.
// Below, `*` represents the ports.
// ┌──*────*────*──┐
// │  HEADER       │
// ├───────────────┤
// *  COLUMN 1     *
// ├───────────────┤
// *  COLUMN 2     *
// ├───────────────┤
// *  COLUMN 3     *
// └──*────*────*──┘
func tableSnapPoints(numColumns int) [][]*geo.RelativePoint {
	var left, right []*geo.RelativePoint
	if numColumns == 0 {
		left = []*geo.RelativePoint{geo.NewRelativePoint(0., 0.5)}
		right = []*geo.RelativePoint{geo.NewRelativePoint(1., 0.5)}
	} else {
		left = make([]*geo.RelativePoint, 0, numColumns+1)
		right = make([]*geo.RelativePoint, 0, numColumns+1)
		// *row* is used here instead of *column* because during rendering of this shape, columns are drawn in rows
		rowHeightPercentage := 1. / (float64(numColumns) + 1.) // +1 because of the table header
		midRowPercentage := rowHeightPercentage / 2.
		// start in rowHeightPercentage + midRowPercentage to skip the header
		for perc := rowHeightPercentage + midRowPercentage; perc < 1.; perc += rowHeightPercentage {
			h := math.Round(perc*10_000) / 10_000 // clips decimal places to avoid numerical issues
			left = append(left, geo.NewRelativePoint(0., h))
			right = append(right, geo.NewRelativePoint(1., h))
		}
	}
	return [][]*geo.RelativePoint{
		// Top
		{
			geo.NewRelativePoint(0.25, 0),
			geo.NewRelativePoint(0.5, 0),
			geo.NewRelativePoint(0.75, 0),
		},
		left,
		// Bottom
		{
			geo.NewRelativePoint(0.25, 1),
			geo.NewRelativePoint(0.5, 1),
			geo.NewRelativePoint(0.75, 1),
		},
		right,
	}
}
