// Package layoutguard protects layout engines that cannot be interrupted once
// their native layout call begins.
package layoutguard

import (
	"context"
	"fmt"

	"github.com/d2lang/d2/d2graph"
)

const (
	// Dagre and ELK do not accept a context during their native layout calls.
	// Keep their raw inputs at or below the compiler's calibrated wildcard
	// boundary so direct graph callers cannot bypass compiler admission.
	maxLayoutObjects = 1_024
	maxLayoutEdges   = 1_024
	// Small graphs are safe to pass through without topology accounting and may
	// intentionally contain many parallel or self-referential edges.
	unconditionallyAllowedEdges = 256
	// Edge interaction work is the sum, for every edge, of the smaller incident
	// degree of its endpoints. It remains low for stars, trees, and other sparse
	// diagrams, but grows quickly for dense subgraphs and cannot be diluted by
	// adding unrelated objects.
	maxEdgeInteractionWork int64 = 8_192
)

// CheckGraph rejects malformed, oversized, or dense graphs before passing
// control to a layout engine that cannot observe context cancellation during
// its native layout call.
func CheckGraph(ctx context.Context, engine string, g *d2graph.Graph) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if g == nil {
		return fmt.Errorf("%s layout requires a graph", engine)
	}
	if len(g.Objects) > maxLayoutObjects {
		return fmt.Errorf("%s layout object count %d exceeds safe limit of %d", engine, len(g.Objects), maxLayoutObjects)
	}
	if len(g.Edges) > maxLayoutEdges {
		return fmt.Errorf("%s layout edge count %d exceeds safe limit of %d", engine, len(g.Edges), maxLayoutEdges)
	}

	incident := make(map[*d2graph.Object]int, len(g.Objects))
	for i, edge := range g.Edges {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		if edge == nil || edge.Src == nil || edge.Dst == nil {
			return fmt.Errorf("%s layout graph contains an edge without endpoints", engine)
		}
		incident[edge.Src]++
		incident[edge.Dst]++
	}
	if len(g.Edges) <= unconditionallyAllowedEdges {
		return nil
	}

	var work int64
	for i, edge := range g.Edges {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		units := incident[edge.Src]
		if incident[edge.Dst] < units {
			units = incident[edge.Dst]
		}
		if int64(units) > maxEdgeInteractionWork-work {
			return fmt.Errorf(
				"%s layout graph is too interconnected: edge interaction work exceeds safe limit of %d",
				engine,
				maxEdgeInteractionWork,
			)
		}
		work += int64(units)
	}
	return nil
}
