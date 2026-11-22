package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

type shapeRealSquare struct {
	*baseShape
}

func NewRealSquare(box *geo.Box) Shape {
	shape := shapeRealSquare{
		baseShape: newShapeBase(REAL_SQUARE_TYPE, box),
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeRealSquare) AspectRatio1() bool {
	return true
}

func (s shapeRealSquare) IsRectangular() bool {
	return true
}

func (s shapeRealSquare) GetDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	sideLength := math.Ceil(math.Max(width+paddingX, height+paddingY))
	return sideLength, sideLength
}
