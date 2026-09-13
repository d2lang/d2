package d2grid

import (
	"fmt"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

type gridDiagram struct {
	objects []*d2graph.Object
	edges   []*d2graph.Edge
	rows    int
	columns int

	// if true, place objects left to right along rows
	// if false, place objects top to bottom along columns
	rowDirected bool

	width  float64
	height float64

	verticalGap   int
	horizontalGap int
}

func newGridDiagram(root *d2graph.Object) (*gridDiagram, error) {
	gd := gridDiagram{
		objects:       root.ChildrenArray,
		verticalGap:   DEFAULT_GAP,
		horizontalGap: DEFAULT_GAP,
	}

	if root.GridRows != nil {
		var err error
		gd.rows, err = strconv.Atoi(root.GridRows.Value)
		if err != nil || gd.rows <= 0 {
			return nil, fmt.Errorf("invalid grid-rows %q", root.GridRows.Value)
		}
		if gd.rows > d2graph.MaxGridDimension {
			return nil, fmt.Errorf("grid-rows %d exceeds the maximum of %d", gd.rows, d2graph.MaxGridDimension)
		}
	}
	if root.GridColumns != nil {
		var err error
		gd.columns, err = strconv.Atoi(root.GridColumns.Value)
		if err != nil || gd.columns <= 0 {
			return nil, fmt.Errorf("invalid grid-columns %q", root.GridColumns.Value)
		}
		if gd.columns > d2graph.MaxGridDimension {
			return nil, fmt.Errorf("grid-columns %d exceeds the maximum of %d", gd.columns, d2graph.MaxGridDimension)
		}
	}
	if len(gd.objects) > d2graph.MaxGridCells {
		return nil, fmt.Errorf("grid object count %d exceeds the limit of %d cells", len(gd.objects), d2graph.MaxGridCells)
	}

	if gd.rows != 0 && gd.columns != 0 {
		capacity, err := d2graph.GridCapacity(gd.rows, gd.columns)
		if err != nil {
			return nil, err
		}
		// . row-directed  column-directed
		// .  ┌───────┐    ┌───────┐
		// .  │ a b c │    │ a d g │
		// .  │ d e f │    │ b e h │
		// .  │ g h i │    │ c f i │
		// .  └───────┘    └───────┘
		// if keyword rows is first, make it row-directed, if columns is first it is column-directed
		if root.GridRows.MapKey == nil || root.GridColumns.MapKey == nil || root.GridRows.MapKey.Range.Before(root.GridColumns.MapKey.Range) {
			gd.rowDirected = true
		}

		// rows and columns specified, but we want to continue naturally if user enters more objects
		// e.g. 2 rows, 3 columns specified + g added:      │ with 3 columns, 2 rows:
		// . original  add row   add column                 │ original  add row   add column
		// . ┌───────┐ ┌───────┐ ┌─────────┐                │ ┌───────┐ ┌───────┐ ┌─────────┐
		// . │ a b c │ │ a b c │ │ a b c d │                │ │ a c e │ │ a d g │ │ a c e g │
		// . │ d e f │ │ d e f │ │ e f g   │                │ │ b d f │ │ b e   │ │ b d f   │
		// . └───────┘ │ g     │ └─────────┘                │ └───────┘ │ c f   │ └─────────┘
		// .           └───────┘ ▲                          │           └───────┘ ▲
		// .           ▲         └─existing objects modified│           ▲         └─existing columns preserved
		// .           └─existing rows preserved            │           └─existing objects modified
		if capacity < len(gd.objects) {
			if gd.rowDirected {
				gd.rows = divideRoundUp(len(gd.objects), gd.columns)
			} else {
				gd.columns = divideRoundUp(len(gd.objects), gd.rows)
			}
			if _, err := d2graph.GridCapacity(gd.rows, gd.columns); err != nil {
				return nil, fmt.Errorf("expanded grid: %w", err)
			}
		}
	} else if gd.columns == 0 {
		gd.rowDirected = true
		// we can only make N rows with N objects
		if len(gd.objects) < gd.rows {
			gd.rows = len(gd.objects)
		}
	} else {
		if len(gd.objects) < gd.columns {
			gd.columns = len(gd.objects)
		}
	}

	// grid gap sets both, but can be overridden
	if root.GridGap != nil {
		gd.verticalGap, _ = strconv.Atoi(root.GridGap.Value)
		gd.horizontalGap = gd.verticalGap
	}
	if root.VerticalGap != nil {
		gd.verticalGap, _ = strconv.Atoi(root.VerticalGap.Value)
	}
	if root.HorizontalGap != nil {
		gd.horizontalGap, _ = strconv.Atoi(root.HorizontalGap.Value)
	}

	for _, o := range gd.objects {
		o.TopLeft = geo.NewPoint(0, 0)
	}

	return &gd, nil
}

func divideRoundUp(numerator, denominator int) int {
	quotient := numerator / denominator
	if numerator%denominator != 0 {
		quotient++
	}
	return quotient
}

func (gd *gridDiagram) shift(dx, dy float64) {
	for _, obj := range gd.objects {
		obj.MoveWithDescendants(dx, dy)
	}
	for _, e := range gd.edges {
		e.Move(dx, dy)
	}
}
