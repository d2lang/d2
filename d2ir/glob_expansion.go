package d2ir

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2ast"
)

// DefaultMaxGlobExpansion is the maximum amount of work globs may perform
// during one compilation. Explicit source fields do not consume this budget.
const DefaultMaxGlobExpansion int64 = 65_536

// maxGlobCreatedFields independently caps retained materialization. Work is
// normally the tighter bound for deep expansions, while this ceiling bounds
// broad, shallow expansions even when callers raise MaxGlobExpansion.
const maxGlobCreatedFields int64 = 10_000

type globExpansionBudget struct {
	limit         int64
	used          int64
	createdFields int64
}

type globExpansionLimitError struct {
	limit int64
}

func (e *globExpansionLimitError) Error() string {
	return fmt.Sprintf("glob expansion exceeds limit of %d work units", e.limit)
}

type globFieldLimitError struct {
	limit int64
}

func (e *globFieldLimitError) Error() string {
	return fmt.Sprintf("glob expansion exceeds limit of %d created fields", e.limit)
}

func newGlobExpansionBudget(limit int64) (*globExpansionBudget, error) {
	if limit < 0 {
		return nil, fmt.Errorf("MaxGlobExpansion must not be negative")
	}
	if limit == 0 {
		limit = DefaultMaxGlobExpansion
	}
	return &globExpansionBudget{limit: limit}, nil
}

func (b *globExpansionBudget) reserve(units int64) error {
	if units < 0 || units > b.limit-b.used {
		return &globExpansionLimitError{limit: b.limit}
	}
	b.used += units
	return nil
}

func (b *globExpansionBudget) reserveField() error {
	if b.createdFields >= maxGlobCreatedFields {
		return &globFieldLimitError{limit: maxGlobCreatedFields}
	}
	b.createdFields++
	return nil
}

func (c *compiler) globExpansionFailure(n d2ast.Node, err error) bool {
	c.globExpansionErr = err
	if n != nil {
		c.errorf(n, "%v", err)
	}
	c.halted = true
	return false
}

func (c *compiler) reserveGlobWork(n d2ast.Node, units int64) bool {
	if c.stopped() {
		return false
	}
	if err := c.globExpansion.reserve(units); err != nil {
		return c.globExpansionFailure(n, err)
	}
	return true
}

func (c *compiler) reserveGlobField(n d2ast.Node) bool {
	if c.stopped() {
		return false
	}
	if err := c.globExpansion.reserveField(); err != nil {
		return c.globExpansionFailure(n, err)
	}
	return true
}

func (c *compiler) globSource(refctx *RefContext) d2ast.Node {
	if refctx != nil && refctx.Key != nil {
		return refctx.Key
	}
	if len(c.globRefContextStack) == 0 {
		return nil
	}
	return c.globRefContextStack[len(c.globRefContextStack)-1].Key
}

// reserveGlobGeneratedFieldWork accounts for the path formatting and applied
// set lookups performed before a glob can materialize a field. Charging the
// prospective depth once per active glob context makes alternating recursive
// rules consume budget before constructing ever-deeper IR paths.
func (c *compiler) reserveGlobGeneratedFieldWork(parent *Map, n d2ast.Node) bool {
	if c.stopped() {
		return false
	}
	active := int64(len(c.globRefContextStack))
	if active == 0 {
		return true
	}

	depth := int64(1)
	for f := ParentField(parent); f != nil && !f.Root(); {
		if depth == math.MaxInt64 {
			return c.globExpansionFailure(n, &globExpansionLimitError{limit: c.globExpansion.limit})
		}
		depth++
		parent = ParentMap(f)
		if parent == nil {
			break
		}
		f = ParentField(parent)
	}
	if depth > math.MaxInt64/active {
		return c.globExpansionFailure(n, &globExpansionLimitError{limit: c.globExpansion.limit})
	}
	return c.reserveGlobWork(n, depth*active)
}
