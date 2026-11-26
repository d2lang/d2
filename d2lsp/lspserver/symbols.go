package lspserver

import (
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
	return mapSymbols(root, objectSymbol)
}

// defaultKind describes keys directly inside this map. Shape/board contents
// are classified by D2 keywords; variable and class maps contain named data.
func mapSymbols(m *d2ast.Map, defaultKind int) []documentSymbol {
	out := []documentSymbol{}
	if m == nil {
		return out
	}
	for _, node := range m.Nodes {
		key := node.MapKey
		if key == nil {
			continue
		}
		kind, childrenKind := defaultKind, objectSymbol
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
			switch keyword {
			case "vars":
				name, kind, childrenKind = keyword, variableSymbol, variableSymbol
			case "classes":
				name, kind, childrenKind = keyword, namespaceSymbol, classSymbol
			case "layers", "scenarios", "steps":
				name, kind, childrenKind = keyword, namespaceSymbol, packageSymbol
			case "":
				// An ordinary shape.
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
		if kind == objectSymbol || kind == operatorSymbol {
			value := key.Primary.Unbox()
			if value == nil {
				value = key.Value.ScalarBox().Unbox()
			}
			if value != nil {
				sym.Detail = value.ScalarString()
			}
		}
		if childrenKind != 0 && key.Value.Map != nil {
			sym.Children = mapSymbols(key.Value.Map, childrenKind)
		}
		out = append(out, sym)
	}
	return out
}
