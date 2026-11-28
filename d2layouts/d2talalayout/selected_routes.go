package d2talalayout

import (
	"context"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/lib/geo"
)

// These optional moves check layout-time geometry. D2-only decorations such as
// badge boxes and rounded corners can occupy space absent from that model.
func supportsRouteRefinements(g *d2graph.Graph) bool {
	if len(g.Edges) == 0 || len(g.Edges) > 256 || len(g.Objects) > 256 {
		return false
	}
	return supportsRefinementStyles(g)
}

func supportsRefinementStyles(g *d2graph.Graph) bool {
	for _, n := range g.Objects {
		if n.Icon != nil || n.Link != nil || n.Tooltip != nil || n.Style.BorderRadius != nil ||
			n.Style.DoubleBorder != nil || n.Style.StrokeWidth != nil || n.Style.Shadow != nil ||
			n.Style.ThreeDee != nil || n.Style.Multiple != nil {
			return false
		}
	}
	for _, e := range g.Edges {
		if e.HasIcon() || e.Tooltip != nil || e.Link != nil || e.Style.Shadow != nil || e.Style.BorderRadius != nil || e.ZIndex != 0 ||
			(e.Style.StrokeWidth != nil && e.Style.StrokeWidth.Value != "2") {
			return false
		}
	}
	return true
}

func refineSelectedRoutes(ctx context.Context, g *layoutgraph.Graph) (err error) {
	hierarchy := slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool { return n.Hierarchy != nil })
	pair := routing.CanRefineLabeledLanes(g)
	// Each stage installs a new slice when changing a route. Keep the old
	// headers so even a failed or interrupted final inspection is reversible.
	original := make([][]*geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		original[i] = e.Points
	}
	restore := func() {
		for i, e := range g.Edges {
			e.Points = original[i]
		}
	}
	accepted := false
	defer func() {
		// An optional stage or inspection may exhaust its own budget. Only
		// caller cancellation invalidates the complete selected layout.
		err = ctx.Err()
		if recover() != nil || !accepted || err != nil {
			restore()
		}
	}()
	if hierarchy {
		if err := routing.RefineHierarchyCorridors(ctx, g); err != nil {
			return err
		}
		if err := routing.RefineHierarchyFanPorts(ctx, g); err != nil {
			return err
		}
	} else if !g.IsRootHierarchy && !slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool { return n.ForceHierarchy }) {
		if err := routing.RefineOrdinaryLabelRoutes(ctx, g); err != nil {
			return err
		}
		if err := routing.RefineOrdinaryDetours(ctx, g); err != nil {
			return err
		}
		if err := routing.RefineOrdinaryFanLanes(ctx, g, g.Edges); err != nil {
			return err
		}
	}
	if pair {
		routing.RefineLabeledLanes(g)
	}
	changed := false
	for i, e := range g.Edges {
		if !slices.Equal(original[i], e.Points) {
			changed = true
			break
		}
	}
	if !changed {
		accepted = true
		return nil
	}
	after, err := quality.Inspect(ctx, g)
	if err != nil {
		return err
	}
	refined := make([][]*geo.Point, len(g.Edges))
	for i, e := range g.Edges {
		refined[i] = e.Points
	}
	restore()
	before, err := quality.Inspect(ctx, g)
	if err != nil {
		return err
	}
	if after.Crossings <= before.Crossings && after.InteriorCrossings <= before.InteriorCrossings &&
		after.RouteObstructions <= before.RouteObstructions && after.TextOcclusions <= before.TextOcclusions {
		for i, e := range g.Edges {
			e.Points = refined[i]
		}
		accepted = true
	}
	return nil
}
