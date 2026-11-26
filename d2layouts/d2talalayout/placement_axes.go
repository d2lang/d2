package d2talalayout

import (
	"cmp"
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placement"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// At most one trial of the selected seed, regardless of the number of ports
// and containers. Its independent workspace leaves the winner intact.
func considerPlacementAxes(ctx context.Context, input seedInput, incumbent seedResult) (seedResult, error) {
	var lengths, gaps map[layoutgraph.EntityID]float64
	var affected map[layoutgraph.EntityID]*layoutgraph.Node
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
			affected = make(map[layoutgraph.EntityID]*layoutgraph.Node)
		}
		lengths[n.ID] = length
		gaps[n.ID] = gap
		peer := n.Adjacent(n.Edges[0])
		affected[n.ID], affected[peer.ID] = n, peer
	}
	var beforeFlow directionAgreement
	distinct := incumbent.directionEligible && !incumbent.horizontalSource && len(lengths) == 0
	if !incumbent.horizontalSource && len(lengths) == 0 && !distinct {
		return incumbent, nil
	}
	if distinct {
		var err error
		beforeFlow, err = measureDirectionAgreement(ctx, incumbent.graph)
		if err != nil {
			return incumbent, err
		}
	}
	return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
		trial := placement.OrientationTrial{TurnSources: incumbent.horizontalSource, DistinctDirections: distinct}
		graph, err := engine.Layout(placement.WithOrientationTrial(ctx, &trial), input.graph, engine.LayoutOptions{Seed: incumbent.seed, PortLengths: lengths})
		if err != nil {
			return incumbent, err
		}
		candidate, err := evaluateSeedResult(ctx, input, graph)
		if err != nil {
			return incumbent, err
		}
		// Turning a source is optional. Preserve the entire drawing's area, even
		// when it contains several individually compact source containers.
		if incumbent.horizontalSource && (!trial.SourceEligible || candidate.score.area > incumbent.score.area) {
			return incumbent, nil
		}
		if distinct {
			afterFlow, err := measureDirectionAgreement(ctx, graph)
			if err != nil {
				return incumbent, err
			}
			if afterFlow.distinct <= beforeFlow.distinct || afterFlow.all < beforeFlow.all || candidate.score.area > incumbent.score.area {
				return incumbent, nil
			}
		}
		if len(affected) > 0 {
			guard, err := limits.NewWorkGuard(ctx, "PortClearance", limits.MaxEngineEdges)
			if err != nil {
				return incumbent, err
			}
			for _, n := range graph.Nodes {
				if err := ctx.Err(); err != nil {
					return incumbent, err
				}
				prior := affected[n.ID]
				if prior == nil {
					continue
				}
				preserved, err := preservesPortClearance(guard, prior, n)
				if err != nil || !preserved {
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
		xsort.Ordered(coords)
		for i := 1; i < len(coords); i++ {
			gap = math.Min(gap, coords[i]-coords[i-1])
		}
	}
	return gap, true
}

// Check each pair against its own previous distance. A preexisting overlap
// elsewhere on this node cannot hide a newly coincident pair. Euclidean
// distance includes neighboring faces, curved shapes and both ends of loops.
func preservesPortClearance(guard *limits.WorkGuard, before, after *layoutgraph.Node) (bool, error) {
	if err := guard.Add(int64(len(before.Edges) + len(after.Edges))); err != nil {
		return false, err
	}
	prior := make(map[layoutgraph.EntityID]*layoutgraph.Edge, len(before.Edges))
	for _, e := range before.Edges {
		prior[e.ID] = e
	}
	type endpoint struct{ old, current *geo.Point }
	ends := make([]endpoint, 0, len(after.Edges))
	for _, e := range after.Edges {
		if e.IsInvisible {
			continue
		}
		old := prior[e.ID]
		if old == nil || len(old.Points) < 2 || len(e.Points) < 2 {
			return false, nil
		}
		if e.From == after {
			ends = append(ends, endpoint{old.SourcePort(), e.SourcePort()})
		}
		if e.To == after {
			ends = append(ends, endpoint{old.TargetPort(), e.TargetPort()})
		}
	}
	// Pairs outside the desired spacing already have sufficient room. Sort by
	// the new x coordinate so that only a narrow strip needs comparisons.
	const spacing = placementcost.SideEdgeSpacing
	xsort.Func(ends, func(a, b endpoint) int { return cmp.Compare(a.current.X, b.current.X) })
	for i, a := range ends {
		for _, b := range ends[i+1:] {
			dx := b.current.X - a.current.X
			if dx >= spacing {
				break
			}
			if err := guard.Step(); err != nil {
				return false, err
			}
			dy := b.current.Y - a.current.Y
			next := dx*dx + dy*dy
			if next >= spacing*spacing {
				continue
			}
			dx, dy = b.old.X-a.old.X, b.old.Y-a.old.Y
			if next+1e-8 < dx*dx+dy*dy {
				return false, nil
			}
		}
	}
	return true, guard.Finish()
}
