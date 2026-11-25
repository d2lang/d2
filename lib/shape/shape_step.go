package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

type shapeStep struct {
	*baseShape
}

func NewStep(box *geo.Box) Shape {
	shape := shapeStep{
		baseShape: newShapeBase(STEP_TYPE, box),
	}
	*shape.FullShape = shape
	return shape
}

const STEP_WEDGE_WIDTH = 35.0

// StepWedgeWidth returns the depth of the two chevrons for a step of this width.
func StepWedgeWidth(width float64) float64 {
	if width <= STEP_WEDGE_WIDTH {
		return width / 2
	}
	return STEP_WEDGE_WIDTH
}

func (s shapeStep) GetInnerBox() *geo.Box {
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

func (s shapeStep) Perimeter() []geo.Intersectable {
	return stepPath(s.Box).Path
}

func (s shapeStep) traceBorder(t *borderTracer) {
	stepOutline(s.Box, t.newPath)
}

func (s shapeStep) GetSVGPathData() []string {
	return []string{
		stepPath(s.Box).PathData(),
	}
}

func (s shapeStep) GetPathCommands() [][]svg.PathCommand {
	return pathCommands(stepPath(s.Box))
}

func (s shapeStep) GetDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	totalWidth := width + paddingX + 2*STEP_WEDGE_WIDTH
	return math.Ceil(totalWidth), math.Ceil(height + paddingY)
}

func (s shapeStep) GetDefaultPadding() (paddingX, paddingY float64) {
	return defaultPadding / 4, defaultPadding + STEP_WEDGE_WIDTH
}
