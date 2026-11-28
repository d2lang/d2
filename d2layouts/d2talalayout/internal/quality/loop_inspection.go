package quality

import (
	"context"
	"fmt"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
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
	fixedEdgeText
)

func nodeItem(node *layoutgraph.Node) inspectionItem  { return inspectionItem{node.ID, nodeBody} }
func routeItem(edge *layoutgraph.Edge) inspectionItem { return inspectionItem{edge.ID, edgeRoute} }

// LoopInspection records multiplicities per obstacle pair. A nonrectangular
// silhouette's bounding box can veto a new conflict, but touching that box
// alone is insufficient reason to change a clean drawing.
type LoopInspection struct {
	pairs         map[[2]inspectionItem]int64
	bounds        map[inspectionItem]geo.Box
	LoopConflicts int64
	incomplete    bool
}

func InspectLoopClearance(ctx context.Context, g *layoutgraph.Graph) (*LoopInspection, error) {
	return inspectClearance(ctx, g, false)
}

// InspectRouteClearance keeps the bounds of blocked segments for a local
// bypass. Ordinary static loop evaluation only needs the collision pairs.
func InspectRouteClearance(ctx context.Context, g *layoutgraph.Graph) (*LoopInspection, error) {
	return inspectClearance(ctx, g, true)
}

func inspectClearance(ctx context.Context, g *layoutgraph.Graph, bounds bool) (*LoopInspection, error) {
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
	if bounds {
		result.bounds = make(map[inspectionItem]geo.Box)
	}
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

// Remember only boxes that a shaft actually intersects.
func (s *LoopInspection) obstructs(item inspectionItem, box geo.Box) {
	if s != nil && !s.incomplete && s.bounds != nil {
		s.bounds[item] = box
	}
}

func (s *LoopInspection) Improves(before *LoopInspection) bool {
	return s.LoopConflicts < before.LoopConflicts && s.Preserves(before)
}

// Preserves checks every contact between two distinct pieces of painted geometry.
func (s *LoopInspection) Preserves(before *LoopInspection) bool {
	if s.incomplete || before.incomplete {
		return false
	}
	for pair, count := range s.pairs {
		if count > before.pairs[pair] {
			return false
		}
	}
	return true
}

// CollectObstructedRoutes identifies the wires whose route or caption acquired
// a collision on a proposed board placement. Ordinary wire crossings are legal.
func (s *LoopInspection) CollectObstructedRoutes(before *LoopInspection, edges, labels map[layoutgraph.EntityID]bool, boxes map[layoutgraph.EntityID][]geo.Box) bool {
	if s.incomplete || before.incomplete {
		return false
	}
	for pair, count := range s.pairs {
		if count <= before.pairs[pair] || pair[0].part == pair[1].part && (pair[0].part == edgeRoute || pair[0].part == interiorRoute) {
			continue
		}
		for i, item := range pair {
			if item.part == edgeText && labels != nil {
				labels[item.id] = true
			}
			// A movable edge caption can yield to a passing wire or another
			// caption. Such contact does not obstruct the attached shaft.
			other := pair[1-i]
			if edges != nil && other.part != edgeText && (item.part == edgeRoute || item.part == fixedEdgeText || item.part == sourceText || item.part == targetText) {
				edges[item.id] = true
				if boxes != nil && item.part == edgeRoute {
					if b, ok := s.bounds[other]; ok {
						boxes[item.id] = append(boxes[item.id], b)
					}
				}
			}
		}
	}
	return true
}
