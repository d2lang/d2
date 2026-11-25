package d2talalayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

// An isolated frame can use its existing space without changing the selected
// packing, moving a neighboring tile, or invalidating a connection.
func clearIsolatedFrameDecoration(source *d2graph.Graph, g *layoutgraph.Graph) {
	if len(source.Objects) != 2 || len(source.Edges) != 0 || len(g.Nodes) != 2 || len(g.Containers[nil]) != 1 || g.IsRootHierarchy {
		return
	}
	frame := g.Containers[nil][0]
	children := g.Containers[frame]
	if len(children) != 1 {
		return
	}
	child := children[0]
	oval := frame.GetType() == shape.OVAL_TYPE || frame.GetType() == shape.CIRCLE_TYPE
	rectangular := func(n *layoutgraph.Node) bool {
		return n.GetType() == shape.SQUARE_TYPE || n.GetType() == shape.REAL_SQUARE_TYPE
	}
	if (!rectangular(frame) && !oval) || !rectangular(child) || frame.ForceHierarchy || child.ForceHierarchy || child.Icon != nil {
		return
	}
	// Font size has already been measured and is also written back by Layout.
	// Other presentation styles can change the painted or visible footprint.
	for _, obj := range source.Objects {
		if obj.NearKey != nil || obj.Left != nil || obj.Top != nil || obj.GridRows != nil || obj.GridColumns != nil ||
			obj.Tooltip != nil || obj.Link != nil || obj.Language != "" || obj.ContentAspectRatio != nil || len(obj.Classes) != 0 ||
			obj.Style != (d2graph.Style{FontSize: obj.Style.FontSize}) || obj.IconStyle != (d2graph.Style{}) || !plainCaptionSpacing(obj.Label.Value) {
			return
		}
	}

	childPaint := renderedDecorationBoxes(child)
	content := *childPaint.body
	if caption := childPaint.caption; caption != nil {
		left := math.Min(content.TopLeft.X, caption.TopLeft.X)
		top := math.Min(content.TopLeft.Y, caption.TopLeft.Y)
		right := math.Max(content.TopLeft.X+content.Width, caption.TopLeft.X+caption.Width)
		bottom := math.Max(content.TopLeft.Y+content.Height, caption.TopLeft.Y+caption.Height)
		content = *geo.NewBox(geo.NewPoint(left, top), right-left, bottom-top)
	}
	framePaint := renderedDecorationBoxes(frame)
	// Set the moving coordinate from its exported origin. Truncating a
	// fractional position after a move across zero can otherwise lose a pixel.
	move := func(delta geo.Point) {
		if delta.X != 0 {
			child.TopLeft.X = childPaint.body.TopLeft.X + delta.X
		}
		if delta.Y != 0 {
			child.TopLeft.Y = childPaint.body.TopLeft.Y + delta.Y
		}
	}

	// A wide anchored caption can escape a wall while there is still room
	// on the opposite side. Try centering the visible footprint along one
	// axis before moving the tile on both axes.
	if frame.Label == nil && frame.Icon == nil && childPaint.caption != nil && child.Label.PositionFixed() && !child.Label.Position.IsOutside() {
		if frameContains(framePaint.body, *childPaint.caption, geo.Point{}, oval, 0) {
			return
		}
		dx := math.Round(framePaint.body.Center().X - content.Center().X)
		dy := math.Round(framePaint.body.Center().Y - content.Center().Y)
		for _, delta := range [...]geo.Point{{X: dx}, {Y: dy}, {X: dx, Y: dy}} {
			if frameContains(framePaint.body, content, delta, oval, layoutgraph.NodeGap) {
				move(delta)
				break
			}
		}
		return
	}

	// A single fixed interior decoration can be checked against both the tile
	// and its caption. Test each separately: their bounding union can have
	// empty space between them.
	var obstacle *geo.Box
	if l := frame.Label; l != nil && !l.Position.IsOutside() {
		if !l.PositionFixed() {
			return
		}
		obstacle = framePaint.caption
	}
	if icon := frame.Icon; icon != nil && !icon.Position.IsOutside() {
		if obstacle != nil || !icon.PositionFixed() {
			return
		}
		obstacle = framePaint.icon
	}
	if obstacle == nil || (!obstacle.Overlaps(*childPaint.body) && (childPaint.caption == nil || !obstacle.Overlaps(*childPaint.caption))) {
		return
	}

	// Separate the rectangles along the nearest feasible side. The minimum
	// gutter keeps the child clear of the wall, including on an ellipse.
	best := geo.Point{}
	bestDistance := math.Inf(1)
	for _, delta := range [...]geo.Point{
		{Y: math.Ceil(obstacle.TopLeft.Y + obstacle.Height + label.PADDING - content.TopLeft.Y)},
		{Y: math.Floor(obstacle.TopLeft.Y - label.PADDING - content.TopLeft.Y - content.Height)},
		{X: math.Ceil(obstacle.TopLeft.X + obstacle.Width + label.PADDING - content.TopLeft.X)},
		{X: math.Floor(obstacle.TopLeft.X - label.PADDING - content.TopLeft.X - content.Width)},
	} {
		distance := math.Abs(delta.X) + math.Abs(delta.Y)
		if distance < bestDistance && frameContains(framePaint.body, content, delta, oval, layoutgraph.NodeGap) {
			best, bestDistance = delta, distance
		}
	}
	if bestDistance < math.Inf(1) {
		move(best)
	}
}

// The farthest padded corner suffices for both a rectangle and an ellipse.
func frameContains(frame *geo.Box, content geo.Box, delta geo.Point, oval bool, margin float64) bool {
	rx, ry := frame.Width/2, frame.Height/2
	center, point := frame.Center(), content.Center()
	dx := math.Abs(point.X+delta.X-center.X) + content.Width/2 + margin
	dy := math.Abs(point.Y+delta.Y-center.Y) + content.Height/2 + margin
	if oval {
		return dx*dx/(rx*rx)+dy*dy/(ry*ry) <= 1
	}
	return dx <= rx && dy <= ry
}
