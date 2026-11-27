package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const (
	packageTopMinHeight     = 34.
	packageTopMaxHeight     = 55.
	packageTopMinWidth      = 50.
	packageTopMaxWidth      = 150.
	packageHorizontalScalar = 0.5
	packageVerticalScalar   = 0.2
)

var packageKind = shapeKind{
	typ:             PACKAGE_TYPE,
	paddingX:        defaultPadding,
	paddingY:        .8 * defaultPadding,
	innerBox:        packageInnerBox,
	dimensionsToFit: packageDimensionsToFit,
	path:            packagePath,
	trace:           packageTrace,
}

func NewPackage(box *geo.Box) Shape {
	return newShape(&packageKind, box)
}

func packageInnerBox(s *shape) *geo.Box {
	tl := s.Box.TopLeft.Copy()
	height := s.Box.Height

	_, topHeight := getTopDimensions(s.Box)
	tl.Y += topHeight
	height -= topHeight
	return geo.NewBox(tl, s.Box.Width, height)
}

func getTopDimensions(box *geo.Box) (width, height float64) {
	width = box.Width * packageHorizontalScalar
	if box.Width >= 2*packageTopMinWidth {
		width = math.Min(packageTopMaxWidth, math.Max(packageTopMinWidth, width))
	}
	height = math.Min(packageTopMaxHeight, box.Height*packageVerticalScalar)
	return width, height
}

func packagePath(box *geo.Box) *svg.SvgPathContext {
	return packageOutline(box, svg.NewSVGPathContext)
}

// packageOutline draws the outline of packagePath into a path made by newPath.
func packageOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	topWidth, topHeight := getTopDimensions(box)

	pc := newPath(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, 0))
	pc.L(false, topWidth, 0)
	pc.L(false, topWidth, topHeight)
	pc.L(false, box.Width, topHeight)
	pc.L(false, box.Width, box.Height)
	pc.L(false, 0, box.Height)
	pc.Z()
	return pc
}

func packageTrace(box *geo.Box, t *borderTracer) {
	packageOutline(box, t.newPath)
}

func packageDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	innerHeight := height + paddingY
	// We want to compute what the topHeight will be to add to inner height;
	// topHeight=(verticalScalar * totalHeight) and totalHeight=(topHeight + innerHeight)
	// so solving for topHeight we get: topHeight=innerHeight * (verticalScalar/(1-verticalScalar))
	topHeight := innerHeight * packageVerticalScalar / (1. - packageVerticalScalar)
	totalHeight := innerHeight + math.Min(topHeight, packageTopMaxHeight)

	return math.Ceil(width + paddingX), math.Ceil(totalHeight)
}
