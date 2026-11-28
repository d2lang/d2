package d2ir

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/internal/htmlentity"
)

type globContext struct {
	root   *globContext
	refctx *RefContext
	// boardGroup is set when root is declared in the map of a board's
	// layers, scenarios or steps.
	boardGroup bool
	// A rule authored inside a glob value lasts only while applying that value.
	inGlobValue bool

	// Keys (see appliedKey) of the fields this glob has already applied to.
	appliedFields map[string]struct{}
	// Keys (see appliedKey) of the edges this glob has already applied to.
	appliedEdges map[string]struct{}
}

type compiler struct {
	arrayAccess         []*arrayAccessCheck
	fieldArrayAccess    map[*Field][]*arrayAccessCheck
	hasTemplateNull     bool
	spreadNulls         []spreadNull
	hasBoardMaps        bool
	hasMapSpread        bool
	hasEdgePropertyGlob bool
	// unreferencedFields is set when an edge's common prefix is left
	// without references (see removeFieldsWithoutReferences).
	unreferencedFields    bool
	spreadRuleCopies      map[spreadRuleScope]*globContext
	fieldOrder            uint64
	err                   *d2parser.ParseError
	ctx                   context.Context
	contextErr            error
	expansionErr          error
	globExpansionErr      error
	halted                bool
	variableExpansion     *variableExpansionBudget
	tweenExpansion        *variableExpansionBudget
	retainTweenBindings   bool
	sampledTemplateScopes map[*Scalar][]*Map
	globExpansion         *globExpansionBudget
	edgeExpansion         *edgeExpansionBudget
	edgeExpansionWork     *edgeExpansionWorkBudget
	edgeExpansionPairs    map[edgeExpansionPair]struct{}

	fs      fs.FS
	imports []string
	// importStack is used to detect cyclic imports.
	importStack []string
	seenImports map[string]struct{}
	// parsedImports and importTemplates are immutable per-compilation caches.
	// Callers always receive deep copies so importer substitutions, references,
	// and glob state cannot leak between import sites.
	parsedImports   map[string]*d2ast.Map
	importTemplates map[string]*importTemplate
	// Composite maps may be revisited through valid variable aliases. Report a
	// rejected cycle once at its source substitution rather than once per alias.
	reportedCompositeCycles map[*d2ast.Substitution]struct{}
	scalarResolutions       map[*Scalar]scalarResolution
	substitutionVisits      map[Node]struct{}
	utf16Pos                bool

	// Stack of globs that must be recomputed at each new object in and below the current scope.
	globContextStack [][]*globContext
	// Used to prevent field globs causing infinite loops.
	globRefContextStack []*RefContext
	// Used to check whether ampersands are allowed in the current map.
	mapRefContextStack   []*RefContext
	filterScopes         map[*Map]*Map
	filterWrites         map[*Map]map[*Field]*Scalar
	collectFilterVars    bool
	filterImports        []filterImport
	filterReads          uint64
	filterOperands       map[filterOperand]filterResult
	lazyGlobBeingApplied bool
	replayingSpreadGlobs bool
	// Steps that the glob being applied has entered, and steps with no map
	// that are to be copied from them once it has applied (see awaitsBase).
	globSteps         map[*Field]struct{}
	pendingStepCopies []*Field
	// Scenarios and steps with no map that a glob made boards by inheritance
	// (see inheritsApplication). Each is made a copy of its base after the key
	// that created it, unless its own value gave it a map.
	inheritingBoards []*Field

	// Lazy field globs are evaluated against newly created fields through a
	// deterministic worklist. This avoids rescanning the entire IR after each
	// insertion while retaining source-order rule application.
	lazyGlobTarget       *Field
	lazyGlobWorklist     []*Field
	lazyGlobQueued       map[*Field]struct{}
	applyingLazyWorklist bool
	lazySettledVersions  map[*Map]uint64
	lazyPostTargets      []*Field
	lazyPostQueued       map[*Field]struct{}
}

type CompileOptions struct {
	// Partial returns the IR built so far together with compile errors. It is
	// for language tooling. A partial IR may be incomplete and must not be rendered.
	Partial bool
	// AnimateDuration overrides the root animate-duration, in milliseconds.
	AnimateDuration *int64
	// AnimateTweens controls sampling of step transitions. Nil follows
	// the root animate-interval. False compiles only the authored endpoints.
	AnimateTweens *bool
	// Context stops parsing and compilation when canceled. A nil Context is
	// treated as context.Background().
	Context  context.Context
	UTF16Pos bool
	// MaxVariableExpansion bounds work added by substitutions and automatic
	// copies. Zero uses DefaultMaxVariableExpansion.
	MaxVariableExpansion int64
	// MaxGlobExpansion bounds work performed by glob matching and
	// materialization. Zero uses DefaultMaxGlobExpansion. Explicit source fields
	// are not counted as materialization work.
	MaxGlobExpansion int64
	// MaxEdgeExpansion bounds distinct edge-segment and endpoint combinations
	// considered by edge globs. Zero uses DefaultMaxEdgeExpansion. Explicit edges
	// do not consume this budget.
	MaxEdgeExpansion int64
	// MaxEdgeExpansionWork bounds all endpoint-pair examinations performed by
	// edge globs, including lazy replays. Zero uses DefaultMaxEdgeExpansionWork.
	MaxEdgeExpansionWork int64
	// FS resolves imports. Nil disables imports. The lib/localfile package
	// provides rooted and explicit unrestricted host-filesystem policies. A filesystem
	// may implement ResolveImportPath(parent, name string) string to use its own
	// path syntax. Other filesystems receive slash-separated io/fs paths.
	FS fs.FS
}

func (c *compiler) errorf(n d2ast.Node, f string, v ...interface{}) {
	c.err.Errors = append(c.err.Errors, d2parser.Errorf(n, f, v...).(d2ast.Error))
}

func (c *compiler) initCompilation() {
	c.err = &d2parser.ParseError{}
	c.seenImports = make(map[string]struct{})
	if c.parsedImports == nil {
		c.parsedImports = make(map[string]*d2ast.Map)
	}
	c.importTemplates = make(map[string]*importTemplate)
	c.reportedCompositeCycles = make(map[*d2ast.Substitution]struct{})
}

func Compile(ast *d2ast.Map, opts *CompileOptions) (*Map, []string, error) {
	if opts == nil {
		opts = &CompileOptions{}
	}
	if opts.AnimateDuration != nil && (*opts.AnimateDuration < 0 || *opts.AnimateDuration > d2target.MaxTweenDurationMS) {
		return nil, nil, errors.New("animate-duration must be between 0 and 600000 milliseconds")
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	variableExpansion, err := newVariableExpansionBudget(opts.MaxVariableExpansion)
	if err != nil {
		return nil, nil, err
	}
	globExpansion, err := newGlobExpansionBudget(opts.MaxGlobExpansion)
	if err != nil {
		return nil, nil, err
	}
	edgeExpansion, err := newEdgeExpansionBudget(opts.MaxEdgeExpansion)
	if err != nil {
		return nil, nil, err
	}
	edgeExpansionWork, err := newEdgeExpansionWorkBudget(opts.MaxEdgeExpansionWork)
	if err != nil {
		return nil, nil, err
	}
	c := &compiler{
		ctx:                 ctx,
		retainTweenBindings: (opts.AnimateTweens == nil || *opts.AnimateTweens) && (opts.AnimateDuration == nil || *opts.AnimateDuration > 0),
		fs:                  opts.FS,
		variableExpansion:   variableExpansion,
		globExpansion:       globExpansion,
		edgeExpansion:       edgeExpansion,
		edgeExpansionWork:   edgeExpansionWork,
		utf16Pos:            opts.UTF16Pos,
	}
	c.initCompilation()
	m := &Map{}
	m.initRoot()
	m.variableExpansion = variableExpansion
	m.parent.(*Field).References[0].Context_.Scope = ast
	m.parent.(*Field).References[0].Context_.ScopeAST = ast

	c.pushImportStack(&d2ast.Import{
		Path: []*d2ast.StringBox{d2ast.RawStringBox(ast.GetRange().Path, true)},
	})
	defer c.popImportStack()

	c.compileMap(m, ast, ast)
	if c.contextErr != nil {
		return nil, nil, c.contextErr
	}
	if err := c.compileLimitError(); err != nil {
		return nil, nil, err
	}
	// Imports are attached now. For an ordinary diagram, two indexed lookups
	// keep endpoint substitution on its usual path without saving expressions.
	c.retainTweenBindings = c.retainTweenBindings &&
		(opts.AnimateDuration != nil || mayResolveTiming(m, "animate-duration")) &&
		(opts.AnimateTweens != nil || mayResolveTiming(m, "animate-interval"))
	c.compileSubstitutions(m, nil)
	c.retainTweenBindings = false
	if c.contextErr != nil {
		return nil, nil, c.contextErr
	}
	if err := c.compileLimitError(); err != nil {
		return nil, nil, err
	}
	c.overlayClasses(m)
	if c.contextErr != nil {
		return nil, nil, c.contextErr
	}
	if err := c.compileLimitError(); err != nil {
		return nil, nil, err
	}
	// Substitutions can grow shared nodes after an earlier alias inserted them
	// (for example through a forward scalar chain in an array spread). Recheck
	// the fully resolved IR before cleanup or any public caller can observe it.
	if err := ReserveVariableExpansionAliases(ctx, m); err != nil {
		return nil, nil, err
	}
	m.removeSuspendedFields()
	if c.err.Empty() && !c.stopped() {
		c.compileTweens(m, opts)
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if err := c.compileLimitError(); err != nil {
		return nil, nil, err
	}
	if !c.err.Empty() {
		if opts.Partial {
			return m, c.imports, c.err
		}
		return nil, nil, c.err
	}
	return m, c.imports, nil
}

func (c *compiler) compileLimitError() error {
	if c.expansionErr == nil && c.globExpansionErr == nil {
		return nil
	}
	if !c.err.Empty() {
		return c.err
	}
	if c.expansionErr != nil {
		return c.expansionErr
	}
	return c.globExpansionErr
}

func (c *compiler) overlayClasses(m *Map) {
	if c.stopped() {
		return
	}
	classes := m.getFieldIndexed(d2ast.FlatUnquotedString("classes"))
	if classes == nil || classes.Map() == nil {
		return
	}

	layersField := m.getFieldIndexed(d2ast.FlatUnquotedString("layers"))
	if layersField == nil {
		return
	}
	layers := layersField.Map()
	if layers == nil {
		return
	}

	for _, lf := range layers.Fields {
		if c.stopped() {
			return
		}
		if lf.Map() == nil || lf.Primary() != nil {
			continue
		}
		l := lf.Map()
		lClasses := l.getFieldIndexed(d2ast.FlatUnquotedString("classes"))

		if lClasses == nil {
			if !c.reserveVariableCopy(lf.LastRef().AST(), classes) {
				return
			}
			lClasses = classes.Copy(l).(*Field)
			l.appendField(lClasses)
		} else if lClasses.Map() != nil {
			if !c.reserveVariableCopy(lf.LastRef().AST(), classes) {
				return
			}
			base := classes.Copy(l).(*Field)
			if !c.reserveVariableCopy(lf.LastRef().AST(), lClasses.Map()) {
				return
			}
			overlayMapIndexed(base.Map(), lClasses.Map())
			l.DeleteField("classes")
			l.appendField(base)
		}

		c.overlayClasses(l)
	}
}

func (c *compiler) compileSubstitutions(m *Map, varsStack []*Map) {
	if c.unreferencedFields {
		removeFieldsWithoutReferences(m)
	}
	outerNulls := c.spreadNulls
	outerChecks, outerFields := c.arrayAccess, c.fieldArrayAccess
	c.spreadNulls, c.arrayAccess, c.fieldArrayAccess = nil, nil, nil
	outerResolutions, outerVisits := c.scalarResolutions, c.substitutionVisits
	c.scalarResolutions = make(map[*Scalar]scalarResolution)
	c.substitutionVisits = make(map[Node]struct{})
	c.compileSubstitutionsWalk(m, varsStack, nil)
	c.scalarResolutions, c.substitutionVisits = outerResolutions, outerVisits
	c.finishSpreadNulls()
	for _, check := range c.arrayAccess {
		if !check.reopened {
			c.errorf(check.ref.String, "cannot index into array")
		}
	}
	c.spreadNulls, c.arrayAccess, c.fieldArrayAccess = outerNulls, outerChecks, outerFields
}

// removeFieldsWithoutReferences removes, from m and the maps in it, every field
// that holds nothing and that no key made. createEdge makes the fields of an
// edge's common prefix without a reference, for the edge's keys to add theirs,
// so they have none if the edge fails or its globs match nothing (near.a ->
// near.b, a.b.*.x -> a.b.*.y); it then sets unreferencedFields, and
// compileSubstitutions calls this. Every pass after that one reads a field's
// references.
func removeFieldsWithoutReferences(m *Map) {
	for i := 0; i < len(m.Fields); i++ {
		f := m.Fields[i]
		fm := f.Map()
		if fm != nil {
			removeFieldsWithoutReferences(fm)
		}
		if len(f.References) > 0 || f.Primary() != nil {
			continue
		}
		if f.Composite != nil && (fm == nil || len(fm.Fields) > 0 || len(fm.Edges) > 0) {
			continue
		}
		m.removeField(i)
		i--
	}
}

func (c *compiler) compileSubstitutionsWalk(m *Map, varsStack []*Map, source d2ast.Node) {
	seen := c.substitutionVisits
	if c.stopped() {
		return
	}
	if !c.visitSubstitutionNode(m, source, seen) {
		return
	}
	for _, f := range m.Fields {
		if c.stopped() {
			return
		}
		if f.Name == nil {
			continue
		}
		if d2ast.Keyword(f.Name) == "vars" && f.Map() != nil {
			varsStack = append([]*Map{f.Map()}, varsStack...)
		}
	}
	for i := 0; i < len(m.Fields); i++ {
		if c.stopped() {
			return
		}
		f := m.Fields[i]
		if !c.visitSubstitutionNode(f, f.LastRef().AST(), seen) {
			return
		}
		if f.Primary() != nil {
			if !c.visitSubstitutionNode(f.Primary(), f.LastRef().AST(), seen) {
				return
			}
			removed := c.resolveSubstitutions(varsStack, f)
			if removed {
				i--
			}
		}
		// Whole-value aliases share their original composite, including its
		// lexical bindings, even if its declaration appears later.
		scopes := varsStack
		if f.Composite != nil {
			if owner, ok := f.Composite.Parent().(*Field); ok && owner != f {
				scopes = declarationScopes(owner, varsStack)
			}
		}
		if arr, ok := f.Composite.(*Array); ok {
			c.compileArraySubstitutions(arr, scopes, f.LastRef().AST())
		} else if f.Map() != nil {
			c.compileSubstitutionsWalk(f.Map(), scopes, f.LastRef().AST())
			if f.Name != nil && d2ast.Keyword(f.Name) == "vars" {
				c.validateConfigs(f.Map().getFieldIndexed(d2ast.FlatUnquotedString("d2-config")))
			}
		}
	}
	for _, e := range m.Edges {
		if c.stopped() {
			return
		}
		if !c.visitSubstitutionNode(e, e.LastRef().AST(), seen) {
			return
		}
		if e.Primary() != nil {
			if !c.visitSubstitutionNode(e.Primary(), e.LastRef().AST(), seen) {
				return
			}
			c.resolveSubstitutions(varsStack, e)
		}
		if e.Map() != nil {
			c.compileSubstitutionsWalk(e.Map(), varsStack, e.LastRef().AST())
		}
	}
}

func (c *compiler) compileArraySubstitutions(arr *Array, scopes []*Map, source d2ast.Node) {
	if !c.visitSubstitutionNode(arr, source, c.substitutionVisits) {
		return
	}
	for i := 0; i < len(arr.Values) && !c.stopped(); {
		val := arr.Values[i]
		if !c.visitSubstitutionNode(val, source, c.substitutionVisits) {
			return
		}
		before := len(arr.Values)
		if scalar, ok := val.(*Scalar); ok {
			c.resolveSubstitutions(scopes, scalar)
		}
		// A spread replaces this element with already visited source elements.
		i += 1 + len(arr.Values) - before
	}
}

func (c *compiler) visitSubstitutionNode(node Node, source d2ast.Node, seen map[Node]struct{}) bool {
	if node == nil {
		return true
	}
	if _, ok := seen[node]; ok {
		return c.reserveVariableExpansion(source, variableExpansionNodeUnits(node))
	}
	seen[node] = struct{}{}
	return true
}

// ParseAnimateInterval returns the source interval in milliseconds. A bare
// integer uses milliseconds, matching the CLI flag.
func ParseAnimateInterval(value string) (int64, bool) {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		value += "ms"
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 || duration%time.Millisecond != 0 {
		return 0, false
	}
	// ParseDuration truncates below a nanosecond. Only an actual zero
	// should turn an animation off.
	if duration == 0 && strings.ContainsAny(value, "123456789") {
		return 0, false
	}
	return duration.Milliseconds(), true
}

func (c *compiler) validateConfigs(configs *Field) {
	if configs == nil || configs.Map() == nil {
		return
	}

	if NodeBoardKind(ParentMap(ParentMap(configs))) == "" {
		c.errorf(configs.LastRef().AST(), `"%s" can only appear at root vars`, configs.Name.ScalarString())
		return
	}

	for _, f := range configs.Map().Fields {
		var val string
		if f.Primary() == nil {
			if f.Name.ScalarString() != "theme-overrides" && f.Name.ScalarString() != "dark-theme-overrides" && f.Name.ScalarString() != "data" {
				c.errorf(f.LastRef().AST(), `"%s" needs a value`, f.Name.ScalarString())
				continue
			}
		} else {
			val = f.Primary().Value.ScalarString()
		}

		name := f.Name.ScalarString()
		if name == "animate-progress" || name == "animate-duration" {
			board := ParentMap(ParentMap(configs))
			if !board.Root() {
				global := RootMap(board).getFieldIndexed(d2ast.FlatUnquotedString("vars"), configs.Name, f.Name)
				if global == nil || global.LastRef().AST() != f.LastRef().AST() {
					c.errorf(f.LastRef().AST(), `"%s" can only be set in the diagram's root vars.d2-config`, name)
					continue
				}
			}
		}
		switch name {
		case "animate-duration":
			if duration, ok := ParseAnimateInterval(val); !ok || duration > d2target.MaxTweenDurationMS || f.Composite != nil {
				c.errorf(f.LastRef().AST(), `expected "animate-duration" between 0ms and 10m in whole milliseconds, got %q`, val)
			}
		case "animate-interval":
			if _, ok := ParseAnimateInterval(val); !ok || f.Composite != nil {
				c.errorf(f.LastRef().AST(), `expected a non-negative whole number of milliseconds or a duration in whole milliseconds for "animate-interval", got %q`, val)
			}
		case "scale":
			scale, err := strconv.ParseFloat(val, 64)
			if err != nil || math.IsNaN(scale) || math.IsInf(scale, 0) || (scale <= 0 && scale != -1) || f.Composite != nil {
				c.errorf(f.LastRef().AST(), `expected a finite positive scale or -1 for automatic sizing, got %q`, val)
			}
		case "sketch", "center", "force-appendix", "animate-progress":
			_, err := strconv.ParseBool(val)
			if err != nil || ((name == "force-appendix" || name == "animate-progress") && f.Composite != nil) {
				c.errorf(f.LastRef().AST(), `expected a boolean for "%s", got "%s"`, f.Name.ScalarString(), val)
				continue
			}
		case "theme-overrides", "dark-theme-overrides", "data":
			if f.Map() == nil {
				c.errorf(f.LastRef().AST(), `"%s" needs a map`, f.Name.ScalarString())
				continue
			}
		case "theme-id", "dark-theme-id":
			if _, err := d2themescatalog.Resolve(val); err != nil {
				c.errorf(f.LastRef().AST(), "%s: %v", f.Name.ScalarString(), err)
			}
		case "pad":
			_, err := strconv.Atoi(val)
			if err != nil {
				c.errorf(f.LastRef().AST(), `expected an integer for "%s", got "%s"`, f.Name.ScalarString(), val)
				continue
			}
		case "layout-engine":
		default:
			c.errorf(f.LastRef().AST(), `"%s" is not a valid config`, f.Name.ScalarString())
		}
	}
}

// A whole-value substitution may be resolved after an overlapping spread
// has supplied children. Merge at the assignment's position so later children
// survive, while an untouched alias can continue sharing its source map.
func (c *compiler) applyCompositeSubstitution(f *Field, value Composite, order fieldOrder, sub *d2ast.Substitution) {
	site := &spreadSite{fieldOrder: order, compiler: c, source: f}
	if _, array := value.(*Array); array && site.applyArrayReset(f) {
		return
	}
	if value.Map() != nil {
		c.noteMapOpening(f, order)
	}
	if f.Map() != nil && value.Map() != nil {
		if order.before(f.mapReset) {
			return
		}
		if !c.reserveVariableCopy(sub, value) {
			return
		}
		if f.Map().Parent() != f {
			if !c.reserveVariableCopy(sub, f.Map()) {
				return
			}
			f.setComposite(f.copyAliasMap(f))
		}
		mergeMap(f.Map(), value.Map(), true, site, site.insertionIndex(f.Map()))
	} else if !order.before(f.compositeOrder) {
		f.setComposite(value)
	}
	if !order.before(f.compositeOrder) {
		f.compositeOrder = order
	}
}

type scalarResolution uint8

const (
	resolvingScalar scalarResolution = iota + 1
	resolvedScalar
	failedScalar
)

func hasSubstitution(s d2ast.Scalar) bool {
	var boxes []d2ast.InterpolationBox
	switch s := s.(type) {
	case *d2ast.UnquotedString:
		boxes = s.Value
	case *d2ast.DoubleQuotedString:
		boxes = s.Value
	case *d2ast.BlockString:
		return strings.Contains(s.Value, "${")
	}
	for _, box := range boxes {
		if box.Substitution != nil {
			return true
		}
	}
	return false
}

func (c *compiler) resolveSubstitutions(varsStack []*Map, node Node) (removedField bool) {
	primary := node.Primary()
	if c.stopped() || !hasSubstitution(primary.Value) || c.scalarResolutions[primary] != 0 {
		return false
	}
	c.scalarResolutions[primary] = resolvingScalar
	errorsBefore := len(c.err.Errors)
	defer func() {
		if len(c.err.Errors) > errorsBefore || c.scalarResolutions[primary] == failedScalar {
			c.scalarResolutions[primary] = failedScalar
		} else {
			c.scalarResolutions[primary] = resolvedScalar
		}
	}()
	if scopes := c.sampledTemplateScopes[primary]; scopes != nil {
		varsStack = scopes
	}
	markdownOuterStart := -1
	if c.retainTweenBindings {
		markdownOuterStart = c.saveTweenBinding(node, varsStack)
	}
	// Partially bound imported strings keep their successful boxes. Visit later
	// references as well so their module-local values survive selection.
	var subbed, missing bool

	switch s := node.Primary().Value.(type) {
	case *d2ast.UnquotedString:
		for i, box := range s.Value {
			if c.stopped() {
				return
			}
			if box.Substitution != nil && box.String == nil {
				resolvedField, _, ok := c.readVariable(varsStack, node, box.Substitution)
				if resolvedField != nil && resolvedField.Primary() != nil {
					if _, ok := resolvedField.Primary().Value.(*d2ast.Null); ok {
						resolvedField = nil
					}
				}
				if !ok || resolvedField == nil {
					if ok {
						c.errorf(node.LastRef().AST(), `could not resolve variable "%s"`, strings.Join(box.Substitution.IDA(), "."))
					}
					if len(c.importStack) <= 1 {
						return
					}
					missing = true
					continue
				}
				if resolvedField.Composite != nil && substitutionUsesComposite(node, box.Substitution.Spread) {
					if compositeContainsNode(resolvedField.Composite, node) ||
						(box.Substitution.Spread && c.spreadCompositeReferencesNode(resolvedField.Composite, node, varsStack)) {
						c.reportCompositeCycle(box.Substitution)
						return
					}
				}
				// A spread, edge or overlay copies child expressions. Bind a
				// forward-declared map before those copies leave its scope.
				if m := resolvedField.Map(); m != nil && copiesSubstitutionMap(node, box.Substitution.Spread) {
					if _, visited := c.substitutionVisits[m]; !visited && !(box.Substitution.Spread && hasUnresolvedMapSpread(m)) {
						c.compileSubstitutionsWalk(m, compositeDeclarationScopes(resolvedField, varsStack), box.Substitution)
						if c.stopped() {
							return
						}
						if compositeContainsNode(m, node) {
							c.reportCompositeCycle(box.Substitution)
							return
						}
					}
				}
				if box.Substitution.Spread {
					if resolvedField.Composite == nil {
						c.errorf(box.Substitution, "cannot spread non-composite")
						continue
					}
					switch n := node.(type) {
					case *Scalar: // Array value
						resolvedArr, ok := resolvedField.Composite.(*Array)
						if !ok {
							c.errorf(box.Substitution, "cannot spread non-array into array")
							continue
						}
						if _, visited := c.substitutionVisits[resolvedArr]; !visited {
							if !c.reserveVariableExpansion(box.Substitution, 1) {
								return
							}
							c.compileArraySubstitutions(resolvedArr, compositeDeclarationScopes(resolvedField, varsStack), box.Substitution)
							if c.stopped() {
								return
							}
						}
						arr := n.parent.(*Array)
						// The values remain shared in the IR, but public IR consumers may
						// later copy or marshal every logical occurrence. Charge the full
						// recursively reachable value cost before inserting the aliases.
						if !c.reserveVariableCopy(box.Substitution, resolvedArr) {
							return
						}
						for i, s := range arr.Values {
							if s == n {
								arr.Values = slices.Replace(arr.Values, i, i+1, resolvedArr.Values...)
								break
							}
						}
					case *Field:
						m := ParentMap(n)
						if resolvedField.Map() != nil {
							if hasUnresolvedMapSpread(resolvedField.Map()) {
								c.errorf(box.Substitution, `cannot spread composite variable "%s" before its spread substitutions are resolved`, strings.Join(box.Substitution.IDA(), "."))
								return
							}
							if !c.reserveVariableCopy(box.Substitution, resolvedField.Map()) {
								return
							}
							expandSubstitution(m, resolvedField.Map(), n, true, c)
							if c.stopped() {
								return
							}
						}
						// Remove the placeholder field
						for i, f2 := range m.Fields {
							if n == f2 {
								m.removeField(i)
								removedField = true
								break
							}
						}

						if removedField && !c.lazyGlobBeingApplied {
							globs := m.globs
							if c.hasEdgePropertyGlob && m.shouldReplayEdgeProperties(resolvedField.Map()) {
								globs = c.edgePropertyGlobsForSpread(m, box.Substitution)
							}
							origGlobStack := c.globContextStack
							c.globContextStack = append(c.globContextStack, globs)
							for _, gctx := range globs {
								old := c.lazyGlobBeingApplied
								priorReplay := c.replayingSpreadGlobs
								c.lazyGlobBeingApplied, c.replayingSpreadGlobs = true, true
								c.compileKey(gctx.refctx)
								c.lazyGlobBeingApplied, c.replayingSpreadGlobs = old, priorReplay
							}
							c.globContextStack = origGlobStack
						}

					}
				}
				useOrder := node.Primary().order
				if resolvedField.Primary() == nil {
					if resolvedField.Composite == nil {
						c.errorf(node.LastRef().AST(), `cannot substitute variable without value: "%s"`, strings.Join(box.Substitution.IDA(), "."))
						return
					}
					if len(s.Value) > 1 {
						c.errorf(node.LastRef().AST(), `cannot substitute composite variable "%s" as part of a string`, strings.Join(box.Substitution.IDA(), "."))
						return
					}
					switch n := node.(type) {
					case *Field:
						n.Primary_ = nil
					case *Edge:
						n.Primary_ = nil
					}
				} else {
					if i == 0 && len(s.Value) == 1 {
						if !c.reserveVariableExpansion(box.Substitution, int64(len(resolvedField.Primary().Value.ScalarString()))) {
							return
						}
						value := resolvedField.Primary()
						node.Primary().Value = value.Value
						if block, ok := value.Value.(*d2ast.BlockString); ok {
							// The two labels can resolve again in different import scopes.
							copy := *block
							node.Primary().Value = &copy
						}
						node.Primary().markdown = value.markdown
						node.Primary().globPattern = value.globPattern
						node.Primary().TweenOffset = value.TweenOffset
					} else {
						s.Value[i].String = go2.Pointer(resolvedField.Primary().Value.ScalarString())
						subbed = true
					}
				}
				if resolvedField.Composite != nil {
					switch n := node.(type) {
					case *Field:
						if !box.Substitution.Spread {
							c.applyCompositeSubstitution(n, resolvedField.Composite, useOrder, box.Substitution)
						}
					case *Edge:
						if resolvedField.Composite.Map() == nil {
							c.errorf(node.LastRef().AST(), `cannot substitute array variable "%s" to an edge`, strings.Join(box.Substitution.IDA(), "."))
							return
						}
						m := resolvedField.Map()
						if !c.reserveVariableCopy(box.Substitution, m) {
							return
						}
						if n.Map_ == nil {
							n.Map_ = m.Copy(n).(*Map)
							n.Map_.nestDeclarationOrder(&useOrder)
						} else {
							site := &spreadSite{fieldOrder: useOrder, compiler: c, source: n}
							mergeMap(n.Map_, m, true, site, site.insertionIndex(n.Map_))
						}
					}
				}
			}
		}
		if subbed && !missing {
			units := interpolationBytes(s.Value)
			if s.Pattern != nil {
				// At most one literal span per parsed token plus the tail.
				addVariableExpansionUnits(&units, len(s.Pattern)+1)
			}
			if !c.reserveVariableExpansion(node.LastRef().AST(), units) {
				return
			}
			boxes := s.Value
			s.Coalesce()
			if s.Pattern != nil {
				node.Primary().globPattern = makeInterpolatedGlobPattern(s.ScalarString(), boxes)
			}
		}
	case *d2ast.DoubleQuotedString:
		for i, box := range s.Value {
			if c.stopped() {
				return
			}
			if box.Substitution != nil && box.String == nil {
				resolvedField, _, ok := c.readVariable(varsStack, node, box.Substitution)
				if !ok || resolvedField == nil {
					if ok {
						c.errorf(node.LastRef().AST(), `could not resolve variable "%s"`, strings.Join(box.Substitution.IDA(), "."))
					}
					if len(c.importStack) <= 1 {
						return
					}
					missing = true
					continue
				}
				primary := resolvedField.Primary()
				if primary == nil {
					name := strings.Join(box.Substitution.IDA(), ".")
					if resolvedField.Composite == nil {
						c.errorf(node.LastRef().AST(), `cannot substitute variable without value: "%s"`, name)
					} else {
						c.errorf(node.LastRef().AST(), `cannot substitute map variable "%s" in quotes`, name)
					}
					return
				}
				s.Value[i].String = go2.Pointer(primary.Value.ScalarString())
				if len(s.Value) == 1 {
					node.Primary().TweenOffset = primary.TweenOffset
				}
				subbed = true
			}
		}
		if subbed && !missing {
			if !c.reserveVariableExpansion(node.LastRef().AST(), interpolationBytes(s.Value)) {
				return
			}
			s.Coalesce()
		}
	case *d2ast.BlockString:
		if strings.Contains(s.Value, "${") {
			s.Value = c.resolveMarkdown(node, s.Value, varsStack, markdownOuterStart)
		}
	}
	return removedField
}

func interpolationBytes(values []d2ast.InterpolationBox) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	var total int64
	for _, box := range values {
		if box.Substitution == nil || box.String == nil {
			continue
		}
		n := int64(len(*box.String))
		if n > maxInt64-total {
			return maxInt64
		}
		total += n
	}
	return total
}

func (c *compiler) reportCompositeCycle(substitution *d2ast.Substitution) {
	if _, reported := c.reportedCompositeCycles[substitution]; reported {
		return
	}
	if c.reportedCompositeCycles == nil {
		c.reportedCompositeCycles = make(map[*d2ast.Substitution]struct{})
	}
	c.reportedCompositeCycles[substitution] = struct{}{}
	c.errorf(substitution, `cyclic composite variable reference "%s"`, strings.Join(substitution.IDA(), "."))
}

func copiesSubstitutionMap(node Node, spread bool) bool {
	switch n := node.(type) {
	case *Edge:
		return true
	case *Field:
		return spread || n.Map() != nil
	default:
		return false
	}
}

func substitutionUsesComposite(node Node, spread bool) bool {
	switch node.(type) {
	case *Field, *Edge:
		return true
	case *Scalar:
		return spread
	default:
		return false
	}
}

// spreadCompositeReferencesNode follows unresolved spread substitutions
// anywhere below composite without mutating it. Map spread expansion copies
// fields, while array spread expansion shares values, so a pointer-only
// containment check cannot see an indirect cycle until after the first
// expansion. Following the spread dependencies first keeps cycle rejection
// ahead of every mutation.
func (c *compiler) spreadCompositeReferencesNode(composite Composite, target Node, varsStack []*Map) bool {
	stack := []Composite{composite}
	seen := make(map[Composite]struct{})
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[current]; ok {
			continue
		}
		seen[current] = struct{}{}
		if compositeContainsNode(current, target) {
			return true
		}

		for _, candidate := range compositeSpreadNodes(current) {
			primary := candidate.Primary()
			if primary == nil {
				continue
			}
			unquoted, ok := primary.Value.(*d2ast.UnquotedString)
			if !ok {
				continue
			}
			for _, box := range unquoted.Value {
				if box.Substitution == nil || !box.Substitution.Spread {
					continue
				}
				resolved, _ := c.resolveVariable(varsStack, candidate, box.Substitution)
				if resolved != nil && resolved.Composite != nil {
					stack = append(stack, resolved.Composite)
				}
			}
		}
	}
	return false
}

func compositeSpreadNodes(composite Composite) (nodes []Node) {
	walkComposite(composite, func(n Node) bool {
		switch n := n.(type) {
		case *Field:
			if n != nil && n.Name == nil {
				nodes = append(nodes, n)
			}
		case *Scalar:
			if n != nil {
				if _, ok := n.Parent().(*Array); ok {
					nodes = append(nodes, n)
				}
			}
		}
		return false
	})
	return nodes
}

func hasUnresolvedMapSpread(m *Map) bool {
	for _, field := range m.Fields {
		if field == nil || field.Name == nil {
			return true
		}
	}
	return false
}

// compositeContainsNode reports whether target is reachable through a
// composite's child structure. Composite substitutions deliberately share the
// resolved value, so attaching a composite below one of its own descendants
// would turn the IR tree into a cycle. Use an iterative walk with a visited set
// because earlier valid substitutions can make the structure a DAG, and the
// check must also terminate defensively if handed an already-cyclic IR.
func compositeContainsNode(composite Composite, target Node) bool {
	return walkComposite(composite, func(n Node) bool {
		return n == target
	})
}

func walkComposite(composite Composite, visit func(Node) bool) bool {
	stack := []Node{composite}
	seen := make(map[Node]struct{})
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		if visit(n) {
			return true
		}

		switch n := n.(type) {
		case *Map:
			if n == nil {
				continue
			}
			for _, f := range n.Fields {
				stack = append(stack, f)
			}
			for _, e := range n.Edges {
				stack = append(stack, e)
			}
		case *Field:
			if n != nil && n.Composite != nil {
				stack = append(stack, n.Composite)
			}
		case *Edge:
			if n != nil && n.Map_ != nil {
				stack = append(stack, n.Map_)
			}
		case *Array:
			if n == nil {
				continue
			}
			for _, value := range n.Values {
				stack = append(stack, value)
			}
		}
	}
	return false
}

// Bind a dependency in its own scope before taking a scalar value or
// descending through a forward map alias.
func (c *compiler) resolveVariableField(scopes []*Map, node Node, f *Field, source d2ast.Node) bool {
	primary := f.Primary()
	if primary == nil {
		return true
	}
	switch c.scalarResolutions[primary] {
	case resolvingScalar:
		c.errorf(source, `cyclic variable reference "%s"`, f.Name.ScalarString())
	case 0:
		if hasSubstitution(primary.Value) {
			if !c.reserveVariableExpansion(source, 1) {
				return false
			}
			c.resolveSubstitutions(declarationScopes(f, scopes), f)
		}
	}
	if state := c.scalarResolutions[primary]; state == resolvingScalar || state == failedScalar {
		c.scalarResolutions[node.Primary()] = failedScalar
		return false
	}
	return true
}

func compositeDeclarationScopes(f *Field, scopes []*Map) []*Map {
	if owner, ok := f.Composite.Parent().(*Field); ok {
		f = owner
	}
	return declarationScopes(f, scopes)
}

// Rebuild the local part of a declaration's environment. The shared
// suffix can include outer scopes of a detached board or animation frame.
func declarationScopes(f *Field, scopes []*Map) []*Map {
	var local []*Map
	for m := ParentMap(f); m != nil; m = ParentMap(m) {
		if i := slices.Index(scopes, m); i >= 0 {
			return append(local, scopes[i:]...)
		}
		if vars := m.getFieldIndexed(d2ast.FlatUnquotedString("vars")); vars != nil && d2ast.Keyword(vars.Name) == "vars" && vars.Map() != nil {
			local = append(local, vars.Map())
		}
	}
	return local
}

// Reserved keys historically allowed distinct quoted and unquoted bindings.
// Search every lexical scope for the original spelling before accepting the
// other spelling. This preserves both shadowing and existing templates.
func (c *compiler) resolveVariable(scopes []*Map, node Node, sub *d2ast.Substitution) (*Field, int) {
	f, index, _ := c.findVariable(scopes, node, sub, false)
	return f, index
}

func (c *compiler) readVariable(scopes []*Map, node Node, sub *d2ast.Substitution) (*Field, int, bool) {
	return c.findVariable(scopes, node, sub, true)
}

func (c *compiler) findVariable(scopes []*Map, node Node, sub *d2ast.Substitution, read bool) (*Field, int, bool) {
	for _, fallback := range []bool{false, true} {
		for i, scope := range scopes {
			for j, p := range sub.Path {
				if c.stopped() {
					return nil, -1, false
				}
				if scope == nil {
					break
				}
				f := lookupVariableField(scope, p.Unbox(), fallback)
				if f == nil {
					break
				}
				last := j == len(sub.Path)-1
				// A declaration can extend its outer binding (x: ${x}-suffix)
				// or read one of its own children (db: ${db.host}).
				if last && i == 0 && f == node {
					break
				}
				// A map can receive children from its primary alias. Its
				// existing children remain readable while evaluating that alias.
				if read && f != node {
					openMap := !last && f.Map() != nil && c.scalarResolutions[f.Primary()] == resolvingScalar
					if !openMap && !c.resolveVariableField(scopes[i:], node, f, sub) {
						return nil, i, false
					}
				}
				if last {
					return f, i, true
				}
				scope = f.Map()
				if read && scope != nil && len(scope.pendingSpreads) != 0 {
					if _, visited := c.substitutionVisits[scope]; !visited {
						c.compileSubstitutionsWalk(scope, compositeDeclarationScopes(f, scopes[i:]), sub)
					}
				}
			}
		}
	}
	return nil, -1, true
}

func lookupVariableField(scope *Map, name d2ast.String, fallback bool) *Field {
	f := scope.getFieldIndexed(name)
	if f != nil || !fallback {
		return f
	}
	if _, reserved := d2ast.ReservedKeywords[strings.ToLower(name.ScalarString())]; !reserved {
		return nil
	}
	key := makeFieldIndexKey(scope.fieldName(name, false))
	key.unquotedReserved = !key.unquotedReserved
	scope.ensureFieldIndex()
	return scope.fieldIndex[key]
}

// A map literal and a dotted key can both create a board. Apply the same
// inheritance rules as soon as either spelling opens its map.
func (c *compiler) openFieldMap(f *Field) {
	f.Composite = &Map{parent: f}
	if c == nil {
		return
	}
	switch NodeBoardKind(f) {
	case BoardScenario, BoardStep, BoardLayer:
		c.hasBoardMaps = true
		c.overlayBase(f)
	}
}

// stepBase returns the map that step i of stepsMap builds on: the map of the
// closest step before it that has one, or else its board's. A step without a
// map is no board.
func stepBase(stepsMap *Map, i int, f *Field) *Map {
	for j := i - 1; j >= 0; j-- {
		if m := stepsMap.Fields[j].Map(); m != nil {
			return m
		}
	}
	return ParentBoard(f).Map()
}

func (c *compiler) overlay(base *Map, f *Field) {
	if f.Map() == nil {
		c.errorf(f.References[0].Context_.Key, "invalid %s", NodeBoardKind(f))
		return
	}

	var source d2ast.Node = f.Name
	if len(f.References) > 0 {
		source = f.LastRef().AST()
	}
	if !c.reserveVariableCopy(source, base) {
		return
	}
	from := base
	c.inheritApplications(base, f)
	base = base.CopyBase(f)
	// Certain fields should never carry forward.
	// If you give your scenario a label, you don't want all steps in a scenario to be labeled the same.
	base.DeleteField("label")
	// An annotation points at something for the length of its own board. The
	// next step starts without the marks and caption of the one before it.
	base.deleteAnnotations()
	if !c.reserveVariableCopy(source, f.Map()) {
		return
	}
	overlayMapIndexed(base, f.Map())
	f.setComposite(base)
	if NodeBoardKind(f) == BoardStep {
		base.globs = c.stepGlobs(from, base)
	}
}

// stepGlobs returns the copies of the globs in scope for step dst, which has
// just been made a copy of board from. Each starts with what its glob has
// applied to in from, as dst's objects are copies of from's. A glob declared
// after this applied to none of them, and its copy in dst starts empty (see
// boardGlob).
func (c *compiler) stepGlobs(from, dst *Map) []*globContext {
	if len(c.globContextStack) == 0 {
		return nil
	}
	var globs []*globContext
	seen := make(map[*globContext]struct{})
	for _, g := range c.globContexts() {
		if _, ok := seen[g.root]; ok || g.refctx.Key.HasTripleGlob() || g.boardGroup {
			continue
		}
		seen[g.root] = struct{}{}
		g2 := g.copy()
		g2.refctx.ScopeMap = dst
		if NodeBoardKind(from) == BoardStep && g.refctx.ScopeMap != from {
			// The previous step's copy, or nil if the glob was declared
			// after that step was made.
			g2.copyApplied(priorBoardCopy(from, g))
		} else {
			g2.copyApplied(appliedIn(from, g))
		}
		globs = append(globs, g2)
	}
	return globs
}

// overlayBase makes scenario or step f, whose map was just created, start as
// a copy of the board it builds on.
func (c *compiler) overlayBase(f *Field) {
	if base := boardBase(f); base != nil {
		c.overlay(base, f)
	}
}

// boardBase returns the map of the board that scenario or step f builds on, or
// nil if f is neither.
func boardBase(f *Field) *Map {
	switch NodeBoardKind(f) {
	case BoardScenario:
		return ParentBoard(f).Map()
	case BoardStep:
		steps := ParentMap(f)
		if i := slices.Index(steps.Fields, f); i >= 0 {
			return stepBase(steps, i, f)
		}
	}
	return nil
}

func (g *globContext) copy() *globContext {
	g2 := *g
	g2.refctx = g.root.refctx.Copy()
	return &g2
}

// boardGlob returns the copy of glob g, in scope where scenario or step dst is
// declared, that applies while dst's map is compiled, or nil if g does not
// apply there.
func (c *compiler) boardGlob(dst *Map, g *globContext) *globContext {
	triple := g.refctx.Key.HasTripleGlob()
	group := !triple && g.boardGroup
	if group && !recursesIntoBoards(g.refctx.Key) && !connectsInBoards(g.refctx.Key) {
		// A glob in a board group's own map, such as * in
		// scenarios: {*: {...}}, selects the boards themselves: scoped to dst,
		// it would select dst's objects instead. A glob whose paths all start
		// with ** reaches the same objects either way, and a connection glob
		// that connectsInBoards applies only in the boards. Their copies apply
		// as objects are created, as a glob declared in dst would, and share
		// what has been applied with g (see appliedKey).
		return nil
	}
	if prior := priorBoardCopy(dst, g); prior != nil {
		// dst is compiled again, as by a glob over boards, or is a step whose
		// copies were made with it. Keep what its copy has applied to.
		return prior
	}
	g2 := g.copy()
	g2.refctx.ScopeMap = dst
	if !triple && !group {
		// Triple globs already apply independently to each board. Any other
		// glob keeps what it applies to in dst to itself. In a scenario, it
		// starts with what it applied to in the base board. In a step, the
		// glob was declared after the step was made (see stepGlobs).
		if NodeBoardKind(dst) == BoardScenario {
			g2.copyApplied(appliedIn(ParentBoard(ParentField(dst)).Map(), g))
		} else {
			g2.copyApplied(nil)
		}
	}
	return g2
}

// appliedIn returns the context that holds what glob g has applied to in
// board map m: its copy in m if g is not scoped to m and m has one, else g.
// A board's scenarios declared by a dotted key, as in steps.s1.scenarios:
// {...}, are compiled with the globs in scope where the key is, which are not
// the copies in their base board s1.
func appliedIn(m *Map, g *globContext) *globContext {
	if g.refctx.ScopeMap != m {
		if prior := priorBoardCopy(m, g); prior != nil {
			return prior
		}
	}
	return g
}

// connectsInBoards reports whether key k, written in the map of a board's
// layers, scenarios or steps, is a connection glob that can only apply inside
// each board: it has no common key, and an endpoint is a single path element
// other than ** (k has no ***). In that map such an endpoint would name a
// board, and boards cannot be connected, so k is not applied there. Each board
// of the map applies a copy instead, as in steps: {(* -> *)[*].style.stroke:
// red} or scenarios: {* -> hub}.
func connectsInBoards(k *d2ast.Key) bool {
	if k.Key != nil || len(k.Edges) == 0 || !k.HasGlob() || k.HasTripleGlob() {
		return false
	}
	for _, e := range k.Edges {
		for _, kp := range []*d2ast.KeyPath{e.Src, e.Dst} {
			if kp == nil || len(kp.Path) != 1 {
				continue
			}
			if us := kp.Path[0].UnquotedString; us == nil || !d2ast.IsDoubleGlob(us.Pattern) {
				return true
			}
		}
	}
	return false
}

// applyInBoards applies glob g, which connectsInBoards in board group m, in
// each board of m that has a map, and in their scenarios and steps, as a glob
// declared in the board after its keys would apply. A board whose map is
// compiled later gets its copy then (see boardGlob).
func (c *compiler) applyInBoards(m *Map, g *globContext) {
	for _, f := range slices.Clone(m.Fields) {
		if c.stopped() {
			return
		}
		bm := f.Map()
		if bm == nil || NodeBoardKind(f) == "" {
			continue
		}
		g2 := c.boardGlob(bm, g)
		if g2 == nil {
			continue
		}
		globs := slices.Clip(bm.globs)
		if !slices.Contains(globs, g2) {
			globs = append(globs, g2)
		}
		c.globContextStack = append(c.globContextStack, globs)
		old := c.lazyGlobBeingApplied
		c.lazyGlobBeingApplied = true
		c.compileKey(g2.refctx)
		c.lazyGlobBeingApplied = old
		bm.globs = c.globContexts()
		c.globContextStack = c.globContextStack[:len(c.globContextStack)-1]
		for _, group := range bm.Fields {
			if group.Name == nil {
				continue
			}
			switch d2ast.Keyword(group.Name) {
			case "scenarios", "steps":
				// Layers do not inherit the globs of their board.
				if group.Map() != nil {
					c.applyInBoards(group.Map(), g2)
				}
			}
		}
	}
}

// recursesIntoBoards reports whether every path of key k starts with **,
// which goes through boards without matching them.
func recursesIntoBoards(k *d2ast.Key) bool {
	if k.Key != nil {
		return startsWithDoubleGlob(k.Key)
	}
	for _, e := range k.Edges {
		if !startsWithDoubleGlob(e.Src) || !startsWithDoubleGlob(e.Dst) {
			return false
		}
	}
	return len(k.Edges) > 0
}

func startsWithDoubleGlob(kp *d2ast.KeyPath) bool {
	if kp == nil || len(kp.Path) == 0 {
		return false
	}
	us := kp.Path[0].UnquotedString
	return us != nil && d2ast.IsDoubleGlob(us.Pattern)
}

// priorBoardCopy returns the copy of g that an earlier compileMap of dst made.
func priorBoardCopy(dst *Map, g *globContext) *globContext {
	for _, prior := range dst.globs {
		if prior.root == g.root && prior.refctx.ScopeMap == dst {
			return prior
		}
	}
	return nil
}

// fields returns the set that holds key k (see appliedKey) of g's
// applications to fields. An absolute key names a board, or a node in a board
// other than g's own. All copies of the glob share these keys, so that a copy
// recognizes what the glob, or another copy of it, applied to in the board
// that a scenario or step is a copy of (see inheritApplications).
func (g *globContext) fields(k string) map[string]struct{} {
	if isAbsoluteAppliedKey(k) {
		return g.root.appliedFields
	}
	return g.appliedFields
}

// edges is fields for g's applications to edges.
func (g *globContext) edges(k string) map[string]struct{} {
	if isAbsoluteAppliedKey(k) {
		return g.root.appliedEdges
	}
	return g.appliedEdges
}

// copyApplied gives g its own copy of what from has applied to, or nothing if
// from is nil.
func (g *globContext) copyApplied(from *globContext) {
	g.appliedFields = make(map[string]struct{})
	g.appliedEdges = make(map[string]struct{})
	if from == nil {
		return
	}
	for k, v := range from.appliedFields {
		g.appliedFields[k] = v
	}
	for k, v := range from.appliedEdges {
		g.appliedEdges[k] = v
	}
}

func (c *compiler) ampersandFilterMap(dst *Map, ast, scopeAST *d2ast.Map) bool {
	for _, n := range ast.Nodes {
		switch {
		case n.MapKey != nil:
			filter := globFilter{compiler: c, candidate: dst.Parent()}
			ok := filter.ampersandFilter(&RefContext{
				Key:      n.MapKey,
				Scope:    ast,
				ScopeMap: dst,
				ScopeAST: scopeAST,
			})
			if n.MapKey.NotAmpersand && !filter.pending {
				ok = !ok
			}
			if !ok {
				if len(c.mapRefContextStack) == 0 {
					return false
				}
				// Unapply glob if appropriate.
				gctx := c.getGlobContext(c.mapRefContextStack[len(c.mapRefContextStack)-1])
				if gctx == nil {
					return false
				}
				ks := gctx.appliedKey(dst)
				delete(gctx.fields(ks), ks)
				delete(gctx.edges(ks), ks)
				return false
			}
		}
	}
	return true
}

func (c *compiler) inheritedGlobs(dst *Map, previousGlobs []*globContext) []*globContext {
	var globs []*globContext
	if len(previousGlobs) > 0 {
		// A root layer with existing glob context stack implies it's an import
		// In which case, the previous globs should be inherited (the else block)
		if NodeBoardKind(dst) == BoardLayer && !dst.Root() {
			for _, g := range previousGlobs {
				if g.refctx.Key.HasTripleGlob() {
					gctx2 := g.copy()
					gctx2.refctx.ScopeMap = dst
					globs = append(globs, gctx2)
				} else if g.boardGroup && g.root.refctx.ScopeMap == ParentMap(ParentField(dst)) && connectsInBoards(g.refctx.Key) {
					// Written in the map of this layer's own layers.
					if g2 := c.boardGlob(dst, g); g2 != nil {
						globs = append(globs, g2)
					}
				}
			}
		} else if NodeBoardKind(dst) == BoardScenario {
			// Each glob is listed twice (see boardGlob for the copies). Only
			// the first is ever looked up, but both run, and dropping one
			// changes the order globs apply in. When the scenario is compiled
			// again, both are its earlier copy.
			for range 2 {
				for _, g := range previousGlobs {
					if g2 := c.boardGlob(dst, g); g2 != nil {
						globs = append(globs, g2)
					}
				}
			}
		} else if NodeBoardKind(dst) == BoardStep {
			// Most copies were made with the step (see stepGlobs).
			for _, g := range previousGlobs {
				if g2 := c.boardGlob(dst, g); g2 != nil {
					globs = append(globs, g2)
				}
			}
		} else {
			globs = append(globs, previousGlobs...)
		}
	}
	return globs
}

func (c *compiler) compileMap(dst *Map, ast, scopeAST *d2ast.Map) {
	c.compileMapWithChecks(dst, ast, scopeAST, true)
}

func (c *compiler) compileMapWithChecks(dst *Map, ast, scopeAST *d2ast.Map, checkFilters bool) {
	if c.stopped() {
		return
	}
	var globs []*globContext
	if len(c.globContextStack) > 0 {
		globs = c.inheritedGlobs(dst, c.globContexts())
	}
	c.globContextStack = append(c.globContextStack, globs)
	defer func() {
		dst.globs = c.globContexts()
		c.globContextStack = c.globContextStack[:len(c.globContextStack)-1]
	}()

	if checkFilters && !c.ampersandFilterMap(dst, ast, scopeAST) {
		return
	}

	for _, n := range ast.Nodes {
		if c.stopped() {
			return
		}
		switch {
		case n.MapKey != nil:
			c.compileKey(&RefContext{
				Key:      n.MapKey,
				Scope:    ast,
				ScopeMap: dst,
				ScopeAST: scopeAST,
			})
		case n.Substitution != nil:
			// placeholder field to be resolved at the end
			c.hasMapSpread = true
			if len(c.globRefContextStack) > 0 {
				if !c.reserveGlobGeneratedFieldWork(dst, n.Substitution) || !c.reserveGlobField(n.Substitution) {
					return
				}
			}
			f := &Field{
				parent: dst,
				Primary_: &Scalar{
					Value: &d2ast.UnquotedString{
						Value: []d2ast.InterpolationBox{{Substitution: n.Substitution}},
					},
				},
				References: []*FieldReference{{
					Context_: &RefContext{
						Scope:    ast,
						ScopeMap: dst,
						ScopeAST: scopeAST,
					},
				}},
			}
			f.declaration = c.assignmentOrder()
			f.Primary_.order = f.declaration
			f.References[0].order = f.declaration
			dst.appendField(f)
		case n.Import != nil:
			// Spread import
			impn, ok := c._import(n.Import, dst)
			if !ok {
				if c.stopped() {
					return
				}
				continue
			}
			if impn.Map() == nil {
				c.errorf(n.Import, "cannot spread import non map into map")
				continue
			}
			impn.(Importable).SetImportAST(n.Import)

			for _, gctx := range impn.Map().globs {
				if c.stopped() {
					return
				}
				if !gctx.refctx.Key.HasTripleGlob() {
					continue
				}
				gctx2 := gctx.copy()
				gctx2.refctx.ScopeMap = dst
				c.compileKey(gctx2.refctx)
				c.ensureGlobContext(gctx2.refctx)
			}

			// Only actual board groups inherit the destination. An imported
			// shape can have ordinary children named scenarios and steps.
			scenariosField := impn.Map().lookupField(d2ast.FlatUnquotedString("scenarios"))
			if scenariosField != nil && scenariosField.Map() != nil {
				for _, sf := range scenariosField.Map().Fields {
					if c.stopped() {
						return
					}
					c.overlay(dst, sf)
				}
			}

			stepsField := impn.Map().lookupField(d2ast.FlatUnquotedString("steps"))
			if stepsField != nil && stepsField.Map() != nil {
				for _, sf := range stepsField.Map().Fields {
					if c.stopped() {
						return
					}
					c.overlay(dst, sf)
				}
			}

			if !c.reserveVariableCopy(n.Import, impn.Map()) {
				return
			}
			overlayMapIndexed(dst, impn.Map())
			c.recordImport(dst, impn.Map())
			impDir := n.Import.Dir()
			c.extendLinks(dst, ParentField(dst), impDir)

			if impnf, ok := impn.(*Field); ok {
				if impnf.Primary_ != nil {
					dstf := ParentField(dst)
					if dstf != nil {
						dstf.Primary_ = impnf.Primary_.Copy(dstf).(*Scalar)
					}
				}
			}
		}
	}
}

func (c *compiler) globContexts() []*globContext {
	return c.globContextStack[len(c.globContextStack)-1]
}

func (c *compiler) getGlobContext(refctx *RefContext) *globContext {
	for _, gctx := range c.globContexts() {
		if gctx.refctx.Equal(refctx) {
			return gctx
		}
	}
	return nil
}

func (c *compiler) ensureGlobContext(refctx *RefContext) *globContext {
	gctx := c.getGlobContext(refctx)
	if gctx != nil {
		return gctx
	}
	gctx = &globContext{
		refctx:        refctx,
		boardGroup:    isBoardGroup(refctx.ScopeMap),
		inGlobValue:   len(c.globRefContextStack) > 1,
		appliedFields: make(map[string]struct{}),
		appliedEdges:  make(map[string]struct{}),
	}
	gctx.root = gctx
	c.globContextStack[len(c.globContextStack)-1] = append(c.globContexts(), gctx)
	return gctx
}

func (c *compiler) compileKey(refctx *RefContext) {
	if c.stopped() {
		return
	}
	postTargetStart := len(c.lazyPostTargets)
	pendingSteps, inheritingBoards := len(c.pendingStepCopies), len(c.inheritingBoards)
	if refctx.Key.HasGlob() || len(c.globRefContextStack) > 0 {
		if !c.reserveGlobWork(refctx.Key, 1) {
			return
		}
	}
	if refctx.order.sequence == 0 {
		refctx.order = c.assignmentOrder()
	}
	var g *globContext
	var declared, inBoards bool
	if refctx.Key.HasGlob() {
		// Replaying a property may first create its style map. Finish that
		// write before evaluating the next deferred selector in source order.
		if stack := c.globRefContextStack; c.replayingSpreadGlobs && len(stack) != 0 &&
			replaysEdgeProperties(stack[len(stack)-1].Key) && replaysEdgeProperties(refctx.Key) {
			return
		}
		for _, refctx2 := range c.globRefContextStack {
			if refctx.Equal(refctx2) {
				// Break the infinite loop.
				return
			}
		}
		c.globRefContextStack = append(c.globRefContextStack, refctx)
		globSteps := c.globSteps
		c.globSteps = nil
		defer func() {
			c.globRefContextStack = c.globRefContextStack[:len(c.globRefContextStack)-1]
			c.globSteps = globSteps
		}()
		declared = c.getGlobContext(refctx) == nil
		g = c.ensureGlobContext(refctx)
		inBoards = isBoardGroup(refctx.ScopeMap) && connectsInBoards(refctx.Key)
		if !inBoards && !c.lazyGlobBeingApplied && replaysEdgeProperties(refctx.Key) {
			refctx.ScopeMap.addEdgeRule(g)
			c.hasEdgePropertyGlob = true
		}
	}
	oldVersion := refctx.ScopeMap.structureVersion
	switch {
	case inBoards:
		// It applies only through each board's copy (see boardGlob). Boards
		// compiled before it get theirs now. The copies are not the glob
		// applying to itself (RefContext.Equal ignores ScopeMap).
		if declared {
			stack := c.globRefContextStack
			c.globRefContextStack = slices.Clip(stack[:len(stack)-1])
			c.applyInBoards(refctx.ScopeMap, g)
			c.globRefContextStack = stack
		}
	case len(refctx.Key.Edges) == 0:
		c.compileField(refctx.ScopeMap, refctx.Key.Key, refctx)
	default:
		c.compileEdges(refctx)
	}
	if refctx.Key.HasGlob() {
		c.copyBases(&c.pendingStepCopies, pendingSteps)
	} else {
		c.copyBases(&c.inheritingBoards, inheritingBoards)
	}
	root := RootMap(refctx.ScopeMap)
	settled := c.lazySettledVersions != nil && c.lazySettledVersions[root] == root.structureVersion
	if oldVersion != refctx.ScopeMap.structureVersion && !c.applyingLazyWorklist && len(c.lazyPostTargets) > postTargetStart {
		targets := c.takeLazyPostTargets(postTargetStart)
		c.applyFieldGlobs(refctx.ScopeMap, targets, true)
		settled = true
	}
	if oldVersion != refctx.ScopeMap.structureVersion && !c.applyingLazyWorklist && !settled {
		for _, gctx2 := range c.globContexts() {
			old := c.lazyGlobBeingApplied
			c.lazyGlobBeingApplied = true
			c.compileKey(gctx2.refctx)
			c.lazyGlobBeingApplied = old
		}
	}
}

func (c *compiler) takeLazyPostTargets(start int) []*Field {
	targets := append([]*Field(nil), c.lazyPostTargets[start:]...)
	c.lazyPostTargets = c.lazyPostTargets[:start]
	for _, target := range targets {
		delete(c.lazyPostQueued, target)
	}
	if len(c.lazyPostQueued) == 0 {
		c.lazyPostQueued = nil
	}
	return targets
}

func (c *compiler) enqueueLazyPostTargets(fields ...*Field) {
	if c.lazyPostQueued == nil {
		c.lazyPostQueued = make(map[*Field]struct{})
	}
	for _, f := range fields {
		if f == nil {
			continue
		}
		if _, exists := c.lazyPostQueued[f]; exists {
			continue
		}
		c.lazyPostQueued[f] = struct{}{}
		c.lazyPostTargets = append(c.lazyPostTargets, f)
	}
}

func (c *compiler) enqueueLazyGlobFields(fields ...*Field) {
	if len(fields) == 0 {
		return
	}
	if c.lazyGlobQueued == nil {
		c.lazyGlobQueued = make(map[*Field]struct{})
	}
	for _, f := range fields {
		if f == nil {
			continue
		}
		if _, queued := c.lazyGlobQueued[f]; queued {
			continue
		}
		c.lazyGlobQueued[f] = struct{}{}
		c.lazyGlobWorklist = append(c.lazyGlobWorklist, f)
	}
}

// An ordinary key inside a map replays rules in that map's scope. Dotted
// writes keep their original references for editing and enter the equivalent
// board scope only for rule evaluation.
func (c *compiler) applyFieldGlobs(scope *Map, fields []*Field, created bool) {
	apply := func(targets []*Field) {
		if !created {
			// Updates to existing fields re-evaluate the applicable rules.
			targets = nil
		}
		c.applyLazyGlobs(targets)
	}
	if !c.hasBoardMaps || len(fields) == 0 || len(c.globRefContextStack) != 0 || c.applyingLazyWorklist {
		apply(fields)
		return
	}
	for len(fields) > 0 {
		board := ParentBoard(fields[0]).Map()
		n := 1
		for n < len(fields) && ParentBoard(fields[n]).Map() == board {
			n++
		}
		pushed := c.pushDottedBoardGlobs(scope, board)
		apply(fields[:n])
		if pushed {
			c.globContextStack = c.globContextStack[:len(c.globContextStack)-1]
		}
		fields = fields[n:]
	}
}

func (c *compiler) pushDottedBoardGlobs(scope, board *Map) bool {
	if !c.hasBoardMaps || len(c.globRefContextStack) != 0 || board == nil || board.Root() {
		return false
	}
	origin := enclosingBoardMap(scope)
	if origin == board {
		return false
	}
	globs := c.dottedBoardGlobs(origin, board, board, c.globContexts())
	c.globContextStack = append(c.globContextStack, globs)
	return true
}

func enclosingBoardMap(scope *Map) *Map {
	if NodeBoardKind(scope) != "" {
		return scope
	}
	return ParentBoard(scope).Map()
}

// Walk the same board boundaries a nested map would cross. Keep local
// rules, and inherit newly declared parent rules in source order.
func (c *compiler) dottedBoardGlobs(origin, board, destination *Map, rules []*globContext) []*globContext {
	if origin == board {
		return rules
	}
	if board.Root() {
		return nil
	}
	parent := ParentBoard(board.Parent()).Map()
	parentRules := c.dottedBoardGlobs(origin, parent, destination, rules)
	inherited := c.inheritedGlobs(board, parentRules)
	if len(board.globs) == 0 {
		board.globs = inherited
		return inherited
	}

	// The saved applications also include rules from the scope where the
	// board was opened. A later dotted write can be outside that scope.
	// Replay rules declared in this board and rules visible across each
	// intervening board boundary, retaining their source order.
	visible := make(map[*globContext]bool, len(inherited))
	for _, rule := range inherited {
		visible[rule.root] = true
	}
	var active []*globContext
	for _, rule := range board.globs {
		_, fromParent := visible[rule.root]
		local := board == destination && !rule.root.inGlobValue && enclosingBoardMap(rule.root.refctx.ScopeMap) == board
		if fromParent || local {
			active = append(active, rule)
			visible[rule.root] = false
		}
	}
	for _, rule := range inherited {
		if visible[rule.root] {
			active = append(active, rule)
			board.globs = append(board.globs, rule)
		}
	}
	return active
}

func (c *compiler) applyLazyGlobs(created []*Field) {
	if len(created) == 0 {
		for _, gctx := range c.globContexts() {
			old := c.lazyGlobBeingApplied
			c.lazyGlobBeingApplied = true
			c.compileKey(gctx.refctx)
			c.lazyGlobBeingApplied = old
		}
		return
	}

	if c.applyingLazyWorklist {
		c.enqueueLazyGlobFields(created...)
		return
	}
	root := RootMap(ParentMap(created[0]))
	if len(c.globContexts()) == 0 {
		// Preserve the settled version and pending post-targets: a later key or
		// nested value can introduce globs. There is no worklist to run yet.
		if c.lazySettledVersions == nil {
			c.lazySettledVersions = make(map[*Map]uint64)
		}
		c.lazySettledVersions[root] = root.structureVersion
		return
	}

	c.applyingLazyWorklist = true
	defer func() {
		if c.lazySettledVersions == nil {
			c.lazySettledVersions = make(map[*Map]uint64)
		}
		c.lazySettledVersions[root] = root.structureVersion
		c.applyingLazyWorklist = false
		c.lazyGlobTarget = nil
		c.lazyGlobWorklist = nil
		c.lazyGlobQueued = nil
	}()
	c.enqueueLazyGlobFields(created...)

	for len(c.lazyGlobWorklist) > 0 {
		if c.stopped() {
			return
		}
		target := c.lazyGlobWorklist[0]
		c.lazyGlobWorklist = c.lazyGlobWorklist[1:]

		var edgeGlobs []*globContext
		for _, gctx := range c.globContexts() {
			if c.stopped() {
				return
			}
			if len(gctx.refctx.Key.Edges) > 0 {
				edgeGlobs = append(edgeGlobs, gctx)
				continue
			}
			c.lazyGlobTarget = target
			old := c.lazyGlobBeingApplied
			c.lazyGlobBeingApplied = true
			c.compileKey(gctx.refctx)
			c.lazyGlobBeingApplied = old
		}
		c.lazyGlobTarget = nil

		// Edge globs can depend on edges emitted by earlier glob rules. Preserve
		// the old fixed-point behavior, but only for edge rules; field rules have
		// already been applied to the precise changed fields above.
		root := RootMap(target.parent.(*Map))
		for len(edgeGlobs) > 0 {
			if c.stopped() {
				return
			}
			before := root.structureVersion
			for _, gctx := range edgeGlobs {
				if c.stopped() {
					return
				}
				old := c.lazyGlobBeingApplied
				c.lazyGlobBeingApplied = true
				c.compileKey(gctx.refctx)
				c.lazyGlobBeingApplied = old
			}
			if before == root.structureVersion {
				break
			}
		}
	}
}

func (c *compiler) compileField(dst *Map, kp *d2ast.KeyPath, refctx *RefContext) {
	if c.stopped() {
		return
	}
	if refctx.Key.Ampersand || refctx.Key.NotAmpersand {
		return
	}

	fa, err := dst.ensureFieldIndexed(kp, refctx, true, c)
	if err != nil {
		c.err.Errors = append(c.err.Errors, err.(d2ast.Error))
		return
	}

	for _, f := range fa {
		if c.stopped() {
			return
		}
		before := f.Primary()
		c._compileField(f, refctx)
		if c.filterReads != 0 && f.Primary() != before && len(c.globRefContextStack) > 0 && IsVar(f) {
			c.updateFilterVariable(f)
		}
	}
}

// Filter-only names such as src and leaf retain their own spelling rules.
func globFilterKey(s d2ast.String) string {
	if !s.IsUnquoted() {
		return ""
	}
	if keyword := d2ast.Keyword(s); keyword != "" {
		return keyword
	}
	return s.ScalarString()
}

func (evaluation *globFilter) ampersandFilter(refctx *RefContext) bool {
	c := evaluation.compiler
	if !refctx.Key.Ampersand && !refctx.Key.NotAmpersand {
		return true
	}
	if len(c.mapRefContextStack) == 0 || !c.mapRefContextStack[len(c.mapRefContextStack)-1].Key.SupportsGlobFilters() {
		c.errorf(refctx.Key, "glob filters cannot be used outside globs")
		return false
	}
	if len(refctx.Key.Edges) > 0 {
		return true
	}

	keyPath := refctx.Key.Key
	if keyPath == nil || len(keyPath.Path) == 0 {
		return false
	}

	filter := refctx.Key.Value.ScalarBox().Unbox()
	if filter == nil {
		c.errorf(refctx.Key, "glob filters cannot be composites")
		return false
	}

	if hasSubstitution(filter) {
		rule := c.mapRefContextStack[len(c.mapRefContextStack)-1]
		operand := &Field{
			parent:     rule.ScopeMap,
			References: []*FieldReference{{Context_: refctx, String: keyPath.Last().Unbox()}},
		}
		filter = evaluation.operand(&Scalar{parent: operand, Value: filter}, rule.ScopeMap)
		if filter == nil {
			return false
		}
	}

	owner := refctx.ScopeMap.Parent()
	scope := refctx.ScopeMap
	firstPart := globFilterKey(keyPath.Path[0].Unbox())
	endpoint := (firstPart == "src" || firstPart == "dst") && len(keyPath.Path) > 1
	if endpoint {
		edge := ParentEdge(scope)
		if edge == nil {
			return false
		}
		path := edge.ID.SrcPath
		if firstPart == "dst" {
			path = edge.ID.DstPath
		}
		node := ParentMap(edge).getFieldIndexed(path...)
		if node == nil {
			return false
		}
		owner, scope = node, node.Map()
		keyPath = &d2ast.KeyPath{Path: keyPath.Path[1:]}
		if len(keyPath.Path) == 1 {
			switch key := globFilterKey(keyPath.Last().Unbox()); key {
			case "level", "leaf", "connected":
				return evaluation.propertyFilter(key, filter, node, refctx.Key)
			}
		}
		refctx = &RefContext{
			Key:      &d2ast.Key{Key: keyPath, Value: refctx.Key.Value},
			ScopeMap: scope,
			ScopeAST: refctx.ScopeAST,
		}
	}

	var fa []*Field
	if scope != nil {
		var err error
		fa, err = scope.ensureFieldIndexed(keyPath, refctx, false, c)
		if err != nil {
			c.err.Errors = append(c.err.Errors, err.(d2ast.Error))
			return false
		}
	}
	// A missing or position-only label inherits the owner's primary or ID.
	if len(keyPath.Path) == 1 && globFilterKey(keyPath.Last().Unbox()) == "label" {
		if len(fa) == 0 || fa[0].Primary() == nil {
			return evaluation.labelMatches(owner, filter)
		}
	}
	if len(fa) == 0 {
		if endpoint && len(keyPath.Path) > 1 && !(len(keyPath.Path) == 2 && d2ast.Keyword(keyPath.Path[0].Unbox()) == "style") {
			return false
		}
		if filter.ScalarString() == "*" {
			return false
		}
		// The field/edge has no value for this filter
		// But the filter might still match default, e.g. opacity 1
		// So we make a fake field for the default
		// NOTE: this does not apply to things that themes control, like stroke and fill
		// Nor does it apply to layout things like width and height
		last := globFilterKey(keyPath.Last().Unbox())
		switch last {
		case "shape":
			return matchesFilterValue(d2ast.FlatUnquotedString("rectangle"), filter)
		case "border-radius", "stroke-dash":
			return matchesFilterValue(d2ast.FlatUnquotedString("0"), filter)
		case "opacity":
			return matchesFilterValue(d2ast.FlatUnquotedString("1"), filter)
		case "stroke-width":
			return matchesFilterValue(d2ast.FlatUnquotedString("2"), filter)
		case "icon", "tooltip", "link", "annotation":
			return matchesFilterValue(d2ast.FlatUnquotedString(""), filter)
		case "shadow", "multiple", "3d", "animated", "filled":
			return matchesFilterValue(d2ast.FlatUnquotedString("false"), filter)

		case "label":
			return evaluation.labelMatches(owner, filter)
		case "src", "dst":
			edge, ok := owner.(*Edge)
			if !ok {
				return false
			}
			path := edge.ID.SrcPath
			if last == "dst" {
				path = edge.ID.DstPath
			}
			// Edge IDs are relative to their owning map.
			path = append(RelIDA(nil, ParentMap(edge)), path...)
			parts := make([]string, len(path))
			for i, part := range path {
				parts[i] = part.ScalarString()
			}
			return strings.Join(parts, ".") == filter.ScalarString()
		default:
			if field, ok := owner.(*Field); ok {
				return evaluation.propertyFilter(last, filter, field, refctx.Key)
			}
			return false
		}
	}
	for _, f := range fa {
		matches := evaluation.matches(f, filter)
		if endpoint && matches || !endpoint && !matches {
			return matches
		}
	}
	return !endpoint
}

// handles filters that are not based on fields
func (evaluation *globFilter) propertyFilter(propName string, filter d2ast.Scalar, node *Field, key *d2ast.Key) bool {
	c := evaluation.compiler
	value := filter.ScalarString()
	switch propName {
	case "level":
		levelVal, err := strconv.Atoi(value)
		if err != nil {
			c.errorf(key, `&level must be a non-negative integer, got %q`, value)
			return false
		}
		if levelVal < 0 {
			c.errorf(key, `&level must be a non-negative integer, got %d`, levelVal)
			return false
		}

		level := 0
		parent := ParentField(node)
		for parent != nil && NodeBoardKind(parent) == "" {
			level++
			parent = ParentField(parent)
		}
		return level == levelVal
	case "leaf":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			c.errorf(key, `&leaf must be "true" or "false", got %q`, value)
			return false
		}
		isLeaf := node.Map() == nil || !c.IsContainer(node.Map())
		return isLeaf == boolVal
	case "connected":
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			c.errorf(key, `&connected must be "true" or "false", got %q`, value)
			return false
		}
		isConnected := false
		for _, r := range node.References {
			if r.InEdge() {
				isConnected = true
				break
			}
		}
		return isConnected == boolVal
	}
	return false
}

func (evaluation *globFilter) labelMatches(node Node, filter d2ast.Scalar) bool {
	if primary := node.Primary(); primary != nil {
		value := evaluation.scalar(primary, false)
		if value != nil || evaluation.pending {
			return matchesFilterValue(value, filter)
		}
	}
	// A label-less shape uses its ID, including a map-only variable.
	if field, ok := node.(*Field); ok {
		return matchesFilterValue(field.Name, filter)
	}
	return false
}

func (evaluation *globFilter) matches(f *Field, filter d2ast.Scalar) bool {
	array, _ := f.Composite.(*Array)
	if f.Primary_ != nil {
		value, resolved := evaluation.value(f.Primary_, false)
		if matchesFilterValue(value, filter) {
			return true
		}
		if resolved != nil {
			if !evaluation.reserveVariableExpansion(f.Primary_.Value, int64(len(resolved.Values))) {
				return false
			}
			array = resolved
		}
	}
	return array != nil && evaluation.arrayMatches(array, filter)
}

func (evaluation *globFilter) arrayMatches(array *Array, filter d2ast.Scalar) bool {
	for _, v := range array.Values {
		if evaluation.stopped() {
			return false
		}
		s, ok := v.(*Scalar)
		if !ok {
			continue
		}
		value, nested := evaluation.value(s, false)
		if matchesFilterValue(value, filter) {
			return true
		}
		if nested != nil {
			// Only a spread contributes the referenced array's elements.
			text := s.Value.(*d2ast.UnquotedString)
			if !text.Value[0].Substitution.Spread {
				continue
			}
			if !evaluation.reserveVariableExpansion(s.Value, int64(len(nested.Values))) {
				return false
			}
			if evaluation.arrayMatches(nested, filter) {
				return true
			}
		}
	}
	return false
}

func matchesFilterValue(value, filter d2ast.Scalar) bool {
	if value == nil {
		return false
	}
	if operand, ok := filter.(interpolatedGlobOperand); ok {
		return operand.matches(value.ScalarString())
	}
	if us, ok := filter.(*d2ast.UnquotedString); ok && us.Pattern != nil {
		return matchPattern(value.ScalarString(), us.Pattern)
	}
	return filter.ScalarString() == value.ScalarString()
}

func (c *compiler) _compileField(f *Field, refctx *RefContext) {
	if c.stopped() {
		return
	}
	// In case of filters, we need to pass filters before continuing
	if refctx.Key.Value.Map != nil && refctx.Key.Value.Map.HasFilter() {
		created := f.Map() == nil
		if created {
			f.Composite = &Map{
				parent: f,
			}
		}
		c.mapRefContextStack = append(c.mapRefContextStack, refctx)
		ok := c.ampersandFilterMap(f.Map(), refctx.Key.Value.Map, refctx.ScopeAST)
		c.mapRefContextStack = c.mapRefContextStack[:len(c.mapRefContextStack)-1]
		if c.stopped() {
			return
		}
		board := NodeBoardKind(f) == BoardScenario || NodeBoardKind(f) == BoardStep
		if !ok {
			if created && board && len(f.Map().Fields) == 0 && len(f.Map().Edges) == 0 {
				// A board that the glob filtered out gets its map, a copy of
				// its base, from its own value.
				f.Composite = nil
			}
			return
		}
		if created && board {
			c.overlayBase(f)
			if c.stopped() {
				return
			}
		}
	}

	if (len(refctx.Key.Edges) == 0 || refctx.Key.EdgeKey != nil) && (refctx.Key.Primary.Null != nil || refctx.Key.Value.Null != nil) {
		// For vars, if we delete the field, it may just resolve to an outer scope var of the same name
		// Instead we keep it around, so that resolveSubstitutions can find it
		if !IsVar(ParentMap(f)) {
			m := ParentMap(f)
			c.recordSpreadDeletion(m, &spreadMember{field: f.Name}, refctx.Key)
			if !c.keepChildAfterGlobDeletion(f, refctx) {
				m.deleteField(f)
			}
			return
		}
	}

	if len(refctx.Key.Edges) == 0 && (refctx.Key.Primary.Suspension != nil || refctx.Key.Value.Suspension != nil) {
		if !c.lazyGlobBeingApplied {
			if refctx.Key.Primary.Suspension != nil {
				f.suspended = refctx.Key.Primary.Suspension.Value
			} else {
				f.suspended = refctx.Key.Value.Suspension.Value
			}
		}
		return
	}

	if refctx.Key.Primary.Unbox() != nil {
		if c.ignoreLazyGlob(f) {
			return
		}
		c.setFieldPrimary(f, refctx.Key.Primary.Unbox())
	}

	if refctx.Key.Value.Array != nil {
		a := &Array{
			parent: f,
		}
		c.compileArray(a, refctx.Key.Value.Array, refctx.ScopeAST)
		if c.stopped() {
			return
		}
		f.Composite = a
		f.compositeOrder = c.assignmentOrder()
		f.mapReset = f.compositeOrder
	} else if refctx.Key.Value.Map != nil {
		f.compositeOrder = c.assignmentOrder()
		scopeAST := refctx.Key.Value.Map
		if f.Map() == nil {
			c.openFieldMap(f)
			if c.stopped() {
				return
			}
			if NodeBoardKind(f) == "" {
				scopeAST = refctx.ScopeAST
			}
		} else {
			scopeAST = refctx.ScopeAST
		}
		c.mapRefContextStack = append(c.mapRefContextStack, refctx)
		// A primary assignment can change the label that just matched.
		pushed := c.hasBoardMaps && NodeBoardKind(f) == "" && c.pushDottedBoardGlobs(refctx.ScopeMap, ParentBoard(f).Map())
		c.compileMapWithChecks(f.Map(), refctx.Key.Value.Map, scopeAST, refctx.Key.Primary.Unbox() == nil)
		if pushed {
			c.globContextStack = c.globContextStack[:len(c.globContextStack)-1]
		}
		c.mapRefContextStack = c.mapRefContextStack[:len(c.mapRefContextStack)-1]
		if c.stopped() {
			return
		}
		switch NodeBoardKind(f) {
		case BoardScenario, BoardStep:
			c.overlayClasses(f.Map())
			if c.stopped() {
				return
			}
		}
	} else if refctx.Key.Value.Import != nil {
		// Non-spread import
		n, ok := c._import(refctx.Key.Value.Import, f)
		if !ok {
			return
		}
		n.(Importable).SetImportAST(refctx.Key.Value.Import)
		// A field import supplies defaults. Existing fields are restored
		// below; earlier variable spreads have not materialized theirs yet.
		var fallback fieldOrder
		switch n := n.(type) {
		case *Field:
			n.nestDeclarationOrder(&fallback)
		case *Map:
			n.nestDeclarationOrder(&fallback)
		}
		var existingEdges []*Edge
		if f.Map() != nil {
			existingEdges = f.Map().Edges
		}
		if !c.reserveVariableCopy(refctx.Key.Value.Import, f) {
			return
		}
		originalF := f.Copy(refctx.ScopeMap).(*Field)
		switch n := n.(type) {
		case *Field:
			if n.Primary_ != nil {
				f.Primary_ = n.Primary_.Copy(f).(*Scalar)
			}
			if n.Composite != nil {
				if !c.reserveVariableCopy(refctx.Key.Value.Import, n.Composite) {
					return
				}
				beforeFields, beforeEdges := structureCounts(f.Map())
				f.setComposite(n.Composite.Copy(f).(Composite))
				f.compositeOrder = n.compositeOrder
				markStructureCountDelta(ParentMap(f), beforeFields, beforeEdges, f.Map())
			}
		case *Map:
			f.compositeOrder = fallback
			c.openFieldMap(f)
			if c.stopped() {
				return
			}
			if !c.reserveVariableCopy(refctx.Key.Value.Import, n) {
				return
			}
			overlayMapIndexed(f.Map(), n)
			impDir := refctx.Key.Value.Import.Dir()
			c.extendLinks(f.Map(), f, impDir)
			switch NodeBoardKind(f) {
			case BoardScenario, BoardStep:
				c.overlayClasses(f.Map())
				if c.stopped() {
					return
				}
			}
		}
		if !c.reserveVariableCopy(refctx.Key.Value.Import, originalF) {
			return
		}
		overlayFieldIndexed(f, originalF)
		switch n := n.(type) {
		case *Field:
			c.recordImport(f.Map(), n.Map())
		case *Map:
			c.recordImport(f.Map(), n)
		}
		if existingEdges != nil && f.Map() != nil {
			for _, edge := range existingEdges {
				exists := false
				for _, currentEdge := range f.Map().Edges {
					if currentEdge.ID.Match(edge.ID) {
						exists = true
						break
					}
				}
				if !exists {
					f.Map().appendEdge(edge)
				}
			}
		}

	} else if refctx.Key.Value.ScalarBox().Unbox() != nil {
		if c.ignoreLazyGlob(f) {
			return
		}
		c.setFieldPrimary(f, refctx.Key.Value.ScalarBox().Unbox())
		// If the link is a board, we need to transform it into an absolute path.
		if d2ast.Keyword(f.Name) == "link" {
			c.compileLink(f, refctx)
		}
	}
}

// recordImport records the fields that import src added to dst, which the
// globs being applied reach, as applied by those globs, as ensureField does for
// the fields that a glob's map value creates. Otherwise a glob such as
// p.**: @f, where f declares p, applies to what its own import added, and again
// to what that adds.
func (c *compiler) recordImport(dst, src *Map) {
	if len(c.globRefContextStack) == 0 || dst == nil || src == nil {
		return
	}
	for _, sf := range src.Fields {
		df := dst.lookupField(sf.Name)
		if df == nil {
			continue
		}
		for _, grefctx := range c.globRefContextStack {
			g := c.getGlobContext(grefctx)
			if g == nil || (g.refctx.ScopeMap != nil && treeRoot(df) != treeRoot(g.refctx.ScopeMap)) {
				continue
			}
			ks := g.appliedKey(df)
			g.fields(ks)[ks] = struct{}{}
		}
		c.recordImport(df.Map(), sf.Map())
	}
}

// Whether the current lazy glob being applied should not override the field
// if already set by a non glob key.
func (c *compiler) ignoreLazyGlob(n Node) bool {
	if c.lazyGlobBeingApplied && n.Primary() != nil {
		// A match that only became visible when a spread was expanded
		// would have been evaluated in source order in an inline map.
		if c.replayingSpreadGlobs && len(c.globRefContextStack) > 0 {
			return !n.Primary().order.before(c.globRefContextStack[0].order)
		}
		lastPrimaryRef := n.LastPrimaryRef()
		if lastPrimaryRef != nil && !lastPrimaryRef.DueToLazyGlob() {
			return true
		}
	}
	return false
}

// When importing a file, all of its board and icon links need to be extended to reflect their new path
func (c *compiler) extendLinks(m *Map, importF *Field, importDir string) {
	nodeBoardKind := NodeBoardKind(m)
	importIDA := IDA(importF)
	for _, f := range m.Fields {
		// A substitute or such
		if f.Name == nil {
			continue
		}
		if d2ast.Keyword(f.Name) == "link" {
			if nodeBoardKind != "" {
				c.errorf(f.LastRef().AST(), "a board itself cannot be linked; only objects within a board can be linked")
				continue
			}
			val := f.Primary().Value.ScalarString()

			u, err := url.Parse(htmlentity.Unescape(val))
			isRemote := err == nil && (u.Scheme != "" || strings.HasPrefix(u.Path, "/"))
			if isRemote {
				continue
			}

			link, err := d2parser.ParseKey(val)
			if err != nil {
				continue
			}
			linkIDA := link.IDA()
			if len(linkIDA) == 0 {
				continue
			}

			for _, id := range linkIDA[1:] {
				if id.ScalarString() == "_" && id.IsUnquoted() {
					if len(linkIDA) < 2 || len(importIDA) < 2 {
						break
					}
					linkIDA = append([]d2ast.String{linkIDA[0]}, linkIDA[2:]...)
					importIDA = importIDA[:len(importIDA)-2]
				} else {
					break
				}
			}

			extendedIDA := append(importIDA, linkIDA[1:]...)
			s := formatBoardPath(extendedIDA)
			f.Primary_.Value = d2ast.MakeValueBox(d2ast.FlatUnquotedString(s)).ScalarBox().Unbox()
		}
		if d2ast.Keyword(f.Name) == "icon" && f.Primary() != nil {
			val := f.Primary().Value.ScalarString()
			// It's likely a substitution
			if val == "" {
				continue
			}
			u, err := url.Parse(htmlentity.Unescape(val))
			isRemoteImg := err == nil && (u.Scheme != "" || strings.HasPrefix(u.Path, "/"))
			if isRemoteImg {
				continue
			}
			val = path.Join(importDir, val)
			f.Primary_.Value = d2ast.MakeValueBox(d2ast.FlatUnquotedString(val)).ScalarBox().Unbox()
		}
		if f.Map() != nil {
			c.extendLinks(f.Map(), importF, importDir)
		}
	}
}

func (c *compiler) compileLink(f *Field, refctx *RefContext) {
	val := refctx.Key.Value.ScalarBox().Unbox().ScalarString()
	link, err := d2parser.ParseKey(val)
	if err != nil {
		return
	}

	scopeIDA := IDA(refctx.ScopeMap)

	if len(scopeIDA) == 0 {
		return
	}

	linkIDA := link.IDA()
	if len(linkIDA) == 0 {
		return
	}

	if !linkIDA[0].IsUnquoted() {
		return
	}

	// If it doesn't start with one of these reserved words, the link is definitely not a board link.
	if !strings.EqualFold(linkIDA[0].ScalarString(), "layers") && !strings.EqualFold(linkIDA[0].ScalarString(), "scenarios") && !strings.EqualFold(linkIDA[0].ScalarString(), "steps") && linkIDA[0].ScalarString() != "_" {
		return
	}

	// Chop off the non-board portion of the scope, like if this is being defined on a nested object (e.g. `x.y.z`)
	for i := len(scopeIDA) - 1; i > 0; i-- {
		if scopeIDA[i-1].IsUnquoted() && (strings.EqualFold(scopeIDA[i-1].ScalarString(), "layers") || strings.EqualFold(scopeIDA[i-1].ScalarString(), "scenarios") || strings.EqualFold(scopeIDA[i-1].ScalarString(), "steps")) {
			scopeIDA = scopeIDA[:i+1]
			break
		}
		if scopeIDA[i-1].ScalarString() == "root" && scopeIDA[i-1].IsUnquoted() {
			scopeIDA = scopeIDA[:i]
			break
		}
	}

	// Resolve underscores
	for len(linkIDA) > 0 && linkIDA[0].ScalarString() == "_" && linkIDA[0].IsUnquoted() {
		if len(scopeIDA) < 2 {
			// Leave the underscore. It will fail in compiler as a standalone board,
			// but if imported, will get further resolved in extendLinks
			break
		}
		// pop 2 off path per one underscore
		scopeIDA = scopeIDA[:len(scopeIDA)-2]
		linkIDA = linkIDA[1:]
	}
	if len(scopeIDA) == 0 {
		scopeIDA = []d2ast.String{d2ast.FlatUnquotedString("root")}
	}

	// Create the absolute path by appending scope path with value specified
	scopeIDA = append(scopeIDA, linkIDA...)
	f.Primary_.Value = d2ast.FlatUnquotedString(formatBoardPath(scopeIDA))
}

// Format a board path like a dotted declaration so a board named WIDTH keeps
// its spelling in a link such as root.layers.WIDTH.
func formatBoardPath(path []d2ast.String) string {
	// "root" and unresolved parent references locate the board scope. Only
	// the remaining path is a declaration relative to that board's root.
	var prefix strings.Builder
	for len(path) > 1 && path[0].IsUnquoted() &&
		(path[0].ScalarString() == "_" || (prefix.Len() == 0 && path[0].ScalarString() == "root")) {
		prefix.WriteString(path[0].ScalarString())
		prefix.WriteByte('.')
		path = path[1:]
	}
	return prefix.String() + d2format.Format(&d2ast.Key{Key: d2ast.MakeKeyPathString(path)})
}

func (c *compiler) compileEdges(refctx *RefContext) {
	if c.stopped() {
		return
	}
	if refctx.Key.Key == nil {
		c._compileEdges(refctx)
		return
	}

	fa, err := refctx.ScopeMap.ensureFieldIndexed(refctx.Key.Key, refctx, true, c)
	if err != nil {
		c.err.Errors = append(c.err.Errors, err.(d2ast.Error))
		return
	}
	for _, f := range fa {
		if c.stopped() {
			return
		}
		if _, ok := f.Composite.(*Array); ok {
			c.errorf(refctx.Key.Key, "cannot index into array")
			return
		}
		if f.Map() == nil {
			c.openFieldMap(f)
			if c.stopped() {
				return
			}
		}
		refctx2 := *refctx
		refctx2.ScopeMap = f.Map()
		pushed := c.hasBoardMaps && c.pushDottedBoardGlobs(refctx.ScopeMap, enclosingBoardMap(f.Map()))
		before := f.Map().structureVersion
		c._compileEdges(&refctx2)
		if pushed {
			if before != f.Map().structureVersion && !c.applyingLazyWorklist {
				c.applyLazyGlobs(nil)
			}
			c.globContextStack = c.globContextStack[:len(c.globContextStack)-1]
		}
	}
}

func (c *compiler) _compileEdges(refctx *RefContext) {
	if c.stopped() {
		return
	}
	eida := NewEdgeIDs(refctx.Key)
	// A null selector with a property suffix clears only that edge field.
	clearing := refctx.Key.Primary.Null != nil || refctx.Key.Value.Null != nil
	deleteEdge := clearing && refctx.Key.EdgeKey == nil
	for i, eid := range eida {
		if c.stopped() {
			return
		}
		if clearing && eid.Glob && refctx.Key.EdgeKey != nil {
			// A pending spread can add matching parallel edges after this
			// selector has already processed the ones currently in scope.
			c.recordSpreadEdgeDeletion(refctx.ScopeMap, eid, refctx.Key)
		}
		if !eid.Glob && deleteEdge {
			c.recordSpreadEdgeDeletion(refctx.ScopeMap, eid, refctx.Key)
			refctx.ScopeMap.DeleteEdge(eid)
			continue
		}

		refctx = refctx.Copy()
		refctx.Edge = refctx.Key.Edges[i]

		var ea []*Edge
		selectExisting := eid.Index != nil || eid.Glob || clearing
		if selectExisting {
			ea = refctx.ScopeMap.getEdgesForCompile(eid, refctx, c)
			if len(ea) == 0 {
				if clearing && !eid.Glob {
					c.recordSpreadEdgeDeletion(refctx.ScopeMap, eid, refctx.Key)
				} else if !eid.Glob {
					c.errorf(refctx.Edge, "indexed edge does not exist")
				}
				continue
			}
			matched := ea[:0]
			for _, e := range ea {
				if c.stopped() {
					return
				}
				if refctx.Key.Value.Map != nil && refctx.Key.Value.Map.HasFilter() {
					if e.Map_ == nil {
						e.Map_ = &Map{
							parent: e,
						}
					}
					c.mapRefContextStack = append(c.mapRefContextStack, refctx)
					ok := c.ampersandFilterMap(e.Map_, refctx.Key.Value.Map, refctx.ScopeAST)
					c.mapRefContextStack = c.mapRefContextStack[:len(c.mapRefContextStack)-1]
					if c.stopped() {
						return
					}
					if !ok {
						continue
					}
				}

				if deleteEdge {
					c.recordSpreadDeletion(ParentMap(e), &spreadMember{edge: e.ID}, refctx.Key)
					ParentMap(e).DeleteEdge(e.ID)
					continue
				}

				if refctx.Key.Primary.Suspension != nil || refctx.Key.Value.Suspension != nil {
					if !c.lazyGlobBeingApplied {
						var suspensionValue bool
						if refctx.Key.Primary.Suspension != nil {
							suspensionValue = refctx.Key.Primary.Suspension.Value
						} else {
							suspensionValue = refctx.Key.Value.Suspension.Value
						}
						e.suspended = suspensionValue

						if !suspensionValue {
							// Restore both endpoints and their containing shapes.
							for _, path := range [][]d2ast.String{e.ID.SrcPath, e.ID.DstPath} {
								for f := ParentMap(e).getFieldIndexed(path...); f != nil && NodeBoardKind(f) == ""; f = ParentField(f) {
									f.suspended = false
								}
							}
						}
					}
				}

				matched = append(matched, e)
				e.appendReference(&EdgeReference{
					Context_:       refctx,
					DueToGlob_:     len(c.globRefContextStack) > 0,
					DueToLazyGlob_: c.lazyGlobBeingApplied,
				})
				refctx.ScopeMap.appendFieldReferences(0, refctx.Edge.Src, refctx, c)
				refctx.ScopeMap.appendFieldReferences(0, refctx.Edge.Dst, refctx, c)
			}
			ea = matched
		} else {
			var err error
			ea, err = refctx.ScopeMap.createEdgeForCompile(eid, refctx, c)
			if err != nil {
				c.err.Errors = append(c.err.Errors, err.(d2ast.Error))
				continue
			}
		}

		for _, e := range ea {
			if c.stopped() {
				return
			}
			if refctx.Key.EdgeKey != nil {
				if e.Map_ == nil {
					e.Map_ = &Map{
						parent: e,
					}
				}
				c.compileField(e.Map_, refctx.Key.EdgeKey, refctx)
			} else {
				if refctx.Key.Primary.Unbox() != nil && refctx.Key.Primary.Suspension == nil {
					if c.ignoreLazyGlob(e) {
						return
					}
					e.Primary_ = &Scalar{
						order:  c.assignmentOrder(),
						parent: e,
						Value:  refctx.Key.Primary.Unbox(),
					}
				}
				if refctx.Key.Value.Array != nil {
					c.errorf(refctx.Key.Value.Unbox(), "edges cannot be assigned arrays")
					continue
				} else if refctx.Key.Value.Map != nil {
					if e.Map_ == nil {
						e.Map_ = &Map{
							parent: e,
						}
					}
					c.mapRefContextStack = append(c.mapRefContextStack, refctx)
					c.compileMapWithChecks(e.Map_, refctx.Key.Value.Map, refctx.ScopeAST, !selectExisting || refctx.Key.Primary.Unbox() == nil)
					c.mapRefContextStack = c.mapRefContextStack[:len(c.mapRefContextStack)-1]
				} else if refctx.Key.Value.ScalarBox().Unbox() != nil && refctx.Key.Value.Suspension == nil {
					if c.ignoreLazyGlob(e) {
						return
					}
					e.Primary_ = &Scalar{
						order:  c.assignmentOrder(),
						parent: e,
						Value:  refctx.Key.Value.ScalarBox().Unbox(),
					}
				}
			}
		}
	}
}

func (c *compiler) compileArray(dst *Array, a *d2ast.Array, scopeAST *d2ast.Map) {
	if c.stopped() {
		return
	}
	for _, an := range a.Nodes {
		if c.stopped() {
			return
		}
		arrayNode := an.Unbox()
		if len(c.globRefContextStack) > 0 && !c.reserveGlobWork(arrayNode, 1) {
			return
		}
		var irv Value
		switch v := arrayNode.(type) {
		case *d2ast.Array:
			ira := &Array{
				parent: dst,
			}
			c.compileArray(ira, v, scopeAST)
			if c.stopped() {
				return
			}
			irv = ira
		case *d2ast.Map:
			irm := &Map{
				parent: dst,
			}
			c.compileMap(irm, v, scopeAST)
			if c.stopped() {
				return
			}
			irv = irm
		case d2ast.Scalar:
			irv = &Scalar{
				parent: dst,
				Value:  v,
			}
		case *d2ast.Import:
			n, ok := c._import(v, dst)
			if !ok {
				if c.stopped() {
					return
				}
				continue
			}
			n.(Importable).SetImportAST(v)
			switch n := n.(type) {
			case *Field:
				if v.Spread {
					a, ok := n.Composite.(*Array)
					if !ok {
						c.errorf(v, "can only spread import array into array")
						continue
					}
					if !c.reserveVariableCopy(v, a) {
						return
					}
					dst.Values = append(dst.Values, a.Values...)
					continue
				}
				if n.Composite != nil {
					irv = n.Composite
				} else {
					irv = n.Primary_
				}
			case *Map:
				if v.Spread {
					c.errorf(v, "can only spread import array into array")
					continue
				}
				irv = n
			}
		case *d2ast.Substitution:
			irv = &Scalar{
				parent: dst,
				Value: &d2ast.UnquotedString{
					Value: []d2ast.InterpolationBox{{Substitution: an.Substitution}},
				},
			}
		case *d2ast.Comment, *d2ast.BlockComment:
			continue
		}

		if c.stopped() {
			return
		}
		dst.Values = append(dst.Values, irv)
	}
}

func (m *Map) removeSuspendedFields() {
	if m == nil {
		return
	}

	for _, f := range m.Fields {
		if f.Map() != nil {
			f.Map().removeSuspendedFields()
		}
	}

	for i := len(m.Fields) - 1; i >= 0; i-- {
		if m.Fields[i].Name == nil {
			continue
		}
		if d2ast.Keyword(m.Fields[i].Name) != "" {
			continue
		}
		if m.Fields[i].suspended {
			m.DeleteField(m.Fields[i].Name.ScalarString())
		}
	}

	for _, e := range m.Edges {
		if e.Map() != nil {
			e.Map().removeSuspendedFields()
		}
	}
	for i := len(m.Edges) - 1; i >= 0; i-- {
		if m.Edges[i].suspended {
			m.DeleteEdge(m.Edges[i].ID)
		}
	}
}
