package d2talalayout

import (
	"image"
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

func plainCaptionSpacing(text string) bool {
	return layoutgraph.PlainCaptionSpacing(text)
}

type nodeDecorationBoxes struct {
	node                *layoutgraph.Node
	body, caption, icon *geo.Box
}

func renderedDecorationBoxes(n *layoutgraph.Node) nodeDecorationBoxes {
	// Use the same integer dimensions and positions as the D2 exporter.
	body := geo.NewBox(geo.NewPoint(math.Trunc(n.TopLeft.X), math.Trunc(n.TopLeft.Y)), math.Trunc(n.Width), math.Trunc(n.Height))
	var exported d2target.Shape
	exported.SetType(d2target.SHAPE_TYPE_TO_DSL_SHAPE[n.ShapeType()])
	s := shape.NewShape(d2target.DSL_SHAPE_TO_SHAPE_TYPE[exported.Type], body)
	anchorBox := func(outer bool) *geo.Box {
		if outer {
			return body
		}
		return s.GetInnerBox()
	}
	item := nodeDecorationBoxes{node: n, body: body}
	if l := n.Label; l != nil && l.Text != "" {
		w, h := math.Trunc(l.Width), math.Trunc(l.Height)
		item.caption = geo.NewBox(l.Position.GetPointOnBox(anchorBox(l.Position.IsOutside() || l.Position.IsBorder()), label.PADDING, w, h), w, h)
	}
	if n.HasIconDecoration() {
		pos := n.Icon.Position
		box := anchorBox(pos.IsOutside())
		size := float64(d2target.GetIconSize(box, pos.String()))
		item.icon = geo.NewBox(pos.GetPointOnBox(box, label.PADDING, size, size), size, size)
	}
	return item
}

// Diagram.BoundingBox truncates label origins before adding their dimensions.
func decorationPixels(b *geo.Box) image.Rectangle {
	x, y := int(b.TopLeft.X), int(b.TopLeft.Y)
	return image.Rect(x, y, x+int(b.Width), y+int(b.Height))
}

func (owner *nodeDecorationBoxes) obscuredBy(other *nodeDecorationBoxes) bool {
	if owner == other {
		return false
	}
	if other.icon != nil && other.node.Icon.PositionFixed() && owner.caption.Overlaps(*other.icon) {
		return true
	}
	return owner.node.Sequence != nil && owner.node.Sequence == other.node.Sequence &&
		other.caption != nil && owner.caption.Overlaps(*other.caption)
}

func clearCaptionPosition(owner *nodeDecorationBoxes, candidate *geo.Box, items []nodeDecorationBoxes) bool {
	room := geo.NewBox(geo.NewPoint(candidate.TopLeft.X-label.PADDING, candidate.TopLeft.Y-label.PADDING),
		candidate.Width+2*label.PADDING, candidate.Height+2*label.PADDING)
	// The caption's center must still identify its own tile as the nearest one.
	center := candidate.Center()
	distance := func(b *geo.Box) float64 {
		dx := max(0, b.TopLeft.X-center.X, center.X-b.TopLeft.X-b.Width)
		dy := max(0, b.TopLeft.Y-center.Y, center.Y-b.TopLeft.Y-b.Height)
		return dx*dx + dy*dy
	}
	ownerDistance := distance(owner.body)
	for i := range items {
		other := &items[i]
		if other.node == owner.node.Container {
			if !layoutgraph.LabelBoxFits(other.body, room) {
				return false
			}
		} else if owner != other && (room.Overlaps(*other.body) || distance(other.body) <= ownerDistance) {
			return false
		}
		if other.icon != nil && room.Overlaps(*other.icon) || owner != other && other.caption != nil && room.Overlaps(*other.caption) {
			return false
		}
	}
	return true
}

// Reposition a colliding automatic caption after seed selection, when every
// other decoration has its final location. An edgeless board includes fused
// step rows whose original transitions have become the joins between steps.
func clearCrowdedCaptions(source *d2graph.Graph, g *layoutgraph.Graph) {
	const maxObjects = 16 // Bounds the pair comparisons in this optional pass.
	if len(g.Edges) != 0 || len(g.Nodes) < 2 || len(g.Nodes) > maxObjects || len(source.Objects) > maxObjects || g.IsRootHierarchy ||
		source.Theme != nil && source.Theme.SpecialRules.OuterContainerDoubleBorder {
		return
	}
	if !slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool {
		return n.Sequence != nil || n.HasIconDecoration() && n.Icon.PositionFixed()
	}) {
		return
	}
	if !slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool {
		return !n.IsContainer() && n.Label != nil && !n.Label.PositionFixed() && n.Label.Position.IsOutside()
	}) {
		return
	}
	for _, o := range source.Objects {
		if o.Style != (d2graph.Style{FontSize: o.Style.FontSize}) || o.IconStyle != (d2graph.Style{}) ||
			o.Tooltip != nil || o.Link != nil || o.Language != "" || len(o.Classes) != 0 || o.IsGridDiagram() || o.ContentAspectRatio != nil {
			return
		}
	}
	items := make([]nodeDecorationBoxes, 0, len(g.Nodes))
	var envelope image.Rectangle
	for _, n := range g.Nodes {
		switch n.ShapeType() {
		case shape.SQUARE_TYPE, shape.STEP_TYPE, shape.PERSON_TYPE:
		default:
			return
		}
		if n.IsInvisible || n.ForceHierarchy || n.Label != nil && !plainCaptionSpacing(n.Label.Text) ||
			n.Container != nil && (n.Container.ShapeType() != shape.SQUARE_TYPE || n.Container.Container != nil) {
			return
		}
		item := renderedDecorationBoxes(n)
		items = append(items, item)
		envelope = envelope.Union(decorationPixels(item.body))
		if item.caption != nil && n.Label.Position.IsOutside() {
			envelope = envelope.Union(decorationPixels(item.caption))
		}
	}
	for i := range items {
		owner := &items[i]
		n := owner.node
		if owner.caption == nil || n.IsContainer() || n.Label.PositionFixed() || !n.Label.Position.IsOutside() {
			continue
		}
		conflict := false
		for j := range items {
			if owner.obscuredBy(&items[j]) {
				conflict = true
				break
			}
		}
		if !conflict {
			continue
		}
		for _, pos := range []label.Position{label.OutsideBottomCenter, label.OutsideTopCenter, label.OutsideBottomLeft, label.OutsideTopLeft,
			label.OutsideBottomRight, label.OutsideTopRight, label.OutsideLeftMiddle, label.OutsideRightMiddle} {
			w, h := owner.caption.Width, owner.caption.Height
			candidate := geo.NewBox(pos.GetPointOnBox(owner.body, label.PADDING, w, h), w, h)
			if decorationPixels(candidate).In(envelope) && clearCaptionPosition(owner, candidate, items) {
				n.Label.Position = pos
				owner.caption = candidate
				break
			}
		}
	}
}
