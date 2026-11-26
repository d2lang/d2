package lspserver

import (
	"strings"
	"unicode"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
)

// LSP SymbolKind values used in the D2 outline.
const (
	namespaceSymbol = 3
	packageSymbol   = 4
	classSymbol     = 5
	variableSymbol  = 13
	objectSymbol    = 19
	operatorSymbol  = 25
)

func symbols(root *d2ast.Map) []documentSymbol {
	if root == nil {
		return []documentSymbol{}
	}
	return mapSymbols(root.Nodes, objectSymbol, true)
}

// defaultKind describes keys directly inside this map. Shape/board contents
// are classified by D2 keywords; variable and class maps contain named data.
func mapSymbols(nodes []d2ast.MapNodeBox, defaultKind int, boardRoot bool) []documentSymbol {
	out := []documentSymbol{}
	type entry struct {
		index    int
		explicit bool
		children []d2ast.MapNodeBox
	}
	objects := make(map[string]*entry)
	for _, node := range nodes {
		key := node.MapKey
		if key == nil || key.EdgeIndex != nil || key.EdgeKey != nil {
			continue
		}
		kind, childrenKind := defaultKind, objectSymbol
		index := len(out)
		implicit := false
		var shape *entry
		var name string
		selection := key.Range
		if key.Key != nil {
			name = d2format.Format(key.Key)
			selection = key.Key.Range
		} else if len(key.Edges) > 0 {
			selection = key.Edges[0].Range
		}

		switch {
		case defaultKind == variableSymbol:
			childrenKind = variableSymbol
		case defaultKind == classSymbol:
			childrenKind = 0
		case defaultKind == packageSymbol:
			// The contents of each board start a fresh outline.
		case len(key.Edges) > 0:
			edge := key.Copy()
			edge.Primary, edge.Value = d2ast.ScalarBox{}, d2ast.ValueBox{}
			name = d2format.Format(edge)
			kind, childrenKind = operatorSymbol, 0
		case key.Key != nil && len(key.Key.Path) > 0:
			keyword := d2ast.Keyword(key.Key.Path[0].Unbox())
			if _, board := d2ast.BoardKeywords[keyword]; board && !boardRoot {
				keyword = "" // a child shape named layers, scenarios or steps
			}
			switch keyword {
			case "vars":
				name, kind, childrenKind = keyword, variableSymbol, variableSymbol
			case "classes":
				name, kind, childrenKind = keyword, namespaceSymbol, classSymbol
			case "layers", "scenarios", "steps":
				name, kind, childrenKind = keyword, namespaceSymbol, packageSymbol
			case "":
				path := shapePrefix(key.Key)
				implicit = path != key.Key
				id := shapeSymbolID(path)
				shape = objects[id]
				if shape == nil {
					shape = &entry{index: index}
					objects[id] = shape
				} else if implicit {
					continue
				}
				index = shape.index
				if implicit {
					name, selection = d2format.Format(path), path.Range
					childrenKind = 0
				}
			default:
				continue
			}
		}
		if name == "" {
			name = "(empty)"
		}
		if selection.Start.Before(key.Range.Start) || key.Range.End.Before(selection.End) || selection.End.Before(selection.Start) {
			selection = key.Range
		}
		sym := documentSymbol{
			Name: name, Kind: kind,
			Range: toLSPRange(key.Range), SelectionRange: toLSPRange(selection),
		}
		if shape != nil && shape.explicit {
			// Keep the first explicit declaration as the navigation target,
			// and extend its range to include children declared later.
			sym = out[index]
			sym.Range.End = toLSPRange(key.Range).End
		}
		if !implicit && (kind == objectSymbol || kind == operatorSymbol) {
			value := key.Primary.Unbox()
			if value == nil {
				value = key.Value.ScalarBox().Unbox()
			}
			if value != nil {
				sym.Detail = value.ScalarString()
			}
		}
		if shape != nil && !implicit {
			shape.explicit = true
			if childMap := key.Value.Map; childMap != nil {
				shape.children = append(shape.children, childMap.Nodes...)
			}
		} else if childrenKind != 0 && key.Value.Map != nil {
			sym.Children = mapSymbols(key.Value.Map.Nodes, childrenKind, kind == packageSymbol)
		}
		if index == len(out) {
			out = append(out, sym)
		} else {
			out[index] = sym
		}
	}
	// D2 merges maps from repeated declarations. Outline their children in one
	// scope so nested shapes can also be declared across multiple blocks.
	for _, shape := range objects {
		if len(shape.children) > 0 {
			out[shape.index].Children = mapSymbols(shape.children, objectSymbol, false)
		}
	}
	return out
}

// After a shape name, a reserved attribute starts a property assignment.
// Board keywords in this position are ordinary child shape names.
func shapePrefix(path *d2ast.KeyPath) *d2ast.KeyPath {
	for i := 1; i < len(path.Path); i++ {
		part := path.Path[i]
		keyword := d2ast.Keyword(part.Unbox())
		if _, board := d2ast.BoardKeywords[keyword]; keyword != "" && !board {
			prefix := *path
			prefix.Path = path.Path[:i]
			prefix.Range.End = prefix.Last().Unbox().GetRange().End
			return &prefix
		}
	}
	return path
}

// Quoting and letter case do not distinguish shapes with the same path.
// FormatKeyPath retains the distinction between a.b and a single name "a.b".
func shapeSymbolID(path *d2ast.KeyPath) string {
	return strings.Map(func(r rune) rune {
		first := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			first = min(first, next)
		}
		return first
	}, d2format.FormatKeyPath(path.IDA()))
}
