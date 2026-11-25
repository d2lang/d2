package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

type shapeHexagon struct {
	*baseShape
}

func NewHexagon(box *geo.Box) Shape {
	shape := shapeHexagon{
		baseShape: newShapeBase(HEXAGON_TYPE, box),
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeHexagon) GetInnerBox() *geo.Box {
	width := s.Box.Width
	height := s.Box.Height
	tl := s.Box.TopLeft.Copy()
	tl.X += width / 6.
	width /= 1.5
	tl.Y += height / 6.
	height /= 1.5
	return geo.NewBox(tl, width, height)
}

func hexagonPath(box *geo.Box) *svg.SvgPathContext {
	return hexagonOutline(box, svg.NewSVGPathContext)
}

// hexagonOutline draws the outline of hexagonPath into a path made by newPath.
func hexagonOutline[P pathBuilder](box *geo.Box, newPath func(*geo.Point, float64, float64) P) P {
	halfYFactor := 43.6 / 87.3
	pc := newPath(box.TopLeft, box.Width, box.Height)
	pc.StartAt(pc.Absolute(0.25, 0))
	pc.L(false, 0, halfYFactor)
	pc.L(false, 0.25, 1)
	pc.L(false, 0.75, 1)
	pc.L(false, 1, halfYFactor)
	pc.L(false, 0.75, 0)
	pc.Z()
	return pc
}

func (s shapeHexagon) Perimeter() []geo.Intersectable {
	return hexagonPath(s.Box).Path
}

func (s shapeHexagon) traceBorder(t *borderTracer) {
	hexagonOutline(s.Box, t.newPath)
}

func (s shapeHexagon) GetSVGPathData() []string {
	return []string{
		hexagonPath(s.Box).PathData(),
	}
}

func (s shapeHexagon) GetPathCommands() [][]svg.PathCommand {
	return pathCommands(hexagonPath(s.Box))
}

func (s shapeHexagon) GetDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	totalWidth := 1.5 * (width + paddingX)
	totalHeight := 1.5 * (height + paddingY)
	return math.Ceil(totalWidth), math.Ceil(totalHeight)
}

func (s shapeHexagon) GetDefaultPadding() (paddingX, paddingY float64) {
	return defaultPadding / 2, defaultPadding / 2
}
