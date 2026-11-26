package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const (
	// the shape is taller than where the bottom of the path ends
	docPathHeight      = 18.925
	docPathInnerBottom = 14
	docPathBottom      = 16.3
)

var documentKind = shapeKind{
	typ:             DOCUMENT_TYPE,
	paddingX:        defaultPadding,
	paddingY:        defaultPadding * docPathInnerBottom / docPathHeight,
	innerBox:        documentInnerBox,
	dimensionsToFit: documentDimensionsToFit,
	path:            documentPath,
	trace:           documentTrace,
}

func NewDocument(box *geo.Box) Shape {
	return newShape(&documentKind, box)
}

func documentInnerBox(s *shape) *geo.Box {
	height := s.Box.Height * docPathInnerBottom / docPathHeight
	return geo.NewBox(s.Box.TopLeft.Copy(), s.Box.Width, height)
}

func documentPath(box *geo.Box) *svg.SvgPathContext {
	return documentOutline(box, svg.NewSVGPathContext)
}

// documentOutline draws the outline of documentPath into a path made by newPath.
func documentOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	pc := newPath(box.TopLeft, box.Width, box.Height)
	pc.StartAt(pc.Absolute(0, docPathBottom/docPathHeight))
	pc.L(false, 0, 0)
	pc.L(false, 1, 0)
	pc.L(false, 1, docPathBottom/docPathHeight)
	pc.C(false, 5/6.0, 12.8/docPathHeight, 2/3.0, 12.8/docPathHeight, 1/2.0, docPathBottom/docPathHeight)
	pc.C(false, 1/3.0, 19.8/docPathHeight, 1/6.0, 19.8/docPathHeight, 0, docPathBottom/docPathHeight)
	pc.Z()
	return pc
}

func documentTrace(box *geo.Box, t *borderTracer) {
	documentOutline(box, t.newPath)
}

func documentDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	baseHeight := (height + paddingY) * docPathHeight / docPathInnerBottom
	return math.Ceil(width + paddingX), math.Ceil(baseHeight)
}
