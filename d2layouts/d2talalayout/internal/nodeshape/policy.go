package nodeshape

import (
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// policy is what TALA knows about a kind of shape.
type policy struct {
	// labels holds the label positions of each preference tier.
	labels [Bad + 1]LabelSet
	ports  *portLayout
	// points holds snap points shared by every shape of the kind, or nil when
	// they depend on the box (see SnapPointPercentages).
	points [][]*geo.RelativePoint
}

// portLayout numbers a shape's ports side by side, in top, left, bottom, and
// right order.
type portLayout struct {
	// sides holds the number of ports on each side.
	sides [4]int
	// centers lists the center ports; its last four are those of the top,
	// left, bottom, and right sides.
	centers []int
	// mirrored lists pairs of ports that reflect onto each other.
	mirrored []int
}

var policies = [...]policy{
	Square:        {labels: squareLabels, ports: &squarePorts, points: squareSnapPoints},
	RealSquare:    {labels: squareLabels, ports: &squarePorts, points: squareSnapPoints},
	Class:         {labels: squareLabels, ports: &squarePorts, points: squareSnapPoints},
	Text:          {labels: squareLabels, ports: &squarePorts, points: squareSnapPoints},
	Code:          {labels: squareLabels, ports: &squarePorts, points: squareSnapPoints},
	Table:         {labels: squareLabels, ports: &tablePorts},
	Circle:        {labels: roundLabels, ports: &squarePorts, points: squareSnapPoints},
	Hexagon:       {labels: roundLabels, ports: &squarePorts, points: hexagonSnapPoints},
	Oval:          {labels: ovalLabels, ports: &squarePorts, points: ovalSnapPoints},
	Page:          {labels: pageLabels, ports: &squarePorts, points: squareSnapPoints},
	Image:         {labels: imageLabels, ports: &squarePorts, points: squareSnapPoints},
	Parallelogram: {labels: parallelogramLabels, ports: &squarePorts},
	Document:      {labels: documentLabels, ports: &squarePorts},
	Cylinder:      {labels: cylinderLabels, ports: &squarePorts},
	Queue:         {labels: queueLabels, ports: &squarePorts},
	StoredData:    {labels: storedDataLabels, ports: &squarePorts},
	Package: {
		labels: packageLabels,
		// Port 0 is also a top center port, but 1 is closer to the center.
		ports: &portLayout{sides: [4]int{3, 3, 3, 3}, centers: []int{0, 1, 4, 7, 10}, mirrored: []int{4, 10}},
	},
	Step: {
		labels: stepLabels,
		ports:  &portLayout{sides: [4]int{3, 3, 3, 1}, centers: []int{1, 4, 7, 9}, mirrored: []int{1, 7, 4, 9}},
	},
	Callout: {
		labels: calloutLabels,
		ports:  &portLayout{sides: [4]int{3, 3, 4, 3}, centers: []int{1, 4, 8, 11}, mirrored: []int{4, 11}},
	},
	Cloud: {
		labels: cloudLabels,
		ports:  &portLayout{sides: [4]int{3, 2, 3, 2}, centers: []int{1, 3, 6, 8}, mirrored: []int{3, 8}},
		points: cloudSnapPoints,
	},
	Diamond: {
		labels: diamondLabels,
		ports:  &portLayout{sides: [4]int{1, 1, 1, 1}, centers: []int{0, 1, 2, 3}, mirrored: []int{0, 2, 1, 3}},
		points: diamondSnapPoints,
	},
	Person: {
		labels: personLabels,
		ports:  &portLayout{sides: [4]int{3, 2, 3, 2}, centers: []int{1, 3, 6, 8}, mirrored: []int{1, 6, 3, 8}},
		points: personSnapPoints,
	},
	C4Person: {
		labels: c4PersonLabels,
		ports:  &portLayout{sides: [4]int{1, 3, 3, 3}, centers: []int{0, 2, 5, 7}, mirrored: []int{1, 6, 3, 8}},
		points: c4PersonSnapPoints,
	},
}

var (
	squarePorts = portLayout{sides: [4]int{3, 3, 3, 3}, centers: []int{1, 4, 7, 10}, mirrored: []int{1, 7, 4, 10}}
	// tablePorts are those of a table without rows: a table with rows has one
	// side port per row and no center ports.
	tablePorts = portLayout{sides: [4]int{3, 1, 3, 1}, centers: []int{1, 3, 5, 7}, mirrored: []int{1, 5, 3, 7}}
)

// Label positions, as sets of one position.
const (
	outTopLeft      LabelSet = 1 << label.OutsideTopLeft
	outTopCenter    LabelSet = 1 << label.OutsideTopCenter
	outTopRight     LabelSet = 1 << label.OutsideTopRight
	outLeftTop      LabelSet = 1 << label.OutsideLeftTop
	outLeftMiddle   LabelSet = 1 << label.OutsideLeftMiddle
	outLeftBottom   LabelSet = 1 << label.OutsideLeftBottom
	outRightTop     LabelSet = 1 << label.OutsideRightTop
	outRightMiddle  LabelSet = 1 << label.OutsideRightMiddle
	outRightBottom  LabelSet = 1 << label.OutsideRightBottom
	outBottomLeft   LabelSet = 1 << label.OutsideBottomLeft
	outBottomCenter LabelSet = 1 << label.OutsideBottomCenter
	outBottomRight  LabelSet = 1 << label.OutsideBottomRight
	inTopLeft       LabelSet = 1 << label.InsideTopLeft
	inTopCenter     LabelSet = 1 << label.InsideTopCenter
	inTopRight      LabelSet = 1 << label.InsideTopRight
	inMiddleLeft    LabelSet = 1 << label.InsideMiddleLeft
	inMiddleCenter  LabelSet = 1 << label.InsideMiddleCenter
	inMiddleRight   LabelSet = 1 << label.InsideMiddleRight
	inBottomLeft    LabelSet = 1 << label.InsideBottomLeft
	inBottomCenter  LabelSet = 1 << label.InsideBottomCenter
	inBottomRight   LabelSet = 1 << label.InsideBottomRight

	inCorners  = inTopLeft | inTopRight | inBottomLeft | inBottomRight
	outCorners = outTopLeft | outTopRight | outLeftTop | outRightTop | outBottomLeft | outBottomRight | outLeftBottom | outRightBottom
)

// Label positions by tier: Good, OK, Unideal, Bad.
var (
	squareLabels = [4]LabelSet{
		outTopCenter | inTopCenter | inMiddleCenter | inBottomCenter | outBottomCenter | inCorners,
		inMiddleLeft | inMiddleRight | outTopLeft | outTopRight | outBottomLeft | outBottomRight,
		outLeftTop | outRightTop | outLeftMiddle | outRightMiddle | outLeftBottom | outRightBottom,
	}
	// roundLabels are those of circles and hexagons.
	roundLabels = [4]LabelSet{
		outTopCenter | inMiddleCenter | outBottomCenter,
		inBottomCenter | inTopCenter | inMiddleLeft | inMiddleRight,
		inCorners | outLeftMiddle | outRightMiddle,
		outCorners,
	}
	ovalLabels = [4]LabelSet{
		outTopCenter | inMiddleCenter | outBottomCenter,
		inBottomCenter | inTopCenter | inMiddleLeft | inMiddleRight,
		outLeftMiddle | outRightMiddle,
		outCorners | inCorners,
	}
	pageLabels = [4]LabelSet{
		inMiddleCenter | inBottomCenter | outBottomCenter,
		inTopLeft | inMiddleLeft | inMiddleRight | inBottomLeft | inBottomRight | outTopLeft | outTopCenter | outBottomLeft | outBottomRight,
		outTopRight | inTopCenter | outRightBottom | outRightMiddle | outLeftTop | outLeftMiddle | outLeftBottom,
		inTopRight | outRightTop,
	}
	imageLabels = [4]LabelSet{
		outBottomCenter | outTopCenter,
		outLeftMiddle | outRightMiddle,
		outCorners,
	}
	parallelogramLabels = [4]LabelSet{
		inTopLeft | inMiddleCenter | inBottomRight | outTopRight | outBottomLeft,
		outTopCenter | inTopCenter | inBottomCenter | outBottomCenter,
		outLeftMiddle | outLeftBottom | outRightTop | outRightMiddle | inTopRight | inMiddleLeft | inMiddleRight | inBottomLeft,
		outTopLeft | outLeftTop | outRightBottom | outBottomRight,
	}
	documentLabels = [4]LabelSet{
		inTopLeft | inTopCenter | inTopRight | inMiddleCenter,
		outTopLeft | outTopCenter | outTopRight | inMiddleLeft | inMiddleRight,
		outLeftTop | outLeftMiddle | outLeftBottom | outRightTop | outRightMiddle | outRightBottom | inBottomCenter | inBottomLeft | inBottomRight,
		outBottomLeft | outBottomCenter | outBottomRight,
	}
	cylinderLabels = [4]LabelSet{
		outTopCenter | inMiddleCenter | outBottomCenter,
		inTopCenter | inBottomCenter | inMiddleLeft | inMiddleRight | inCorners |
			outTopLeft | outTopRight | outBottomLeft | outBottomRight | outLeftMiddle | outRightMiddle,
		outLeftTop | outRightTop | outLeftBottom | outRightBottom,
	}
	queueLabels = [4]LabelSet{
		outTopCenter | inTopCenter | inMiddleCenter | inBottomCenter | outBottomCenter,
		inTopLeft | inBottomLeft | inMiddleRight,
		inTopRight | inBottomRight | outLeftMiddle | outRightMiddle | inMiddleLeft,
		outCorners,
	}
	storedDataLabels = [4]LabelSet{
		outTopCenter | inTopCenter | inMiddleCenter | inBottomCenter | outBottomCenter,
		outTopRight | outBottomRight | inTopRight | inBottomRight | inMiddleRight | inTopLeft | inBottomLeft,
		inMiddleLeft | outLeftMiddle | outTopLeft | outBottomLeft,
		outRightMiddle | outLeftTop | outRightTop | outLeftBottom | outRightBottom,
	}
	packageLabels = [4]LabelSet{
		inMiddleCenter | inMiddleLeft | inBottomLeft | inBottomCenter | outBottomLeft | outBottomCenter | inTopLeft | inTopCenter | inTopRight,
		outTopLeft | inMiddleRight | inBottomRight | outBottomRight,
		outTopCenter | outLeftMiddle | outRightMiddle | outLeftTop | outLeftBottom | outRightBottom,
		outTopRight | outRightTop,
	}
	stepLabels = [4]LabelSet{
		inMiddleCenter,
		inTopCenter | inTopRight | inBottomRight | inMiddleLeft | inMiddleRight | inBottomCenter | outTopLeft | outTopCenter | outBottomLeft | outBottomCenter,
		outRightMiddle | outLeftTop | outLeftBottom | inTopLeft | inBottomLeft,
		outTopRight | outLeftMiddle | outRightTop | outRightBottom | outBottomRight,
	}
	calloutLabels = [4]LabelSet{
		inTopCenter | inMiddleLeft | inMiddleCenter | inMiddleRight | inBottomCenter | inCorners,
		outTopLeft | outTopCenter | outTopRight | outBottomCenter,
		outLeftTop | outLeftMiddle | outRightTop | outRightMiddle,
		outLeftBottom | outRightBottom | outBottomLeft | outBottomRight,
	}
	cloudLabels = [4]LabelSet{
		inMiddleCenter | inBottomCenter | outBottomCenter | inTopCenter,
		outTopCenter | outBottomLeft | outBottomRight | inCorners | inMiddleLeft | inMiddleRight,
		outLeftMiddle | outRightMiddle | outLeftBottom | outRightBottom,
		outTopLeft | outTopRight | outLeftTop | outRightTop,
	}
	diamondLabels = [4]LabelSet{
		inMiddleCenter,
		outTopCenter | outBottomCenter | inMiddleLeft | inMiddleRight | outLeftMiddle | outRightMiddle | inBottomCenter | inTopCenter,
		inCorners,
		outCorners,
	}
	personLabels = [4]LabelSet{
		outBottomCenter,
		outTopCenter | outBottomLeft | outBottomRight,
		inTopCenter | inMiddleCenter | inBottomCenter | inBottomLeft | inBottomRight | outLeftMiddle | outLeftBottom | outRightMiddle | outRightBottom,
		inTopLeft | inTopRight | inMiddleLeft | inMiddleRight | outRightTop | outLeftTop | outTopLeft | outTopRight,
	}
	c4PersonLabels = [4]LabelSet{
		inMiddleCenter,
		inTopCenter | inBottomCenter,
		inCorners | inMiddleLeft | inMiddleRight,
		outTopCenter | outBottomCenter | outCorners | outLeftMiddle | outRightMiddle,
	}
)
