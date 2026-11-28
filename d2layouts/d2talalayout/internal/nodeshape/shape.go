// Package nodeshape defines the shape-specific port and label-placement policy
// used by TALA nodes.
package nodeshape

import (
	"slices"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// LabelTier groups label positions by how suitable they are for a shape.
type LabelTier int

// Label preference tiers run from Good to Bad.
const (
	Good LabelTier = iota
	OK
	Unideal
	Bad
)

// Kind identifies the TALA behavior associated with a node shape.
type Kind uint8

// Shape kinds identify the policy of the shapes returned by New.
const (
	Square Kind = iota
	RealSquare
	Parallelogram
	Document
	Cylinder
	Queue
	Page
	Package
	Step
	Callout
	StoredData
	Person
	C4Person
	Diamond
	Oval
	Circle
	Hexagon
	Cloud
	Table
	Class
	Text
	Code
	Image
)

// kindTypes holds the D2 shape type of each kind.
var kindTypes = [...]string{
	Square:        shape.SQUARE_TYPE,
	RealSquare:    shape.REAL_SQUARE_TYPE,
	Parallelogram: shape.PARALLELOGRAM_TYPE,
	Document:      shape.DOCUMENT_TYPE,
	Cylinder:      shape.CYLINDER_TYPE,
	Queue:         shape.QUEUE_TYPE,
	Page:          shape.PAGE_TYPE,
	Package:       shape.PACKAGE_TYPE,
	Step:          shape.STEP_TYPE,
	Callout:       shape.CALLOUT_TYPE,
	StoredData:    shape.STORED_DATA_TYPE,
	Person:        shape.PERSON_TYPE,
	C4Person:      shape.C4_PERSON_TYPE,
	Diamond:       shape.DIAMOND_TYPE,
	Oval:          shape.OVAL_TYPE,
	Circle:        shape.CIRCLE_TYPE,
	Hexagon:       shape.HEXAGON_TYPE,
	Cloud:         shape.CLOUD_TYPE,
	Table:         shape.TABLE_TYPE,
	Class:         shape.CLASS_TYPE,
	Text:          shape.TEXT_TYPE,
	Code:          shape.CODE_TYPE,
	Image:         shape.IMAGE_TYPE,
}

// String returns the D2 shape type represented by kind.
func (kind Kind) String() string {
	if int(kind) < len(kindTypes) {
		return kindTypes[kind]
	}
	return ""
}

// LabelSet is a set of label positions: position p is a member when bit p is
// set.
type LabelSet uint64

// Has reports whether position is in the set.
func (s LabelSet) Has(position label.Position) bool {
	return position >= 0 && position < 64 && s&(1<<position) != 0
}

// Shape is D2's geometric shape with the ports and label-position policy
// needed by TALA. Its D2 shape methods are those of the embedded shape.
type Shape struct {
	shape.Shape
	kind Kind
	// numColumns is the number of rows of a table with row ports, and 0 for
	// every other shape.
	numColumns int
}

// New constructs TALA's shape around a D2 shape. An empty type uses square
// behavior while retaining the empty D2 type. Unsupported nonempty types
// return ok=false so callers can preserve their existing shape.
func New(typeName string, box *geo.Box) (*Shape, Kind, bool) {
	kind := Square
	if typeName != "" {
		i := slices.Index(kindTypes[:], typeName)
		if i < 0 {
			return nil, 0, false
		}
		kind = Kind(i)
	}
	return &Shape{Shape: shape.NewShape(typeName, box), kind: kind}, kind, true
}

// GetLabelBox delegates to the embedded D2 shape's inside label placement box.
func (s *Shape) GetLabelBox(position label.Position, padding, width, height float64) *geo.Box {
	return shape.GetInsideLabelBox(s.Shape, position, padding, width, height)
}

// LabelPositionPreferences returns the positions in a preference tier.
func (s *Shape) LabelPositionPreferences(tier LabelTier) LabelSet {
	if tier < Good || tier > Bad {
		return 0
	}
	return policies[s.kind].labels[tier]
}

// SnapPointPercentages returns port groups in top, left, bottom, and right order.
// Shapes whose ports do not depend on their box return one shared value:
// callers must not modify the result.
func (s *Shape) SnapPointPercentages() [][]*geo.RelativePoint {
	if points := policies[s.kind].points; points != nil {
		return points
	}
	box := s.GetBox()
	switch s.kind {
	case Parallelogram:
		return parallelogramSnapPoints(box.Width)
	case Document:
		return documentSnapPoints(box.Width, box.Height)
	case Cylinder:
		return cylinderSnapPoints(box.Width, box.Height)
	case Queue:
		return queueSnapPoints(box.Width, box.Height)
	case Package:
		return packageSnapPoints(box.Width, box.Height)
	case Step:
		return stepSnapPoints(box.Width)
	case Callout:
		return calloutSnapPoints(box.Width, box.Height)
	case StoredData:
		return storedDataSnapPoints(box.Width, box.Height)
	default: // Table
		return tableSnapPoints(s.numColumns)
	}
}

// sidePorts returns the number of ports on each side, in top, left, bottom,
// and right order.
func (s *Shape) sidePorts() [4]int {
	sides := policies[s.kind].ports.sides
	if s.numColumns > 0 {
		// A table has one side port per row.
		sides[1], sides[3] = s.numColumns, s.numColumns
	}
	return sides
}

// orientationSides lists the sides whose ports serve each orientation.
var orientationSides = [...][]int{
	geo.TopLeft:     {0, 1},
	geo.TopRight:    {0, 3},
	geo.BottomLeft:  {2, 1},
	geo.BottomRight: {2, 3},
	geo.Top:         {0},
	geo.Right:       {3},
	geo.Bottom:      {2},
	geo.Left:        {1},
}

// PortIndices returns the ports available for an orientation.
func (s *Shape) PortIndices(orientation geo.Orientation) []int {
	indices := []int{}
	if orientation < 0 || int(orientation) >= len(orientationSides) {
		return indices
	}
	sides := s.sidePorts()
	for _, side := range orientationSides[orientation] {
		first := 0
		for _, n := range sides[:side] {
			first += n
		}
		for i := range sides[side] {
			indices = append(indices, first+i)
		}
	}
	return indices
}

// CenterPortIndices returns the center port on each side.
func (s *Shape) CenterPortIndices() []int {
	if s.numColumns > 0 {
		return nil
	}
	return append([]int(nil), policies[s.kind].ports.centers...)
}

// CenterPortIndex returns the center port for an orientation, or -1 if the
// shape does not expose one.
func (s *Shape) CenterPortIndex(orientation geo.Orientation) int {
	centers := policies[s.kind].ports.centers
	if s.numColumns > 0 || orientation < geo.Top || orientation > geo.Left {
		return -1
	}
	// The center ports of the four sides come last.
	return centers[len(centers)-4+orientationSides[orientation][0]]
}

// MirroredPortIndices maps each port index to its reflected counterpart.
func (s *Shape) MirroredPortIndices() map[int]int {
	if s.numColumns > 0 {
		return nil
	}
	pairs := policies[s.kind].ports.mirrored
	mirrored := make(map[int]int, len(pairs))
	for i := 0; i < len(pairs); i += 2 {
		mirrored[pairs[i]] = pairs[i+1]
		mirrored[pairs[i+1]] = pairs[i]
	}
	return mirrored
}
