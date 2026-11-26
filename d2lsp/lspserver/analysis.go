package lspserver

import (
	"context"
	"io/fs"
	"reflect"
	"runtime/debug"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

// Snapshots belong to the request goroutine. An edit to any open file or a
// disk change can invalidate imports even if this document's version is stable.
type snapshot struct {
	epoch       uint64
	version     int32
	ast         *d2ast.Map
	parseErr    error
	attemptedIR bool
	ir          *d2ir.Map
	index       []occurrence
}

func (s *Server) analyze(ctx context.Context, doc document) *snapshot {
	if cached := s.snapshots[doc.uri]; cached != nil && cached.epoch == s.epoch && cached.version == doc.version {
		return cached
	}
	root, err := d2parser.ParseContext(ctx, doc.path, strings.NewReader(doc.text), &d2parser.ParseOptions{UTF16Pos: true})
	snap := &snapshot{epoch: s.epoch, version: doc.version, ast: root, parseErr: err}
	s.snapshots[doc.uri] = snap
	return snap
}

// IR compilation can rewrite interpolated scalars in its input. Keep the
// editor's original AST for features such as go to definition on a substitution.
func (s *Server) ensureIR(ctx context.Context, doc document, snap *snapshot) {
	if snap.attemptedIR {
		return
	}
	snap.attemptedIR = true
	defer func() {
		if v := recover(); v != nil {
			s.log.Printf("analyzing %s: %v\n%s", doc.uri, v, debug.Stack())
		}
	}()
	input, _ := d2parser.ParseContext(ctx, doc.path, strings.NewReader(doc.text), &d2parser.ParseOptions{UTF16Pos: true})
	if input == nil {
		return
	}
	off := false
	ir, _, _ := d2ir.Compile(input, &d2ir.CompileOptions{
		Context: ctx, UTF16Pos: true, FS: s.fileSystem(doc), Partial: true, AnimateTweens: &off,
	})
	snap.index = s.buildIndex(doc, ir)
	snap.ir = ir
}

// AST children and IR references can contain typed nil pointers.
func isNilNode(n d2ast.Node) bool {
	return n == nil || reflect.ValueOf(n).IsNil()
}

func contains(r d2ast.Range, pos lspPosition) bool {
	bounds := toLSPRange(r)
	return comparePosition(bounds.Start, pos) <= 0 && comparePosition(pos, bounds.End) <= 0
}

func nodePathAt(root d2ast.Node, pos lspPosition) []d2ast.Node {
	var path []d2ast.Node
	for n := root; !isNilNode(n) && contains(n.GetRange(), pos); {
		path = append(path, n)
		var next d2ast.Node
		for _, child := range n.Children() {
			if !isNilNode(child) && contains(child.GetRange(), pos) {
				next = child
				break
			}
		}
		n = next
	}
	return path
}

type symbolKind int

const (
	kindObject symbolKind = iota
	kindKeyword
	kindClass
	kindVariable
	kindBoard
	kindEdge
)

type occurrence struct {
	rng   d2ast.Range
	field *d2ir.Field
	edge  *d2ir.Edge
	kind  symbolKind
	glob  bool
	name  d2ast.String // nil for edges
	group int          // shared by all copies of an authored symbol
}

type walkContext int

const (
	ctxObjects walkContext = iota
	ctxVariables
	ctxClasses
	ctxBoards
	ctxKeywords
)

func (s *Server) buildIndex(doc document, root *d2ir.Map) []occurrence {
	var out []occurrence
	type scope struct {
		m   *d2ir.Map
		ctx walkContext
	}
	seen := make(map[scope]bool)
	lines := make(map[string][]string)
	var walk func(m *d2ir.Map, ctx walkContext)
	walk = func(m *d2ir.Map, ctx walkContext) {
		key := scope{m, ctx}
		if m == nil || seen[key] {
			return
		}
		seen[key] = true
		for _, f := range m.Fields {
			kind, next := classify(ctx, d2ast.Keyword(f.Name))
			for _, ref := range f.References {
				name, ok := ref.AST().(d2ast.String)
				if !ok || isNilNode(name) {
					continue
				}
				if name.IsUnquoted() && name.ScalarString() == "_" {
					continue
				}
				out = append(out, occurrence{
					rng: s.fieldRange(doc, name, lines), field: f, kind: kind, name: name,
					glob: ref.DueToGlob() || ref.DueToLazyGlob(),
				})
			}
			walk(f.Map(), next)
		}
		for _, e := range m.Edges {
			for _, ref := range e.References {
				n := ref.AST()
				if isNilNode(n) {
					continue
				}
				out = append(out, occurrence{
					rng: n.GetRange(), edge: e, kind: kindEdge,
					glob: ref.DueToGlob() || ref.DueToLazyGlob(),
				})
			}
			walk(e.Map(), ctxKeywords)
		}
	}
	walk(root, ctxObjects)
	groupSymbols(out)
	return out
}

func classify(ctx walkContext, keyword string) (symbolKind, walkContext) {
	switch ctx {
	case ctxVariables:
		return kindVariable, ctxVariables
	case ctxClasses:
		return kindClass, ctxKeywords
	case ctxBoards:
		return kindBoard, ctxObjects
	case ctxKeywords:
		return kindKeyword, ctxKeywords
	}
	switch {
	case keyword == "vars":
		return kindKeyword, ctxVariables
	case keyword == "classes":
		return kindKeyword, ctxClasses
	case keyword == "layers" || keyword == "scenarios" || keyword == "steps":
		return kindKeyword, ctxBoards
	case keyword != "":
		return kindKeyword, ctxKeywords
	}
	return kindObject, ctxObjects
}

type symbol struct {
	hit         occurrence
	occurrences []occurrence
	fields      map[*d2ir.Field]bool
}

func (snap *snapshot) symbolAt(path string, pos lspPosition) *symbol {
	// An edge covers both its arrow and its endpoints. Never let that wider
	// range turn a parent selector or a glob into an editable symbol.
	nodes := nodePathAt(snap.ast, pos)
	if len(nodes) > 0 {
		if s, ok := nodes[len(nodes)-1].(*d2ast.UnquotedString); ok && (s.ScalarString() == "_" || len(s.Pattern) > 0) {
			return nil
		}
	}
	var hit *occurrence
	for i := range snap.index {
		o := &snap.index[i]
		if o.group < 0 || o.rng.Path != path || !contains(o.rng, pos) {
			continue
		}
		if hit == nil || o.rng.End.Byte-o.rng.Start.Byte < hit.rng.End.Byte-hit.rng.Start.Byte {
			hit = o
		}
	}
	if hit == nil {
		return nil
	}

	// Scenario and step boards copy IR nodes. All owners of this spelling
	// contribute references, in compiler order, with shared ranges emitted once.
	sym := &symbol{hit: *hit, fields: make(map[*d2ir.Field]bool)}
	seen := make(map[d2ast.Range]bool)
	for _, o := range snap.index {
		if o.group != hit.group {
			continue
		}
		if o.field != nil {
			sym.fields[o.field] = true
		}
		if !seen[o.rng] {
			seen[o.rng] = true
			sym.occurrences = append(sym.occurrences, o)
		}
	}
	return sym
}

// Each scenario copy can share a base occurrence without sharing its siblings'
// board-local occurrences. Join by owner and by source range so navigation and
// edits reach the entire group regardless of which occurrence was selected.
func groupSymbols(index []occurrence) {
	type owner struct {
		field *d2ir.Field
		edge  *d2ir.Edge
		kind  symbolKind
	}
	type source struct {
		r    d2ast.Range
		kind symbolKind
	}
	owners := make(map[owner]int)
	sources := make(map[source]int)
	parent := make([]int, len(index))
	root := func(i int) int {
		for i != parent[i] {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for i := range index {
		o := &index[i]
		// Zero-width synthetic nodes have no authored spelling to link or edit.
		if o.glob || !o.rng.Start.Before(o.rng.End) {
			o.group = -1
			continue
		}
		parent[i] = i
		key := owner{o.field, o.edge, o.kind}
		if prev, ok := owners[key]; ok {
			parent[i] = root(prev)
		} else {
			owners[key] = i
		}
		site := source{o.rng, o.kind}
		if prev, ok := sources[site]; ok {
			parent[root(i)] = root(prev)
		} else {
			sources[site] = i
		}
	}
	for i := range index {
		if index[i].group >= 0 {
			index[i].group = root(i)
		}
	}
}

// For an unquoted key ending in an escape, the parser's end is just after
// the backslash. Include its operand only if the raw spelling confirms it.
func (s *Server) fieldRange(doc document, name d2ast.String, lines map[string][]string) d2ast.Range {
	r := name.GetRange()
	bare, ok := name.(*d2ast.UnquotedString)
	if !ok || len(bare.Value) != 1 || bare.Value[0].StringRaw == nil {
		return r
	}
	raw := *bare.Value[0].StringRaw
	last, n := utf8.DecodeLastRuneInString(raw)
	prefix := raw[:len(raw)-n]
	if (len(prefix)-len(strings.TrimRight(prefix, `\`)))%2 == 0 {
		return r
	}
	if r.OneLine() {
		if r.Start.AdvanceString(prefix, true) == r.End {
			r.End = r.End.Advance(last, true)
		}
	} else if s.escapedOperandAt(doc, r, last, lines) {
		// Raw strings omit folded line continuations. Check the source at the
		// actual endpoint before extending a multiline range.
		r.End = r.End.Advance(last, true)
	}
	return r
}

func (s *Server) escapedOperandAt(doc document, r d2ast.Range, operand rune, cache map[string][]string) bool {
	lines, ok := cache[r.Path]
	if !ok {
		if r.Path == doc.path {
			lines = strings.Split(doc.text, "\n")
		} else if text, err := fs.ReadFile(s.overlay, r.Path); err == nil {
			lines = strings.Split(string(text), "\n")
		}
		cache[r.Path] = lines
	}
	if r.End.Line >= len(lines) {
		return false
	}
	line := lines[r.End.Line]
	column := 0
	var previous rune
	for _, ch := range line {
		if column == r.End.Column {
			return previous == '\\' && ch == operand
		}
		previous = ch
		column += utf16.RuneLen(ch)
	}
	return false
}
