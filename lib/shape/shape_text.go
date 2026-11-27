package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Text is basically a rectangle
var textKind = shapeKind{
	typ:         TEXT_TYPE,
	rectangular: true,
	paddingX:    0,
	paddingY:    0,
}

func NewText(box *geo.Box) Shape {
	return newShape(&textKind, box)
}
