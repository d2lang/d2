package d2compiler

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2target"
)

func sequencePathKey(path []d2ast.String) string {
	parts := make([]string, len(path))
	for i, p := range path {
		parts[i] = strings.ToLower(p.ScalarString())
	}
	return strings.Join(parts, "\x00")
}

type sequenceClassProperty struct {
	class    *d2ir.Map
	branched bool
}

func (c *compiler) sequenceShape(m *d2ir.Map) string {
	return c.sequenceShapeValue(m, false)
}

func (c *compiler) sequenceShapeValue(m *d2ir.Map, branched bool) string {
	if m == nil || c.stopped() {
		return ""
	}
	path := []d2ast.String{d2ast.FlatUnquotedString("shape")}
	if branched {
		path = append(path, d2ast.FlatUnquotedString("branched"))
	}
	f := m.GetField(path...)
	if f != nil && f.Primary() != nil {
		return strings.ToLower(f.Primary().Value.ScalarString())
	}
	class := m.GetField(d2ast.FlatUnquotedString("class"))
	if class == nil {
		return ""
	}
	var names []string
	if class.Primary() != nil {
		names = append(names, class.Primary().String())
	} else if array, ok := class.Composite.(*d2ir.Array); ok {
		for _, value := range array.Values {
			if scalar, ok := value.(*d2ir.Scalar); ok {
				names = append(names, scalar.Value.ScalarString())
			}
		}
	}
	shape := ""
	for _, name := range names {
		classMap := c.getClassMap(m, name)
		if classMap == nil {
			continue
		}
		if c.sequenceClassValues == nil {
			c.sequenceClassValues = make(map[sequenceClassProperty]string)
		}
		property := sequenceClassProperty{classMap, branched}
		inherited, known := c.sequenceClassValues[property]
		if !known {
			// A temporary empty value also terminates cyclic lookups. The
			// normal class compiler reports cycles at their source keys.
			c.sequenceClassValues[property] = ""
			inherited = c.sequenceShapeValue(classMap, branched)
			c.sequenceClassValues[property] = inherited
		}
		if inherited != "" {
			shape = inherited
		}
	}
	return shape
}

func sequenceObjectField(f *d2ir.Field) bool {
	_, reserved := d2ast.ReservedKeywords[strings.ToLower(f.Name.ScalarString())]
	return !(reserved && f.Name.IsUnquoted())
}

// Normalize v2 message groups only after imports, variables, and boards have
// been resolved by the IR compiler. Legacy sequence_diagram scopes retain their
// original implicit actor resolution.
func (c *compiler) sequenceDiagramMaps(m *d2ir.Map, selected map[*d2ir.Map]bool) bool {
	if c.stopped() {
		return false
	}
	if c.sequenceShape(m) == d2target.ShapeSequenceDiagramV2 {
		selected[m] = true
		return true
	}
	found := false
	for _, f := range m.Fields {
		if sequenceObjectField(f) && f.Map() != nil && c.sequenceDiagramMaps(f.Map(), selected) {
			found = true
		}
	}
	if found {
		selected[m] = false
	}
	return found
}

func (c *compiler) preprocessSequenceDiagram(diagram *d2ir.Map) error {
	type scope struct {
		m            *d2ir.Map
		original     []d2ast.String
		canonical    []d2ast.String
		messageGroup bool
		branched     bool
	}
	var scopes []scope
	invalid := false
	paths := make(map[string][]d2ast.String)
	base := d2ir.BoardIDA(diagram)
	var collect func(*d2ir.Map, []d2ast.String, []d2ast.String, bool)
	collect = func(m *d2ir.Map, canonical, group []d2ast.String, isGroup bool) {
		original := d2ir.BoardIDA(m)
		branched := isGroup && c.sequenceShapeValue(m, true) == "true"
		if branched && len(m.Edges) > 0 {
			c.errorf(m.Edges[0].LastRef().AST(), "place messages inside a named branch of the branched edge-group")
			invalid = true
		}
		scopes = append(scopes, scope{m, original, canonical, isGroup, branched})
		paths[sequencePathKey(original)] = canonical
		// Retain the containing message group as the reference scope of moved
		// actors' contents. Their graph parent remains the owning actor.
		referenceScope := canonical
		if group != nil {
			referenceScope = group
		}
		c.sequenceScopes[sequencePathKey(original)] = referenceScope
		if group != nil {
			for _, e := range m.Edges {
				for _, ref := range e.References {
					c.recordMaterializedScope(ref, group)
				}
			}
		}
		for _, f := range m.Fields {
			if !sequenceObjectField(f) {
				continue
			}
			if group != nil {
				for _, ref := range f.References {
					c.recordMaterializedScope(ref, group)
				}
			}
			parent := canonical
			childGroup := group
			childShape := c.sequenceShape(f.Map())
			if childShape == d2target.ShapeSequenceDiagramV2 || childShape == d2target.ShapeSequenceDiagram {
				c.errorf(f.LastRef().AST(), "actors in sequence diagrams cannot themselves be sequence diagrams: %s", f.Name.ScalarString())
				invalid = true
				continue
			}
			if branched && childShape != "" {
				c.errorf(f.LastRef().AST(), "a branch cannot have a shape; place nested shapes inside the branch")
				invalid = true
			}
			childIsGroup := childShape == d2target.ShapeSequenceDiagramEdgeGroup || branched
			if isGroup && !childIsGroup {
				parent = base
				if existing := diagram.GetField(f.Name); existing != nil &&
					c.sequenceShape(existing.Map()) == d2target.ShapeSequenceDiagramEdgeGroup {
					c.errorf(f.LastRef().AST(), "sequence diagram actor %q conflicts with an edge-group of the same name", f.Name.ScalarString())
					invalid = true
				}
			}
			childPath := append(append([]d2ast.String(nil), parent...), f.Name)
			if childIsGroup {
				childGroup = childPath
			}
			originalChild := append(append([]d2ast.String(nil), original...), f.Name)
			paths[sequencePathKey(originalChild)] = childPath
			if f.Map() != nil {
				collect(f.Map(), childPath, childGroup, childIsGroup)
			}
		}
	}
	collect(diagram, base, nil, false)
	if invalid {
		return nil
	}

	// Deleting an actor or one of its attributes must also clear preceding
	// declarations in sibling frames. Do this while each endpoint still has
	// the original edge scope that created it.
	if slices.ContainsFunc(scopes, func(s scope) bool { return s.m.HasSequenceDeletions() }) {
		actorScopes := []*d2ir.Map{diagram}
		shared := make(map[string][]*d2ir.Map)
		for _, s := range scopes {
			if s.messageGroup && !s.branched {
				actorScopes = append(actorScopes, s.m)
			} else if s.m != diagram {
				key := sequencePathKey(s.canonical)
				shared[key] = append(shared[key], s.m)
			}
		}
		if err := d2ir.ApplySequenceDeletions(c.ctx, actorScopes, func(f *d2ir.Field) bool {
			return sequenceObjectField(f) && c.sequenceShape(f.Map()) != d2target.ShapeSequenceDiagramEdgeGroup
		}); err != nil {
			return err
		}
		for _, parts := range shared {
			if err := d2ir.ApplySequenceDeletions(c.ctx, parts, nil); err != nil {
				return err
			}
		}
	}

	// Resolve both endpoints independently in the original hierarchy, then
	// express their canonical paths relative to their eventual graph scope.
	// This also handles parent paths and edges within hoisted actor maps.
	for _, s := range scopes {
		for _, e := range s.m.Edges {
			// Graph edge indexes span frames. Source indexes belong to the
			// original IR scope, including materialized imports and variables.
			e.SequenceKey = &d2ast.Key{Edges: []*d2ast.Edge{e.ID.AST()}, EdgeIndex: &d2ast.EdgeIndex{Int: e.ID.Index}}
			if len(s.original) != 0 {
				e.SequenceKey.Key = d2ast.MakeKeyPathString(s.original)
			}
			for _, endpoint := range []*[]d2ast.String{&e.ID.SrcPath, &e.ID.DstPath} {
				absolute := append([]d2ast.String(nil), s.original...)
				path := *endpoint
				for len(path) > 0 && path[0].IsUnquoted() && path[0].ScalarString() == "_" {
					if len(absolute) > 0 {
						absolute = absolute[:len(absolute)-1]
					}
					path = path[1:]
				}
				absolute = append(absolute, path...)
				for i := len(absolute); i >= len(base); i-- {
					if canonical, ok := paths[sequencePathKey(absolute[:i])]; ok {
						absolute = append(append([]d2ast.String(nil), canonical...), absolute[i:]...)
						break
					}
				}
				common := 0
				for common < len(absolute) && common < len(s.canonical) &&
					sequencePathKey(absolute[common:common+1]) == sequencePathKey(s.canonical[common:common+1]) {
					common++
				}
				// An endpoint naming this scope or one of its ancestors must
				// retain a final identifier. Empty paths (or only underscores)
				// are not valid IR edge paths during the subsequent hoist.
				if common == len(absolute) && common > 0 {
					common--
				}
				var relative []d2ast.String
				for i := common; i < len(s.canonical); i++ {
					relative = append(relative, d2ast.FlatUnquotedString("_"))
				}
				*endpoint = append(relative, absolute[common:]...)
			}
		}
	}

	// Iterate the snapshot: moving fields must not skip siblings or process
	// newly appended actors again. Copy through OverlayMap to retain IR parents.
	for _, s := range scopes {
		if !s.messageGroup || s.branched {
			continue
		}
		var retained []*d2ir.Field
		for _, f := range s.m.Fields {
			if !sequenceObjectField(f) || c.sequenceShape(f.Map()) == d2target.ShapeSequenceDiagramEdgeGroup {
				retained = append(retained, f)
				continue
			}
			d2ir.OverlaySequenceActor(diagram, f)
		}
		// This is a move, not a user deletion: DeleteField would also remove
		// messages whose original references named the moved actors.
		s.m.Fields = retained
	}
	return nil
}

// An imported reference has no local ScopeMap; a reusable map's reference
// points back to vars. Record its owning frame before moving the actor.
func (c *compiler) recordMaterializedScope(ref d2ir.Reference, group []d2ast.String) {
	m := ref.Context().ScopeMap
	if m != nil && !d2ir.IsVar(m) {
		return
	}
	if c.sequenceRefScopes == nil {
		c.sequenceRefScopes = make(map[d2ir.Reference][]d2ast.String)
	}
	c.sequenceRefScopes[ref] = group
}

func (c *compiler) sequenceScope(ref d2ir.Reference) ([]d2ast.String, bool) {
	m := ref.Context().ScopeMap
	if m == nil || d2ir.IsVar(m) {
		group, ok := c.sequenceRefScopes[ref]
		return group, ok
	}
	path := d2ir.BoardIDA(m)
	if len(c.sequenceScopes) != 0 {
		if canonical, ok := c.sequenceScopes[sequencePathKey(path)]; ok {
			return canonical, true
		}
	}
	return path, true
}

func (c *compiler) compileSequenceOptions(obj *d2graph.Object, m *d2ir.Map) {
	group := obj.Shape.Value == d2target.ShapeSequenceDiagramEdgeGroup
	owner := "sequence diagram"
	if group {
		owner = "edge-group"
	}
	options := obj.Sequence
	if len(m.Edges) > 0 {
		c.errorf(m.Edges[0].LastRef().AST(), "%s shape options cannot contain connections", owner)
		return
	}
	for _, f := range m.Fields {
		if c.stopped() {
			return
		}
		name := strings.ToLower(f.Name.ScalarString())
		var option *bool
		switch {
		case group && name == "branched":
			option = &options.Branched
		case !group && name == "mirrored":
			option = &options.Mirrored
		case !group && name == "numbered":
			option = &options.Numbered
		default:
			c.errorf(f.LastRef().AST(), "unknown %s shape option %q", owner, f.Name.ScalarString())
			continue
		}
		if f.Primary() != nil && f.Composite == nil {
			switch f.Primary().Value.ScalarString() {
			case "true", "false":
				*option = f.Primary().Value.ScalarString() == "true"
				continue
			}
		}
		c.errorf(f.LastRef().AST(), "%s shape.%s must be true or false", owner, name)
	}
}

func (c *compiler) compileSequenceDiagrams(g *d2graph.Graph) {
	for _, obj := range append([]*d2graph.Object{g.Root}, g.Objects...) {
		switch obj.Shape.Value {
		case d2target.ShapeSequenceDiagramEdgeGroup:
			if !obj.OuterSequenceDiagram().IsSequenceDiagramV2() ||
				(!obj.Parent.IsSequenceDiagramV2() && !obj.Parent.IsSequenceDiagramGroup() && !obj.Parent.IsSequenceDiagramBranch()) {
				c.errorf(obj.Shape.MapKey, "edge-group must be inside a sequence-diagram or another edge-group")
			}
		case d2target.ShapeSequenceDiagramActorGroup:
			if !obj.Parent.IsSequenceDiagramV2() && !obj.Parent.IsSequenceDiagramActorGroup() {
				c.errorf(obj.Shape.MapKey, "actor-group must be inside a sequence-diagram or another actor-group")
			}
		case d2target.ShapeSequenceDiagramActor:
			if !obj.IsSequenceDiagramActorRepeat() {
				c.errorf(obj.Shape.MapKey, "shape actor must be a descendant of a sequence-diagram actor")
			}
		}
		if len(c.sequenceScopes) == 0 {
			continue
		}
		if obj.IsSequenceDiagramBranch() && obj.Label.Value != "" && obj.Language == "" &&
			!(strings.HasPrefix(obj.Label.Value, "[") && strings.HasSuffix(obj.Label.Value, "]")) {
			obj.Label.Value = "[" + obj.Label.Value + "]"
		}
		if obj.IsSequenceDiagramSpan() && !c.hasExplicitLabel(&obj.Attributes) {
			obj.Label.Value = ""
		}
		if (obj.IsSequenceDiagramNote() || obj.IsSequenceDiagramEvent() || obj.IsSequenceDiagramActorRepeat()) &&
			obj.OuterSequenceDiagram().IsSequenceDiagramV2() && len(obj.ChildrenArray) > 0 {
			c.errorf(obj.Label.MapKey, "sequence diagram notes, events, and repeated actors cannot have children")
		}
	}

	if len(c.sequenceScopes) == 0 {
		return
	}
	edges := append([]*d2graph.Edge(nil), g.Edges...)
	sort.SliceStable(edges, func(i, j int) bool {
		return slices.Compare(edges[i].SequenceOrder, edges[j].SequenceOrder) < 0
	})
	numbers := make(map[*d2graph.Object]int)
	for _, e := range edges {
		srcDiagram, dstDiagram := e.Src.OuterSequenceDiagram(), e.Dst.OuterSequenceDiagram()
		if !srcDiagram.IsSequenceDiagramV2() && !dstDiagram.IsSequenceDiagramV2() {
			continue
		}
		if srcDiagram != dstDiagram {
			c.errorf(e.GetAstEdge(), "sequence diagram messages must connect actors or spans in the same diagram")
			continue
		}
		if (!e.Src.IsSequenceDiagramActor() && !e.Src.IsSequenceDiagramSpan()) ||
			(!e.Dst.IsSequenceDiagramActor() && !e.Dst.IsSequenceDiagramSpan()) {
			c.errorf(e.GetAstEdge(), "sequence diagram messages can only connect actors or spans")
			continue
		}
		if near := e.Attributes.LabelPosition; near != nil && !d2ast.LabelPositionsMapping[near.Value].IsEdgePosition() {
			c.errorf(near.MapKey, "sequence message label.near must use outside-top, center, or outside-bottom with left, center, or right")
		}
		if srcDiagram.Sequence != nil && srcDiagram.Sequence.Numbered {
			numbers[srcDiagram]++
			prefix := fmt.Sprintf("%d", numbers[srcDiagram])
			if e.Label.Value != "" {
				prefix += ". " + e.Label.Value
			}
			e.Label.Value = prefix
		}
	}
}
