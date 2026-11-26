package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const storedDataWedgeWidth = 15.

var storedDataKind = shapeKind{
	typ:             STORED_DATA_TYPE,
	paddingX:        defaultPadding - 10,
	paddingY:        defaultPadding,
	innerBox:        storedDataInnerBox,
	dimensionsToFit: storedDataDimensionsToFit,
	path:            storedDataPath,
	trace:           storedDataTrace,
}

func NewStoredData(box *geo.Box) Shape {
	return newShape(&storedDataKind, box)
}

func storedDataInnerBox(s *shape) *geo.Box {
	width := s.Box.Width
	tl := s.Box.TopLeft.Copy()
	width -= 2 * storedDataWedgeWidth
	tl.X += storedDataWedgeWidth
	return geo.NewBox(tl, width, s.Box.Height)
}

func storedDataPath(box *geo.Box) *svg.SvgPathContext {
	return storedDataOutline(box, svg.NewSVGPathContext)
}

// storedDataOutline draws the outline of storedDataPath into a path made by newPath.
func storedDataOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	wedgeWidth := storedDataWedgeWidth
	multiplier := 0.27
	if box.Width < wedgeWidth*2 {
		wedgeWidth = box.Width / 2.0
	}
	pc := newPath(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(wedgeWidth, 0))
	pc.H(true, box.Width-wedgeWidth)
	pc.C(false, box.Width-wedgeWidth*multiplier, 0, box.Width-wedgeWidth, box.Height*multiplier, box.Width-wedgeWidth, box.Height/2.0)
	pc.C(false, box.Width-wedgeWidth, box.Height-box.Height*multiplier, box.Width-wedgeWidth*multiplier, box.Height, box.Width, box.Height)
	pc.H(true, -(box.Width - wedgeWidth))
	pc.C(false, wedgeWidth-wedgeWidth*multiplier, box.Height, 0, box.Height-box.Height*multiplier, 0, box.Height/2.0)
	pc.C(false, 0, box.Height*multiplier, wedgeWidth-wedgeWidth*multiplier, 0, wedgeWidth, 0)
	pc.Z()
	return pc
}

func storedDataTrace(box *geo.Box, t *borderTracer) {
	storedDataOutline(box, t.newPath)
}

func storedDataDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	totalWidth := width + paddingX + 2*storedDataWedgeWidth
	return math.Ceil(totalWidth), math.Ceil(height + paddingY)
}
