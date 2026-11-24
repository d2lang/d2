package d2talalayout

import (
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
)

// Allow a single extra placement and routing trial on hierarchies with
// room to tighten. The incumbent's seed reproduces the order of real and dummy
// nodes, including dummy chains that do not survive in the completed graph.
func considerCompactHierarchy(ctx context.Context, input seedInput, incumbent seedResult) (seedResult, error) {
	if !hasCompactableRanks(ctx, incumbent.graph) {
		return incumbent, ctx.Err()
	}
	return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
		graph, err := engine.Layout(ctx, input.graph, engine.LayoutOptions{Seed: incumbent.seed, CompactHierarchySpacing: true})
		if err != nil {
			return seedResult{}, err
		}
		candidate, err := evaluateSeedResult(ctx, input, graph)
		if err != nil {
			return seedResult{}, err
		}
		if candidate.score.compare(incumbent.score) >= 0 || candidate.score.area >= incumbent.score.area {
			return incumbent, nil
		}
		before, err := quality.Inspect(ctx, incumbent.graph)
		if err != nil {
			return seedResult{}, err
		}
		after, err := quality.Inspect(ctx, graph)
		if err != nil {
			return seedResult{}, err
		}
		// Fewer bends can improve the aggregate score even when a new crossing or
		// obstruction appears. A spacing refinement must preserve these measures.
		if after.Crossings > before.Crossings || after.InteriorCrossings > before.InteriorCrossings ||
			after.RouteObstructions > before.RouteObstructions || after.NodeOverlaps > before.NodeOverlaps || after.TextOcclusions > before.TextOcclusions {
			return incumbent, nil
		}
		return candidate, nil
	})
}

func hasCompactableRanks(ctx context.Context, g *layoutgraph.Graph) bool {
	type rank struct {
		h     *layoutgraph.Hierarchy
		level int
	}
	// Once a second member is found on a level, its outer spacing can matter.
	// For singleton rows, reuse the level map to count boundary edges below.
	type boundary struct {
		edges   int
		labeled bool
	}
	ranks := make(map[rank]boundary)
	for _, node := range g.Nodes {
		if ctx.Err() != nil {
			return false
		}
		if node.Hierarchy == nil || node.IsInvisible {
			continue
		}
		key := rank{node.Hierarchy, node.HierarchyLevel()}
		if _, ok := ranks[key]; ok {
			return true
		}
		ranks[key] = boundary{}
	}
	if len(ranks) == 0 {
		return false
	}
	for _, e := range g.Edges {
		if ctx.Err() != nil {
			return false
		}
		h := e.From.Hierarchy
		if h == nil || h != e.To.Hierarchy {
			continue
		}
		a, b := e.From.HierarchyLevel(), e.To.HierarchyLevel()
		if a == b {
			continue
		}
		// A long edge adds another lane on an interior rank, even when
		// the completed graph has only one real node per level.
		if max(a, b)-min(a, b) > 1 {
			return true
		}
		key := rank{h, min(a, b)}
		gap := ranks[key]
		gap.edges++
		gap.labeled = gap.labeled || e.Label != nil
		ranks[key] = gap
	}
	for _, gap := range ranks {
		if gap.edges == 1 && !gap.labeled {
			return true
		}
	}
	return false
}
