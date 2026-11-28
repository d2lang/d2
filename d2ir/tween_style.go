package d2ir

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/lib/animation"
)

// A sampled frame copies the destination board's fields and edge order. Use
// names for fields because child boards are omitted from each frame.
type propertyPathPart struct {
	field d2ast.String
	edge  int
}

type tweenChange struct {
	path     []propertyPathPart
	property d2ast.String
	value    tweenValue
	// Geometry is sampled at the shape, even when inherited from a class or
	// bound to a variable that other properties sample on the step clock.
	geometry *Field
}

func hasTweenMotion(changes []tweenChange) bool {
	return slices.ContainsFunc(changes, func(change tweenChange) bool { return change.value != nil })
}

func (c *compiler) stepChanges(previous, board *Map) []tweenChange {
	var changes []tweenChange
	classKey := d2ast.FlatUnquotedString("classes")
	previousClasses := previous.lookupField(classKey).Map()
	classes := board.lookupField(classKey).Map()
	var walk func(previous, current *Map, path []propertyPathPart)
	walk = func(previous, current *Map, path []propertyPathPart) {
		if previous == nil || current == nil || c.stopped() {
			return
		}
		if current != board && ParentMap(current) != classes {
			changes = appendGeometry(changes, previous, current, previousClasses, classes, path)
		}
		for _, field := range current.Fields {
			if c.stopped() {
				return
			}
			if field.Name == nil || field.Map() == nil {
				continue
			}
			next := append(path, propertyPathPart{field: field.Name})
			if field.Name.IsUnquoted() {
				switch strings.ToLower(field.Name.ScalarString()) {
				case "vars":
					changes = c.variableChanges(changes, tweenVarScopes(previous), field.Map(), next, nil)
					continue
				case "layers", "scenarios", "steps":
					continue
				case "style":
					changes = appendStyles(changes, previous, previousClasses, field, next)
					continue
				}
			}
			walk(previous.lookupField(field.Name).Map(), field.Map(), next)
		}

		// Edge order may differ between authored boards. Match the scoped D2
		// connection and its parallel-edge index, then use its frame position.
		var byDeclaration map[*EdgeReference]*Edge
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
			// Removing a parallel edge can reuse its index for a different
			// declaration. Inherited IR edges share their original reference.
			if len(edge.References) > 0 && (start == nil || len(start.References) == 0 || start.References[0] != edge.References[0]) {
				if byDeclaration == nil {
					byDeclaration = make(map[*EdgeReference]*Edge)
					for _, old := range previous.Edges {
						if len(old.References) != 0 {
							ref := old.References[0]
							if _, duplicate := byDeclaration[ref]; duplicate {
								byDeclaration[ref] = nil
							} else {
								byDeclaration[ref] = old
							}
						}
					}
				}
				start = byDeclaration[edge.References[0]]
			}
			walk(start.Map(), edge.Map(), append(path, propertyPathPart{edge: i}))
		}
	}
	walk(previous, board, nil)
	return changes
}

func appendStyles(changes []tweenChange, previous, classes *Map, style *Field, path []propertyPathPart) []tweenChange {
	before := previous.lookupField(style.Name).Map()
	for _, property := range style.Map().Fields {
		if property.Primary() == nil || !property.Name.IsUnquoted() {
			continue
		}
		start := before.lookupField(property.Name)
		if start == nil {
			start = inheritedProperty(previous, classes, style.Name, property.Name)
		}
		changes = appendDirectChange(changes, path, start, property)
	}
	return changes
}

func appendGeometry(changes []tweenChange, previous, current, previousClasses, classes *Map, path []propertyPathPart) []tweenChange {
	owner, ok := current.Parent().(*Field)
	if !ok || owner.Name == nil || makeFieldIndexKey(owner.Name).unquotedReserved {
		return changes
	}
	for _, name := range []string{"left", "top"} {
		key := d2ast.FlatUnquotedString(name)
		if end := effectiveProperty(current, classes, key); end != nil {
			changes = appendGeometryChange(changes, path, effectiveProperty(previous, previousClasses, key), end)
		}
	}
	width := d2ast.FlatUnquotedString("width")
	height := d2ast.FlatUnquotedString("height")
	w, h := effectiveProperty(current, classes, width), effectiveProperty(current, classes, height)
	if w == nil && h == nil {
		return changes
	}
	oldW := effectiveProperty(previous, previousClasses, width)
	oldH := effectiveProperty(previous, previousClasses, height)
	shape := effectiveProperty(current, classes, d2ast.FlatUnquotedString("shape"))
	if w != nil && h != nil && shape != nil && shape.Primary() != nil && !equalDimensions(oldW, oldH) {
		switch strings.ToLower(shape.Primary().Value.ScalarString()) {
		case "circle", "square":
			// Starting from an implicit or unequal size would make an
			// invalid intermediate shape. Hold both authored dimensions.
			for _, field := range []*Field{w, h} {
				if field.Primary() != nil && field.Primary().binding != nil {
					changes = append(changes, tweenChange{path: slices.Clone(path), property: field.Name, geometry: field})
				}
			}
			return changes
		}
	}
	changes = appendGeometryChange(changes, path, oldW, w)
	return appendGeometryChange(changes, path, oldH, h)
}

func appendGeometryChange(changes []tweenChange, path []propertyPathPart, start, end *Field) []tweenChange {
	if start == nil || end == nil || start.Primary() == nil || end.Primary() == nil {
		return changes
	}
	value := newIntegerTween(start.Primary().Value.ScalarString(), end.Primary().Value.ScalarString())
	if value == nil {
		return changes
	}
	return append(changes, tweenChange{path: slices.Clone(path), property: end.Name, value: value, geometry: end})
}

func equalDimensions(a, b *Field) bool {
	return a != nil && b != nil && a.Primary() != nil && b.Primary() != nil &&
		a.Primary().Value.ScalarString() == b.Primary().Value.ScalarString()
}

func appendDirectChange(changes []tweenChange, path []propertyPathPart, start, end *Field) []tweenChange {
	if start == nil || end == nil || start.Primary() == nil || end.Primary() == nil {
		return changes
	}
	// An inherited expression follows its variables. Reassigning the
	// property (including switching palette entries) animates its values.
	if end.Primary().binding != nil && start.LastPrimaryKey() == end.LastPrimaryKey() {
		return changes
	}
	from, to := start.Primary().Value.ScalarString(), end.Primary().Value.ScalarString()
	if from == to {
		return changes
	}
	kind := strings.ToLower(end.Name.ScalarString())
	var value tweenValue
	switch kind {
	case "fill", "stroke", "font-color":
		paint, ok := newTweenPaint(from, to)
		if !ok {
			return changes
		}
		value = paint
	case "opacity":
		number, ok := newTweenNumber(from, to)
		if !ok || number.unit != "" {
			return changes
		}
		// Opacity is continuous even when authored simply as 0 and 1.
		number.precision = -1
		value = number
	case "stroke-width", "stroke-dash", "border-radius", "font-size":
		value = newIntegerTween(from, to)
		if value == nil {
			return changes
		}
	default:
		return changes
	}
	return append(changes, tweenChange{path: slices.Clone(path), property: end.Name, value: value})
}

func newIntegerTween(from, to string) tweenValue {
	a, errA := strconv.Atoi(from)
	b, errB := strconv.Atoi(to)
	if errA != nil || errB != nil || a == b {
		return nil
	}
	if int64(a) >= -(1<<53) && int64(a) <= 1<<53 && int64(b) >= -(1<<53) && int64(b) <= 1<<53 {
		return tweenNumericValue{from: float64(a), to: float64(b), start: from, end: to}
	}
	return largeIntegerTween{from: a, to: b, start: from, end: to}
}

// Interpolate the distance from the lower endpoint. Computing with absolute
// float64 endpoints would lose adjacent integers near the platform limit and
// could round past the largest value accepted by an integer property.
type largeIntegerTween struct {
	from, to   int
	start, end string
}

func (v largeIntegerTween) at(fraction float64) string {
	switch fraction {
	case 0:
		return v.start
	case 1:
		return v.end
	}
	lo, hi := v.from, v.to
	if lo > hi {
		lo, hi, fraction = hi, lo, 1-fraction
	}
	span := uint(hi) - uint(lo)
	whole, remainder := math.Modf(float64(span) * fraction)
	result := uint(lo) + uint(whole)
	// Match the existing numeric variable formatter's ties-to-even rounding.
	if remainder > 0.5 || remainder == 0.5 && result&1 != 0 {
		result++
	}
	return strconv.Itoa(int(result))
}

func effectiveProperty(object, classes *Map, name d2ast.String) *Field {
	if field := object.lookupField(name); field != nil {
		return field
	}
	return inheritedProperty(object, classes, name)
}

// Later classes override earlier ones in a D2 class array.
func inheritedProperty(object, classes *Map, path ...d2ast.String) *Field {
	class := object.lookupField(d2ast.FlatUnquotedString("class"))
	if class == nil || classes == nil {
		return nil
	}
	lookup := func(name string) *Field {
		return classes.lookupField(d2ast.FlatUnquotedString(name)).Map().getFieldIndexed(path...)
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

func applyTweenChanges(frame *Map, changes []tweenChange, fraction float64) {
	travel := animation.EaseInOut(fraction)
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
		t := fraction
		if field := change.geometry; field != nil {
			if target == nil {
				// Override inherited geometry on this shape only. Other
				// objects (including arrowheads) can use the same class.
				target = &Field{parent: m, Name: change.property, References: field.References}
				target.Primary_ = &Scalar{parent: target}
				m.appendField(target)
			}
			if change.value == nil {
				target.Primary().Value = field.Primary().Value
				continue
			}
			t = travel
		}
		setTweenValue(target.Primary(), change.value, t)
	}
}
