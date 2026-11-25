package d2lib

import (
	"math"
	"reflect"

	"github.com/d2lang/d2/d2target"
)

// Tables grow from their existing top edge. A column's contents stay at their
// authored endpoint, including the type alignment shared by the whole table.
type sqlRowReveal struct {
	tables map[int]int // Shape index to number of existing columns.
}

func planSQLRowReveal(prev, next *d2target.Diagram) *sqlRowReveal {
	if prev == nil || next == nil || len(prev.Shapes) != len(next.Shapes) {
		return nil
	}
	var plan *sqlRowReveal
	for i := range next.Shapes {
		from, to := &prev.Shapes[i], &next.Shapes[i]
		if to.Type != d2target.ShapeSQLTable || len(to.Columns) <= len(from.Columns) {
			continue
		}
		if from.Type != to.Type || from.Pos != to.Pos || from.Width != to.Width || from.Height <= 0 ||
			from.SQLRowHeight() != to.SQLRowHeight() || from.Icon != nil ||
			len(from.Columns) == 0 && from.BorderRadius > 0 ||
			len(from.Columns) > 0 && !reflect.DeepEqual(from.Columns, to.Columns[:len(from.Columns)]) {
			return nil
		}
		if len(from.Columns) > 0 && widestColumnName(from.Columns) != widestColumnName(to.Columns) {
			return nil
		}
		if plan == nil {
			plan = &sqlRowReveal{tables: make(map[int]int)}
		}
		plan.tables[i] = len(from.Columns)
	}
	if plan == nil || !sameBoardCanvas(prev, next) || !reflect.DeepEqual(prev.Connections, next.Connections) {
		return nil
	}
	for i := range next.Shapes {
		from, to := &prev.Shapes[i], next.Shapes[i]
		if _, grows := plan.tables[i]; grows {
			to.Columns, to.Height = from.Columns, from.Height
		}
		if !reflect.DeepEqual(*from, to) {
			return nil
		}
	}
	return plan
}

func widestColumnName(cols []d2target.SQLColumn) int {
	width := 0
	for _, c := range cols {
		width = max(width, c.Name.LabelWidth)
	}
	return width
}

func (p *sqlRowReveal) apply(frame *d2target.Diagram, t float64) {
	for i, count := range p.tables {
		table := &frame.Shapes[i]
		added := len(table.Columns) - count
		// Open the space before bringing in its lettering. Each row follows
		// the same timing, including when several arrive in one step.
		at := min(float64(added), t*float64(added))
		whole, local := math.Modf(at)
		slot := boardStroke(min(1, local/.8))
		height := table.SQLRowHeight() * (1 + float64(count) + whole + slot)
		table.RowReveal = &d2target.SQLRowReveal{
			FullHeight:  table.Height,
			VisibleRows: float64(count) + whole + max(0, min(1, (local-.52)/.40)),
		}
		table.Height = int(math.Round(height))
	}
}
