package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

type shapeSquare struct {
	*baseShape
}

func NewSquare(box *geo.Box) Shape {
	shape := shapeSquare{
		baseShape: newShapeBase(SQUARE_TYPE, box),
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeSquare) IsRectangular() bool {
	return true
}
