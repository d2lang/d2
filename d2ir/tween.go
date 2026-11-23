package d2ir

import (
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
)

type tweenVariable struct {
	path            []d2ast.String
	from, to        float64
	precision       int
	unit, startText string
}

// Substitution normally consumes a string's expression. Retain expressions in
// step scopes so imports and the authored endpoints can resolve normally before
// determining whether this step has any numeric changes to sample.
func (c *compiler) saveTweenBinding(node Node, varsStack []*Map) {
	primary := node.Primary()
	if primary.binding != nil {
		return
	}
	var boxes []d2ast.InterpolationBox
	switch value := primary.Value.(type) {
	case *d2ast.UnquotedString:
		boxes = value.Value
	case *d2ast.DoubleQuotedString:
		boxes = value.Value
	case *d2ast.BlockString:
		if !strings.Contains(value.Value, "${") {
			return
		}
	default:
		return
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
			return
		}
	}
	for _, vars := range varsStack {
		if NodeBoardKind(ParentMap(vars)) != BoardStep {
			continue
		}
		// Substitution replaces boxes or coalesces the string. Its original
		// text and substitution paths remain immutable and can be shared.
		switch value := primary.Value.(type) {
		case *d2ast.UnquotedString:
			binding := *value
			binding.Value = slices.Clone(boxes)
			primary.binding = &binding
		case *d2ast.DoubleQuotedString:
			binding := *value
			binding.Value = slices.Clone(boxes)
			primary.binding = &binding
		case *d2ast.BlockString:
			binding := *value
			primary.binding = &binding
		}
		return
	}
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
	if opts.AnimateTweens == nil && getTiming("animate-interval") == 0 {
		return
	}
	duration := getTiming("animate-duration")
	if opts.AnimateDuration != nil {
		duration = *opts.AnimateDuration
	}
	if duration > 0 {
		c.compileStepTweens(root, int(duration))
	}
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
				var changes []tweenVariable
				c.findTweenChanges(previous, vars, nil, &changes)
				if len(changes) > 0 {
					c.sampleStep(child, changes, duration)
				}
				previous = child
			}
			c.compileStepTweens(child, duration)
		}
	}
}

func (c *compiler) findTweenChanges(previous, vars *Map, prefix []d2ast.String, changes *[]tweenVariable) {
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
		to, endUnit, endPrecision, ok := tweenNumber(field)
		if !ok {
			continue
		}
		var start *Field
		for m := previous; m != nil; m = ParentMap(m) {
			decls := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map()
			if decls != nil {
				start = decls.getFieldIndexed(path...)
				if start != nil {
					break
				}
			}
		}
		from, unit, startPrecision, ok := tweenNumber(start)
		if ok && unit == endUnit && from != to && !start.suspended {
			*changes = append(*changes, tweenVariable{
				path: append([]d2ast.String(nil), path...), from: from, to: to, unit: unit,
				precision: max(startPrecision, endPrecision), startText: start.Primary().Value.ScalarString(),
			})
		}
	}
}

func tweenNumber(field *Field) (number float64, unit string, precision int, valid bool) {
	if field == nil || field.Primary() == nil || field.Composite != nil {
		return
	}
	raw := field.Primary().Value.ScalarString()
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

func (c *compiler) sampleStep(board *Map, changes []tweenVariable, duration int) {
	// The parent step's samples never include its child boards.
	view := *board
	view.Fields = nil
	for _, f := range board.Fields {
		if f.Name == nil || findBoardKeyword(f.Name) == -1 {
			view.Fields = append(view.Fields, f)
		}
	}
	var outerVars []*Map
	for m := ParentMap(board); m != nil; m = ParentMap(m) {
		if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
			outerVars = append(outerVars, vars)
		}
	}
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
func (c *compiler) sampleTween(template *Map, changes []tweenVariable, outerVars []*Map, fraction float64) *Map {
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
			if !c.reserveVariableExpansion(template.LastRef().AST(), variableExpansionScalarValueUnits(p.binding)) {
				return true
			}
			p.Value = cloneASTScalar(p.binding)
			p.binding = nil
		}
		return false
	})
	if c.stopped() {
		return nil
	}
	vars := frame.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map()
	for _, v := range changes {
		value := v.startText
		if fraction > 0 {
			number := (1-fraction)*v.from + fraction*v.to
			value = strconv.FormatFloat(number, 'f', v.precision, 64) + v.unit
		}
		vars.getFieldIndexed(v.path...).Primary_.Value = d2ast.FlatUnquotedString(value)
	}
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
