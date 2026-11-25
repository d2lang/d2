package asciicanvas

import (
	"bytes"
	"slices"
	"strings"
	"unicode"

	"github.com/d2lang/d2/d2renderers/d2ascii/charset"
)

type Canvas struct {
	grid        [][]string
	labelsAbove map[int][]edgeLabel
}

type edgeLabel struct {
	x    int
	text string
}

func New(width, height int) *Canvas {
	grid := make([][]string, height)
	for i := range grid {
		grid[i] = make([]string, width)
		for j := range grid[i] {
			grid[i][j] = " "
		}
	}
	return &Canvas{grid: grid}
}

func (c *Canvas) Set(x, y int, char string) {
	if c.IsInBounds(x, y) {
		c.grid[y][x] = char
	}
}

func (c *Canvas) Get(x, y int) string {
	if c.IsInBounds(x, y) {
		return c.grid[y][x]
	}
	return ""
}

func (c *Canvas) IsInBounds(x, y int) bool {
	return y >= 0 && y < len(c.grid) && x >= 0 && x < len(c.grid[y])
}

func (c *Canvas) Width() int {
	if len(c.grid) > 0 {
		return len(c.grid[0])
	}
	return 0
}

func (c *Canvas) Height() int {
	return len(c.grid)
}

func (c *Canvas) DrawLabel(x, y int, label string) {
	if !c.IsInBounds(x, y) {
		return
	}
	lines := strings.Split(label, "\n")
	for lineIdx, line := range lines {
		// NOTE: range over a string yields byte indexes, not rune indexes.
		// Use a rune counter so multi-byte characters (e.g. Vietnamese)
		// occupy exactly one column each.
		col := 0
		for _, ch := range line {
			c.Set(x+col, y+lineIdx, string(ch))
			col++
		}
	}
}

func (c *Canvas) ContainsAlphaNumeric(x, y int) bool {
	char := c.Get(x, y)
	for _, r := range char {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// DrawLabelAbove reserves output rows above an edge. Inserting them during
// serialization preserves the grid coordinates used to draw other routes.
func (c *Canvas) DrawLabelAbove(x, y int, text string) {
	if c.labelsAbove == nil {
		c.labelsAbove = make(map[int][]edgeLabel)
	}
	c.labelsAbove[y] = append(c.labelsAbove[y], edgeLabel{x: x, text: text})
}

func (c *Canvas) rowsWithLabels(chars charset.Set) ([][]string, map[int]bool) {
	if len(c.labelsAbove) == 0 {
		return c.grid, nil
	}
	vertical := chars.Vertical()
	labelRows := make(map[int]bool)
	rows := make([][]string, 0, len(c.grid))
	for y, row := range c.grid {
		if labels := c.labelsAbove[y]; len(labels) > 0 {
			// Continue vertical strokes through the extra rows.
			guides := make([]string, len(row))
			for x, cell := range row {
				guides[x] = " "
				if cell == vertical || (cell != " " && c.Get(x, y-1) == vertical) {
					guides[x] = vertical
				}
			}
			for _, label := range labels {
				for _, line := range strings.Split(label.text, "\n") {
					caption := slices.Clone(guides)
					x := label.x
					for _, ch := range line {
						if x >= 0 && x < len(caption) {
							caption[x] = string(ch)
						}
						x++
					}
					labelRows[len(rows)] = true
					rows = append(rows, caption)
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, labelRows
}

func (c *Canvas) ToByteArray(chars charset.Set) []byte {
	grid, labelRows := c.rowsWithLabels(chars)
	var buf bytes.Buffer
	startRow := 0
	endRow := len(grid) - 1

	for i, row := range grid {
		if strings.TrimSpace(strings.Join(row, "")) != "" {
			startRow = i
			break
		}
	}

	for i := len(grid) - 1; i >= 0; i-- {
		if strings.TrimSpace(strings.Join(grid[i], "")) != "" {
			endRow = i
			break
		}
	}

	startCol := 0
	endCol := 0
	if len(grid) > 0 {
		// Find leftmost column with content
		for col := 0; col < len(grid[0]); col++ {
			hasContent := false
			for row := startRow; row <= endRow; row++ {
				if col < len(grid[row]) && grid[row][col] != " " {
					hasContent = true
					break
				}
			}
			if hasContent {
				startCol = col
				break
			}
		}

		// Find rightmost column with content
		for col := len(grid[0]) - 1; col >= 0; col-- {
			hasContent := false
			for row := startRow; row <= endRow; row++ {
				if col < len(grid[row]) && grid[row][col] != " " {
					hasContent = true
					break
				}
			}
			if hasContent {
				endCol = col
				break
			}
		}
	}

	var prevRouteColumns []int
	var prevRouteType rune
	for i := startRow; i <= endRow; i++ {
		rowData := grid[i]
		if startCol < len(rowData) {
			rowData = rowData[startCol:]
		}
		if endCol-startCol+1 < len(rowData) {
			rowData = rowData[:endCol-startCol+1]
		}
		line := strings.Join(rowData, "")

		var routeColumns []int
		var routeType rune
		// Preserve label lines even when they contain only spaces or route glyphs.
		isOnlyRouteChars := !labelRows[i]

		verticalChar := []rune(chars.Vertical())[0]
		horizontalChar := []rune(chars.Horizontal())[0]

		for pos, char := range line {
			if char == verticalChar || char == horizontalChar {
				routeColumns = append(routeColumns, pos)
				if routeType == 0 {
					routeType = char
				} else if routeType != char {
					isOnlyRouteChars = false
					break
				}
			} else if char != ' ' {
				isOnlyRouteChars = false
			}
		}

		hasRoutes := len(routeColumns) > 0
		samePattern := len(routeColumns) == len(prevRouteColumns) && routeType == prevRouteType
		if samePattern {
			for j, col := range routeColumns {
				if j >= len(prevRouteColumns) || col != prevRouteColumns[j] {
					samePattern = false
					break
				}
			}
		}

		if isOnlyRouteChars && hasRoutes && samePattern && len(prevRouteColumns) > 0 {
			continue
		}

		if isOnlyRouteChars && hasRoutes {
			prevRouteColumns = routeColumns
			prevRouteType = routeType
		} else {
			prevRouteColumns = nil
			prevRouteType = 0
		}

		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}
