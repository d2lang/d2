package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

var squareKind = shapeKind{
	typ:         SQUARE_TYPE,
	rectangular: true,
	paddingX:    defaultPadding,
	paddingY:    defaultPadding,
}

func NewSquare(box *geo.Box) Shape {
	return newShape(&squareKind, box)
}
