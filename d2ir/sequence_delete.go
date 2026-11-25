package d2ir

import (
	"context"
	"slices"

	"github.com/d2lang/d2/d2ast"
)

// Graph compilation unifies actor declarations from separate sequence frames
// after ordinary IR deletes have already taken effect in each local map.
// Entries are immutable so board copies can share the same list. Including a
// template or an import adds one link with the order of that occurrence.
type sequenceDeletion struct {
	previous *sequenceDeletion
	included *sequenceDeletion
	path     []d2ast.String
	order    fieldOrder
	// Retain only the shape selectors of a removed object. A nested frame
	// may have the same ID as an actor on the enclosing diagram.
	selectors []*Field
}

func combineSequenceDeletions(previous, included *sequenceDeletion, site *fieldOrder) *sequenceDeletion {
	if included == nil {
		return previous
	}
	if site == nil && previous == nil {
		return included
	}
	d := &sequenceDeletion{previous: previous, included: included}
	if site != nil {
		d.order = *site
	}
	return d
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
	previous := m.sequenceDeletes
	// Repeated clears of an unchanged name have only one effect. Keep shape-
	// dependent entries separate because a frame can share an actor's name.
	if previous != nil && len(deletion.selectors) == 0 && len(previous.selectors) == 0 &&
		slices.EqualFunc(previous.path, path, func(a, b d2ast.String) bool {
			return makeFieldIndexKey(a) == makeFieldIndexKey(b)
		}) {
		if !previous.order.before(order) {
			return
		}
		previous = previous.previous
	}
	deletion.previous = previous
	m.sequenceDeletes = &deletion
}

// HasSequenceDeletions reports whether this occurrence has field deletions to
// reconcile with declarations from other frames.
func (m *Map) HasSequenceDeletions() bool { return m.sequenceDeletes != nil }

// Charge only repeated visits during sequence normalization. Ordinary boards
// share these records without expanding them, even when they use many nulls.
func (d *sequenceDeletion) walk(ctx context.Context, budget *variableExpansionBudget, visit func(*sequenceDeletion, fieldOrder)) error {
	if d == nil {
		return nil
	}
	if budget.sequenceDeletesSeen == nil {
		budget.sequenceDeletesSeen = make(map[*sequenceDeletion]struct{})
	}
	type occurrence struct {
		deletions *sequenceDeletion
		outer     *fieldOrder
	}
	stack := []occurrence{{deletions: d}}
	for len(stack) != 0 {
		next := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for entry := next.deletions; entry != nil; entry = entry.previous {
			if err := ctx.Err(); err != nil {
				return err
			}
			if _, seen := budget.sequenceDeletesSeen[entry]; seen {
				if err := budget.reserve(1); err != nil {
					return err
				}
			} else {
				budget.sequenceDeletesSeen[entry] = struct{}{}
			}
			order := entry.order
			if next.outer != nil && order.sequence != 0 {
				order = order.within(next.outer)
			}
			if entry.included == nil {
				visit(entry, order)
				continue
			}
			outer := next.outer
			if order.sequence != 0 {
				outer = &order
			}
			stack = append(stack, occurrence{entry.included, outer})
		}
	}
	return nil
}

// ApplySequenceDeletions shares null assignments among maps that declare the
// same participant or timeline item. The maps still have their original edges,
// which lets removal of an endpoint also remove its earlier messages.
func ApplySequenceDeletions(ctx context.Context, scopes []*Map, accepts func(*Field) bool) error {
	if len(scopes) < 2 {
		return nil
	}
	type deletionTree struct {
		order    fieldOrder
		children map[fieldIndexKey]*deletionTree
	}
	root := new(deletionTree)
	budget := ensureVariableExpansionBudget(scopes[0])
	for _, m := range scopes {
		err := m.sequenceDeletes.walk(ctx, budget, func(deletion *sequenceDeletion, order fieldOrder) {
			if accepts != nil && len(deletion.selectors) != 0 {
				original := &Field{Name: deletion.path[0], parent: m}
				original.Composite = &Map{parent: original, Fields: deletion.selectors}
				if !accepts(original) {
					return
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
			if node.order.before(order) {
				node.order = order
			}
		})
		if err != nil {
			return err
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
	return nil
}
