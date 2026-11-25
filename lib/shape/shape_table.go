package shape

import (
	"github.com/d2lang/d2/lib/geo"
)

// Table is basically a rectangle
type shapeTable struct {
	shapeSquare
}

func NewTable(box *geo.Box) Shape {
	shape := shapeTable{
		shapeSquare{
			baseShape: newShapeBase(TABLE_TYPE, box),
		},
	}
	*shape.FullShape = shape
	return shape
}

func (s shapeTable) GetDefaultPadding() (paddingX, paddingY float64) {
	return 0, 0
}
