package d2lib

import (
	"math"
	"reflect"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/animation"
)

// Add or remove trailing rows beneath a stationary table.
type sqlRowReveal struct {
	tables map[int]sqlRowChange
}

type sqlRowChange struct {
	short, full *d2target.Shape
	removing    bool
}

func planSQLRowReveal(prev, next *d2target.Diagram) *sqlRowReveal {
	if prev == nil || next == nil || len(prev.Shapes) != len(next.Shapes) {
		return nil
	}
	var plan *sqlRowReveal
	for i := range next.Shapes {
		from, to := &prev.Shapes[i], &next.Shapes[i]
		if to.Type != d2target.ShapeSQLTable || len(to.Columns) == len(from.Columns) {
			continue
		}
		change := sqlRowChange{short: from, full: to}
		if len(from.Columns) > len(to.Columns) {
			change = sqlRowChange{short: to, full: from, removing: true}
		}
		if from.Type != to.Type || from.Pos != to.Pos || from.Width != to.Width || from.Width <= 0 || from.Icon != nil ||
			len(change.short.Columns) == 0 && change.short.BorderRadius > 0 ||
			!change.full.SQLTable.AppendsRowsTo(&change.short.SQLTable, change.short.Height, change.full.Height) {
			return nil
		}
		if plan == nil {
			plan = &sqlRowReveal{tables: make(map[int]sqlRowChange)}
		}
		plan.tables[i] = change
	}
	if plan == nil || !sameBoardCanvas(prev, next) || !reflect.DeepEqual(prev.Connections, next.Connections) {
		return nil
	}
	for i := range next.Shapes {
		from, to := &prev.Shapes[i], next.Shapes[i]
		if _, changed := plan.tables[i]; changed {
			to.Columns, to.Height = from.Columns, from.Height
		}
		if !reflect.DeepEqual(*from, to) {
			return nil
		}
	}
	return plan
}

func (p *sqlRowReveal) apply(frame *d2target.Diagram, t float64) {
	for i, change := range p.tables {
		progress := t
		if change.removing {
			progress = 1 - t
		}
		retained := len(change.short.Columns)
		whole, local := math.Modf(progress * float64(len(change.full.Columns)-retained))
		height := int(math.Round(change.full.SQLRowHeight() * (1 + float64(retained) + whole + animation.EaseInOut(local))))
		table := &frame.Shapes[i]
		if height == change.short.Height && change.removing {
			continue // The sample already contains the shorter destination table.
		}
		// Keep the outgoing columns and their original spacing until the moving
		// bottom edge has covered them, one row at a time from the bottom up.
		table.Columns = change.full.Columns
		table.Height = height
		if height < change.full.Height {
			table.RowReveal = &d2target.SQLRowReveal{FullHeight: change.full.Height}
		}
	}
}
