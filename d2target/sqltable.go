package d2target

import (
	"reflect"
	"strings"
)

const (
	NamePadding       = 10
	TypePadding       = 20
	ConstraintPadding = 20
	HeaderPadding     = 10

	// Setting table font size sets it for columns
	// The header needs to be a little larger for visual hierarchy
	HeaderFontAdd = 4
)

type SQLTable struct {
	Columns []SQLColumn `json:"columns"`
}

// AppendsRowsTo reports whether adding fields preserves existing row spacing
// and the horizontal alignment of the type column.
func (t *SQLTable) AppendsRowsTo(old *SQLTable, oldHeight, newHeight int) bool {
	if t == nil || old == nil || len(t.Columns) <= len(old.Columns) || oldHeight <= 0 {
		return false
	}
	if float64(oldHeight)/float64(len(old.Columns)+1) != float64(newHeight)/float64(len(t.Columns)+1) {
		return false
	}
	if len(old.Columns) == 0 {
		return true
	}
	if !reflect.DeepEqual(old.Columns, t.Columns[:len(old.Columns)]) {
		return false
	}
	widest := 0
	for _, col := range old.Columns {
		widest = max(widest, col.Name.LabelWidth)
	}
	for _, col := range t.Columns[len(old.Columns):] {
		if col.Name.LabelWidth > widest {
			return false
		}
	}
	return true
}

// SQLRowReveal fixes row spacing at the authored height while the table's
// moving bottom edge uncovers each appended row.
type SQLRowReveal struct {
	FullHeight int `json:"fullHeight"`
}

func (s *Shape) SQLRowHeight() float64 {
	height := s.Height
	if s.RowReveal != nil {
		height = s.RowReveal.FullHeight
	}
	return float64(height) / float64(1+len(s.Columns))
}

// SQLRowVisibleHeight is the portion of a row below its stationary top edge.
// While the slot opens, its contents travel with the bottom of the table.
func (s *Shape) SQLRowVisibleHeight(top, height float64) float64 {
	if s.RowReveal == nil {
		return height
	}
	return max(0, min(height, float64(s.Pos.Y)+float64(s.Height)-top))
}

// SQLRowDivider keeps the separator and moving bottom border together.
func (s *Shape) SQLRowDivider(index int, y, radius float64) (bottom, inset float64, last bool) {
	last = index == len(s.Columns)-1
	if s.RowReveal != nil {
		edge := float64(s.Pos.Y) + float64(s.Height)
		if y >= edge {
			return edge, radius, true
		}
		// Keep the former bottom divider inside the rounded corners until
		// the moving border has cleared it.
		return y, max(0, radius-(edge-y)), last
	}
	if last {
		return y, radius, true
	}
	return y, 0, false
}

type SQLColumn struct {
	Name       Text     `json:"name"`
	Type       Text     `json:"type"`
	Constraint []string `json:"constraint"`
	Reference  string   `json:"reference"`
}

func (c SQLColumn) Texts(fontSize int) []*MText {
	return []*MText{
		{
			Text:     c.Name.Label,
			FontSize: fontSize,
			IsBold:   false,
			IsItalic: false,
			Shape:    "sql_table",
		},
		{
			Text:     c.Type.Label,
			FontSize: fontSize,
			IsBold:   false,
			IsItalic: false,
			Shape:    "sql_table",
		},
		{
			Text:     c.ConstraintAbbr(),
			FontSize: fontSize,
			IsBold:   false,
			IsItalic: false,
			Shape:    "sql_table",
		},
	}
}

func (c SQLColumn) ConstraintAbbr() string {
	constraints := make([]string, len(c.Constraint))

	for i, constraint := range c.Constraint {
		switch constraint {
		case "primary_key":
			constraint = "PK"
		case "foreign_key":
			constraint = "FK"
		case "unique":
			constraint = "UNQ"
		}

		constraints[i] = constraint
	}

	return strings.Join(constraints, ", ")
}
