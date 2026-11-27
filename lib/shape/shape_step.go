package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

const STEP_WEDGE_WIDTH = 35.0

var stepKind = shapeKind{
	typ:             STEP_TYPE,
	paddingX:        defaultPadding / 4,
	paddingY:        defaultPadding + STEP_WEDGE_WIDTH,
	innerBox:        stepInnerBox,
	dimensionsToFit: stepDimensionsToFit,
	path:            stepPath,
	trace:           stepTrace,
}

func NewStep(box *geo.Box) Shape {
	return newShape(&stepKind, box)
}

// StepWedgeWidth returns the depth of the two chevrons for a step of this width.
func StepWedgeWidth(width float64) float64 {
	if width <= STEP_WEDGE_WIDTH {
		return width / 2
	}
	return STEP_WEDGE_WIDTH
}

func stepInnerBox(s *shape) *geo.Box {
	width := s.Box.Width
	tl := s.Box.TopLeft.Copy()
	width -= 2 * STEP_WEDGE_WIDTH
	tl.X += STEP_WEDGE_WIDTH
	return geo.NewBox(tl, width, s.Box.Height)
}

func stepPath(box *geo.Box) *svg.SvgPathContext {
	return stepOutline(box, svg.NewSVGPathContext)
}

// stepOutline draws the outline of stepPath into a path made by newPath.
func stepOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	wedgeWidth := StepWedgeWidth(box.Width)
	pc := newPath(box.TopLeft, 1, 1)
	pc.StartAt(pc.Absolute(0, 0))
	pc.L(false, box.Width-wedgeWidth, 0)
	pc.L(false, box.Width, box.Height/2)
	pc.L(false, box.Width-wedgeWidth, box.Height)
	pc.L(false, 0, box.Height)
	pc.L(false, wedgeWidth, box.Height/2)
	pc.Z()
	return pc
}

func stepTrace(box *geo.Box, t *borderTracer) {
	stepOutline(box, t.newPath)
}

func stepDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	totalWidth := width + paddingX + 2*STEP_WEDGE_WIDTH
	return math.Ceil(totalWidth), math.Ceil(height + paddingY)
}
