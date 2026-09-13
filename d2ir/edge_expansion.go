package d2ir

import (
	"fmt"

	"github.com/d2lang/d2/d2ast"
)

// DefaultMaxEdgeExpansion is the maximum number of distinct edge-segment and
// endpoint combinations that edge globs may consider during one compilation.
// Explicit edges do not consume this budget because their work is proportional
// to the input size. The limit is calibrated so a sparse star at the boundary
// remains bounded in the built-in Dagre and ELK layouts, while larger wildcard
// fanout stops before layout.
const DefaultMaxEdgeExpansion int64 = 1_024

// DefaultMaxEdgeExpansionWork is the maximum number of endpoint-pair
// examinations edge globs may perform, including repeated lazy replays.
const DefaultMaxEdgeExpansionWork int64 = 65_536

type edgeExpansionPair struct {
	glob     *globContext
	segment  *d2ast.Edge
	src      *Field
	dst      *Field
	selector bool
}

type edgeExpansionBudget struct {
	limit int64
	used  int64
}

type edgeExpansionLimitError struct {
	limit int64
}

type edgeExpansionWorkBudget struct {
	limit int64
	used  int64
}

type edgeExpansionWorkLimitError struct {
	limit int64
}

func (e *edgeExpansionWorkLimitError) Error() string {
	return fmt.Sprintf("edge glob expansion exceeds work limit of %d endpoint-pair examinations", e.limit)
}

func (e *edgeExpansionLimitError) Error() string {
	return fmt.Sprintf("edge glob expansion exceeds limit of %d endpoint pairs", e.limit)
}

func newEdgeExpansionBudget(limit int64) (*edgeExpansionBudget, error) {
	if limit < 0 {
		return nil, fmt.Errorf("MaxEdgeExpansion must not be negative")
	}
	if limit == 0 {
		limit = DefaultMaxEdgeExpansion
	}
	return &edgeExpansionBudget{limit: limit}, nil
}

func newEdgeExpansionWorkBudget(limit int64) (*edgeExpansionWorkBudget, error) {
	if limit < 0 {
		return nil, fmt.Errorf("MaxEdgeExpansionWork must not be negative")
	}
	if limit == 0 {
		limit = DefaultMaxEdgeExpansionWork
	}
	return &edgeExpansionWorkBudget{limit: limit}, nil
}

func (b *edgeExpansionBudget) reserve() error {
	if b == nil || b.used >= b.limit {
		limit := int64(0)
		if b != nil {
			limit = b.limit
		}
		return &edgeExpansionLimitError{limit: limit}
	}
	b.used++
	return nil
}

func (b *edgeExpansionWorkBudget) reserve() error {
	if b == nil || b.used >= b.limit {
		limit := int64(0)
		if b != nil {
			limit = b.limit
		}
		return &edgeExpansionWorkLimitError{limit: limit}
	}
	b.used++
	return nil
}

func (c *compiler) reserveEdgeExpansion(segment *d2ast.Edge, glob *globContext, src, dst *Field, selector bool) bool {
	if c.stopped() {
		return false
	}
	if err := c.edgeExpansionWork.reserve(); err != nil {
		return c.rejectEdgeExpansion(segment, err)
	}
	pair := edgeExpansionPair{glob: glob, segment: segment, src: src, dst: dst, selector: selector}
	if _, ok := c.edgeExpansionPairs[pair]; ok {
		return true
	}
	if err := c.edgeExpansion.reserve(); err != nil {
		return c.rejectEdgeExpansion(segment, err)
	}
	if c.edgeExpansionPairs == nil {
		c.edgeExpansionPairs = make(map[edgeExpansionPair]struct{})
	}
	c.edgeExpansionPairs[pair] = struct{}{}
	return true
}

func (c *compiler) rejectEdgeExpansion(n d2ast.Node, err error) bool {
	c.expansionErr = err
	if n != nil {
		c.errorf(n, "%v", err)
	}
	c.halted = true
	return false
}
