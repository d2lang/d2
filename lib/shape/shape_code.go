package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

type shapeCode struct {
	shapeSquare
}

func NewCode(box *geo.Box) Shape {
	shape := shapeCode{
		shapeSquare: shapeSquare{
			baseShape: newShapeBase(CODE_TYPE, box),
		},
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeCode) GetDefaultPadding() (paddingX, paddingY float64) {
	return 0, 0
}
