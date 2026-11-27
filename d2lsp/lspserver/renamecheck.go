package lspserver

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

// A compiled IR deliberately omits deleted and suspended fields. Selective
// imports also retain the selected value without a field reference for the
// selector. Do not send partial edits when such a spelling can bind the name.
func (s *Server) renameConflict(ctx context.Context, doc document, snap *snapshot, sym *symbol, newName string) string {
	authored := make(map[d2ast.Range]bool)
	editedPaths := make(map[string]bool)
	for _, o := range sym.occurrences {
		authored[o.rng] = true
		if o.near == nil && o.name != nil {
			authored[o.name.GetRange()] = true
		}
		editedPaths[o.rng.Path] = true
	}
	if path := s.conflictingBuffer(editedPaths); path != "" {
		return fmt.Sprintf("%s is open in multiple buffers with different contents", path)
	}
	oldName := sym.hit.name.ScalarString()
	ancestorFields := make(map[*d2ir.Field]bool)
	ownerMaps := make(map[*d2ir.Map]bool)
	for f := range sym.fields {
		ownerMaps[d2ir.ParentMap(f)] = true
		for parent := d2ir.ParentField(f); parent != nil; parent = d2ir.ParentField(parent) {
			ancestorFields[parent] = true
		}
	}
	ancestorGroups := make(map[int]bool)
	indexed := make(map[d2ast.Range]int)
	for _, o := range snap.index {
		if n := o.near; n != nil && ownerMaps[n.constantScope] && strings.EqualFold(o.name.ScalarString(), newName) {
			// This placement currently resolves to a constant. A renamed sibling
			// in the same board could turn it into an object-relative placement.
			return "the new name would capture a near placement constant"
		}
		if o.group < 0 {
			continue
		}
		if o.kind != kindObject && authored[o.rng] {
			return "this shape shares its declaration with a different kind of symbol"
		}
		if o.kind == kindObject && o.near == nil {
			indexed[o.name.GetRange()] = o.group
			if ancestorFields[o.field] {
				ancestorGroups[o.group] = true
			}
		}
	}

	// Resolve a selective import against its own file. Its target may be a
	// shape re-exported from another file through a chain of spread imports.
	files := map[string]*renameFile{doc.path: {doc, snap}}
	for path := range snap.imports {
		if path == doc.path {
			continue
		}
		file, err := s.renameFile(ctx, path)
		if err != nil {
			return err.Error()
		}
		if file != nil {
			files[path] = file
		}
	}
	for path, file := range files {
		if file.snap.ast == nil {
			continue
		}
		var reason string
		d2ast.Walk(file.snap.ast, func(n d2ast.Node) bool {
			if reason != "" {
				return false
			}
			switch n := n.(type) {
			case *d2ast.Import:
				if len(n.Path) > 1 {
					target := files[importPath(path, n)]
					reason = s.selectiveImportConflict(ctx, target, n.IDA(), oldName, authored)
				}
			case *d2ast.Key:
				if removedFieldConflict(file.snap.ast, n, oldName, newName, indexed, ancestorGroups) {
					reason = "a removed or suspended reference with this name cannot be resolved"
				}
			}
			return reason == ""
		})
		if reason != "" {
			return reason
		}
	}
	return ""
}

func (s *Server) selectiveImportConflict(ctx context.Context, target *renameFile, names []d2ast.String, oldName string, authored map[d2ast.Range]bool) string {
	if target == nil {
		return ""
	}
	for i, part := range names {
		if !strings.EqualFold(part.ScalarString(), oldName) {
			continue
		}
		s.ensureIR(ctx, target.doc, target.snap)
		if target.snap.ir == nil {
			return "cannot resolve a selective import using this name"
		}
		selected := target.snap.ir.GetField(names[:i+1]...)
		if selected == nil {
			continue
		}
		for _, ref := range selected.References {
			if node := ref.AST(); !isNilNode(node) && authored[node.GetRange()] {
				return "a selective import uses this shape name"
			}
		}
	}
	return ""
}

func removedFieldConflict(ast *d2ast.Map, key *d2ast.Key, oldName, newName string, indexed map[d2ast.Range]int, ancestors map[int]bool) bool {
	if key.Value.Null == nil && key.Primary.Null == nil && key.Value.Suspension == nil && key.Primary.Suspension == nil {
		return false
	}
	// The IR can discard the endpoints along with a removed edge. Its shared
	// prefix and each endpoint are shape paths, just like a removed field.
	conflicts := func(path *d2ast.KeyPath, shared *d2ast.KeyPath) bool {
		if path == nil {
			return false
		}
		for i, box := range path.Path {
			if box == nil || box.Unbox() == nil {
				continue
			}
			name := box.Unbox()
			if _, resolved := indexed[name.GetRange()]; resolved {
				continue
			}
			if strings.EqualFold(name.ScalarString(), oldName) || strings.EqualFold(name.ScalarString(), newName) {
				prefix := path.Path[:i]
				if shared != nil {
					prefix = slices.Concat(shared.Path, prefix)
				}
				if scope, known := removalScope(ast, key, prefix, indexed); !known || ancestors[scope] {
					return true
				}
			}
		}
		return false
	}
	if conflicts(key.Key, nil) {
		return true
	}
	for _, e := range key.Edges {
		if e != nil && (conflicts(e.Src, key.Key) || conflicts(e.Dst, key.Key)) {
			return true
		}
	}
	return false
}

// A surviving authored prefix identifies the container even after the IR has
// discarded the last component. It may occur in this dotted key or outside its
// braces. Parent selectors or globs make that syntactic prefix unreliable.
func removalScope(ast *d2ast.Map, key *d2ast.Key, prefix []*d2ast.StringBox, indexed map[d2ast.Range]int) (int, bool) {
	nodes := nodePathAt(ast, toLSPRange(key.Range).Start)
	group, known := 0, false
	for _, n := range nodes {
		k, ok := n.(*d2ast.Key)
		if !ok {
			continue
		}
		var path []*d2ast.StringBox
		if k == key {
			path = prefix
		} else if k.Key != nil {
			path = k.Key.Path
		}
		for _, box := range path {
			if box == nil || box.Unbox() == nil {
				return 0, false
			}
			name := box.Unbox()
			if g, ok := indexed[name.GetRange()]; ok {
				group, known = g, true
			} else {
				known = false
			}
		}
		if k == key {
			break
		}
	}
	return group, known
}

type renameFile struct {
	doc  document
	snap *snapshot
}

func (s *Server) renameFile(ctx context.Context, path string) (*renameFile, error) {
	if doc, open := s.documentForPath(path); open {
		if len(doc.text) > maxDocumentBytes {
			return nil, fmt.Errorf("cannot check rename in oversized file %s", path)
		}
		return &renameFile{doc, s.analyze(ctx, doc)}, nil
	}
	file, err := s.overlay.Open(path)
	if err != nil {
		return nil, nil // unresolved import in a partial document
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil || len(data) > maxDocumentBytes {
		return nil, fmt.Errorf("cannot check rename in imported file %s", path)
	}
	doc := document{uri: pathToURI(path), path: path, text: string(data)}
	ast, err := d2parser.ParseContext(ctx, path, strings.NewReader(doc.text), &d2parser.ParseOptions{UTF16Pos: true})
	return &renameFile{doc, &snapshot{ast: ast, parseErr: err}}, nil
}
