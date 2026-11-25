package d2target

import "strings"

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

// SQLRowReveal holds the final table height for row layout while an incoming
// step expands the body. The fractional row count controls the new text's opacity.
type SQLRowReveal struct {
	FullHeight  int     `json:"fullHeight"`
	VisibleRows float64 `json:"visibleRows"`
}

func (s *Shape) SQLRowHeight() float64 {
	height := s.Height
	if s.RowReveal != nil {
		height = s.RowReveal.FullHeight
	}
	return float64(height) / float64(1+len(s.Columns))
}

func (s *Shape) SQLRowOpacity(index int) float64 {
	if s.RowReveal == nil {
		return 1
	}
	return max(0, min(1, s.RowReveal.VisibleRows-float64(index)))
}

// SQLRowDivider positions the separator below a row. The growing body's
// bottom border moves ahead of the text; keep its divider on that border.
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
