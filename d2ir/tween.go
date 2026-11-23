package d2ir

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
)

type tweenVariable struct {
	variable, annotation *Field
	durationMS           int
	from, to             float64
	precision            int
	unit                 string
	startText, endText   string
}

type stepTween struct {
	board, previous, template *Map
	variables                 []tweenVariable
}

func variableTweenAnnotation(v *Field) *Field {
	if v.Name == nil || v.Primary() == nil || v.Map() == nil {
		return nil
	}
	marker := v.Map().getFieldIndexed(d2ast.FlatUnquotedString("tween"))
	if marker == nil || !marker.Name.IsUnquoted() {
		return nil
	}
	return marker
}

func (m *Map) hasTweenAnnotations() bool {
	if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
		for _, v := range vars.Fields {
			if variableTweenAnnotation(v) != nil {
				return true
			}
		}
	}
	return false
}

// The annotation belongs to a scalar variable, so unwrapping it before
// substitutions preserves scalar references such as label: ${charge}.
func (m *Map) takeTweenAnnotations() []tweenVariable {
	var vars []tweenVariable
	declarations := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map()
	if declarations == nil {
		return nil
	}
	for _, v := range declarations.Fields {
		marker := variableTweenAnnotation(v)
		if marker == nil {
			continue
		}
		vars = append(vars, tweenVariable{variable: v, annotation: marker})
		v.Map().DeleteField(marker.Name.ScalarString())
		if len(v.Map().Fields) == 0 && len(v.Map().Edges) == 0 {
			v.setComposite(nil)
		}
	}
	return vars
}

func (c *compiler) prepareTweens(board, previous *Map, sample bool) []stepTween {
	if board == nil || c.stopped() {
		return nil
	}
	var result []stepTween
	variables := board.takeTweenAnnotations()
	if len(variables) > 0 {
		tween := stepTween{board: board, previous: previous, variables: variables}
		for i := range variables {
			v := &variables[i]
			marker := v.annotation
			if NodeBoardKind(board) != BoardStep {
				c.errorf(marker.LastRef().AST(), "a variable tween must be declared in a step")
				continue
			}
			if v.variable.Composite != nil {
				c.errorf(marker.LastRef().AST(), "a tweened variable must be a scalar")
				continue
			}
			var duration time.Duration
			if marker.Primary() != nil && marker.Composite == nil {
				duration, _ = time.ParseDuration(marker.Primary().Value.ScalarString())
			}
			if duration < time.Millisecond || duration > d2target.MaxTweenDurationMS*time.Millisecond || duration%time.Millisecond != 0 {
				c.errorf(marker.LastRef().AST(), "tween duration must be a whole number of milliseconds between 1ms and 10m")
				continue
			}
			v.durationMS = int(duration.Milliseconds())
		}
		if sample && c.err.Empty() {
			// Exclude children without changing the endpoint or charging/copying
			// a nested storyboard for every sample of its parent.
			view := *board
			view.Fields = nil
			for _, f := range board.Fields {
				if f.Name != nil && findBoardKeyword(f.Name) != -1 {
					continue
				}
				view.Fields = append(view.Fields, f)
			}
			if !c.reserveVariableCopy(board.LastRef().AST(), &view) {
				return nil
			}
			tween.template = view.Copy(board.parent).(*Map)
		}
		result = append(result, tween)
	}
	for _, group := range []string{"layers", "scenarios", "steps"} {
		groupMap := board.getFieldIndexed(d2ast.FlatUnquotedString(group)).Map()
		if groupMap == nil {
			continue
		}
		base := board
		for _, child := range groupMap.Fields {
			if c.stopped() {
				return result
			}
			result = append(result, c.prepareTweens(child.Map(), base, sample)...)
			if group == "steps" {
				base = child.Map()
			}
		}
	}
	return result
}

func lookupTweenStart(board *Map, name d2ast.String) *Field {
	for m := board; m != nil; m = ParentMap(m) {
		if f := m.getFieldIndexed(d2ast.FlatUnquotedString("vars"), name); f != nil {
			return f
		}
	}
	return nil
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

func (c *compiler) finishTween(t stepTween) {
	durationMS := 0
	for i := range t.variables {
		v := &t.variables[i]
		start := lookupTweenStart(t.previous, v.variable.Name)
		from, unit, firstPrecision, okFrom := tweenNumber(start)
		to, endUnit, lastPrecision, okTo := tweenNumber(v.variable)
		if !okFrom || !okTo || unit != endUnit {
			c.errorf(v.annotation.LastRef().AST(), "tween for %q requires an inherited start and destination that are both numbers or both percentages", v.variable.Name.ScalarString())
			return
		}
		v.from, v.to, v.unit = from, to, unit
		v.precision = max(firstPrecision, lastPrecision)
		v.startText = start.Primary().Value.ScalarString()
		v.endText = v.variable.Primary().Value.ScalarString()
		durationMS = max(durationMS, v.durationMS)
	}
	if t.template == nil {
		return
	}

	// A board's own vars are added by compileSubstitutions. Include any outer
	// scope that can be seen by the corresponding authored board.
	var outerVars []*Map
	for m := ParentMap(t.board); m != nil; m = ParentMap(m) {
		if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(); vars != nil {
			outerVars = append(outerVars, vars)
		}
	}
	n := min(d2target.MaxTweenFrames, (durationMS+39)/40)
	for i := 0; i < n; i++ {
		frame := c.sampleTween(t, outerVars, float64(i*durationMS)/float64(n))
		if frame == nil {
			return
		}
		t.board.TweenFrames = append(t.board.TweenFrames, frame)
	}
	t.board.TweenDurationMS = durationMS
}

// Each sampled board obeys the same expansion limit as an authored board.
// An additional shared budget limits the sum across the entire animation to
// one full-length tween's worth of that work, even for many short tweens.
func (c *compiler) sampleTween(t stepTween, outerVars []*Map, elapsedMS float64) *Map {
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
	if !c.reserveVariableCopy(t.board.LastRef().AST(), t.template) {
		return nil
	}
	frame := t.template.Copy(nil).(*Map)
	frame.variableExpansion = c.variableExpansion
	for _, v := range t.variables {
		field := frame.getFieldIndexed(d2ast.FlatUnquotedString("vars"), v.variable.Name)
		value := v.endText
		if elapsedMS == 0 {
			value = v.startText
		} else if elapsedMS < float64(v.durationMS) {
			fraction := elapsedMS / float64(v.durationMS)
			number := (1-fraction)*v.from + fraction*v.to
			value = strconv.FormatFloat(number, 'f', v.precision, 64) + v.unit
		}
		field.Primary_.Value = d2ast.FlatUnquotedString(value)
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
