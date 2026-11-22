package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Class is basically a rectangle
type shapeClass struct {
	shapeSquare
}

func NewClass(box *geo.Box) Shape {
	shape := shapeClass{
		shapeSquare{
			baseShape: newShapeBase(CLASS_TYPE, box),
		},
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeClass) GetDefaultPadding() (paddingX, paddingY float64) {
	return 0, 0
}
