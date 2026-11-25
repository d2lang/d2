package d2ir

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
)

// Graph compilation unifies actor declarations from separate sequence frames
// after ordinary IR deletes have already taken effect in each local map.
type sequenceDeletion struct {
	path  []d2ast.String
	order fieldOrder
	// Retain only the shape selectors of a removed object. A nested frame
	// may have the same ID as an actor on the enclosing diagram.
	selectors []*Field
}

func (m *Map) rememberSequenceDeletion(f *Field, order fieldOrder) {
	path := []d2ast.String{f.Name}
	// An empty style holder disappears along with its last property. Anchor
	// property deletions at the object where all of its declarations meet.
	for p := ParentField(m); p != nil && p.Name != nil && p.Name.IsUnquoted(); p = ParentField(m) {
		name := p.Name.ScalarString()
		if name != "style" && name != "label" && name != "shape" && name != "icon" {
			break
		}
		path = append(path, p.Name)
		m = ParentMap(p)
	}
	slices.Reverse(path)
	deletion := sequenceDeletion{path: path, order: order}
	if len(path) == 1 && f.Map() != nil {
		for _, name := range []string{"shape", "class"} {
			if attr := f.Map().GetField(d2ast.FlatUnquotedString(name)); attr != nil {
				selector := &Field{Name: attr.Name}
				if scalar := attr.Primary(); scalar != nil {
					selector.Primary_ = scalar.Copy(selector).(*Scalar)
				}
				if arr, ok := attr.Composite.(*Array); name == "class" && ok {
					selector.Composite = arr.Copy(selector).(*Array)
				}
				deletion.selectors = append(deletion.selectors, selector)
			}
		}
	}
	m.sequenceDeletes = append(m.sequenceDeletes, deletion)
}

// HasSequenceDeletions reports whether this occurrence has field deletions to
// reconcile with declarations from other frames.
func (m *Map) HasSequenceDeletions() bool { return len(m.sequenceDeletes) > 0 }

// ApplySequenceDeletions shares null assignments among maps that declare the
// same participant or timeline item. The maps still have their original edges,
// which lets removal of an endpoint also remove its earlier messages.
func ApplySequenceDeletions(scopes []*Map, accepts func(*Field) bool) {
	if len(scopes) < 2 {
		return
	}
	type deletionTree struct {
		order    fieldOrder
		children map[fieldIndexKey]*deletionTree
	}
	root := new(deletionTree)
	for _, m := range scopes {
		for _, deletion := range m.sequenceDeletes {
			if accepts != nil && len(deletion.selectors) != 0 {
				original := &Field{Name: deletion.path[0], parent: m}
				original.Composite = &Map{parent: original, Fields: deletion.selectors}
				if !accepts(original) {
					continue
				}
			}
			node := root
			for _, name := range deletion.path {
				key := makeFieldIndexKey(name)
				if node.children == nil {
					node.children = make(map[fieldIndexKey]*deletionTree)
				}
				if node.children[key] == nil {
					node.children[key] = new(deletionTree)
				}
				node = node.children[key]
			}
			if node.order.before(deletion.order) {
				node.order = deletion.order
			}
		}
	}
	var apply func(*Map, *deletionTree)
	apply = func(m *Map, node *deletionTree) {
		for i := len(m.Fields) - 1; i >= 0; i-- {
			f := m.Fields[i]
			deletion := node.children[makeFieldIndexKey(f.Name)]
			if deletion == nil || node == root && accepts != nil && !accepts(f) {
				continue
			}
			if deletion.order.sequence != 0 {
				m.deleteEdgesForReferences(f.References, &deletion.order)
				if !f.retainAfter(deletion.order) {
					m.deleteField(f)
					continue
				}
			}
			if f.Map() != nil && len(deletion.children) > 0 {
				apply(f.Map(), deletion)
			}
		}
	}
	if len(root.children) != 0 {
		for _, m := range scopes {
			apply(m, root)
		}
	}
}
