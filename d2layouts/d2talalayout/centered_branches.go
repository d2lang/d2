package d2talalayout

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/hierarchy"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
)

func considerCenteredBranches(ctx context.Context, graph *d2graph.Graph, input seedInput, incumbent seedResult) (seedResult, error) {
	if graph.Root.WidthAttr != nil || graph.Root.HeightAttr != nil || graph.Root.GridRows != nil || graph.Root.GridColumns != nil ||
		len(graph.Objects) > 64 || len(graph.Edges) < len(graph.Objects) || !supportsRouteRefinements(graph) {
		return incumbent, nil
	}
	for _, object := range graph.Objects {
		if object.NearKey != nil || object.Top != nil || object.Left != nil || object.GridRows != nil || object.GridColumns != nil {
			return incumbent, nil
		}
	}
	return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
		trial, err := hierarchy.CenteredBranchCandidate(ctx, incumbent.graph)
		if err != nil || trial == incumbent.graph {
			return incumbent, err
		}
		after, err := quality.Inspect(ctx, trial)
		if err != nil || after.NodeOverlaps != 0 || after.RouteObstructions != 0 || after.InteriorCrossings != 0 || after.TextOcclusions != 0 {
			return incumbent, err
		}
		before, err := quality.Inspect(ctx, incumbent.graph)
		if err != nil || after.Crossings > before.Crossings || after.RouteLength > before.RouteLength {
			return incumbent, err
		}
		candidate, err := evaluateSeedResult(ctx, input, trial)
		if err != nil {
			return incumbent, err
		}
		candidate.seed = incumbent.seed
		return candidate, nil
	})
}
