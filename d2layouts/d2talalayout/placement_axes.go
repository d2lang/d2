package d2talalayout

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placement"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
)

// At most one trial of the selected seed, regardless of the number of ports
// and containers. Its independent workspace leaves the winner intact.
func considerPlacementAxes(ctx context.Context, input seedInput, incumbent seedResult) (seedResult, error) {
	var lengths, gaps map[layoutgraph.EntityID]float64
	for _, n := range incumbent.graph.Nodes {
		if err := ctx.Err(); err != nil {
			return incumbent, err
		}
		if (n.DesiredHeight == nil) == (n.DesiredWidth == nil) || len(n.Edges) < 2 ||
			n.FixedTopLeft != nil || !n.IsRectangular() || n.AspectRatio1() || n.IsContainer() ||
			n.IsTable() || n.IsClass() || n.IsImage() || n.IsInvisible {
			continue
		}
		length := float64(len(n.Edges)+1) * placementcost.SideEdgeSpacing
		free := n.Height
		if n.DesiredHeight != nil {
			free = n.Width
		}
		if length <= free || length > limits.MaxGraphSize {
			continue
		}
		gap, ok := parallelPortGap(n, true)
		if !ok || gap >= placementcost.SideEdgeSpacing {
			continue
		}
		if lengths == nil {
			lengths = make(map[layoutgraph.EntityID]float64)
			gaps = make(map[layoutgraph.EntityID]float64)
		}
		lengths[n.ID] = length
		gaps[n.ID] = gap
	}
	if !incumbent.horizontalSource && len(lengths) == 0 {
		return incumbent, nil
	}
	return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
		trial := placement.SourceOrientationTrial{Apply: incumbent.horizontalSource}
		graph, err := engine.Layout(placement.WithSourceOrientationTrial(ctx, &trial), input.graph, engine.LayoutOptions{Seed: incumbent.seed, PortLengths: lengths})
		if err != nil {
			return incumbent, err
		}
		candidate, err := evaluateSeedResult(ctx, input, graph)
		if err != nil {
			return incumbent, err
		}
		// Turning a source is optional. Preserve the entire drawing's area, even
		// when it contains several individually compact source containers.
		if incumbent.horizontalSource && (!trial.Eligible || candidate.score.area > incumbent.score.area) {
			return incumbent, nil
		}
		if len(gaps) > 0 {
			for _, n := range graph.Nodes {
				if err := ctx.Err(); err != nil {
					return incumbent, err
				}
				if gap, ok := gaps[n.ID]; ok {
					after, supported := parallelPortGap(n, false)
					if !supported || after <= gap {
						return incumbent, nil
					}
				}
			}
		}
		before, err := quality.Inspect(ctx, incumbent.graph)
		if err != nil {
			return incumbent, err
		}
		after, err := quality.Inspect(ctx, graph)
		if err != nil {
			return incumbent, err
		}
		if after.NodeOverlaps > before.NodeOverlaps || after.TextOcclusions > before.TextOcclusions ||
			after.RouteObstructions > before.RouteObstructions || after.InteriorCrossings > before.InteriorCrossings || after.Crossings > before.Crossings {
			return incumbent, nil
		}
		candidate.seed = incumbent.seed
		return candidate, nil
	})
}

// A free dimension can spread ports only on its two corresponding faces.
// For a parallel bundle to one peer, a crowded face can also receive ports
// that routing had to send around a corner.
func parallelPortGap(n *layoutgraph.Node, freeFacesOnly bool) (float64, bool) {
	if len(n.Edges) < 2 {
		return 0, false
	}
	var faces [4][]float64
	peer := n.Adjacent(n.Edges[0])
	for _, e := range n.Edges {
		if e.IsInvisible || e.IsLoop() || n.Adjacent(e) != peer || len(e.Points) < 2 {
			return 0, false
		}
		p := e.SourcePort()
		if e.To == n {
			p = e.TargetPort()
		}
		if !freeFacesOnly || n.DesiredWidth != nil {
			if math.Abs(p.X-n.TopLeft.X) < 1e-6 {
				faces[0] = append(faces[0], p.Y)
			} else if math.Abs(p.X-(n.TopLeft.X+n.Width)) < 1e-6 {
				faces[1] = append(faces[1], p.Y)
			}
		}
		if !freeFacesOnly || n.DesiredHeight != nil {
			if math.Abs(p.Y-n.TopLeft.Y) < 1e-6 {
				faces[2] = append(faces[2], p.X)
			} else if math.Abs(p.Y-(n.TopLeft.Y+n.Height)) < 1e-6 {
				faces[3] = append(faces[3], p.X)
			}
		}
	}
	gap := placementcost.SideEdgeSpacing
	for _, coords := range faces {
		slices.Sort(coords)
		for i := 1; i < len(coords); i++ {
			gap = math.Min(gap, coords[i]-coords[i-1])
		}
	}
	return gap, true
}
