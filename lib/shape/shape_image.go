package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

var imageKind = shapeKind{
	typ:         IMAGE_TYPE,
	rectangular: true,
	paddingX:    0,
	paddingY:    0,
}

func NewImage(box *geo.Box) Shape {
	return newShape(&imageKind, box)
}
