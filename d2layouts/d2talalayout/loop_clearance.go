package d2talalayout

import (
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
)

// Preserve the completed seed for clean boards. When its loops collide, try
// spacing the occupied ranks and distributing compatible loops across corners.
func considerLoopClearance(ctx context.Context, input seedInput, incumbent seedResult) (seedResult, error) {
	for _, edge := range incumbent.graph.Edges {
		if err := ctx.Err(); err != nil {
			return incumbent, err
		}
		if edge.IsInvisible || !edge.IsLoop() {
			continue
		}
		return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
			before, err := quality.InspectLoopClearance(ctx, incumbent.graph)
			if err != nil || before.LoopConflicts == 0 {
				return incumbent, err
			}
			options := engine.LayoutOptions{
				Seed: incumbent.seed, CompactHierarchySpacing: incumbent.compactHierarchySpacing,
				LoopClearance: true,
			}
			graph, err := engine.Layout(ctx, input.graph, options)
			if err != nil {
				return incumbent, err
			}
			after, err := quality.InspectLoopClearance(ctx, graph)
			if err != nil || !after.Improves(before) {
				return incumbent, err
			}
			candidate, err := evaluateSeedResult(ctx, input, graph)
			candidate.seed = incumbent.seed
			candidate.compactHierarchySpacing = incumbent.compactHierarchySpacing
			return candidate, err
		})
	}
	return incumbent, ctx.Err()
}
