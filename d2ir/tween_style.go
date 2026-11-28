package d2ir

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
)

// A sampled frame copies the destination board's fields and edge order. Use
// names for fields because child boards are omitted from each frame.
type propertyPathPart struct {
	field   d2ast.String
	edge    int
	inherit *d2ast.Substitution // Lexical fallback for a sample-local map variable.
}

type tweenChange struct {
	path     []propertyPathPart
	property *Field
	value    tweenValue // nil holds the resolved endpoint during the transition
}

func hasTweenMotion(changes []tweenChange) bool {
	return slices.ContainsFunc(changes, func(change tweenChange) bool { return change.value != nil })
}

func (c *compiler) boardChanges(previous, board *Map) []tweenChange {
	var changes []tweenChange
	scenario := NodeBoardKind(board) == BoardScenario
	className := d2ast.FlatUnquotedString("class")
	classKey := d2ast.FlatUnquotedString("classes")
	previousClasses := previous.lookupField(classKey).Map()
	classes := board.lookupField(classKey).Map()
	var walk func(previous, current *Map, path []propertyPathPart)
	walk = func(previous, current *Map, path []propertyPathPart) {
		if previous == nil || current == nil || c.stopped() {
			return
		}
		if scenario {
			changes = c.scenarioVariableFallbacks(changes, previous, current, path)
		}
		// A sibling scenario may drop an override simply by omitting it.
		// If the destination uses a class, compare its resolved properties
		// and put any needed overrides on the sampled instance only.
		classFallback := scenario && classes != nil && current.lookupField(className) != nil
		if classFallback {
			changes = appendScenarioClassStyles(changes, previous, current, previousClasses, classes, path)
		}
		if current != board && ParentMap(current) != classes {
			changes = appendDimensions(changes, previous, current, previousClasses, classes, path, classFallback)
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
		changes = appendDirectChange(changes, path, start, property, false)
	}
	return changes
}

func appendScenarioClassStyles(changes []tweenChange, previous, current, previousClasses, classes *Map, path []propertyPathPart) []tweenChange {
	style := d2ast.FlatUnquotedString("style")
	field := current.lookupField(style)
	direct := field.Map()
	if field != nil && direct == nil {
		// Graph validation reports the invalid style. Sampled class
		// properties cannot be overlaid on an authored scalar or array.
		return changes
	}
	before := previous.lookupField(style).Map()
	path = append(path, propertyPathPart{field: style})
	for _, name := range []string{"fill", "stroke", "font-color", "opacity", "stroke-width", "stroke-dash", "border-radius", "font-size"} {
		key := d2ast.FlatUnquotedString(name)
		if direct.lookupField(key) != nil {
			continue // appendStyles handles the explicit property.
		}
		end := inheritedProperty(current, classes, style, key)
		if end == nil {
			continue
		}
		start := before.lookupField(key)
		if start == nil {
			start = inheritedProperty(previous, previousClasses, style, key)
		}
		changes = appendDirectChange(changes, path, start, end, true)
	}
	return changes
}

func appendDimensions(changes []tweenChange, previous, current, previousClasses, classes *Map, path []propertyPathPart, classFallback bool) []tweenChange {
	owner, ok := current.Parent().(*Field)
	if !ok || owner.Name == nil || makeFieldIndexKey(owner.Name).unquotedReserved {
		return changes
	}
	width := d2ast.FlatUnquotedString("width")
	height := d2ast.FlatUnquotedString("height")
	w, h := current.lookupField(width), current.lookupField(height)
	if classFallback {
		w, h = effectiveProperty(current, classes, width), effectiveProperty(current, classes, height)
	}
	if w == nil && h == nil {
		return changes
	}
	oldW := effectiveProperty(previous, previousClasses, width)
	oldH := effectiveProperty(previous, previousClasses, height)
	shape := effectiveProperty(current, classes, d2ast.FlatUnquotedString("shape"))
	if shape != nil && shape.Primary() != nil {
		switch strings.ToLower(shape.Primary().Value.ScalarString()) {
		case "circle", "square":
			// Two supplied dimensions must agree on every frame. A shape
			// change, implicit size or new class can make their starting
			// values incompatible. Use the final dimensions in that case,
			// even when just one of the dimensions reads a moving variable.
			endW := effectiveProperty(current, classes, width)
			endH := effectiveProperty(current, classes, height)
			if endW != nil && endH != nil && (w == nil || h == nil || !equalDimensions(oldW, oldH)) {
				for _, field := range []*Field{endW, endH} {
					if field.Primary() != nil && field.Primary().binding != nil {
						changes = append(changes, tweenChange{path: slices.Clone(path), property: field})
					}
				}
				return changes
			}
		}
	}
	changes = appendDirectChange(changes, path, oldW, w, classFallback && current.lookupField(width) == nil)
	return appendDirectChange(changes, path, oldH, h, classFallback && current.lookupField(height) == nil)
}

func equalDimensions(a, b *Field) bool {
	return a != nil && b != nil && a.Primary() != nil && b.Primary() != nil &&
		a.Primary().Value.ScalarString() == b.Primary().Value.ScalarString()
}

func appendDirectChange(changes []tweenChange, path []propertyPathPart, start, end *Field, fromClass bool) []tweenChange {
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
		// A different class or variable binding can move while this
		// instance has the same paint or dimensions at both endpoints.
		if (fromClass || end.Primary().binding != nil) && start.LastPrimaryKey() != end.LastPrimaryKey() {
			changes = append(changes, tweenChange{path: slices.Clone(path), property: end})
		}
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
	case "width", "height", "stroke-width", "stroke-dash", "border-radius", "font-size":
		a, errA := strconv.Atoi(from)
		b, errB := strconv.Atoi(to)
		if errA != nil || errB != nil || a == b {
			return changes
		}
		// Keep ordinary integer styles in lockstep with a numeric variable
		// driving the same dimension, including rounding at sample boundaries.
		if int64(a) >= -(1<<53) && int64(a) <= 1<<53 && int64(b) >= -(1<<53) && int64(b) <= 1<<53 {
			value = tweenNumericValue{from: float64(a), to: float64(b), start: from, end: to}
		} else {
			value = largeIntegerTween{from: a, to: b, start: from, end: to}
		}
	default:
		return changes
	}
	return append(changes, tweenChange{path: slices.Clone(path), property: end, value: value})
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

func (c *compiler) applyTweenChanges(frame *Map, changes []tweenChange, fraction float64) {
	for _, change := range changes {
		m := frame
		for _, part := range change.path {
			if part.field == nil {
				m = m.Edges[part.edge].Map()
			} else {
				f := m.lookupField(part.field)
				if f == nil {
					f = &Field{parent: m, Name: part.field, References: ParentField(change.property).References}
					if part.inherit != nil {
						f.Primary_ = &Scalar{parent: f, Value: &d2ast.UnquotedString{Value: []d2ast.InterpolationBox{{Substitution: part.inherit}}}}
					}
					f.setComposite(&Map{parent: f})
					if !c.reserveVariableCopy(change.property.Name, f) {
						return
					}
					m.appendField(f)
				}
				m = f.Map()
			}
		}
		target := m.lookupField(change.property.Name)
		if target == nil {
			target = &Field{parent: m, Name: change.property.Name, References: change.property.References}
			target.Primary_ = &Scalar{parent: target, Value: change.property.Primary().Value, order: change.property.Primary().order}
			if !c.reserveVariableCopy(change.property.Name, target) {
				return
			}
			m.appendField(target)
		}
		if change.value == nil {
			target.Primary().Value = change.property.Primary().Value
		} else {
			setTweenValue(target.Primary(), change.value, fraction)
		}
	}
}
