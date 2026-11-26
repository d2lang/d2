package lspserver

import (
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
)

// A substitution takes its variables from the map containing its value. A
// dotted assignment and an edge label can both be several scopes below the
// nearest written set of braces, so start from the owner in the compiled IR.
func variableAt(snap *snapshot, board *d2ir.Map, key *d2ast.Key, sub *d2ast.Substitution) *d2ir.Field {
	scope := board
	owner := substitutionOwner(snap, board, key)
	if owner != nil {
		scope = d2ir.ParentMap(owner)
		if sub.Spread && key.Value.Map != nil && contains(key.Value.Map.Range, toLSPRange(sub.Range).Start) {
			scope = owner.Map()
		}
	}
	var scopes []*d2ir.Map
	for m := scope; m != nil; m = d2ir.ParentMap(m) {
		if vars := m.GetField(d2ast.FlatUnquotedString("vars")); vars.Map() != nil {
			scopes = append(scopes, vars.Map())
		}
	}

	// The compiler searches all scopes for the requested spelling before
	// trying the other quoting of a reserved word.
	for _, fallback := range []bool{false, true} {
		for i, vars := range scopes {
			field := variableField(vars, sub.Path, fallback)
			if field != nil && !(i == 0 && field == owner) {
				return field
			}
		}
	}
	return nil
}

func substitutionOwner(snap *snapshot, board *d2ir.Map, key *d2ast.Key) d2ir.Node {
	if key == nil {
		return nil
	}
	var start d2ast.Range
	path := attributePath(key)
	isEdge := len(key.Edges) > 0 && path == nil
	if isEdge {
		start = key.Edges[0].Range
	} else if path != nil && len(path.Path) > 0 && path.Last() != nil && path.Last().Unbox() != nil {
		start = path.Last().Unbox().GetRange()
	} else {
		return nil
	}
	for _, o := range snap.index {
		if o.near != nil || o.rng.Path != start.Path || o.rng.Start != start.Start {
			continue
		}
		var owner d2ir.Node
		if !isEdge && o.field != nil {
			owner = o.field
		} else if isEdge && o.edge != nil {
			owner = o.edge
		}
		if owner != nil {
			if b := d2ir.ParentBoard(owner); b != nil && b.Map() == board {
				return owner
			}
		}
	}
	return nil
}

func variableField(scope *d2ir.Map, path []*d2ast.StringBox, fallback bool) *d2ir.Field {
	var field *d2ir.Field
	for _, part := range path {
		if scope == nil || part == nil || part.Unbox() == nil {
			return nil
		}
		name := part.Unbox()
		field = scope.GetField(name)
		if _, reserved := d2ast.ReservedKeywords[strings.ToLower(name.ScalarString())]; reserved {
			if field != nil && field.Name.IsUnquoted() != name.IsUnquoted() && !fallback {
				return nil
			}
			if field == nil && fallback {
				if name.IsUnquoted() {
					field = scope.GetField(d2ast.FlatDoubleQuotedString(name.ScalarString()))
				} else {
					field = scope.GetField(d2ast.FlatUnquotedString(name.ScalarString()))
				}
			}
		}
		scope = field.Map()
	}
	return field
}
