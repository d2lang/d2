package quality

import (
	"context"
	"fmt"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// Keep the identities of colliding shapes, routes, labels and icons only during
// a loop trial. Trading a collision on one branch for one on another branch
// must not pass just because the aggregate counts stayed the same.
type inspectionItem struct {
	id   layoutgraph.EntityID
	part uint8
}

const (
	nodeBody uint8 = iota
	nodeText
	nodeIcon
	edgeRoute
	edgeText
	sourceText
	targetText
	interiorRoute
)

func nodeItem(node *layoutgraph.Node) inspectionItem  { return inspectionItem{node.ID, nodeBody} }
func routeItem(edge *layoutgraph.Edge) inspectionItem { return inspectionItem{edge.ID, edgeRoute} }

// LoopInspection records multiplicities per obstacle pair. A nonrectangular
// silhouette's bounding box can veto a new conflict, but touching that box
// alone is insufficient reason to change a clean drawing.
type LoopInspection struct {
	pairs         map[[2]inspectionItem]int64
	LoopConflicts int64
	incomplete    bool
}

func InspectLoopClearance(ctx context.Context, g *layoutgraph.Graph) (*LoopInspection, error) {
	if g == nil {
		return nil, fmt.Errorf("cannot inspect loop clearance on a nil graph")
	}
	for _, edge := range g.Edges {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// The route-point model does not certify the interior of a Bezier curve.
		if !edge.IsInvisible && edge.IsCurve {
			return &LoopInspection{incomplete: true}, nil
		}
	}
	result := &LoopInspection{pairs: make(map[[2]inspectionItem]int64)}
	_, _, err := inspectWithIssues(ctx, g, maxEvaluationWorkUnits, result)
	return result, err
}

func (s *LoopInspection) add(a, b inspectionItem, count int64, concernsLoop bool) {
	if s == nil || count == 0 || s.incomplete {
		return
	}
	if b.id < a.id || b.id == a.id && b.part < a.part {
		a, b = b, a
	}
	key := [2]inspectionItem{a, b}
	// An optional trial need not retain an unbounded collision matrix for an
	// already severely occluded board. An incomplete comparison never wins.
	if s.pairs[key] == 0 && len(s.pairs) == 4096 {
		s.incomplete = true
		s.LoopConflicts = 0
		return
	}
	s.pairs[key] += count
	if concernsLoop {
		s.LoopConflicts += count
	}
}

func (s *LoopInspection) Improves(before *LoopInspection) bool {
	if s.incomplete || before.incomplete || s.LoopConflicts >= before.LoopConflicts {
		return false
	}
	for pair, count := range s.pairs {
		if count > before.pairs[pair] {
			return false
		}
	}
	return true
}
