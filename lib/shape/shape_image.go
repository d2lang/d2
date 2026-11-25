package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

type shapeImage struct {
	*baseShape
}

func NewImage(box *geo.Box) Shape {
	shape := shapeImage{
		baseShape: newShapeBase(IMAGE_TYPE, box),
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeImage) IsRectangular() bool {
	return true
}

func (s shapeImage) GetDefaultPadding() (paddingX, paddingY float64) {
	return 0, 0
}
