// Completion implements lsp autocomplete features
// Currently handles:
// - Complete dot and inside maps for reserved keyword holders (style, labels, etc)
// - Complete discrete values for keywords like shape
// - Complete suggestions for formats for keywords like opacity
package d2lsp

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2target"
)

type CompletionKind int

const (
	KeywordCompletion CompletionKind = iota
	StyleCompletion
	ShapeCompletion
)

type CompletionItem struct {
	Label      string
	Kind       CompletionKind
	Detail     string
	InsertText string
}

func GetCompletionItems(text string, line, column int) ([]CompletionItem, error) {
	return getCompletionItems(text, line, column, false)
}

// GetCompletionItemsUTF16 returns completions for a position whose column is
// measured in UTF-16 code units, as required by browser and LSP clients.
func GetCompletionItemsUTF16(text string, line, column int) ([]CompletionItem, error) {
	return getCompletionItems(text, line, column, true)
}

func getCompletionItems(text string, line, column int, utf16Pos bool) ([]CompletionItem, error) {
	var parseOpts *d2parser.ParseOptions
	if utf16Pos {
		parseOpts = &d2parser.ParseOptions{UTF16Pos: true}
	}

	ast, err := d2parser.Parse("", strings.NewReader(text), parseOpts)
	if err != nil {
		ast, _ = d2parser.Parse("", strings.NewReader(getTextUntilPosition(text, line, column, utf16Pos)), parseOpts)
	}

	keyword := getKeywordContext(text, ast, line, column, utf16Pos, "")
	switch keyword {
	case "style", "style.":
		return getStyleCompletions(), nil
	case "shape", "shape:":
		return getShapeCompletions(), nil
	case "shadow", "3d", "multiple", "animated", "bold", "italic", "underline", "filled", "double-border",
		"shadow:", "3d:", "multiple:", "animated:", "bold:", "italic:", "underline:", "filled:", "double-border:",
		"style.shadow:", "style.3d:", "style.multiple:", "style.animated:", "style.bold:", "style.italic:", "style.underline:", "style.filled:", "style.double-border:":
		return getBooleanCompletions(), nil
	case "fill-pattern", "fill-pattern:", "style.fill-pattern:":
		return getFillPatternCompletions(), nil
	case "text-transform", "text-transform:", "style.text-transform:":
		return getTextTransformCompletions(), nil
	case "opacity", "stroke-width", "stroke-dash", "border-radius", "font-size",
		"stroke", "fill", "font-color":
		return getValueCompletions(keyword), nil
	case "opacity:", "stroke-width:", "stroke-dash:", "border-radius:", "font-size:",
		"stroke:", "fill:", "font-color:",
		"style.opacity:", "style.stroke-width:", "style.stroke-dash:", "style.border-radius:", "style.font-size:",
		"style.stroke:", "style.fill:", "style.font-color:":
		return getValueCompletions(strings.TrimSuffix(strings.TrimPrefix(keyword, "style."), ":")), nil
	case "width", "height", "top", "left":
		return getValueCompletions(keyword), nil
	case "width:", "height:", "top:", "left:":
		return getValueCompletions(keyword[:len(keyword)-1]), nil
	case "source-arrowhead", "target-arrowhead":
		return getArrowheadCompletions(), nil
	case "source-arrowhead.shape:", "target-arrowhead.shape:":
		return getArrowheadShapeCompletions(), nil
	case "label", "label.":
		return getLabelCompletions(), nil
	case "icon", "icon:":
		return getIconCompletions(), nil
	case "connection.icon.":
		return []CompletionItem{
			{Label: "near", Kind: StyleCompletion, Detail: "static icon position", InsertText: "near: "},
			{Label: "shape", Kind: StyleCompletion, Detail: "native icon", InsertText: "shape: "},
			{Label: "style", Kind: StyleCompletion, Detail: "icon animation and border radius", InsertText: "style."},
		}, nil
	case "connection.icon.near:":
		var items []CompletionItem
		for _, pos := range d2ast.LabelPositionsArray {
			if d2ast.LabelPositionsMapping[pos].IsEdgePosition() {
				items = append(items, CompletionItem{Label: pos, Kind: KeywordCompletion, InsertText: pos})
			}
		}
		return items, nil
	case "connection.icon.shape:":
		return []CompletionItem{{Label: d2target.ShapeCircle, Kind: ShapeCompletion, Detail: "filled dot", InsertText: d2target.ShapeCircle}}, nil
	case "connection.icon.style.":
		return []CompletionItem{
			{Label: "animated", Kind: StyleCompletion, Detail: "loop or launch icons along the connection", InsertText: "animated: "},
			{Label: "border-radius", Kind: StyleCompletion, Detail: "round image corners", InsertText: "border-radius: "},
		}, nil
	case "connection.icon.style.animated:":
		return append(getBooleanCompletions(),
			CompletionItem{Label: "2/s", Kind: KeywordCompletion, Detail: "launch two icons per second", InsertText: "2/s"},
			CompletionItem{Label: "200ms", Kind: KeywordCompletion, Detail: "loop one icon every 200 milliseconds", InsertText: "200ms"},
		), nil
	case "icon.":
		return getLabelCompletions(), nil
	case "near", "near:":
		return getNearCompletions(), nil
	case "tooltip:", "tooltip":
		return getTooltipCompletions(), nil
	case "annotation", "annotation.":
		return getAnnotationCompletions(), nil
	case "annotation.shape:":
		return getAnnotationShapeCompletions(), nil
	case "annotation.near:":
		return getAnnotationNearCompletions(), nil
	case "direction:", "direction":
		return getDirectionCompletions(), nil
	default:
		return nil, nil
	}
}

func getTextUntilPosition(text string, line, column int, utf16Pos bool) string {
	lines := strings.Split(text, "\n")
	if line >= len(lines) {
		return text
	}

	result := strings.Join(lines[:line], "\n")
	if len(result) > 0 {
		result += "\n"
	}
	column = columnByteOffset(lines[line], column, utf16Pos)
	if column > len(lines[line]) {
		result += lines[line]
	} else {
		result += lines[line][:column]
	}
	return result
}

func getKeywordContext(text string, m *d2ast.Map, line, column int, utf16Pos bool, edgeParent string) string {
	if m == nil {
		return ""
	}
	lines := strings.Split(text, "\n")
	lineColumn := column
	if line >= 0 && line < len(lines) {
		lineColumn = columnByteOffset(lines[line], column, utf16Pos)
	}

	for _, n := range m.Nodes {
		if n.MapKey == nil {
			continue
		}

		var firstPart, lastPart string
		edgeField := edgeParent
		var key *d2ast.KeyPath
		if len(n.MapKey.Edges) > 0 {
			edgeField = "connection"
			key = n.MapKey.EdgeKey
		} else {
			key = n.MapKey.Key
		}
		if key != nil && len(key.Path) > 0 {
			edgeField = connectionIconKey(edgeField, key)
			firstKey := key.Path[0].Unbox()
			if firstKey.IsUnquoted() {
				firstPart = firstKey.ScalarString()
			}

			pathLen := len(key.Path)
			if pathLen > 1 {
				lastKey := key.Path[pathLen-1].Unbox()
				if lastKey.IsUnquoted() {
					lastPart = lastKey.ScalarString()
					_, isHolderLast := d2ast.ReservedKeywordHolders[lastPart]
					if !isHolderLast {
						_, isHolderLast = d2ast.CompositeReservedKeywords[lastPart]
					}
					end := lastKey.GetRange().End
					if isHolderLast && line == end.Line && column == end.Column+1 && isAfterDot(lines[line], lineColumn) {
						if edgeField == "connection.icon" || edgeField == "connection.icon.style" {
							return edgeField + "."
						}
						return lastPart + "."
					}
				}
			}
		}
		if _, isBoard := d2ast.BoardKeywords[firstPart]; isBoard {
			firstPart = ""
		}
		if firstPart == "classes" {
			firstPart = ""
		}

		_, isHolder := d2ast.ReservedKeywordHolders[firstPart]
		if !isHolder {
			_, isHolder = d2ast.CompositeReservedKeywords[firstPart]
		}

		// Check nested map
		if n.MapKey.Value.Map != nil && isPositionInMap(line, column, n.MapKey.Value.Map) {
			if nested := getKeywordContext(text, n.MapKey.Value.Map, line, column, utf16Pos, edgeField); nested != "" {
				holder, inHolder := firstPart, isHolder
				if strings.EqualFold(lastPart, "annotation") {
					// x.annotation: note {shape: } completes the annotation's
					// marks, not the shapes of x.
					holder, inHolder = "annotation", true
				}
				if inHolder {
					// If we got a direct key completion from inside a holder's map,
					// prefix it with the holder's name
					if strings.HasSuffix(nested, ":") && !strings.Contains(nested, ".") {
						return holder + "." + strings.TrimSuffix(nested, ":") + ":"
					}
				}
				return nested
			}
			if edgeField == "connection.icon" || edgeField == "connection.icon.style" {
				return edgeField + "."
			}
			return firstPart
		}

		keyRange := n.MapKey.Range
		if line != keyRange.End.Line {
			continue
		}

		// 1) Skip if cursor is well above/below this key
		if line < keyRange.Start.Line || line > keyRange.End.Line {
			continue
		}

		// 2) If on the start line, skip if before the key
		if line == keyRange.Start.Line && column < keyRange.Start.Column {
			continue
		}

		// 3) If on the end line, allow up to keyRange.End.Column + 1
		if line == keyRange.End.Line && column > keyRange.End.Column+1 {
			continue
		}

		lineText := lines[keyRange.End.Line]

		if isAfterColon(lineText, lineColumn) {
			if edgeField == "connection.icon.near" || edgeField == "connection.icon.shape" || edgeField == "connection.icon.style.animated" {
				return edgeField + ":"
			}
			if key != nil && len(key.Path) > 1 {
				if isHolder && (firstPart == "source-arrowhead" || firstPart == "target-arrowhead") {
					return firstPart + "." + lastPart + ":"
				}
				if parent := key.Path[len(key.Path)-2].Unbox(); parent.IsUnquoted() && strings.EqualFold(parent.ScalarString(), "annotation") {
					return "annotation." + lastPart + ":"
				}

				_, isHolder := d2ast.ReservedKeywordHolders[lastPart]
				if !isHolder {
					return lastPart
				}
			}
			return firstPart + ":"
		}

		if isAfterDot(lineText, lineColumn) && isHolder {
			if edgeField == "connection.icon" || edgeField == "connection.icon.style" {
				return edgeField + "."
			}
			return firstPart
		}
	}

	return ""
}

// Follow only the connection icon's property path. Shape maps and arrowhead
// attributes use their own existing completion contexts.
func connectionIconKey(parent string, key *d2ast.KeyPath) string {
	if parent == "" {
		return ""
	}
	for _, part := range key.Path {
		switch path := parent + "." + d2ast.Keyword(part.Unbox()); path {
		case "connection.icon", "connection.icon.style", "connection.icon.near", "connection.icon.shape", "connection.icon.style.animated":
			parent = path
		default:
			return ""
		}
	}
	return parent
}

func columnByteOffset(text string, column int, utf16Pos bool) int {
	if column <= 0 {
		return 0
	}
	if !utf16Pos {
		return column
	}

	units := 0
	for byteOffset, r := range text {
		if units >= column {
			return byteOffset
		}
		runeUnits := utf16.RuneLen(r)
		if units+runeUnits > column {
			return byteOffset
		}
		units += runeUnits
	}
	return len(text)
}

func isAfterDot(text string, pos int) bool {
	return pos > 0 && pos <= len(text) && text[pos-1] == '.'
}

func isAfterColon(text string, pos int) bool {
	if pos < 1 || pos > len(text) {
		return false
	}
	i := pos - 1
	for i >= 0 && unicode.IsSpace(rune(text[i])) {
		i--
	}
	return i >= 0 && text[i] == ':'
}

func isPositionInMap(line, column int, m *d2ast.Map) bool {
	if m == nil {
		return false
	}

	mapRange := m.Range
	if line < mapRange.Start.Line || line > mapRange.End.Line {
		return false
	}

	if line == mapRange.Start.Line && column < mapRange.Start.Column {
		return false
	}
	if line == mapRange.End.Line && column > mapRange.End.Column {
		return false
	}
	return true
}

func getShapeCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, len(d2target.Shapes))
	for _, shape := range d2target.Shapes {
		item := CompletionItem{
			Label:      shape,
			Kind:       ShapeCompletion,
			Detail:     "shape",
			InsertText: shape,
		}
		items = append(items, item)
	}
	return items
}

func getValueCompletions(property string) []CompletionItem {
	switch property {
	case "opacity":
		return []CompletionItem{{
			Label:      "(number between 0.0 and 1.0)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 0.4",
			InsertText: "",
		}}
	case "stroke-width":
		return []CompletionItem{{
			Label:      "(number between 0 and 15)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 2",
			InsertText: "",
		}}
	case "font-size":
		return []CompletionItem{{
			Label:      "(number between 8 and 100)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 14",
			InsertText: "",
		}}
	case "stroke-dash":
		return []CompletionItem{{
			Label:      "(number between 0 and 10)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 5",
			InsertText: "",
		}}
	case "border-radius":
		return []CompletionItem{{
			Label:      "(number greater than or equal to 0)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 4",
			InsertText: "",
		}}
	case "font-color", "stroke", "fill":
		return []CompletionItem{{
			Label:      "(color name, hex code, or gradient)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. blue, \"#ff0000\", \"linear-gradient(red, blue)\"",
			InsertText: "",
		}}
	case "width", "height", "top", "left":
		return []CompletionItem{{
			Label:      "(pixels)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. 400",
			InsertText: "",
		}}
	}
	return nil
}

func getStyleCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, len(d2ast.StyleKeywords))
	for keyword := range d2ast.StyleKeywords {
		item := CompletionItem{
			Label:      keyword,
			Kind:       StyleCompletion,
			Detail:     "style property",
			InsertText: keyword + ": ",
		}
		items = append(items, item)
	}
	return items
}

func getBooleanCompletions() []CompletionItem {
	return []CompletionItem{
		{
			Label:      "true",
			Kind:       KeywordCompletion,
			Detail:     "boolean",
			InsertText: "true",
		},
		{
			Label:      "false",
			Kind:       KeywordCompletion,
			Detail:     "boolean",
			InsertText: "false",
		},
	}
}

func getFillPatternCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, len(d2ast.FillPatterns))
	for _, pattern := range d2ast.FillPatterns {
		item := CompletionItem{
			Label:      pattern,
			Kind:       KeywordCompletion,
			Detail:     "fill pattern",
			InsertText: pattern,
		}
		items = append(items, item)
	}
	return items
}

func getTextTransformCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, len(d2ast.TextTransforms))
	for _, transform := range d2ast.TextTransforms {
		item := CompletionItem{
			Label:      transform,
			Kind:       KeywordCompletion,
			Detail:     "text transform",
			InsertText: transform,
		}
		items = append(items, item)
	}
	return items
}

func getLabelCompletions() []CompletionItem {
	return []CompletionItem{{
		Label:      "near",
		Kind:       StyleCompletion,
		Detail:     "label position",
		InsertText: "near: ",
	}}
}

func getNearCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, len(d2ast.LabelPositionsArray)+1)

	items = append(items, CompletionItem{
		Label:      "(object ID)",
		Kind:       KeywordCompletion,
		Detail:     "e.g. container.inner_shape",
		InsertText: "",
	})

	for _, pos := range d2ast.LabelPositionsArray {
		item := CompletionItem{
			Label:      pos,
			Kind:       KeywordCompletion,
			Detail:     "label position",
			InsertText: pos,
		}
		items = append(items, item)
	}
	return items
}

func getTooltipCompletions() []CompletionItem {
	return []CompletionItem{
		{
			Label:      "(markdown)",
			Kind:       KeywordCompletion,
			Detail:     "markdown formatted text",
			InsertText: "|md\n  # Tooltip\n  Hello world\n|",
		},
	}
}

func getAnnotationCompletions() []CompletionItem {
	completions := []struct{ label, detail string }{
		{"shape", "the mark to draw"},
		{"near", "side of the target for the note"},
		{"style.stroke", "color of the mark and its note"},
		{"style.font-color", "color of the note alone"},
	}
	items := make([]CompletionItem, len(completions))
	for i, c := range completions {
		items[i] = CompletionItem{
			Label:      c.label,
			Kind:       StyleCompletion,
			Detail:     c.detail,
			InsertText: c.label + ": ",
		}
	}
	return items
}

func getAnnotationShapeCompletions() []CompletionItem {
	details := map[string]string{
		"circle":    "ring around a shape (default)",
		"box":       "frame around a shape",
		"underline": "line under the shape's label (default for text)",
		"brace":     "curly brace along one side (default for containers)",
		"cross":     "strike a shape or connection out",
		"trace":     "follow a connection's route (default for connections)",
		"highlight": "highlighter over the label of a shape or connection",
		"arrow":     "arrow from the note to the shape or connection",
		"lasso":     "one loop round every shape a class or glob gives it to",
	}
	items := make([]CompletionItem, len(d2ast.AnnotationShapesArray))
	for i, shape := range d2ast.AnnotationShapesArray {
		items[i] = CompletionItem{
			Label:      shape,
			Kind:       ShapeCompletion,
			Detail:     details[shape],
			InsertText: shape,
		}
	}
	return items
}

func getAnnotationNearCompletions() []CompletionItem {
	items := make([]CompletionItem, len(d2ast.AnnotationPositionsArray))
	for i, position := range d2ast.AnnotationPositionsArray {
		items[i] = CompletionItem{
			Label:      position,
			Kind:       KeywordCompletion,
			InsertText: position,
		}
	}
	return items
}

func getIconCompletions() []CompletionItem {
	return []CompletionItem{
		{
			Label:      "(URL, e.g. https://icons.d2lang.com/xyz.svg)",
			Kind:       KeywordCompletion,
			Detail:     "icon URL",
			InsertText: "https://icons.d2lang.com/essentials%2F073-add.svg",
		},
	}
}

func getDirectionCompletions() []CompletionItem {
	directions := []string{"up", "down", "right", "left"}
	items := make([]CompletionItem, len(directions))
	for i, dir := range directions {
		items[i] = CompletionItem{
			Label:      dir,
			Kind:       KeywordCompletion,
			Detail:     "direction",
			InsertText: dir,
		}
	}
	return items
}

func getArrowheadShapeCompletions() []CompletionItem {
	arrowheads := []string{
		"triangle",
		"arrow",
		"diamond",
		"circle",
		"cf-one", "cf-one-required",
		"cf-many", "cf-many-required",
	}

	items := make([]CompletionItem, len(arrowheads))
	details := map[string]string{
		"triangle":         "default",
		"arrow":            "like triangle but pointier",
		"cf-one":           "crows foot one",
		"cf-one-required":  "crows foot one (required)",
		"cf-many":          "crows foot many",
		"cf-many-required": "crows foot many (required)",
	}

	for i, shape := range arrowheads {
		detail := details[shape]
		if detail == "" {
			detail = "arrowhead shape"
		}
		items[i] = CompletionItem{
			Label:      shape,
			Kind:       ShapeCompletion,
			Detail:     detail,
			InsertText: shape,
		}
	}
	return items
}

func getArrowheadCompletions() []CompletionItem {
	completions := []string{
		"shape",
		"label",
		"style.filled",
	}

	items := make([]CompletionItem, len(completions))

	for i, shape := range completions {
		items[i] = CompletionItem{
			Label:      shape,
			Kind:       ShapeCompletion,
			InsertText: shape,
		}
	}
	return items
}
