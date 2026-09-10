package quality

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// scoreAuthoredFlow keeps an explicit reading direction in the completed-layout
// objective. A perpendicular connection costs one crossing (two bends); a
// reversed connection costs two. The bounded, dimensionless term is unaffected
// by translation or uniform scale. It describes endpoint placement, leaving
// the existing bend, crossing, and label terms to assess the routed geometry.
func scoreAuthoredFlow(g *layoutgraph.Graph, guard *limits.WorkGuard) (float64, error) {
	if len(g.Directions) == 0 {
		return 0, nil
	}
	score := 0.0
	ancestors := make(map[*layoutgraph.Node]struct{})
	for _, edge := range g.Edges {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		from, to, directed := edge.DirectedEndpoints()
		if !directed || edge.IsLoop() || edge.IsInvisible || from.IsInvisible || to.IsInvisible {
			continue
		}
		// Only a shared owning scope controls a cross-container connection.
		// A direction local to one endpoint's interior must not leak outside it.
		clear(ancestors)
		for scope := from.Container; ; scope = scope.Container {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			ancestors[scope] = struct{}{}
			if scope == nil {
				break
			}
		}
		scope := to.Container
		for {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			if _, shared := ancestors[scope]; shared {
				break
			}
			scope = scope.Container
		}
		direction := geo.NONE
		for {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			direction = g.Direction(scope)
			if direction != geo.NONE || scope == nil {
				break
			}
			scope = scope.Container
		}
		first, last := from.Center(), to.Center()
		dx, dy := last.X-first.X, last.Y-first.Y
		length := math.Abs(dx) + math.Abs(dy)
		if length == 0 {
			continue
		}
		var progress float64
		switch direction {
		case geo.Right:
			progress = dx
		case geo.Left:
			progress = -dx
		case geo.Bottom:
			progress = dy
		case geo.Top:
			progress = -dy
		default:
			continue
		}
		score += 1 - progress/length
	}
	return score, nil
}
