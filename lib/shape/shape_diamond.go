package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

var diamondKind = shapeKind{
	typ:             DIAMOND_TYPE,
	paddingX:        defaultPadding / 4,
	paddingY:        defaultPadding / 2,
	innerBox:        diamondInnerBox,
	dimensionsToFit: diamondDimensionsToFit,
	path:            diamondPath,
	trace:           diamondTrace,
}

func NewDiamond(box *geo.Box) Shape {
	return newShape(&diamondKind, box)
}

func diamondInnerBox(s *shape) *geo.Box {
	width := s.Box.Width
	height := s.Box.Height
	tl := s.Box.TopLeft.Copy()
	tl.X += width / 4.
	tl.Y += height / 4.
	width /= 2.
	height /= 2.
	return geo.NewBox(tl, width, height)
}

func diamondPath(box *geo.Box) *svg.SvgPathContext {
	return diamondOutline(box, svg.NewSVGPathContext)
}

// diamondOutline draws the outline of diamondPath into a path made by newPath.
func diamondOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	pc := newPath(box.TopLeft, box.Width/77, box.Height/76.9)
	pc.StartAt(pc.Absolute(38.5, 76.9))
	pc.C(true, -0.3, 0, -0.5, -0.1, -0.7, -0.3)
	pc.L(false, 0.3, 39.2)
	pc.C(true, -0.4, -0.4, -0.4, -1, 0, -1.4)
	pc.L(false, 37.8, 0.3)
	pc.C(true, 0.4, -0.4, 1, -0.4, 1.4, 0)
	pc.L(true, 37.5, 37.5)
	pc.C(true, 0.4, 0.4, 0.4, 1, 0, 1.4)
	pc.L(false, 39.2, 76.6)
	pc.C(false, 39, 76.8, 38.8, 76.9, 38.5, 76.9)
	pc.Z()
	return pc
}

func diamondTrace(box *geo.Box, t *borderTracer) {
	diamondOutline(box, t.newPath)
}

func diamondDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	totalWidth := 2 * (width + paddingX)
	totalHeight := 2 * (height + paddingY)
	return math.Ceil(totalWidth), math.Ceil(totalHeight)
}
