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

	keyword := getKeywordContext(text, ast, line, column, utf16Pos)
	switch keyword {
	case "style", "style.":
		return getStyleCompletions(), nil
	case "shape", "shape:":
		return getShapeCompletions(), nil
	case "sequence-diagram.shape.":
		return getSequenceOptionCompletions(), nil
	case "edge-group.shape.":
		return getBranchOptionCompletions(), nil
	case "sequence-diagram.shape.numbered:", "sequence-diagram.shape.mirrored:", "edge-group.shape.branched:",
		"shape.numbered:", "shape.mirrored:", "shape.branched:":
		return getBooleanCompletions(), nil
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
	case "edge-group.label.":
		return frameLabelCompletions(), nil
	case "edge-group.label.style.":
		return []CompletionItem{{Label: "fill", Kind: StyleCompletion, Detail: "frame caption background", InsertText: "fill: "}}, nil
	case "label", "label.":
		return getLabelCompletions(), nil
	case "icon", "icon:":
		return getIconCompletions(), nil
	case "icon.":
		return getLabelCompletions(), nil
	case "near", "near:":
		return getNearCompletions(), nil
	case "tooltip:", "tooltip":
		return getTooltipCompletions(), nil
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

func getKeywordContext(text string, m *d2ast.Map, line, column int, utf16Pos bool) string {
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
		var key *d2ast.KeyPath
		if len(n.MapKey.Edges) > 0 {
			key = n.MapKey.EdgeKey
		} else {
			key = n.MapKey.Key
		}
		if key != nil && len(key.Path) > 0 {
			firstKey := key.Path[0].Unbox()
			if firstKey.IsUnquoted() {
				firstPart = firstKey.ScalarString()
			} else if len(key.Path) == 1 && n.MapKey.Value.Map == nil {
				continue
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
					keyRange := n.MapKey.Range
					lineText := lines[keyRange.End.Line]
					dotColumn := columnByteOffset(lineText, column, utf16Pos)
					if isHolderLast && keyRange.End.Line == line && isAfterDot(lineText, dotColumn) {
						if lastPart == "shape" {
							return shapeOptionsContext(m, key)
						}
						if len(n.MapKey.Edges) == 0 {
							if caption := captionContext(m, key); caption != "" {
								return caption
							}
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
			if key != nil && len(key.Path) > 0 && key.Last().Unbox().IsUnquoted() && strings.EqualFold(key.Last().Unbox().ScalarString(), "shape") {
				prefix := shapeOptionsContext(m, key)
				if prefix == "" {
					return ""
				}
				nested := getKeywordContext(text, n.MapKey.Value.Map, line, column, utf16Pos)
				return prefix + strings.ToLower(nested)
			}
			nested := getKeywordContext(text, n.MapKey.Value.Map, line, column, utf16Pos)
			if len(n.MapKey.Edges) == 0 && key != nil {
				if caption := captionContext(m, key); caption != "" {
					switch nested {
					case "":
						return caption
					case "style", "style.":
						return caption + "style."
					}
				}
			}
			if nested != "" {
				if isHolder {
					// If we got a direct key completion from inside a holder's map,
					// prefix it with the holder's name
					if strings.HasSuffix(nested, ":") && !strings.Contains(nested, ".") {
						return firstPart + "." + strings.TrimSuffix(nested, ":") + ":"
					}
				}
				return nested
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
			if key != nil && len(key.Path) > 1 {
				parent := key.Path[len(key.Path)-2].Unbox()
				if parent.IsUnquoted() && strings.EqualFold(parent.ScalarString(), "shape") {
					prefix := shapeOptionsContext(m, &d2ast.KeyPath{Path: key.Path[:len(key.Path)-1]})
					if prefix == "" {
						prefix = "shape."
					}
					return prefix + strings.ToLower(lastPart) + ":"
				}
				if isHolder && (firstPart == "source-arrowhead" || firstPart == "target-arrowhead") {
					return firstPart + "." + lastPart + ":"
				}

				_, isHolder := d2ast.ReservedKeywordHolders[lastPart]
				if !isHolder {
					return lastPart
				}
			}
			return firstPart + ":"
		}

		if isAfterDot(lineText, lineColumn) && isHolder {
			if firstPart == "shape" {
				return shapeOptionsContext(m, key)
			}
			if len(n.MapKey.Edges) == 0 && key != nil {
				if caption := captionContext(m, key); caption != "" {
					return caption
				}
			}
			return firstPart
		}
	}

	return ""
}

// Look up the shape belonging to the label, including declarations split
// between a dotted path and nested maps. Only frame captions expose a fill.
func captionContext(m *d2ast.Map, key *d2ast.KeyPath) string {
	i := len(key.Path) - 1
	option := "label."
	if i >= 0 && key.Path[i].Unbox().IsUnquoted() && key.Path[i].ScalarString() == "style" {
		i--
		option = "label.style."
	}
	if i < 0 || !key.Path[i].Unbox().IsUnquoted() || key.Path[i].ScalarString() != "label" {
		return ""
	}
	// Append to a copy so the incomplete AST key still names the label.
	path := append(key.Path[:i:i], d2ast.MakeKeyPath([]string{"shape"}).Path[0])
	if shape := declaredShape(m, path); shape != nil && strings.EqualFold(shape.ScalarString(), d2target.ShapeSequenceDiagramEdgeGroup) {
		return "edge-group." + option
	}
	return option
}

// Only suggest sequence options after a dot when the local shape declaration
// identifies a sequence diagram. The cursor may be at an incomplete key.
func shapeOptionsContext(m *d2ast.Map, key *d2ast.KeyPath) string {
	shape := declaredShape(m, key.Path)
	if shape != nil {
		switch strings.ToLower(shape.ScalarString()) {
		case d2target.ShapeSequenceDiagramV2:
			return "sequence-diagram.shape."
		case d2target.ShapeSequenceDiagramEdgeGroup:
			return "edge-group.shape."
		}
	}
	return ""
}

// A declaration for a.b.shape may be split across maps or dotted keys.
func declaredShape(m *d2ast.Map, path []*d2ast.StringBox) d2ast.Scalar {
	for i := len(m.Nodes) - 1; i >= 0; i-- {
		k := m.Nodes[i].MapKey
		if k == nil || len(k.Edges) != 0 || k.NotAmpersand || k.Key == nil {
			continue
		}
		n := len(k.Key.Path)
		if n == 0 || n > len(path) || !k.Key.Equals(&d2ast.KeyPath{Path: path[:n]}) {
			continue
		}
		if n < len(path) {
			if k.Value.Map != nil {
				if shape := declaredShape(k.Value.Map, path[n:]); shape != nil {
					return shape
				}
			}
			continue
		}
		if !k.Key.Last().Unbox().IsUnquoted() {
			continue
		}
		shape := k.Primary.Unbox()
		if shape == nil {
			shape = k.Value.ScalarBox().Unbox()
		}
		if shape != nil {
			return shape
		}
	}
	return nil
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

func getBranchOptionCompletions() []CompletionItem {
	return []CompletionItem{{Label: "branched", Kind: KeywordCompletion, Detail: "Divide an edge-group into named branches", InsertText: "branched: "}}
}

func getSequenceOptionCompletions() []CompletionItem {
	return []CompletionItem{
		{Label: "numbered", Kind: KeywordCompletion, Detail: "Number sequence messages", InsertText: "numbered: "},
		{Label: "mirrored", Kind: KeywordCompletion, Detail: "Repeat actors at the bottom of the sequence", InsertText: "mirrored: "},
	}
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
			Label:      "(color name or hex code)",
			Kind:       KeywordCompletion,
			Detail:     "e.g. blue, #ff0000",
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

func frameLabelCompletions() []CompletionItem {
	return append(getLabelCompletions(), CompletionItem{Label: "style.fill", Kind: StyleCompletion, Detail: "frame caption background", InsertText: "style.fill: "})
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
