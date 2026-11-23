package d2ir

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
)

type tweenBinding struct {
	source        d2ast.Scalar
	markdownOuter map[string]string
}

type stepChanges struct {
	variables []tweenVariable
	direct    []directChange
}

type tweenValue interface {
	at(fraction float64) string
}

type tweenVariable struct {
	path  []d2ast.String
	value tweenValue
}

type tweenNumericValue struct {
	from, to         float64
	precision        int
	unit, start, end string
}

// Substitution normally consumes a string's expression. Retain expressions in
// step scopes so imports and the authored endpoints can resolve normally before
// determining which step values need interpolation.
func (c *compiler) saveTweenBinding(node Node, varsStack []*Map) (markdownOuterStart int) {
	primary := node.Primary()
	if primary.binding != nil {
		return -1
	}
	var boxes []d2ast.InterpolationBox
	switch value := primary.Value.(type) {
	case *d2ast.UnquotedString:
		boxes = value.Value
	case *d2ast.DoubleQuotedString:
		boxes = value.Value
	case *d2ast.BlockString:
		if !strings.Contains(value.Value, "${") {
			return -1
		}
	default:
		return -1
	}
	if boxes != nil {
		hasBinding := false
		for _, box := range boxes {
			if box.Substitution != nil && !box.Substitution.Spread {
				hasBinding = true
				break
			}
		}
		if !hasBinding {
			return -1
		}
	}
	for step, vars := range varsStack {
		if NodeBoardKind(ParentMap(vars)) != BoardStep {
			continue
		}
		boxes = slices.Clone(boxes)
		if len(c.importStack) > 1 {
			// A selected board can leave its module's outer scope. Bind those
			// references here while leaving its own step variables editable.
			for i, box := range boxes {
				if box.Substitution == nil {
					continue
				}
				for j, scope := range varsStack {
					value := c.resolveSubstitution(scope, node, box.Substitution, j == 0)
					if value != nil {
						if j > step && value.Primary() != nil {
							text := value.Primary().Value.ScalarString()
							boxes[i] = d2ast.InterpolationBox{String: &text}
						}
						break
					}
				}
			}
		}
		// Substitution replaces boxes or coalesces the string. Its original
		// text and substitution paths remain immutable and can be shared.
		switch value := primary.Value.(type) {
		case *d2ast.UnquotedString:
			binding := *value
			binding.Value = boxes
			primary.binding = &tweenBinding{source: &binding}
		case *d2ast.DoubleQuotedString:
			binding := *value
			binding.Value = boxes
			primary.binding = &tweenBinding{source: &binding}
		case *d2ast.BlockString:
			binding := *value
			primary.binding = &tweenBinding{source: &binding}
			if len(c.importStack) > 1 && step+1 < len(varsStack) {
				return step + 1
			}
		}
		return -1
	}
	return -1
}

// A whole-map substitution or a spread can provide configuration during
// endpoint resolution even when the individual field is not present yet.
func mayResolveTiming(root *Map, key string) bool {
	m := root
	for _, name := range []string{"vars", "d2-config", key} {
		f := m.getFieldIndexed(d2ast.FlatUnquotedString(name))
		if f == nil {
			return hasUnresolvedMapSpread(m)
		}
		if f.Map() == nil {
			return f.Primary() != nil
		}
		m = f.Map()
	}
	return true
}

func (c *compiler) compileTweens(root *Map, opts *CompileOptions) {
	if opts.AnimateTweens != nil && !*opts.AnimateTweens {
		return
	}
	config := root.getFieldIndexed(d2ast.FlatUnquotedString("vars"), d2ast.FlatUnquotedString("d2-config")).Map()
	getTiming := func(key string) int64 {
		if config != nil {
			if f := config.getFieldIndexed(d2ast.FlatUnquotedString(key)); f != nil && f.Primary() != nil {
				val, _ := ParseAnimateInterval(f.Primary().Value.ScalarString())
				return val
			}
		}
		return 0
	}
	duration := getTiming("animate-duration")
	if opts.AnimateDuration != nil {
		duration = *opts.AnimateDuration
	}
	if duration <= 0 {
		return
	}
	if opts.AnimateTweens == nil && getTiming("animate-interval") == 0 {
		// An explicit zero requests static SVGs. Without an interval, only a
		// sequence enables an animated SVG and needs incoming value samples.
		if config != nil && config.getFieldIndexed(d2ast.FlatUnquotedString("animate-interval")) != nil {
			return
		}
		if !c.containsSequenceShape(root, root.lookupField(d2ast.FlatUnquotedString("classes")).Map()) {
			return
		}
	}
	c.compileStepTweens(root, int(duration))
}

// Check effective shapes on object and board maps after substitution. An unused
// class or a sequence-shaped value in vars does not start an animation.
func (c *compiler) containsSequenceShape(m, classes *Map) bool {
	if c.stopped() {
		return false
	}
	if f := effectiveProperty(m, classes, d2ast.FlatUnquotedString("shape")); f != nil && f.Primary() != nil &&
		strings.EqualFold(f.Primary().Value.ScalarString(), d2target.ShapeSequenceDiagram) {
		return true
	}
	for _, f := range m.Fields {
		child := f.Map()
		if child == nil || f.Name == nil {
			continue
		}
		if f.Name.IsUnquoted() {
			name := strings.ToLower(f.Name.ScalarString())
			if _, boardGroup := d2ast.BoardKeywords[name]; boardGroup {
				for _, board := range child.Fields {
					if bm := board.Map(); bm != nil && c.containsSequenceShape(bm, bm.lookupField(d2ast.FlatUnquotedString("classes")).Map()) {
						return true
					}
				}
				continue
			}
			if _, reserved := d2ast.ReservedKeywords[name]; reserved {
				continue
			}
		}
		if c.containsSequenceShape(child, classes) {
			return true
		}
	}
	return false
}

func (c *compiler) compileStepTweens(board *Map, duration int) {
	for _, group := range []string{"layers", "scenarios", "steps"} {
		children := board.getFieldIndexed(d2ast.FlatUnquotedString(group)).Map()
		if children == nil {
			continue
		}
		previous := board
		for _, field := range children.Fields {
			if c.stopped() || !c.err.Empty() {
				return
			}
			child := field.Map()
			if child == nil {
				continue
			}
			if group == "steps" {
				vars := child.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map()
				changes := stepChanges{direct: c.directChanges(previous, child)}
				if vars != nil {
					c.findTweenChanges(tweenVarScopes(previous), vars, nil, &changes.variables)
				}
				if len(changes.variables) > 0 || hasDirectMotion(changes.direct) {
					c.sampleStep(child, changes, duration)
				}
				previous = child
			}
			c.compileStepTweens(child, duration)
		}
	}
}

func tweenVarScopes(board *Map) (scopes []*Map) {
	for m := board; m != nil; m = ParentMap(m) {
		if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
			scopes = append(scopes, vars)
		}
	}
	return scopes
}

func (c *compiler) findTweenChanges(previous []*Map, vars *Map, prefix []d2ast.String, changes *[]tweenVariable) {
	if vars == nil || c.stopped() {
		return
	}
	for _, field := range vars.Fields {
		if field.suspended || field.Name == nil || (len(prefix) == 0 && field.Name.ScalarString() == "d2-config") {
			continue
		}
		path := append(prefix, field.Name)
		if field.Map() != nil {
			c.findTweenChanges(previous, field.Map(), path, changes)
			continue
		}
		if field.Primary() == nil || field.Composite != nil {
			continue
		}
		var start *Field
		for _, scope := range previous {
			start = scope.getFieldIndexed(path...)
			if start != nil {
				break
			}
		}
		if start == nil || start.suspended || start.Primary() == nil || start.Composite != nil {
			continue
		}
		value, ok := tweenScalarChange(start.Primary().Value.ScalarString(), field.Primary().Value.ScalarString())
		if ok {
			*changes = append(*changes, tweenVariable{path: slices.Clone(path), value: value})
		}
	}
}

func tweenScalarChange(start, end string) (tweenValue, bool) {
	if start == end {
		return nil, false
	}
	if number, ok := newTweenNumber(start, end); ok {
		return number, true
	}
	return newTweenPaint(start, end)
}

func newTweenNumber(start, end string) (tweenNumericValue, bool) {
	if start == end {
		return tweenNumericValue{}, false
	}
	to, unit, precision, ok := tweenNumber(end)
	if !ok {
		return tweenNumericValue{}, false
	}
	from, fromUnit, fromPrecision, ok := tweenNumber(start)
	if !ok || unit != fromUnit || from == to {
		return tweenNumericValue{}, false
	}
	return tweenNumericValue{start: start, end: end, from: from, to: to, unit: unit,
		precision: max(precision, fromPrecision)}, true
}

func (v tweenNumericValue) at(fraction float64) string {
	switch fraction {
	case 0:
		return v.start
	case 1:
		return v.end
	}
	number := (1-fraction)*v.from + fraction*v.to
	return strconv.FormatFloat(number, 'f', v.precision, 64) + v.unit
}

func tweenNumber(raw string) (number float64, unit string, precision int, valid bool) {
	if strings.HasSuffix(raw, "%") {
		unit = "%"
		raw = strings.TrimSuffix(raw, "%")
	}
	number, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || strings.ContainsAny(raw, "_xXpP") {
		return 0, "", 0, false
	}
	decimal := raw
	exponent := 0
	if pos := strings.IndexAny(raw, "Ee"); pos >= 0 {
		decimal = raw[:pos]
		n, err := strconv.ParseInt(raw[pos+1:], 10, 32)
		if err != nil {
			return 0, "", 0, false
		}
		exponent = int(n)
	}
	if pos := strings.IndexByte(decimal, '.'); pos >= 0 {
		precision = len(decimal) - pos - 1
	}
	// Keep the precision of decimal endpoints without allowing format strings
	// whose expanded output exceeds the decimal range of float64.
	precision = int(min(int64(324), max(int64(0), int64(precision)-int64(exponent))))
	return number, unit, precision, true
}

func (c *compiler) sampleStep(board *Map, changes stepChanges, duration int) {
	// The parent step's samples never include its child boards.
	view := *board
	view.Fields = nil
	for _, f := range board.Fields {
		if f.Name == nil || findBoardKeyword(f.Name) == -1 {
			view.Fields = append(view.Fields, f)
		}
	}
	outerVars := tweenVarScopes(ParentMap(board))
	n := min(d2target.MaxTweenFrames, (duration+39)/40)
	for i := 0; i < n; i++ {
		frame := c.sampleTween(&view, changes, outerVars, float64(i)/float64(n))
		if frame == nil {
			return
		}
		board.TweenFrames = append(board.TweenFrames, frame)
	}
	board.TweenDurationMS = duration
}

// Each sampled board obeys the same expansion limit as an authored board.
// An additional shared budget limits the sum across the entire animation to
// one full-length tween's worth of that work, even for many short tweens.
func (c *compiler) sampleTween(template *Map, changes stepChanges, outerVars []*Map, fraction float64) *Map {
	base := c.variableExpansion
	if c.tweenExpansion == nil {
		limit := int64(math.MaxInt64)
		if base.limit <= math.MaxInt64/d2target.MaxTweenFrames {
			limit = base.limit * d2target.MaxTweenFrames
		}
		c.tweenExpansion = &variableExpansionBudget{limit: limit}
	}
	c.variableExpansion = &variableExpansionBudget{limit: min(base.limit, c.tweenExpansion.remaining())}
	defer func() {
		c.tweenExpansion.used += c.variableExpansion.used
		c.variableExpansion = base
	}()
	if !c.reserveVariableCopy(template.LastRef().AST(), template) {
		return nil
	}
	frame := template.Copy(nil).(*Map)
	frame.variableExpansion = c.variableExpansion
	walkComposite(frame, func(n Node) bool {
		if p := n.Primary(); p != nil && p.binding != nil {
			if !c.reserveVariableExpansion(template.LastRef().AST(), variableExpansionScalarValueUnits(p.binding.source)) {
				return true
			}
			p.Value = cloneASTScalar(p.binding.source)
			if len(p.binding.markdownOuter) > 0 {
				block := p.Value.(*d2ast.BlockString)
				block.Value = c.replaceMarkdownVariables(n, block.Value, p.binding.markdownOuter)
			}
			p.binding = nil
		}
		return false
	})
	if c.stopped() {
		return nil
	}
	vars := frame.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map()
	for _, v := range changes.variables {
		vars.getFieldIndexed(v.path...).Primary_.Value = d2ast.FlatUnquotedString(v.value.at(fraction))
	}
	applyDirectChanges(frame, changes.direct, fraction)
	c.compileSubstitutions(frame, outerVars)
	if c.stopped() || !c.err.Empty() {
		return nil
	}
	if err := ReserveVariableExpansionAliases(c.ctx, frame); err != nil {
		c.expansionErr = err
		c.halted = true
		return nil
	}
	frame.removeSuspendedFields()
	return frame
}
