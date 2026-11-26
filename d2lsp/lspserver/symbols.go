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
		index        int
		explicit     bool
		childrenKind int
		children     []d2ast.MapNodeBox
	}
	type symbolID struct {
		kind int
		path string
	}
	entries := make(map[symbolID]*entry)
	for _, node := range nodes {
		key := node.MapKey
		if key == nil || key.EdgeIndex != nil || key.EdgeKey != nil {
			continue
		}
		kind, childrenKind := defaultKind, objectSymbol
		key = splitNamespaceKey(key, defaultKind, boardRoot)
		path := key.Key
		index := len(out)
		implicit := false
		var item *entry
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
				path = shapePrefix(key.Key)
				implicit = path != key.Key
				if implicit {
					name, selection = d2format.Format(path), path.Range
					childrenKind = 0
				}
			default:
				continue
			}
		}
		if path != nil && len(key.Edges) == 0 {
			id := symbolID{kind, shapeSymbolID(path)}
			item = entries[id]
			if item == nil {
				item = &entry{index: index}
				entries[id] = item
			} else if implicit {
				continue
			}
			index = item.index
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
		if item != nil && item.explicit {
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
		if item != nil && !implicit {
			item.explicit = true
			item.childrenKind = childrenKind
			if childMap := key.Value.Map; childrenKind != 0 && childMap != nil {
				item.children = append(item.children, childMap.Nodes...)
			}
		}
		if index == len(out) {
			out = append(out, sym)
		} else {
			out[index] = sym
		}
	}
	// D2 merges maps from repeated declarations. Outline their children in one
	// scope so nested shapes can also be declared across multiple blocks.
	for _, item := range entries {
		if len(item.children) > 0 {
			sym := &out[item.index]
			sym.Children = mapSymbols(item.children, item.childrenKind, sym.Kind == packageSymbol)
		}
	}
	return out
}

// A dotted declaration such as layers.detail is equivalent to layers: {detail}.
// Split off namespace and member names for the outline without changing the
// editor's AST. Ordinary dotted shape names retain their source spelling.
func splitNamespaceKey(key *d2ast.Key, kind int, boardRoot bool) *d2ast.Key {
	path := key.Key
	if path == nil || len(path.Path) < 2 || len(key.Edges) > 0 {
		return key
	}
	if kind != packageSymbol && kind != classSymbol {
		keyword := d2ast.Keyword(path.Path[0].Unbox())
		_, board := d2ast.BoardKeywords[keyword]
		if kind != objectSymbol || !(keyword == "classes" || keyword == "vars" || (boardRoot && board)) {
			return key
		}
	}
	head, tail := *path, *path
	head.Path, tail.Path = path.Path[:1], path.Path[1:]
	head.Range.End = head.Path[0].Unbox().GetRange().End
	tail.Range.Start = tail.Path[0].Unbox().GetRange().Start
	child := *key
	child.Key, child.Range.Start = &tail, tail.Range.Start
	return &d2ast.Key{
		Key: &head, Range: key.Range,
		Value: d2ast.ValueBox{Map: &d2ast.Map{Nodes: []d2ast.MapNodeBox{{MapKey: &child}}}},
	}
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
