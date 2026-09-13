package d2ir

import (
	"context"
	"errors"
	"fmt"

	"github.com/d2lang/d2/d2ast"
)

// DefaultMaxVariableExpansion is the maximum amount of work that variable
// substitutions and the copies they induce may add to one compilation. One
// work unit is one copied IR node or copied slice element, one inserted array
// member, one produced string byte, one Markdown match candidate, or one byte
// prepared for a Markdown variable lookup pattern.
const DefaultMaxVariableExpansion int64 = 65_536

type variableExpansionBudget struct {
	limit int64
	used  int64
}

type variableExpansionLimitError struct {
	limit int64
}

func (e *variableExpansionLimitError) Error() string {
	return fmt.Sprintf("variable substitution expansion exceeds limit of %d work units", e.limit)
}

func newVariableExpansionBudget(limit int64) (*variableExpansionBudget, error) {
	if limit < 0 {
		return nil, fmt.Errorf("MaxVariableExpansion must not be negative")
	}
	if limit == 0 {
		limit = DefaultMaxVariableExpansion
	}
	return &variableExpansionBudget{limit: limit}, nil
}

func (b *variableExpansionBudget) remaining() int64 {
	if b == nil || b.used >= b.limit {
		return 0
	}
	return b.limit - b.used
}

func (b *variableExpansionBudget) reserve(units int64) error {
	if units < 0 || units > b.limit-b.used {
		return &variableExpansionLimitError{limit: b.limit}
	}
	b.used += units
	return nil
}

func (c *compiler) stopped() bool {
	if c.halted {
		return true
	}
	if c.ctx == nil {
		c.ctx = context.Background()
	}
	if c.variableExpansion == nil {
		c.variableExpansion = &variableExpansionBudget{limit: DefaultMaxVariableExpansion}
	}
	if c.globExpansion == nil {
		c.globExpansion = &globExpansionBudget{limit: DefaultMaxGlobExpansion}
	}
	if c.edgeExpansion == nil {
		c.edgeExpansion = &edgeExpansionBudget{limit: DefaultMaxEdgeExpansion}
	}
	if c.edgeExpansionWork == nil {
		c.edgeExpansionWork = &edgeExpansionWorkBudget{limit: DefaultMaxEdgeExpansionWork}
	}
	if err := c.ctx.Err(); err != nil {
		c.contextErr = err
		c.halted = true
		return true
	}
	return false
}

func (c *compiler) reserveVariableExpansion(n d2ast.Node, units int64) bool {
	if c.stopped() {
		return false
	}
	if err := c.variableExpansion.reserve(units); err != nil {
		c.expansionErr = err
		if n != nil {
			c.errorf(n, "%v", err)
		}
		c.halted = true
		return false
	}
	return true
}

func (c *compiler) reserveVariableCopy(n d2ast.Node, root Node) bool {
	err := walkVariableExpansion(c.ctx, root, nil, func(units int64) error {
		if !c.reserveVariableExpansion(n, units) {
			if c.contextErr != nil {
				return c.contextErr
			}
			return &variableExpansionLimitError{limit: c.variableExpansion.limit}
		}
		return nil
	})
	return c.handleVariableExpansionWalkError(err)
}

func (c *compiler) reserveVariableASTCopy(source d2ast.Node, root *d2ast.Map) bool {
	ok := true
	d2ast.Walk(root, func(node d2ast.Node) bool {
		if !ok || c.stopped() {
			ok = false
			return false
		}
		ok = c.reserveVariableExpansion(source, variableExpansionASTNodeUnits(node))
		return ok
	})
	return ok
}

func (c *compiler) handleVariableExpansionWalkError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		c.contextErr = err
		c.halted = true
	}
	return false
}

func variableExpansionBudgetFor(m *Map) *variableExpansionBudget {
	if m == nil {
		return nil
	}
	root := variableExpansionRoot(m)
	return root.variableExpansion
}

func ensureVariableExpansionBudget(m *Map) *variableExpansionBudget {
	root := variableExpansionRoot(m)
	if root.variableExpansion == nil {
		root.variableExpansion = &variableExpansionBudget{limit: DefaultMaxVariableExpansion}
	}
	return root.variableExpansion
}

func variableExpansionRoot(m *Map) *Map {
	for m != nil {
		parent := ParentMap(m)
		if parent == nil {
			return m
		}
		m = parent
	}
	return nil
}

// ReserveVariableExpansionAliases accounts for logical IR nodes reached more
// than once through composite aliases. It must run before graph materialization,
// which turns those shared references into distinct graph objects.
func ReserveVariableExpansionAliases(ctx context.Context, m *Map) error {
	if m == nil {
		return fmt.Errorf("cannot reserve variable expansion for a nil IR map")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	budget := ensureVariableExpansionBudget(m)
	seen := make(map[Node]struct{})
	return walkVariableExpansion(ctx, m, seen, budget.reserve)
}

// ReserveVariableExpansionCopy accounts for the logical IR nodes an automatic
// copy will materialize. It shares the same compilation-wide budget used by
// substitutions and alias materialization.
func ReserveVariableExpansionCopy(ctx context.Context, m *Map) error {
	if m == nil {
		return fmt.Errorf("cannot reserve variable expansion copy for a nil IR map")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	budget := ensureVariableExpansionBudget(m)
	return walkVariableExpansion(ctx, m, nil, budget.reserve)
}

// walkVariableExpansion walks logical occurrences rather than unique pointers.
// When seen is non-nil, only repeated occurrences consume budget.
func walkVariableExpansion(ctx context.Context, root Node, seen map[Node]struct{}, reserve func(int64) error) error {
	stack := []Node{root}
	var seenScalarValues map[d2ast.Scalar]struct{}
	if seen != nil {
		seenScalarValues = make(map[d2ast.Scalar]struct{})
	}
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == nil {
			continue
		}

		charge := seen == nil
		if seen != nil {
			if _, ok := seen[n]; ok {
				charge = true
			} else {
				seen[n] = struct{}{}
			}
		}
		if charge {
			if err := reserve(variableExpansionNodeUnits(n)); err != nil {
				return err
			}
		}
		if scalar, ok := n.(*Scalar); ok && scalar.Value != nil && seenScalarValues != nil {
			if _, valueSeen := seenScalarValues[scalar.Value]; valueSeen {
				// Whole-scalar substitutions can create distinct IR Scalar nodes
				// that share one mutable AST value. If it grows later, pointer-only
				// Node accounting misses the duplicated logical value.
				if !charge {
					if err := reserve(variableExpansionScalarValueUnits(scalar.Value)); err != nil {
						return err
					}
				}
			} else {
				seenScalarValues[scalar.Value] = struct{}{}
			}
		}

		switch n := n.(type) {
		case *Map:
			for i := len(n.Edges) - 1; i >= 0; i-- {
				stack = append(stack, n.Edges[i])
			}
			for i := len(n.Fields) - 1; i >= 0; i-- {
				stack = append(stack, n.Fields[i])
			}
		case *Field:
			if n.Composite != nil {
				stack = append(stack, n.Composite)
			}
			if n.Primary_ != nil {
				stack = append(stack, n.Primary_)
			}
		case *Edge:
			if n.Map_ != nil {
				stack = append(stack, n.Map_)
			}
			if n.Primary_ != nil {
				stack = append(stack, n.Primary_)
			}
		case *Array:
			for i := len(n.Values) - 1; i >= 0; i-- {
				stack = append(stack, n.Values[i])
			}
		}
	}
	return nil
}

func variableExpansionNodeUnits(n Node) int64 {
	units := int64(1)

	switch n := n.(type) {
	case *Map:
		addVariableExpansionUnits(&units, len(n.Fields))
		addVariableExpansionUnits(&units, len(n.Edges))
	case *Field:
		addVariableExpansionUnits(&units, len(n.References))
		if n.Name != nil {
			addVariableExpansionUnits64(&units, variableExpansionScalarValueUnits(n.Name))
		}
	case *Edge:
		addVariableExpansionUnits(&units, len(n.References))
		if n.ID != nil {
			addVariableExpansionUnits(&units, len(n.ID.SrcPath))
			addVariableExpansionUnits(&units, len(n.ID.DstPath))
			for _, part := range n.ID.SrcPath {
				addVariableExpansionUnits64(&units, variableExpansionScalarValueUnits(part))
			}
			for _, part := range n.ID.DstPath {
				addVariableExpansionUnits64(&units, variableExpansionScalarValueUnits(part))
			}
		}
	case *Scalar:
		addVariableExpansionUnits64(&units, variableExpansionScalarValueUnits(n.Value))
	case *Array:
		addVariableExpansionUnits(&units, len(n.Values))
	}
	return units
}

func variableExpansionScalarValueUnits(scalar d2ast.Scalar) int64 {
	units := int64(0)
	switch scalar := scalar.(type) {
	case *d2ast.UnquotedString:
		addVariableExpansionUnits(&units, len(scalar.Pattern))
		addVariableExpansionUnits64(&units, interpolationCopyUnits(scalar.Value))
	case *d2ast.DoubleQuotedString:
		addVariableExpansionUnits64(&units, interpolationCopyUnits(scalar.Value))
	case *d2ast.SingleQuotedString:
		addVariableExpansionUnits(&units, len(scalar.Value))
	case *d2ast.BlockString:
		addVariableExpansionUnits(&units, len(scalar.Value))
	case *d2ast.Number:
		addVariableExpansionNumberUnits(&units, scalar)
	}
	return units
}

func variableExpansionASTNodeUnits(n d2ast.Node) int64 {
	units := int64(1)
	switch n := n.(type) {
	case *d2ast.Map:
		addVariableExpansionUnits(&units, len(n.Nodes))
	case *d2ast.Array:
		addVariableExpansionUnits(&units, len(n.Nodes))
	case *d2ast.Key:
		addVariableExpansionUnits(&units, len(n.Edges))
	case *d2ast.KeyPath:
		addVariableExpansionUnits(&units, len(n.Path))
	case *d2ast.Substitution:
		addVariableExpansionUnits(&units, len(n.Path))
	case *d2ast.Import:
		addVariableExpansionUnits(&units, len(n.Path))
	case *d2ast.UnquotedString:
		addVariableExpansionUnits(&units, len(n.Pattern))
		addVariableExpansionUnits64(&units, interpolationCopyUnits(n.Value))
	case *d2ast.DoubleQuotedString:
		addVariableExpansionUnits64(&units, interpolationCopyUnits(n.Value))
	case *d2ast.SingleQuotedString:
		addVariableExpansionUnits(&units, len(n.Value))
	case *d2ast.BlockString:
		addVariableExpansionUnits(&units, len(n.Value))
	case *d2ast.Number:
		addVariableExpansionNumberUnits(&units, n)
	}
	return units
}

func addVariableExpansionNumberUnits(total *int64, number *d2ast.Number) {
	addVariableExpansionUnits(total, len(number.Raw))
	if number.Value == nil {
		return
	}
	for _, bits := range []int{number.Value.Num().BitLen(), number.Value.Denom().BitLen()} {
		bytes := int64(bits / 8)
		if bits%8 != 0 {
			bytes++
		}
		addVariableExpansionUnits64(total, bytes)
	}
}

func interpolationCopyUnits(values []d2ast.InterpolationBox) int64 {
	units := int64(0)
	addVariableExpansionUnits(&units, len(values))
	for _, value := range values {
		if value.String != nil {
			addVariableExpansionUnits(&units, len(*value.String))
		}
		if value.StringRaw != nil {
			addVariableExpansionUnits(&units, len(*value.StringRaw))
		}
		if value.Substitution != nil {
			addVariableExpansionUnits(&units, len(value.Substitution.Path))
		}
	}
	return units
}

func addVariableExpansionUnits(total *int64, count int) {
	if count <= 0 {
		return
	}
	addVariableExpansionUnits64(total, int64(count))
}

func addVariableExpansionUnits64(total *int64, count int64) {
	if count <= 0 {
		return
	}
	const maxInt64 = int64(^uint64(0) >> 1)
	if count > maxInt64-*total {
		*total = maxInt64
		return
	}
	*total += count
}
