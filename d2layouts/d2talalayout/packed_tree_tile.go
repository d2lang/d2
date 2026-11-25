package d2talalayout

import (
	"image"
	"math"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// The router considers distinct parent ports only for multiple annotated
// incident branches. Bound the final comparison on the same input.
func hasCompetingTreeCaptions(g *layoutgraph.Graph) bool {
	if len(g.Nodes) > 16 || len(g.Edges) < 2 || len(g.Edges) > 16 {
		return false
	}
	for _, n := range g.Nodes {
		labels := 0
		for _, e := range n.Edges {
			if (e.From == n && e.SourceArrowheadLabel != nil) || (e.To == n && e.TargetArrowheadLabel != nil) {
				labels++
				if labels > 1 {
					return true
				}
			}
		}
	}
	return false
}

// Endpoint coverage uses the final integer shape boxes and rendered routes.
// Badges, custom paint and animation may change the visible extent of either.
func supportsTreeCaptionComparison(g *d2graph.Graph) bool {
	if g.Theme != nil && (g.Theme.SpecialRules.OuterContainerDoubleBorder || g.Theme.SpecialRules.C4) {
		return false
	}
	plain := func(a *d2graph.Attributes) bool {
		if a == nil {
			return true
		}
		return a.Icon == nil && a.Link == nil && a.Tooltip == nil && len(a.Classes) == 0 && a.Language == "" &&
			a.Style == (d2graph.Style{FontSize: a.Style.FontSize, StrokeWidth: a.Style.StrokeWidth}) &&
			(a.Style.StrokeWidth == nil || a.Style.StrokeWidth.TweenOffset == 0)
	}
	for _, o := range g.Objects {
		if !plain(&o.Attributes) || o.Style.StrokeWidth != nil || o.IsGridDiagram() || o.Class != nil || o.SQLTable != nil || o.ContentAspectRatio != nil || o.ZIndex != 0 {
			return false
		}
	}
	for _, e := range g.Edges {
		if !plain(&e.Attributes) || !plain(e.SrcArrowhead) || !plain(e.DstArrowhead) || e.ZIndex != 0 {
			return false
		}
	}
	return true
}

// Keep both the selected ports and their placement when a single free tile
// can slide clear of the captions inside the existing canvas.
func clearPackedTreeTile(source *d2graph.Graph, g *layoutgraph.Graph) bool {
	if !hasCompetingTreeCaptions(g) || len(g.Containers[nil]) != len(g.Nodes) || len(source.Objects) != len(g.Nodes) || !supportsTreeCaptionComparison(source) {
		return false
	}
	for _, o := range source.Objects {
		if o.NearKey != nil || (o.Left == nil) != (o.Top == nil) {
			return false
		}
	}
	// Round obstructions outwards. Rendered node bodies already use integers.
	pixels := func(b *geo.Box) image.Rectangle {
		return image.Rect(int(math.Floor(b.TopLeft.X)), int(math.Floor(b.TopLeft.Y)), int(math.Ceil(b.TopLeft.X+b.Width)), int(math.Ceil(b.TopLeft.Y+b.Height)))
	}
	var tile *layoutgraph.Node
	var body, envelope image.Rectangle
	var siblings, blocks []image.Rectangle
	for _, n := range g.Nodes {
		switch n.ShapeType() {
		case shape.SQUARE_TYPE, shape.OVAL_TYPE, shape.CIRCLE_TYPE, shape.DOCUMENT_TYPE:
		default:
			return false
		}
		if len(n.Nears) != 0 || n.Hierarchy != nil || n.Sequence != nil || n.IsInvisible || n.Cluster != nil || n.Icon != nil || n.IsMultiple || n.Is3D {
			return false
		}
		d := renderedDecorationBoxes(n)
		b := decorationPixels(d.body)
		envelope = envelope.Union(b)
		if len(n.Edges) == 0 {
			if tile != nil || n.ShapeType() != shape.SQUARE_TYPE || n.FixedTopLeft != nil || d.caption != nil && !pixels(d.caption).In(b) {
				return false
			}
			tile, body = n, b
		} else {
			siblings = append(siblings, b)
			blocks = append(blocks, b.Inset(-int(layoutgraph.NodeGap)))
			if d.caption != nil {
				blocks = append(blocks, pixels(d.caption).Inset(-label.PADDING))
			}
		}
	}
	if tile == nil {
		return false
	}
	endpoints, other, ok := packedTreeCaptionBoxes(g)
	tileBox := geo.NewBox(geo.NewPoint(float64(body.Min.X), float64(body.Min.Y)), float64(body.Dx()), float64(body.Dy()))
	if !ok || !slices.ContainsFunc(endpoints, tileBox.Overlaps) {
		return false
	}
	text := append(endpoints, other...)
	for _, cap := range text {
		blocks = append(blocks, pixels(&cap).Inset(-label.PADDING))
	}
	segments := 0
	for _, e := range g.Edges {
		segments += len(e.Points) - 1
		if len(e.Points) < 2 || segments > 128 {
			return false
		}
		for _, head := range [...]layoutgraph.Arrowhead{e.SourceArrowhead, e.TargetArrowhead} {
			if head != "" && head != layoutgraph.NoArrowhead && head != layoutgraph.TriangleArrowhead {
				return false
			}
		}
		for i, b := range e.Points[1:] {
			a := e.Points[i]
			p := geo.NewBox(geo.NewPoint(min(a.X, b.X), min(a.Y, b.Y)), math.Abs(a.X-b.X), math.Abs(a.Y-b.Y))
			blocks = append(blocks, pixels(p).Inset(-int(layoutgraph.NodeGap)))
		}
	}
	align := func(a0, a1, b0, b1 int) int {
		if a0+a1 == b0+b1 {
			return 2
		}
		if a0 == b0 || a1 == b1 {
			return 1
		}
		return 0
	}
	best := body
	distance := math.MaxInt
	try := func(dx, dy int) {
		shift := max(dx, -dx, dy, -dy)
		if shift == 0 || shift >= distance || shift > max(body.Dx(), body.Dy()) {
			return
		}
		box := body.Add(image.Pt(dx, dy))
		if !box.In(envelope) || slices.ContainsFunc(blocks, box.Overlaps) {
			return
		}
		for _, b := range siblings {
			if align(box.Min.X, box.Max.X, b.Min.X, b.Max.X) < align(body.Min.X, body.Max.X, b.Min.X, b.Max.X) ||
				align(box.Min.Y, box.Max.Y, b.Min.Y, b.Max.Y) < align(body.Min.Y, body.Max.Y, b.Min.Y, b.Max.Y) {
				return
			}
		}
		best, distance = box, shift
	}
	// Try just the sides of obstructed text and pre-existing rows or columns.
	for _, caption := range text {
		b := pixels(&caption).Inset(-label.PADDING)
		if !b.Overlaps(body) {
			continue
		}
		try(b.Min.X-body.Max.X, 0)
		try(b.Max.X-body.Min.X, 0)
		try(0, b.Min.Y-body.Max.Y)
		try(0, b.Max.Y-body.Min.Y)
	}
	for _, b := range siblings {
		try(b.Min.X-body.Min.X, 0)
		try(b.Max.X-body.Max.X, 0)
		try(0, b.Min.Y-body.Min.Y)
		try(0, b.Max.Y-body.Max.Y)
	}
	if best == body {
		return false
	}
	tile.TopLeft = geo.NewPoint(float64(best.Min.X), float64(best.Min.Y))
	return true
}
