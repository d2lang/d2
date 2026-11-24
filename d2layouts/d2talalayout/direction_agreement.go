package d2talalayout

import (
	"cmp"
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

type directionAgreement struct {
	distinct, all int
}

// Count forward minus backward relationships using the centers that appear in
// the finished drawing. Each explicitly directed container has its own axis.
func measureDirectionAgreement(ctx context.Context, graph *layoutgraph.Graph) (directionAgreement, error) {
	var score directionAgreement
	if len(graph.Directions) == 0 || len(graph.Edges) < 3 {
		return score, nil
	}
	guard, err := limits.NewWorkGuard(ctx, "DirectionAgreement", limits.MaxEngineEdges)
	if err != nil {
		return score, err
	}
	seen := make(map[[2]*layoutgraph.Node]struct{})
	for _, edge := range graph.Edges {
		if err := guard.Step(); err != nil {
			return score, err
		}
		from, to, directed := edge.DirectedEndpoints()
		if !directed || edge.IsInvisible || from == to || from.Container != to.Container {
			continue
		}
		direction := graph.Direction(from.Container)
		if direction == geo.NONE {
			continue
		}
		a, b := from.Center(), to.Center()
		vote := cmp.Compare(b.X, a.X)
		if direction.IsVertical() {
			vote = cmp.Compare(b.Y, a.Y)
		}
		if direction == geo.Left || direction == geo.Top {
			vote = -vote
		}
		score.all += vote
		pair := [2]*layoutgraph.Node{from, to}
		if _, exists := seen[pair]; exists {
			continue
		}
		seen[pair] = struct{}{}
		score.distinct += vote
	}
	return score, guard.Finish()
}
