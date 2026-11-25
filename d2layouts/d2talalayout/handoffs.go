package d2talalayout

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/hierarchy"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
)

func considerHandoffs(ctx context.Context, source *d2graph.Graph, input seedInput, incumbent seedResult) (seedResult, error) {
	if !supportsHandoffs(source) {
		return incumbent, nil
	}
	order := make(map[layoutgraph.EntityID]int, len(source.Objects))
	for i, obj := range source.Objects {
		order[input.bindings.objectIDs[obj]] = i
	}
	return refineSeedResult(ctx, incumbent, func() (seedResult, error) {
		g, err := layoutgraph.Clone(ctx, incumbent.graph)
		if err != nil {
			return seedResult{}, err
		}
		// Clone carries the selected positions but clears the seed's
		// placement flags. Retain title positions supplied by the caller.
		for _, panel := range g.Containers[nil] {
			if panel.Label != nil && source.Objects[order[panel.ID]].LabelPosition != nil {
				panel.Label.FixPosition()
			}
		}
		changed, err := hierarchy.PlaceHandoffs(ctx, g, order)
		if err != nil || !changed {
			return incumbent, err
		}
		before, err := quality.Inspect(ctx, incumbent.graph)
		if err != nil {
			return seedResult{}, err
		}
		after, err := quality.Inspect(ctx, g)
		if err != nil {
			return seedResult{}, err
		}
		if after.InteriorCrossings > before.InteriorCrossings || after.Crossings > before.Crossings ||
			after.NodeOverlaps > before.NodeOverlaps || after.TextOcclusions > before.TextOcclusions ||
			after.RouteObstructions > before.RouteObstructions || after.RouteLength >= before.RouteLength {
			return incumbent, nil
		}
		result, err := evaluateSeedResult(ctx, input, g)
		result.seed = incumbent.seed
		return result, err
	})
}

// The two facing panels have automatic dimensions and horizontal flow. Keep
// explicitly constrained frames, captions and detailed endpoint shapes in the
// ordinary pipeline; leaf dimensions and ordinary paint styles can be retained.
func supportsHandoffs(source *d2graph.Graph) bool {
	root := source.Root
	if len(root.ChildrenArray) != 2 ||
		(root.Direction.Value != "right" && root.Direction.Value != "left") ||
		root.Shape.Value != "" && root.Shape.Value != d2target.ShapeRectangle ||
		root.WidthAttr != nil || root.HeightAttr != nil || root.GridColumns != nil || root.GridRows != nil {
		return false
	}
	rows := len(root.ChildrenArray[0].ChildrenArray)
	// A unique perfect matching has at most the entries of a triangular
	// matrix: all additional edges point forward in a topological row order.
	if rows < 2 || rows > hierarchy.MaxHandoffRows || len(root.ChildrenArray[1].ChildrenArray) != rows ||
		len(source.Objects) != 2+2*rows || len(source.Edges) < rows || len(source.Edges) > rows*(rows+1)/2 ||
		!supportsRouteRefinements(source) {
		return false
	}
	for _, n := range source.Objects {
		if n.Shape.Value != "" && n.Shape.Value != d2target.ShapeRectangle ||
			n.Left != nil || n.Top != nil || n.NearKey != nil || n.Attributes.LabelPosition != nil ||
			n.Direction.Value != "" || n.GridColumns != nil || n.GridRows != nil ||
			n.Language != "" || n.Style.Opacity != nil || n.ZIndex != 0 {
			return false
		}
		if len(n.ChildrenArray) > 0 {
			if n.WidthAttr != nil || n.HeightAttr != nil {
				return false
			}
			// A previous layout or API caller may have placed the title on
			// either face. Reserve its existing band in the joint drawing.
			if n.LabelPosition != nil {
				switch label.FromString(*n.LabelPosition) {
				case label.InsideTopCenter, label.InsideBottomCenter:
				default:
					return false
				}
			}
		}
	}
	for _, e := range source.Edges {
		if e.SrcArrow == e.DstArrow || e.SrcArrowhead != nil || e.DstArrowhead != nil ||
			e.Label.Value != "" || e.Style.Opacity != nil {
			return false
		}
	}
	return true
}
