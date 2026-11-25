package d2oracle

import (
	"errors"
	"slices"
	"strings"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

// The editor receives both render-target IDs and scoped D2 keys. A frame
// shares actors with its diagram, but keeps its own message indexes in source.
func sequenceEditEdge(g *d2graph.Graph, mk *d2ast.Key) *d2graph.Edge {
	if len(mk.Edges) != 1 || mk.EdgeIndex == nil || mk.EdgeIndex.Int == nil {
		return nil
	}
	scope := g.Root
	inside := scope.IsSequenceDiagramV2()
	if mk.Key != nil {
		for _, part := range d2graph.Key(mk.Key) {
			scope = scope.Children[strings.ToLower(part)]
			if scope == nil {
				break
			}
			inside = inside || scope.IsSequenceDiagramV2()
		}
	}
	if !inside {
		return nil
	}
	if scope != nil {
		if edge, ok := scope.HasEdge(mk); ok {
			if edge.SequenceKey != nil {
				return edge
			}
			return nil
		}
	}
	for _, edge := range g.Edges {
		if key := edge.SequenceKey; key != nil && *key.EdgeIndex.Int == *mk.EdgeIndex.Int && sameMessageFamily(key, mk) {
			return edge
		}
	}
	return nil
}

// Topology operations use the shared actor graph, even when the caller uses
// a frame-local edge index.
func sequenceGraphKey(g *d2graph.Graph, mk *d2ast.Key) *d2ast.Key {
	if edge := sequenceEditEdge(g, mk); edge != nil {
		key, _ := d2parser.ParseMapKey(edge.AbsID())
		key.EdgeKey = mk.EdgeKey
		return key
	}
	return mk
}

func sameMessageFamily(a, b *d2ast.Key) bool {
	if !equalSequencePath(a.Key, b.Key) {
		return false
	}
	ae, be := d2ir.NewEdgeIDs(a)[0], d2ir.NewEdgeIDs(b)[0]
	ae.Index = nil
	return ae.Match(be)
}

func equalSequencePath(a, b *d2ast.KeyPath) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Path) != len(b.Path) {
		return false
	}
	for i := range a.Path {
		if !strings.EqualFold(a.Path[i].Unbox().ScalarString(), b.Path[i].Unbox().ScalarString()) {
			return false
		}
	}
	return true
}

// Variable substitutions and spreads refer to the same template AST. A write
// to a template declaration would modify every occurrence of the message.
func variableSequenceMessage(edge *d2graph.Edge) bool {
	for _, ref := range edge.References {
		if ref.IsVar && ref.MapKey.EdgeIndex == nil {
			return true
		}
	}
	return false
}

func localSequenceDeclaration(edge *d2graph.Edge, ast *d2ast.Map) bool {
	if len(edge.References) != 1 {
		return false
	}
	r := edge.References[0]
	return r.ScopeAST == ast && r.MapKey.Range.Path == ast.Range.Path && !r.IsVar &&
		r.MapKey.EdgeIndex == nil && len(r.MapKey.Edges) == 1 && !r.MapKey.HasGlob()
}

// Set and reset only this materialized message, even if its source is a chain,
// a glob, an imported map, or an inherited board. Keeping the override at the
// board scope also avoids writing to a shared template.
func writeSequenceOverride(ast *d2ast.Map, edge *d2graph.Edge, change *d2ast.Key) {
	key := *edge.SequenceKey
	key.EdgeIndex = &d2ast.EdgeIndex{Int: go2.Pointer(*key.EdgeIndex.Int)}
	key.EdgeKey, key.Value = change.EdgeKey, change.Value
	if change.EdgeKey != nil && change.Value.Null != nil {
		// A flat indexed key with a null value deletes the whole edge in D2.
		key.EdgeKey = nil
		key.Value = d2ast.MakeValueBox(&d2ast.Map{
			Range: d2ast.MakeRange(",1:0:0-1:0:0"),
			Nodes: []d2ast.MapNodeBox{{MapKey: &d2ast.Key{Key: change.EdgeKey, Value: change.Value}}},
		})
		appendMapKey(ast, &key)
		return
	}
	// A repeated edit can replace the last local assignment in place.
	if change.Value.Null == nil && len(ast.Nodes) > 0 {
		last := ast.Nodes[len(ast.Nodes)-1].MapKey
		if last != nil && len(last.Edges) == 1 && last.EdgeIndex != nil && last.EdgeIndex.Int != nil &&
			*last.EdgeIndex.Int == *key.EdgeIndex.Int && sameMessageFamily(last, &key) &&
			equalSequencePath(last.EdgeKey, key.EdgeKey) {
			last.Value = key.Value
			return
		}
	}
	appendMapKey(ast, &key)
}

func deleteSequenceMessage(g *d2graph.Graph, ast *d2ast.Map, edge *d2graph.Edge, key *d2ast.Key) {
	key.Value = d2ast.MakeValueBox(&d2ast.Null{})
	// A reset applies to one field, including inherited styles and values
	// supplied by wildcard setters.
	if key.EdgeKey != nil {
		writeSequenceOverride(ast, edge, key)
		return
	}
	for _, ref := range edge.References {
		if ref.MapKey.EdgeIndex == nil && (ref.IsVar || ref.ScopeAST != ast || ref.MapKey.Range.Path != ast.Range.Path ||
			ref.Edge.Src.HasGlob() || ref.Edge.Dst.HasGlob()) {
			writeSequenceOverride(ast, edge, key)
			return
		}
	}

	// The declaration can be physically removed. Preserve implicit endpoints,
	// and only shift explicit references in this source scope's index family.
	var removed []*d2ast.Edge
	for _, ref := range edge.References {
		removed = append(removed, ref.Edge)
	}
	for i := len(edge.References) - 1; i >= 0; i-- {
		ref := edge.References[i]
		if ref.ScopeAST != ast || ref.IsVar || ref.Edge.Src.HasGlob() || ref.Edge.Dst.HasGlob() ||
			ref.MapKey.EdgeIndex != nil && ref.MapKey.EdgeIndex.Glob {
			continue
		}
		if ref.MapKey.EdgeIndex == nil {
			if !hasOtherReference(edge.Src, removed) {
				ensureNode(g, removed, ref.ScopeObj, ref.Scope, ref.MapKey, ref.Edge.Src, true)
			}
			if !hasOtherReference(edge.Dst, removed) {
				ensureNode(g, removed, ref.ScopeObj, ref.Scope, ref.MapKey, ref.Edge.Dst, false)
			}
		}
		deleteEdge(g, ref.Scope, ref.MapKey, ref.MapKeyEdgeIndex)
	}
	shiftSequenceReferences(g, ast, edge.SequenceKey, *edge.SequenceKey.EdgeIndex.Int+1, -1, edge)
}

func shiftSequenceReferences(g *d2graph.Graph, ast *d2ast.Map, family *d2ast.Key, from, delta int, except *d2graph.Edge) {
	for _, sibling := range g.Edges {
		k := sibling.SequenceKey
		if sibling == except || k == nil || *k.EdgeIndex.Int < from || !sameMessageFamily(k, family) {
			continue
		}
		for _, ref := range sibling.References {
			index := ref.MapKey.EdgeIndex
			if ref.ScopeAST == ast && !ref.IsVar && !ref.MapKey.HasGlob() && index != nil && index.Int != nil && *index.Int == *k.EdgeIndex.Int {
				*index.Int += delta
			}
		}
	}
}

// The source path can have a frame prefix (f.a), parent prefix (_.a), or be
// local (a). Replace just the participant portion, preserving that spelling.
func sequenceReconnectPath(path *d2ast.KeyPath, old, next *d2graph.Object) *d2ast.KeyPath {
	if next == nil {
		return path
	}
	oldKey, _ := d2parser.ParseKey(old.AbsID())
	newKey, _ := d2parser.ParseKey(next.AbsID())
	diagram := old.OuterSequenceDiagram()
	if diagram != nil && diagram == next.OuterSequenceDiagram() && diagram.Parent != nil {
		prefix, _ := d2parser.ParseKey(diagram.AbsID())
		oldKey.Path = oldKey.Path[len(prefix.Path):]
		newKey.Path = newKey.Path[len(prefix.Path):]
	}
	// Keep the authored qualification, such as a frame prefix or leading
	// underscores. For an implicit actor prefix, navigate from that actor
	// to the new participant or activation.
	n := matchingSequenceSuffix(path, oldKey)
	base := oldKey.Path[:len(oldKey.Path)-n]
	common := 0
	for common < len(base) && common < len(newKey.Path)-1 &&
		strings.EqualFold(base[common].Unbox().ScalarString(), newKey.Path[common].Unbox().ScalarString()) {
		common++
	}
	out := slices.Clone(path.Path[:len(path.Path)-n])
	for i := common; i < len(base); i++ {
		out = append(out, d2ast.MakeValueBox(d2ast.FlatUnquotedString("_")).StringBox())
	}
	return &d2ast.KeyPath{Path: append(out, newKey.Path[common:]...)}
}

func matchingSequenceSuffix(a, b *d2ast.KeyPath) int {
	n := 0
	for n < len(a.Path) && n < len(b.Path) && strings.EqualFold(a.Path[len(a.Path)-n-1].Unbox().ScalarString(), b.Path[len(b.Path)-n-1].Unbox().ScalarString()) {
		n++
	}
	return n
}

// Indexed overrides may spell an edge in reverse, including a symmetric
// arrow. Keep that spelling when reconnecting its endpoints.
func reconnectSequencePaths(e *d2ast.Edge, edge *d2graph.Edge, src, dst *d2graph.Object) {
	reversed := (e.SrcArrow != "") != edge.SrcArrow
	if edge.SrcArrow == edge.DstArrow && edge.Src != edge.Dst {
		s, _ := d2parser.ParseKey(edge.Src.AbsID())
		d, _ := d2parser.ParseKey(edge.Dst.AbsID())
		reversed = matchingSequenceSuffix(e.Src, d)+matchingSequenceSuffix(e.Dst, s) > matchingSequenceSuffix(e.Src, s)+matchingSequenceSuffix(e.Dst, d)
	}
	if reversed {
		e.Src = sequenceReconnectPath(e.Src, edge.Dst, dst)
		e.Dst = sequenceReconnectPath(e.Dst, edge.Src, src)
	} else {
		e.Src = sequenceReconnectPath(e.Src, edge.Src, src)
		e.Dst = sequenceReconnectPath(e.Dst, edge.Dst, dst)
	}
}

func validateSequenceReconnect(edge *d2graph.Edge, src, dst *d2graph.Object) error {
	for _, node := range []*d2graph.Object{src, dst} {
		if node != nil && (node.OuterSequenceDiagram() != edge.Src.OuterSequenceDiagram() || !node.IsSequenceDiagramActor() && !node.IsSequenceDiagramSpan()) {
			return errors.New("sequence diagram messages must connect actors or spans in the same diagram")
		}
	}
	return nil
}

func reconnectSequenceIndexes(g *d2graph.Graph, ast *d2ast.Map, edge *d2graph.Edge, src, dst *d2graph.Object) {
	next := *edge.SequenceKey
	endpoints := *next.Edges[0]
	reconnectSequencePaths(&endpoints, edge, src, dst)
	next.Edges = []*d2ast.Edge{&endpoints}
	if next.Key != nil {
		next.Key = cloneKey(next.Key)
	}
	edgeTrimCommon(&next)
	if sameMessageFamily(edge.SequenceKey, &next) {
		return
	}
	index := 0
	for _, other := range g.Edges {
		if other.SequenceKey != nil && sameMessageFamily(other.SequenceKey, &next) && other.SequenceGraphOrder < edge.SequenceGraphOrder {
			index++
		}
	}
	shiftSequenceReferences(g, ast, edge.SequenceKey, *edge.SequenceKey.EdgeIndex.Int+1, -1, edge)
	shiftSequenceReferences(g, ast, &next, index, 1, edge)
	for _, r := range edge.References {
		if r.ScopeAST == ast && !r.IsVar && !r.MapKey.HasGlob() && r.MapKey.EdgeIndex != nil && r.MapKey.EdgeIndex.Int != nil {
			*r.MapKey.EdgeIndex.Int = index
		}
	}
}
