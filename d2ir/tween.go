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
	// Templates expand in their declaration scope even when copied into a shape with its own vars.
	templateVars *Map
}

type tweenValue interface {
	at(fraction float64) string
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
	outer := stepVariableScopeCount(varsStack)
	if outer == 0 {
		return -1
	}
	boxes = slices.Clone(boxes)
	if len(c.importStack) > 1 {
		// A selected board can leave its module's outer scope. Bind those
		// references here while leaving its own step variables editable.
		for i, box := range boxes {
			if box.Substitution == nil {
				continue
			}
			value, scopeIndex, ok := c.readVariable(varsStack, node, box.Substitution)
			if ok && scopeIndex >= outer && value.Primary() != nil {
				text := value.Primary().Value.ScalarString()
				boxes[i] = d2ast.InterpolationBox{String: &text}
			}
		}
	}
	// Substitution replaces boxes or coalesces the string. Its original
	// text and substitution paths remain immutable and can be shared.
	primary.binding = &tweenBinding{}
	if owner := ParentField(node); owner != nil && IsVar(owner.Parent()) {
		primary.binding.templateVars = varsStack[0]
	}
	switch value := primary.Value.(type) {
	case *d2ast.UnquotedString:
		binding := *value
		binding.Value = boxes
		primary.binding.source = &binding
	case *d2ast.DoubleQuotedString:
		binding := *value
		binding.Value = boxes
		primary.binding.source = &binding
	case *d2ast.BlockString:
		binding := *value
		primary.binding.source = &binding
		if len(c.importStack) > 1 && outer < len(varsStack) {
			return outer
		}
	}
	return -1
}

// The innermost step can have variables only in containers, for example when
// timing comes from CLI flags. All scopes up through that step stay live.
func stepVariableScopeCount(scopes []*Map) int {
	if len(scopes) == 0 {
		return 0
	}
	i := 0
	for m := ParentMap(scopes[0]); m != nil; m = ParentMap(m) {
		if i < len(scopes) && m == ParentMap(scopes[i]) {
			i++
		}
		if NodeBoardKind(m) == BoardStep {
			return i
		}
	}
	return 0
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

func (c *compiler) compileStepTweens(root *Map, duration int) {
	type step struct {
		board   *Map
		changes []tweenChange
	}
	var steps []step
	authored := 1 // The root is also a render-target board.
	var walk func(*Map)
	walk = func(board *Map) {
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
				authored++
				child := field.Map()
				if child == nil {
					continue
				}
				if group == "steps" {
					changes := c.stepChanges(previous, child)
					if hasTweenMotion(changes) {
						steps = append(steps, step{child, changes})
					}
					previous = child
				}
				walk(child)
			}
		}
	}
	walk(root)
	if len(steps) == 0 || c.stopped() || !c.err.Empty() {
		return
	}
	available := d2target.MaxRenderTargetBoards - authored
	if available < len(steps) {
		c.errorf(steps[0].board.LastRef().AST(), "animation needs at least one sample per transition within the limit of %d total boards", d2target.MaxRenderTargetBoards)
		return
	}
	// Later passes may animate structure or annotations on the remaining
	// authored boards. Leave one slot per possible transition when it fits.
	available -= min(available-len(steps), authored-1-len(steps))
	wanted := d2target.TweenFrameCount(duration)
	for i, step := range steps {
		if c.stopped() || !c.err.Empty() {
			return
		}
		count := min(wanted, available/(len(steps)-i))
		c.sampleStep(step.board, step.changes, duration, count)
		available -= count
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

// framePath addresses the destination vars map in each sample. variablePath
// addresses the same variable in the previous board's lexical scopes.
func (c *compiler) variableChanges(changes []tweenChange, previous []*Map, vars *Map, framePath []propertyPathPart, variablePath []*d2ast.StringBox) []tweenChange {
	for _, field := range vars.Fields {
		if c.stopped() {
			break
		}
		if field.suspended || field.Name == nil || (len(variablePath) == 0 && field.Name.ScalarString() == "d2-config") {
			continue
		}
		path := append(variablePath, d2ast.MakeValueBox(field.Name).StringBox())
		if field.Map() != nil {
			changes = c.variableChanges(changes, previous, field.Map(), append(framePath, propertyPathPart{field: field.Name}), path)
		}
		if field.Primary() == nil {
			continue
		}
		start, _ := c.resolveVariable(previous, vars, &d2ast.Substitution{Path: path})
		if start == nil || start.suspended || start.Primary() == nil {
			continue
		}
		value, ok := tweenScalarChange(start.Primary().Value.ScalarString(), field.Primary().Value.ScalarString())
		if ok {
			changes = append(changes, tweenChange{path: slices.Clone(framePath), property: field.Name, value: value})
		}
	}
	return changes
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

func setTweenValue(s *Scalar, value tweenValue, fraction float64) {
	sample := value.at(fraction)
	s.Value = d2ast.FlatUnquotedString(sample)
	s.TweenOffset = 0
	number, numeric := value.(tweenNumericValue)
	s.NumericTween = numeric && number.unit == "" && number.from != number.to
	if s.NumericTween && number.precision >= 0 && fraction != 0 && fraction != 1 {
		// Labels preserve the authored precision. Continuous paint properties
		// can recover the unrounded value, even through a variable alias.
		exact := (1-fraction)*number.from + fraction*number.to
		rounded, _ := strconv.ParseFloat(sample, 64)
		s.TweenOffset = exact - rounded
	}
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

func (c *compiler) sampleStep(board *Map, changes []tweenChange, duration, count int) {
	// The parent step's samples never include its child boards.
	view := *board
	view.Fields = nil
	for _, f := range board.Fields {
		if _, board := d2ast.BoardKeywords[d2ast.Keyword(f.Name)]; !board {
			view.Fields = append(view.Fields, f)
		}
	}
	outerVars := tweenVarScopes(ParentMap(board))
	for i := range count {
		if c.stopped() {
			return
		}
		frame := c.sampleTween(&view, changes, outerVars, float64(i)/float64(count))
		if frame == nil {
			return
		}
		board.TweenFrames = append(board.TweenFrames, frame)
	}
	board.TweenDurationMS = duration
}

// Locate a template's declaration in the sampled step. The template may be
// inside a container or an edge with variables of its own.
func sampledTemplateVars(template, frame, declaration *Map, outer []*Map) []*Map {
	var path []propertyPathPart
	m := declaration
	for ; m != nil && NodeBoardKind(m) != BoardStep; m = ParentMap(m) {
		switch owner := m.Parent().(type) {
		case *Field:
			if owner.Name == nil {
				return nil
			}
			path = append(path, propertyPathPart{field: owner.Name})
		case *Edge:
			path = append(path, propertyPathPart{edge: slices.Index(ParentMap(owner).Edges, owner)})
		default:
			return nil
		}
	}
	// An imported template has already resolved in its module. Inheriting
	// that finished board into another step preserves the resolved value.
	if m == nil || m.Parent() != template.Parent() {
		return nil
	}
	m = frame
	for i := len(path) - 1; i >= 0; i-- {
		part := path[i]
		if part.field != nil {
			m = m.lookupField(part.field).Map()
		} else if part.edge >= 0 && part.edge < len(m.Edges) {
			m = m.Edges[part.edge].Map()
		} else {
			return nil
		}
		if m == nil {
			return nil
		}
	}
	return append(tweenVarScopes(ParentMap(m)), outer...)
}

// Each sampled board obeys the same expansion limit as an authored board.
// An additional shared budget limits the sum across the entire animation to
// one full-length tween's worth of that work, even for many short tweens.
func (c *compiler) sampleTween(template *Map, changes []tweenChange, outerVars []*Map, fraction float64) *Map {
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
	scopesByDeclaration := make(map[*Map][]*Map)
	c.sampledTemplateScopes = make(map[*Scalar][]*Map)
	defer func() { c.sampledTemplateScopes = nil }()
	walkComposite(frame, func(n Node) bool {
		if p := n.Primary(); p != nil && p.binding != nil {
			if decl := p.binding.templateVars; decl != nil {
				scopes, found := scopesByDeclaration[decl]
				if !found {
					scopes = sampledTemplateVars(template, frame, decl, outerVars)
					scopesByDeclaration[decl] = scopes
				}
				if scopes == nil {
					// A selected import can leave its declaration outside the frame.
					return false
				}
				c.sampledTemplateScopes[p] = scopes
			}
			if !c.reserveVariableExpansion(template.LastRef().AST(), variableExpansionScalarValueUnits(p.binding.source)) {
				return true
			}
			p.Value = cloneASTScalar(p.binding.source)
			p.markdown = nil
			if len(p.binding.markdownOuter) > 0 {
				// Keep module bindings and resolve live step variables together.
				// Each replacement is inserted once, even if it contains ${...}.
				p.markdown = &markdownBinding{source: p.Value.ScalarString(), bound: p.binding.markdownOuter}
			}
			p.binding = nil
		}
		return false
	})
	if c.stopped() {
		return nil
	}
	applyTweenChanges(frame, changes, fraction)
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
