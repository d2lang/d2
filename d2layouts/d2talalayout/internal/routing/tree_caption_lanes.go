package routing

import (
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/labeling"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/quality"
	"github.com/d2lang/d2/lib/geo"
)

type captionBranch struct {
	tree    *layoutgraph.Tree
	parent  *layoutgraph.Node
	face    geo.Orientation
	caption geo.Box
}

// RefineCrowdedTreeFace can move one of two colliding upright captions from
// the same top or bottom face back to the side facing the horizontal tree.
// A dogleg keeps its child port. A straight route may slide an unannotated
// child port on the same side. Only a shorter or equal-length wire that
// clears the captions and introduces no new contact is eligible.
func RefineCrowdedTreeFace(ctx context.Context, g *layoutgraph.Graph) error {
	if g.IsRootHierarchy || len(g.Nodes) > 16 || len(g.Edges) > 16 {
		return nil
	}
	for _, e := range g.Edges {
		if e.Label != nil || e.IsLoop() || e.IsInvisible || e.IsCurve || e.HasTableColumn() || len(e.Points) > 6 {
			return nil
		}
	}
	var branches []captionBranch
	for _, n := range g.Nodes {
		if !ordinaryRectangle(n) || n.IsContainer() || n.Container != nil || n.Icon != nil || n.Sequence != nil || n.ForceHierarchy {
			return nil
		}
		if n.Label != nil {
			box := nodeTextBox(n)
			if !layoutgraph.LabelBoxFits(&n.Box, &box) {
				return nil
			}
		}
		tree := g.NodeToTree[n]
		if tree == nil || tree.SentinelEdge == nil {
			continue
		}
		e, parent := tree.SentinelEdge, tree.SentinelNode()
		caption := labeling.PositionArrowheadLabel(e, parent == e.To, e.Points)
		if caption == nil {
			continue
		}
		tip, next := fanEndpoint(e, parent)
		face := fanFace(parent, tip, next)
		if len(branches) == 2 || len(e.Points) != 3 || !tree.Orientation.IsHorizontal() || !face.IsVertical() {
			return nil
		}
		branches = append(branches, captionBranch{tree, parent, face, caption.Box})
	}
	if len(branches) != 2 {
		return nil
	}
	a, b := branches[0], branches[1]
	if a.parent != b.parent || a.face != b.face || !a.caption.Overlaps(b.caption) ||
		a.tree.SentinelEdge.ReversedForLayout != b.tree.SentinelEdge.ReversedForLayout {
		return nil
	}
	// Integer node bounds are already on the exported canvas. Keep the
	// proposed stroke, turns, markers and text within that envelope.
	left, top, right, bottom := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, n := range g.Nodes {
		x, y := math.Trunc(n.TopLeft.X), math.Trunc(n.TopLeft.Y)
		left, top = min(left, x), min(top, y)
		right, bottom = max(right, x+math.Trunc(n.Width)), max(bottom, y+math.Trunc(n.Height))
	}
	canvas := geo.NewBox(geo.NewPoint(left-1, top-1), right-left+2, bottom-top+2)
	before, err := quality.InspectLoopClearance(ctx, g)
	if err != nil {
		return ctx.Err()
	}
	// An unannotated child port may also slide on its existing face when the
	// two rectangles already have enough common height for a straight wire.
	for _, branch := range branches {
		e, child, parent := branch.tree.SentinelEdge, branch.tree.Node, branch.parent
		if child == e.To && e.TargetArrowheadLabel != nil || child == e.From && e.SourceArrowheadLabel != nil {
			continue
		}
		direction := branch.tree.Orientation
		childTip, next := fanEndpoint(e, child)
		if fanFace(child, childTip, next) != direction.GetOpposite() {
			continue
		}
		lo := max(parent.TopLeft.Y, child.TopLeft.Y) + fanMarkerPitch/2
		hi := min(parent.TopLeft.Y+parent.Height, child.TopLeft.Y+child.Height) - fanMarkerPitch/2
		if math.Ceil(lo) > math.Floor(hi) {
			continue
		}
		y := math.Round((lo + hi) / 2)
		x := parent.TopLeft.X
		if direction == geo.Right {
			x += parent.Width
		}
		points := []*geo.Point{geo.NewPoint(x, y), geo.NewPoint(childTip.X, y)}
		if fanFace(parent, points[0], points[1]) != direction {
			continue
		}
		if e.To == parent {
			slices.Reverse(points)
		}
		if shortcutLength(points) > shortcutLength(e.Points) {
			continue
		}
		clear, err := treeCaptionLaneClear(ctx, g, e, points, canvas, before)
		if err != nil {
			return err
		}
		if clear {
			e.Points = points
			return nil
		}
	}
	for _, branch := range branches {
		e, child, direction := branch.tree.SentinelEdge, branch.tree.Node, branch.tree.Orientation
		tip, next := fanEndpoint(e, child)
		childFace := fanFace(child, tip, next)
		if childFace != direction.GetOpposite() {
			continue
		}
		// Rectangles supply three ports on each side, independent of body size.
		for _, port := range branch.parent.PortsByOrientation(direction) {
			for _, offset := range [...]float64{0, -fanMarkerPitch, fanMarkerPitch} {
				first, last := treeEdgeMidpoints(branch.parent, direction, &port, tip)
				first.X += offset
				last.X += offset
				if fanFace(branch.parent, &port, first) != direction || fanFace(child, tip, last) != childFace || first.Y == last.Y {
					continue
				}
				points := []*geo.Point{&port, first, last, tip}
				if e.To == branch.parent {
					slices.Reverse(points)
				}
				if shortcutLength(points) > shortcutLength(e.Points) {
					continue
				}
				clear, err := treeCaptionLaneClear(ctx, g, e, points, canvas, before)
				if err != nil {
					return err
				}
				if clear {
					e.Points = points
					return nil
				}
			}
		}
	}
	return ctx.Err()
}

func treeCaptionLaneClear(ctx context.Context, g *layoutgraph.Graph, edge *layoutgraph.Edge, points []*geo.Point, canvas *geo.Box, before *quality.LoopInspection) (bool, error) {
	original := edge.Points
	edge.Points = points
	defer func() { edge.Points = original }()

	for _, endpoint := range []*layoutgraph.Node{edge.From, edge.To} {
		tip, _ := fanEndpoint(edge, endpoint)
		old := original[0]
		if endpoint == edge.To {
			old = original[len(original)-1]
		}
		if tip.Equals(old) {
			continue
		}
		for _, other := range endpoint.Edges {
			if other == edge {
				continue
			}
			port, _ := fanEndpoint(other, endpoint)
			if math.Hypot(tip.X-port.X, tip.Y-port.Y) < fanMarkerPitch {
				return false, nil
			}
		}
	}

	moved, ok := edgeLabelInk(edge)
	if !ok {
		return false, nil
	}
	for _, part := range moved {
		if !layoutgraph.LabelBoxFits(canvas, &part.box) {
			return false, nil
		}
	}
	for _, other := range g.Edges {
		if other == edge {
			continue
		}
		stationary, ok := edgeLabelInk(other)
		if !ok || !treeMarkersClear(edge, stationary) || !treeMarkersClear(other, moved) {
			return false, nil
		}
		for i, p := range points[1:] {
			a := points[i]
			for j, q := range other.Points[1:] {
				b := other.Points[j]
				if balanceCollinearOverlap(a, p, b, q) > 0 {
					return false, nil
				}
			}
		}
	}

	var captions []geo.Box
	for _, e := range g.Edges {
		for _, target := range [...]bool{false, true} {
			caption := labeling.PositionArrowheadLabel(e, target, e.Points)
			if caption == nil {
				continue
			}
			// Keep a pixel of clearance across exporter rounding.
			box := geo.NewBox(geo.NewPoint(caption.TopLeft.X-1, caption.TopLeft.Y-1), caption.Width+2, caption.Height+2)
			for _, n := range g.Nodes {
				if box.Overlaps(n.Box) {
					return false, nil
				}
			}
			if slices.ContainsFunc(captions, box.Overlaps) {
				return false, nil
			}
			captions = append(captions, caption.Box)
		}
	}
	after, err := quality.InspectLoopClearance(ctx, g)
	if err != nil {
		return false, ctx.Err()
	}
	return after.Preserves(before), nil
}

// An endpoint marker occupies more space than the centerline.
func treeMarkersClear(e *layoutgraph.Edge, ink []labelInk) bool {
	for _, n := range []*layoutgraph.Node{e.From, e.To} {
		tip, next := fanEndpoint(e, n)
		marker, ok := hierarchyMarkerBox(e, n, tip, next)
		if !ok {
			return false
		}
		if marker != nil && slices.ContainsFunc(ink, func(part labelInk) bool { return marker.Overlaps(part.box) }) {
			return false
		}
	}
	return true
}
