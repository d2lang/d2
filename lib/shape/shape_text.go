package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Text is basically a rectangle
type shapeText struct {
	shapeSquare
}

func NewText(box *geo.Box) Shape {
	shape := shapeText{
		shapeSquare: shapeSquare{
			baseShape: newShapeBase(TEXT_TYPE, box),
		},
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeText) GetDefaultPadding() (paddingX, paddingY float64) {
	return 0, 0
}
