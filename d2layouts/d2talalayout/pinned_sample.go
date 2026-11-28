package d2talalayout

import (
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// A tween of independent, fully specified boxes needs no placement search.
// Reuse the regular input validation, measurements, font metadata and result
// commit. Only accept samples whose integer layout is determined by the pins.
func placePinnedSample(source *d2graph.Graph, input seedInput) bool {
	g := input.graph
	const maxBoxes = 32 // Bound pairwise checks for overlapping labels.
	if !source.IsWholeLayout() || source.Root.IsGridDiagram() || source.Root.IsSequenceDiagram() ||
		len(g.Nodes) == 0 || len(g.Nodes) > maxBoxes || len(g.Edges) != 0 || g.IsRootHierarchy ||
		len(g.Nodes) != len(g.Containers[nil]) {
		return false
	}
	moving := false
	for _, obj := range source.Objects {
		if obj.NearKey != nil || obj.Left == nil || obj.Top == nil || obj.ContentAspectRatio != nil || obj.Language != "" || obj.Direction.Value != "" {
			return false
		}
		switch obj.Shape.Value {
		case "", d2target.ShapeRectangle, d2target.ShapeSquare, d2target.ShapeCircle, d2target.ShapeOval:
		default:
			return false
		}
		moving = moving || obj.Left.TweenedPin || obj.Top.TweenedPin
	}
	if !moving {
		return false
	}
	for i, n := range g.Nodes {
		// Leave large canvases to the ordinary stage-by-stage extent checks,
		// which include intermediate packing and spacing.
		if n.FixedTopLeft == nil || n.DesiredWidth == nil || n.DesiredHeight == nil ||
			n.Width != *n.DesiredWidth || n.Height != *n.DesiredHeight || n.Shape.AspectRatio1() && n.Width != n.Height ||
			n.Is3D || n.IsMultiple || n.Icon != nil || n.IsInvisible ||
			n.FixedTopLeft.X < 0 || n.FixedTopLeft.Y < 0 || n.FixedTopLeft.X+n.Width > limits.MaxGraphSize/2 || n.FixedTopLeft.Y+n.Height > limits.MaxGraphSize/2 {
			return false
		}
		if l := n.Label; l != nil {
			// Rounded outlines have different interior text bounds. Let the label
			// planner handle them, along with external and edge-aligned captions.
			if n.Shape.GetType() != shape.SQUARE_TYPE && n.Shape.GetType() != shape.REAL_SQUARE_TYPE {
				return false
			}
			if (l.Position != label.Unset && l.Position != label.InsideMiddleCenter) || l.Width+2*label.PADDING > n.Width || l.Height+2*label.PADDING > n.Height {
				return false
			}
		}
		for _, other := range g.Nodes[:i] {
			// An unlabeled rail may intentionally sit behind a marker. A shared
			// area containing lettering instead needs the ordinary label planner.
			if n.Label == nil && other.Label == nil {
				continue
			}
			a, b := n.FixedTopLeft, other.FixedTopLeft
			if a.X+n.Width > b.X && b.X+other.Width > a.X && a.Y+n.Height > b.Y && b.Y+other.Height > a.Y {
				return false
			}
		}
	}
	for _, n := range g.Nodes {
		n.TopLeft = n.FixedTopLeft.Copy()
		if n.Label != nil {
			n.Label.Position = label.InsideMiddleCenter
		}
	}
	return true
}
