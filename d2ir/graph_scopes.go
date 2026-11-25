package d2ir

import (
	"context"
	"slices"
)

// EditGraphScopes materializes selected submaps for graph normalization. A
// true entry selects the whole scope; false marks an ancestor on the way to
// one. Other maps are read-only during graph compilation and can be shared.
func EditGraphScopes(ctx context.Context, root *Map, scopes map[*Map]bool, owned bool, edit func(*Map) error) (*Map, error) {
	budget := ensureVariableExpansionBudget(root)
	var visit func(*Map, *Map, *Field, bool) (*Map, error)
	visit = func(src, selection *Map, parent *Field, writable bool) (*Map, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m := src
		if scopes[selection] {
			if !writable {
				if err := ReserveVariableExpansionCopy(ctx, src); err != nil {
					return nil, err
				}
				if parent == nil {
					m = src.Copy(nil).(*Map)
				} else {
					m = src.Copy(parent).(*Map)
				}
			} else if err := detachGraphAliases(ctx, m); err != nil {
				return nil, err
			}
			return m, edit(m)
		}
		if !writable {
			// Only copy the slots needed to substitute the edited children.
			if err := budget.reserve(int64(len(src.Fields)) + 1); err != nil {
				return nil, err
			}
			copy := *src
			m = &copy
			m.Fields = slices.Clone(src.Fields)
			m.fieldIndex, m.firstEdgeField = nil, nil
			if parent != nil {
				m.parent = parent
			} else {
				m.initRoot()
			}
		}
		for i, original := range src.Fields {
			child := original.Map()
			selectedChild := selection.Fields[i].Map()
			if _, selected := scopes[selectedChild]; !selected {
				continue
			}
			f := original
			if !writable {
				copy := *original
				f = &copy
				f.parent = m
				m.Fields[i] = f
			}
			childWritable := writable
			if child.Parent() != original {
				// A composite variable can back multiple occurrences. Its
				// nested orders and graph paths must belong to this use.
				if err := ReserveVariableExpansionCopy(ctx, child); err != nil {
					return nil, err
				}
				detached := original.copyAliasMap(f)
				child = detached
				childWritable = true
			}
			edited, err := visit(child, selectedChild, f, childWritable)
			if err != nil {
				return nil, err
			}
			f.Composite = edited
		}
		return m, nil
	}
	return visit(root, root, nil, owned)
}

func detachGraphAliases(ctx context.Context, m *Map) error {
	for _, f := range m.Fields {
		child := f.Map()
		if child == nil || (f.Name.IsUnquoted() && (f.Name.ScalarString() == "vars" || f.Name.ScalarString() == "classes")) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if child.Parent() != f {
			if err := ReserveVariableExpansionCopy(ctx, child); err != nil {
				return err
			}
			f.Composite = f.copyAliasMap(f)
		} else if err := detachGraphAliases(ctx, child); err != nil {
			return err
		}
	}
	return nil
}
