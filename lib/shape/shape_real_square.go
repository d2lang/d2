package shape

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

var realSquareKind = shapeKind{
	typ:             REAL_SQUARE_TYPE,
	aspectRatio1:    true,
	rectangular:     true,
	paddingX:        defaultPadding,
	paddingY:        defaultPadding,
	dimensionsToFit: realSquareDimensionsToFit,
}

func NewRealSquare(box *geo.Box) Shape {
	return newShape(&realSquareKind, box)
}

func realSquareDimensionsToFit(width, height, paddingX, paddingY float64) (float64, float64) {
	sideLength := math.Ceil(math.Max(width+paddingX, height+paddingY))
	return sideLength, sideLength
}
