package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Table is basically a rectangle
var tableKind = shapeKind{
	typ:         TABLE_TYPE,
	rectangular: true,
	paddingX:    0,
	paddingY:    0,
}

func NewTable(box *geo.Box) Shape {
	return newShape(&tableKind, box)
}
