package lspserver

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2ir"
)

func (s *Server) definition(doc document, snap *snapshot, pos lspPosition) *location {
	nodes := nodePathAt(snap.ast, pos)
	var key *d2ast.Key
	var sub *d2ast.Substitution
	for _, n := range nodes {
		switch n := n.(type) {
		case *d2ast.Import:
			return s.importLocation(doc, n)
		case *d2ast.Key:
			key = n // the innermost key
		case *d2ast.Substitution:
			sub = n
		}
	}
	if snap.ir == nil {
		return nil
	}
	if class := classAt(key, pos); class != nil {
		parts := []d2ast.String{d2ast.FlatUnquotedString("classes"), d2ast.RawString(class.ScalarString(), true)}
		return s.declaration(doc, snap, nodes, parts)
	}
	if sub == nil && key != nil {
		// An interpolated string's range can end before its substitution.
		// Look inside the key's values even when nodePathAt stopped at the key.
		d2ast.Walk(key, func(n d2ast.Node) bool {
			switch n := n.(type) {
			case *d2ast.Map:
				return false
			case *d2ast.Substitution:
				if contains(n.Range, pos) {
					sub = n
				}
			}
			return sub == nil
		})
	}
	if sub != nil && len(sub.Path) != 0 {
		board := boardAt(snap.ir, nodes)
		field := variableAt(snap, board, key, sub)
		return s.fieldDeclaration(doc, snap, board, field)
	}

	sym := snap.symbolAt(doc.path, pos)
	if sym == nil || sym.hit.kind == kindKeyword || len(sym.occurrences) == 0 {
		return nil
	}
	loc := s.sourceLocation(doc, sym.occurrences[0].rng)
	return &loc
}

func classAt(key *d2ast.Key, pos lspPosition) d2ast.Scalar {
	if key == nil {
		return nil
	}
	path := attributePath(key)
	if path == nil || len(path.Path) == 0 || path.Last() == nil || d2ast.Keyword(path.Last().Unbox()) != "class" {
		return nil
	}
	if value := key.Primary.Unbox(); value != nil && contains(value.GetRange(), pos) {
		return value
	}
	if value := key.Value.ScalarBox().Unbox(); value != nil && contains(value.GetRange(), pos) {
		return value
	}
	if key.Value.Array != nil {
		for _, item := range key.Value.Array.Nodes {
			if value, ok := item.Unbox().(d2ast.Scalar); ok && contains(value.GetRange(), pos) {
				return value
			}
		}
	}
	return nil
}

func (s *Server) declaration(doc document, snap *snapshot, nodes []d2ast.Node, path []d2ast.String) *location {
	board := boardAt(snap.ir, nodes)
	field := board.GetField(path...)
	if field == nil && board != snap.ir {
		board = snap.ir
		field = board.GetField(path...)
	}
	return s.fieldDeclaration(doc, snap, board, field)
}

func (s *Server) fieldDeclaration(doc document, snap *snapshot, board *d2ir.Map, field *d2ir.Field) *location {
	if field == nil {
		return nil
	}
	// A board-local override retains its inherited references. Prefer the
	// first declaration on this board, falling back to the inherited one.
	var first *location
	for _, ref := range field.References {
		if ref.DueToGlob() || ref.DueToLazyGlob() {
			continue
		}
		name, ok := ref.AST().(d2ast.String)
		if !ok || isNilNode(name) {
			continue
		}
		r := name.GetRange()
		loc := s.sourceLocation(doc, r)
		if first == nil {
			first = &loc
		}
		if board == snap.ir || len(field.References) == 1 {
			return &loc
		}
		if r.Path == doc.path && boardAt(snap.ir, nodePathAt(snap.ast, loc.Range.Start)) == board {
			return &loc
		}
	}
	return first
}

// Walk the board kind and name pairs under the cursor. Each pair may be written
// as a dotted key or across nested maps. A board name alone is ambiguous when
// a layer and scenario share it.
func boardAt(root *d2ir.Map, nodes []d2ast.Node) *d2ir.Map {
	board := root
	var kind d2ast.String
	for _, n := range nodes {
		key, ok := n.(*d2ast.Key)
		if !ok || key.Key == nil {
			continue
		}
		for _, part := range key.Key.Path {
			if part == nil || part.Unbox() == nil {
				return board
			}
			name := part.Unbox()
			if kind == nil {
				if _, ok := d2ast.BoardKeywords[d2ast.Keyword(name)]; !ok {
					return board
				}
				kind = name
				continue
			}
			child := board.GetField(kind, name)
			if child == nil || child.Map() == nil {
				return board
			}
			board, kind = child.Map(), nil
		}
	}
	return board
}

func (s *Server) sourceLocation(doc document, r d2ast.Range) location {
	return location{URI: s.uriForPath(doc, r.Path), Range: toLSPRange(r)}
}

func (s *Server) references(doc document, snap *snapshot, pos lspPosition, includeDeclaration bool) []location {
	if snap.ir == nil {
		return nil
	}
	sym := snap.symbolAt(doc.path, pos)
	if sym == nil || sym.hit.kind == kindKeyword {
		return []location{}
	}
	locs := make([]location, 0, len(sym.occurrences))
	for i, o := range sym.occurrences {
		if i != 0 || includeDeclaration {
			locs = append(locs, s.sourceLocation(doc, o.rng))
		}
	}
	return locs
}

func prepareRename(doc document, snap *snapshot, pos lspPosition) *prepareRenameResult {
	sym := snap.symbolAt(doc.path, pos)
	if sym == nil || sym.hit.kind != kindObject {
		return nil
	}
	return &prepareRenameResult{Range: toLSPRange(sym.hit.rng), Placeholder: sym.hit.name.ScalarString()}
}

func (s *Server) rename(ctx context.Context, doc document, snap *snapshot, pos lspPosition, newName string) (*workspaceEdit, *rpcError) {
	sym := snap.symbolAt(doc.path, pos)
	if sym == nil || sym.hit.kind != kindObject {
		return nil, &rpcError{-32803, "only shapes can be renamed"}
	}
	if newName == "" || strings.ContainsAny(newName, "\r\n") {
		return nil, &rpcError{-32803, "invalid name"}
	}
	for _, use := range sym.occurrences {
		if root := snap.unresolvedSources[use.rng.Path]; root != "" {
			return nil, &rpcError{-32803, fmt.Sprintf("cannot resolve references in %s", root)}
		}
		if use.near != nil && use.near.computed {
			return nil, &rpcError{-32803, "cannot rename this shape because a near value referring to it is computed"}
		}
	}
	if snap.sharedNearTarget(sym) {
		return nil, &rpcError{-32803, "a shared near value refers to different shapes on different boards"}
	}
	if reason := s.renameConflict(ctx, doc, snap, sym, newName); reason != "" {
		return nil, &rpcError{-32803, reason}
	}
	for owner := range sym.fields {
		parent, ok := owner.Parent().(*d2ir.Map)
		if !ok {
			return nil, &rpcError{-32803, "cannot resolve the containing map for this shape"}
		}
		for _, sibling := range parent.Fields {
			if sibling != owner && sibling.Name != nil && strings.EqualFold(sibling.Name.ScalarString(), newName) {
				return nil, &rpcError{-32803, fmt.Sprintf("%q already exists in this container", newName)}
			}
		}
	}

	name := d2ast.RawString(newName, true)
	_, reserved := d2ast.ReservedKeywords[strings.ToLower(newName)]
	if reserved || newName == "_" || strings.EqualFold(newName, "null") {
		// A bare underscore selects a parent. The formatter canonicalizes
		// an unquoted null to lowercase, even when used as a shape name.
		name = d2ast.FlatDoubleQuotedString(newName)
	}
	// Format a key path to use key escaping rules for literal dollars.
	path := &d2ast.KeyPath{Path: []*d2ast.StringBox{d2ast.MakeValueBox(name).StringBox()}}
	replacement := d2format.Format(path)
	changes := make(map[string][]textEdit)
	for _, o := range sym.occurrences {
		uri := s.uriForPath(doc, o.rng.Path)
		change := textEdit{Range: toLSPRange(o.rng), NewText: replacement}
		if o.near != nil {
			change = o.near.rename(name)
		}
		changes[uri] = append(changes[uri], change)
	}
	if !s.documentChanges {
		return &workspaceEdit{Changes: changes}, nil
	}
	edit := &workspaceEdit{}
	for _, uri := range slices.Sorted(maps.Keys(changes)) {
		target := optionalVersionedTextDocumentIdentifier{URI: uri}
		if open, ok := s.document(uri); ok {
			target.Version = &open.version
		}
		edit.DocumentChanges = append(edit.DocumentChanges, textDocumentEdit{TextDocument: target, Edits: changes[uri]})
	}
	return edit, nil
}
