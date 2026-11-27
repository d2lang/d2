package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const (
	defaultArcDepth = 24.
)

var cylinderKind = shapeKind{
	typ:             CYLINDER_TYPE,
	paddingX:        defaultPadding,
	paddingY:        defaultPadding / 2,
	innerBox:        cylinderInnerBox,
	dimensionsToFit: cylinderDimensionsToFit,
	path:            cylinderOuterPath,
	extraPath:       cylinderInnerPath,
	trace:           cylinderTrace,
}

func NewCylinder(box *geo.Box) Shape {
	return newShape(&cylinderKind, box)
}

func getArcHeight(box *geo.Box) float64 {
	arcHeight := defaultArcDepth
	// Note: box height should always be larger than 3*default
	// this just handles after collapsing into an oval
	if box.Height < arcHeight*2 {
		arcHeight = box.Height / 2.0
	}
	return arcHeight
}

func cylinderInnerBox(s *shape) *geo.Box {
	height := s.Box.Height
	tl := s.Box.TopLeft.Copy()
	arc := getArcHeight(s.Box)
	height -= 3 * arc
	tl.Y += 2 * arc
	return geo.NewBox(tl, s.Box.Width, height)
}

func cylinderOuterPath(box *geo.Box) *svg.SvgPathContext {
	return cylinderOuterOutline(box, svg.NewSVGPathContext)
}

// cylinderOuterOutline draws the outline of cylinderOuterPath into a path made by newPath.
func cylinderOuterOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	arcHeight := getArcHeight(box)
	multiplier := 0.45
	pc := newPath(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, arcHeight))
	pc.C(false, 0, 0, box.Width*multiplier, 0, box.Width/2, 0)
	pc.C(false, box.Width-box.Width*multiplier, 0, box.Width, 0, box.Width, arcHeight)
	pc.V(true, box.Height-arcHeight*2)
	pc.C(false, box.Width, box.Height, box.Width-box.Width*multiplier, box.Height, box.Width/2, box.Height)
	pc.C(false, box.Width*multiplier, box.Height, 0, box.Height, 0, box.Height-arcHeight)
	pc.V(true, -(box.Height - arcHeight*2))
	pc.Z()
	return pc
}

func cylinderInnerPath(box *geo.Box) *svg.SvgPathContext {
	arcHeight := getArcHeight(box)
	multiplier := 0.45
	pc := svg.NewSVGPathContext(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, arcHeight))
	pc.C(false, 0, arcHeight*2, box.Width*multiplier, arcHeight*2, box.Width/2, arcHeight*2)
	pc.C(false, box.Width-box.Width*multiplier, arcHeight*2, box.Width, arcHeight*2, box.Width, arcHeight)
	return pc
}

func cylinderTrace(box *geo.Box, t *borderTracer) {
	cylinderOuterOutline(box, t.newPath)
}

func cylinderDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	// 2 arcs top, height + padding, 1 arc bottom
	totalHeight := height + paddingY + 3*defaultArcDepth
	return math.Ceil(width + paddingX), math.Ceil(totalHeight)
}
