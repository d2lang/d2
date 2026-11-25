package d2talalayout

import (
	"cmp"
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

type stepRouteDecoration struct {
	visible *geo.Box
	bounds  *geo.Box
}

// A straight wire between matching chevrons can slide up or down. Both ends
// follow the same slope, so a side caption can stay visible at no extra length.
func clearStepConnectorDecorations(source *d2graph.Graph, g *layoutgraph.Graph) {
	if len(g.Nodes) < 2 || len(g.Nodes) > 16 || !g.Nodes[0].IsSequenceStep() || len(g.Edges) != len(g.Nodes)-1 ||
		len(source.Objects) != len(g.Nodes) || len(source.Edges) != len(g.Edges) || g.IsRootHierarchy {
		return
	}
	if !slices.ContainsFunc(g.Nodes, func(n *layoutgraph.Node) bool {
		return n.Label != nil && n.Label.PositionFixed() && n.Label.Position.IsOutside() ||
			n.HasIconDecoration() && n.Icon.PositionFixed() && n.Icon.Position.IsOutside()
	}) || !plainStepConnections(source) {
		return
	}

	nodes := slices.Clone(g.Nodes)
	slices.SortFunc(nodes, func(a, b *layoutgraph.Node) int { return cmp.Compare(a.TopLeft.X, b.TopLeft.X) })
	items := make([]nodeDecorationBoxes, len(nodes))
	index := make(map[*layoutgraph.Node]int, len(nodes))
	var decorations []*geo.Box
	var anchored []stepRouteDecoration
	first := nodes[0]
	for i, n := range nodes {
		if !n.IsSequenceStep() || n.Container != nil || n.Sequence != nil || n.IsInvisible ||
			n.TopLeft.Y != first.TopLeft.Y || n.Height != first.Height || shape.StepWedgeWidth(n.Width) != shape.StepWedgeWidth(first.Width) ||
			i > 0 && n.TopLeft.X <= nodes[i-1].TopLeft.X+nodes[i-1].Width {
			return
		}
		item := renderedDecorationBoxes(n)
		items[i], index[n] = item, i
		// Use the same conservative visible band as grouping for deciding
		// whether a side label is struck through. Full measured boxes still
		// protect every caption and icon at the proposed route.
		visible := n.StepDecorations(item.body)
		for k, bounds := range [2]*geo.Box{item.caption, item.icon} {
			v := visible[k]
			if bounds != nil && v.Box.TopLeft != nil && v.Side.IsHorizontal() && (k == 0 && n.Label.PositionFixed() || k == 1 && n.Icon.PositionFixed()) {
				anchored = append(anchored, stepRouteDecoration{&v.Box, bounds})
			}
		}
		if item.caption != nil {
			decorations = append(decorations, item.caption)
		}
		if item.icon != nil {
			decorations = append(decorations, item.icon)
		}
	}

	// Consecutive, non-overlapping step bodies bound each wire's corridor.
	byGap := make([]*layoutgraph.Edge, len(g.Edges))
	for _, edge := range g.Edges {
		i := index[edge.From]
		if index[edge.To] != i+1 || byGap[i] != nil || len(edge.Points) != 2 || edge.IsCurve ||
			edge.Points[0].Y != edge.Points[1].Y {
			return
		}
		byGap[i] = edge
	}
	for i, edge := range byGap {
		slideClearOfStepDecoration(edge, items[i].body, items[i+1].body, anchored, decorations, byGap)
	}
}

func plainStepConnections(g *d2graph.Graph) bool {
	if g.Theme != nil && (g.Theme.SpecialRules.OuterContainerDoubleBorder || g.Theme.SpecialRules.C4) {
		return false
	}
	for _, o := range g.Objects {
		if o.Style != (d2graph.Style{FontSize: o.Style.FontSize, Fill: o.Style.Fill, Opacity: o.Style.Opacity}) || o.IconStyle != (d2graph.Style{}) ||
			o.Link != nil || o.Tooltip != nil || o.Language != "" || o.ZIndex != 0 || len(o.Classes) > 0 ||
			o.ContentAspectRatio != nil || o.Style.Opacity != nil && o.Style.Opacity.TweenOffset != 0 {
			return false
		}
	}
	for _, e := range g.Edges {
		if e.DstArrow == e.SrcArrow || e.DstArrowhead != nil || e.SrcArrowhead != nil || e.Label.Value != "" ||
			e.Icon != nil || e.Link != nil || e.Tooltip != nil || e.ZIndex != 0 || len(e.Classes) > 0 ||
			e.Style != (d2graph.Style{StrokeWidth: e.Style.StrokeWidth}) ||
			e.Style.StrokeWidth != nil && (e.Style.StrokeWidth.Value != "2" || e.Style.StrokeWidth.TweenOffset != 0) {
			return false
		}
	}
	return true
}

func slideClearOfStepDecoration(e *layoutgraph.Edge, from, to *geo.Box, anchored []stepRouteDecoration, decorations []*geo.Box, neighbors []*layoutgraph.Edge) {
	const stroke = 2.
	markerLength, markerHeight := d2target.TriangleArrowhead.Dimensions(stroke)
	// Allow a stroke radius and an extra pixel for the SVG coordinate rounding.
	clearance := markerHeight/2 + stroke/2 + 1
	a, b := e.Points[0], e.Points[1]
	if b.X-a.X < 2*markerLength {
		return
	}
	envelope := func(left, right *geo.Point) *geo.Box {
		return geo.NewBox(geo.NewPoint(left.X-1, left.Y-clearance), right.X-left.X+2, 2*clearance)
	}
	shaft := geo.NewBox(geo.NewPoint(a.X, a.Y-stroke/2), b.X-a.X, stroke)
	bestDistance := math.Inf(1)
	for _, obstacle := range anchored {
		if !shaft.Overlaps(*obstacle.visible) {
			continue
		}
		for _, y := range [...]float64{math.Floor(obstacle.bounds.TopLeft.Y - clearance), math.Ceil(obstacle.bounds.TopLeft.Y + obstacle.bounds.Height + clearance)} {
			if y < from.TopLeft.Y+clearance || y > from.TopLeft.Y+from.Height-clearance || math.Abs(y-a.Y) >= bestDistance {
				continue
			}
			l, r := geo.NewPoint(from.TopLeft.X+from.Width, y), geo.NewPoint(to.TopLeft.X, y)
			left := shape.TraceToShapeBorder(shape.NewStep(from), l, r)
			right := shape.TraceToShapeBorder(shape.NewStep(to), r, l)
			if right.X <= left.X || right.X-left.X > b.X-a.X {
				continue
			}
			ink := envelope(left, right)
			if slices.ContainsFunc(decorations, func(box *geo.Box) bool { return ink.Overlaps(*box) }) ||
				slices.ContainsFunc(neighbors, func(other *layoutgraph.Edge) bool {
					return other != e && ink.Overlaps(*envelope(other.Points[0], other.Points[1]))
				}) {
				continue
			}
			bestDistance = math.Abs(y - a.Y)
			e.Points = []*geo.Point{left, right}
		}
	}
}
