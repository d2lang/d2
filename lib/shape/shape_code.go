package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

var codeKind = shapeKind{
	typ:         CODE_TYPE,
	rectangular: true,
	paddingX:    0,
	paddingY:    0,
}

func NewCode(box *geo.Box) Shape {
	return newShape(&codeKind, box)
}
