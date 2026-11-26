package d2talalayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// A pinned frame can have enough authored height for its two connected
// children even though their selected row covers a neighboring pin.
func clearPinnedFrameNeighbor(source *d2graph.Graph, g *layoutgraph.Graph) {
	if len(source.Objects) != 4 || len(g.Nodes) != 4 || len(source.Edges) != 1 || len(g.Edges) != 1 ||
		len(g.Containers[nil]) != 2 || g.IsRootHierarchy || source.Theme != nil && source.Theme.SpecialRules.OuterContainerDoubleBorder {
		return
	}
	edge, authored := g.Edges[0], source.Edges[0]
	a, b := edge.From, edge.To
	frame := a.Container
	if a == b || frame == nil || b.Container != frame || frame.Container != nil || frame.FixedTopLeft == nil ||
		frame.DesiredHeight == nil || frame.Height != *frame.DesiredHeight || frame.DesiredWidth != nil ||
		a.FixedTopLeft != nil || b.FixedTopLeft != nil || len(g.Containers[frame]) != 2 {
		return
	}
	if authored.SrcArrow || !authored.DstArrow || authored.Style != (d2graph.Style{}) ||
		edge.KeepExplicit || edge.Label != nil || edge.IsCurve || len(edge.Points) != 2 {
		return
	}
	neighbor := g.Containers[nil][0]
	if neighbor == frame {
		neighbor = g.Containers[nil][1]
	}
	if neighbor.FixedTopLeft == nil || !frame.Overlaps(neighbor.Box) {
		return
	}
	for _, obj := range source.Objects {
		if obj.Shape.Value != "" && obj.Shape.Value != d2target.ShapeRectangle || obj.Direction.Value != "" && obj.Direction.Value != "down" ||
			obj.NearKey != nil || obj.Icon != nil || len(obj.Classes) > 0 ||
			obj.Style != (d2graph.Style{FontSize: obj.Style.FontSize}) || obj.Tooltip != nil || obj.Link != nil ||
			obj.Language != "" || obj.IsGridDiagram() || obj.ContentAspectRatio != nil || (obj.Left == nil) != (obj.Top == nil) {
			return
		}
	}
	if d := source.Root.Direction.Value; d != "" && d != "down" {
		return
	}
	for _, n := range g.Nodes {
		if n.Shape.GetType() != shape.SQUARE_TYPE || n.IsInvisible || n.Is3D || n.IsMultiple || n.Sequence != nil ||
			n.Width != math.Trunc(n.Width) || n.Height != math.Trunc(n.Height) {
			return
		}
	}
	for _, n := range []*layoutgraph.Node{a, b} {
		if l := n.Label; l != nil && (l.Position != label.InsideMiddleCenter || l.Width+2*label.PADDING > n.Width || l.Height+2*label.PADDING > n.Height) {
			return
		}
	}

	// Keep a straight, center-to-center connection with exactly the same
	// length. Put the two tiles on a common centerline and retain the side
	// margins they had in the row.
	start, end := edge.Points[0], edge.Points[1]
	if start.X != a.TopLeft.X+a.Width || start.Y != a.Center().Y || end.X != b.TopLeft.X ||
		end.Y != b.Center().Y || start.Y != end.Y || end.X <= start.X {
		return
	}
	gap := end.X - start.X
	span := max(a.Width, b.Width)
	width := frame.Width - (b.TopLeft.X + b.Width - a.TopLeft.X) + span
	right := frame.TopLeft.X + width
	if right+layoutgraph.NodeGap > neighbor.TopLeft.X {
		return
	}
	if caption := renderedDecorationBoxes(neighbor).caption; caption != nil && right+layoutgraph.NodeGap > caption.TopLeft.X {
		return
	}
	x, y := a.TopLeft.X+span/2, min(a.TopLeft.Y, b.TopLeft.Y)
	// The exporter uses integer tile origins. Keep both center ports exact
	// after export, including for rectangles of odd width.
	if math.Trunc(x-a.Width/2) != x-a.Width/2 || math.Trunc(x-b.Width/2) != x-b.Width/2 || math.Trunc(y) != y || math.Trunc(gap) != gap {
		return
	}
	bottom := y + a.Height + gap + b.Height
	if bottom+max(layoutgraph.NodeGap, y-frame.TopLeft.Y) > frame.TopLeft.Y+frame.Height {
		return
	}
	if l := frame.Label; l != nil {
		if l.Position != label.InsideTopCenter || l.Width+2*layoutgraph.NodeGap > width {
			return
		}
		if title := renderedDecorationBoxes(frame).caption; title != nil && title.TopLeft.Y+title.Height+label.PADDING > y {
			return
		}
	}
	frame.Width = width
	a.TopLeft = geo.NewPoint(x-a.Width/2, y)
	b.TopLeft = geo.NewPoint(x-b.Width/2, y+a.Height+gap)
	edge.Points = []*geo.Point{geo.NewPoint(x, y+a.Height), geo.NewPoint(x, b.TopLeft.Y)}
}
