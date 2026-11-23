package d2ir

import (
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
)

// A sampled frame copies the destination board's fields and edge order. Use
// names for fields because child boards are omitted from each frame.
type colorPathPart struct {
	field d2ast.String
	edge  int
}

type directColorChange struct {
	path     []colorPathPart
	property d2ast.String
	color    tweenColor
}

func (c *compiler) directColorChanges(previous, board *Map) []directColorChange {
	var changes []directColorChange
	classes := previous.lookupField(d2ast.FlatUnquotedString("classes")).Map()
	var walk func(previous, current *Map, path []colorPathPart)
	walk = func(previous, current *Map, path []colorPathPart) {
		if previous == nil || current == nil || c.stopped() {
			return
		}
		for _, field := range current.Fields {
			if c.stopped() {
				return
			}
			if field.Name == nil || field.Map() == nil {
				continue
			}
			next := append(path, colorPathPart{field: field.Name})
			if field.Name.IsUnquoted() {
				switch strings.ToLower(field.Name.ScalarString()) {
				case "vars", "layers", "scenarios", "steps":
					continue
				case "style":
					changes = appendStyleColors(changes, previous, classes, field, next)
					continue
				}
			}
			walk(previous.lookupField(field.Name).Map(), field.Map(), next)
		}

		// Edge order may differ between authored boards. Match the scoped D2
		// connection and its parallel-edge index, then use its frame position.
		for i, edge := range current.Edges {
			if c.stopped() {
				return
			}
			if edge.Map() == nil {
				continue
			}
			var start *Edge
			if i < len(previous.Edges) && previous.Edges[i].ID.Match(edge.ID) {
				start = previous.Edges[i]
			} else if matches := previous.getEdgesIndexed(edge.ID); len(matches) == 1 {
				start = matches[0]
			}
			walk(start.Map(), edge.Map(), append(path, colorPathPart{edge: i}))
		}
	}
	walk(previous, board, nil)
	return changes
}

func appendStyleColors(changes []directColorChange, previous, classes *Map, style *Field, path []colorPathPart) []directColorChange {
	before := previous.lookupField(style.Name).Map()
	for _, property := range style.Map().Fields {
		if property.Primary() == nil || !property.Name.IsUnquoted() {
			continue
		}
		switch strings.ToLower(property.Name.ScalarString()) {
		case "fill", "stroke", "font-color":
		default:
			continue
		}
		start := before.lookupField(property.Name)
		if start == nil {
			start = inheritedColorField(previous, classes, property.Name)
		}
		if start == nil || start.Primary() == nil {
			continue
		}
		color, ok := newTweenColor(start.Primary().Value.ScalarString(), property.Primary().Value.ScalarString())
		// An inherited expression follows its variables. Reassigning the
		// property (including switching palette entries) animates its colors.
		if ok && (property.Primary().binding == nil || start.LastPrimaryKey() != property.LastPrimaryKey()) {
			changes = append(changes, directColorChange{
				path: slices.Clone(path), property: property.Name, color: color,
			})
		}
	}
	return changes
}

// A direct declaration can replace a solid color supplied by the object's
// previous class. Later classes override earlier ones in a D2 class array.
func inheritedColorField(object, classes *Map, property d2ast.String) *Field {
	class := object.lookupField(d2ast.FlatUnquotedString("class"))
	if class == nil || classes == nil {
		return nil
	}
	lookup := func(name string) *Field {
		return classes.getFieldIndexed(d2ast.FlatUnquotedString(name), d2ast.FlatUnquotedString("style"), property)
	}
	if class.Primary() != nil {
		return lookup(class.Primary().Value.ScalarString())
	}
	if names, ok := class.Composite.(*Array); ok {
		for i := len(names.Values) - 1; i >= 0; i-- {
			if name, ok := names.Values[i].(*Scalar); ok {
				if field := lookup(name.Value.ScalarString()); field != nil {
					return field
				}
			}
		}
	}
	return nil
}

func applyDirectColors(frame *Map, changes []directColorChange, fraction float64) {
	for _, change := range changes {
		m := frame
		for _, part := range change.path {
			if part.field == nil {
				m = m.Edges[part.edge].Map()
			} else {
				m = m.lookupField(part.field).Map()
			}
		}
		target := m.lookupField(change.property)
		target.Primary().Value = d2ast.FlatUnquotedString(change.color.at(fraction))
	}
}
