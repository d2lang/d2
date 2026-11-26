package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Class is basically a rectangle
var classKind = shapeKind{
	typ:         CLASS_TYPE,
	rectangular: true,
	paddingX:    0,
	paddingY:    0,
}

func NewClass(box *geo.Box) Shape {
	return newShape(&classKind, box)
}
