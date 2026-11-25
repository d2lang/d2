package d2grid

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/util-go/go2"
)

type lanes struct {
	first                   *d2graph.Edge
	count, used, labels     int
	labelWidth, labelHeight float64
}

// RouteEdges is the fallback router for placed cells and cross-graph edges.
// Parallel and reciprocal connections share a set of distinct attachment lanes.
func RouteEdges(ctx context.Context, _ *d2graph.Graph, edges []*d2graph.Edge) error {
	type pair struct{ src, dst *d2graph.Object }
	groups := make(map[pair]*lanes, len(edges))
	for _, e := range edges {
		key := pair{e.Src, e.Dst}
		group := groups[key]
		if group == nil {
			group = &lanes{}
			groups[key], groups[pair{e.Dst, e.Src}] = group, group
		}
		group.count++
		if e.Label.Value != "" {
			group.labels++
			group.labelWidth = max(group.labelWidth, float64(e.LabelDimensions.Width))
			group.labelHeight = max(group.labelHeight, float64(e.LabelDimensions.Height))
		}
	}
	for _, e := range edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := groups[pair{e.Src, e.Dst}]
		routeEdge(e, group)
		if e.Label.Value != "" {
			e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
			if group.labels > 1 && e.Src != e.Dst {
				group.placeCaption(e)
			}
		}
		group.used++
	}
	return nil
}

func tablePortY(obj *d2graph.Object, column *int) float64 {
	rowHeight := obj.Height / float64(len(obj.SQLTable.Columns)+1)
	return obj.TopLeft.Y + (1.5+float64(*column))*rowHeight
}

func routeEdge(e *d2graph.Edge, group *lanes) {
	index, count := group.used, group.count
	e.IsCurve = false
	a, b := e.Src.Center(), e.Dst.Center()
	srcRow := e.Src.SQLTable != nil && e.SrcTableColumnIndex != nil
	dstRow := e.Dst.SQLTable != nil && e.DstTableColumnIndex != nil
	if srcRow {
		a.Y = tablePortY(e.Src, e.SrcTableColumnIndex)
	}
	if dstRow {
		b.Y = tablePortY(e.Dst, e.DstTableColumnIndex)
	}

	route := []*geo.Point{a, b}
	lane := float64(index) - float64(count-1)/2
	switch {
	case e.Src == e.Dst:
		reach := 24 + 16*float64(index)
		right := e.Src.TopLeft.X + e.Src.Width + reach
		if a.Y == b.Y && (srcRow || dstRow) {
			// A same-row reference uses opposite faces, preserving both row ports.
			left, top := e.Src.TopLeft.X-reach, e.Src.TopLeft.Y-reach
			route = []*geo.Point{a, geo.NewPoint(right, a.Y), geo.NewPoint(right, top), geo.NewPoint(left, top), geo.NewPoint(left, b.Y), b}
		} else {
			if a.Y == b.Y {
				a.Y -= e.Src.Height / 4
				b.Y += e.Src.Height / 4
			}
			route = []*geo.Point{a, geo.NewPoint(right, a.Y), geo.NewPoint(right, b.Y), b}
		}
	case srcRow || dstRow:
		srcRight, dstRight := e.Src.TopLeft.X+e.Src.Width, e.Dst.TopLeft.X+e.Dst.Width
		leftToRight := srcRight < e.Dst.TopLeft.X
		rightToLeft := dstRight < e.Src.TopLeft.X
		if leftToRight || rightToLeft {
			// Pin only the named ends to the facing side; other shapes are
			// clipped against their actual outlines from their centers.
			if srcRow {
				a.X = e.Src.TopLeft.X
				if leftToRight {
					a.X = srcRight
				}
			}
			if dstRow {
				b.X = e.Dst.TopLeft.X
				if rightToLeft {
					b.X = dstRight
				}
			}
			if count > 1 {
				// Keep the ports fixed while fanning connections through the gap.
				midX := (srcRight + e.Dst.TopLeft.X) / 2
				if rightToLeft {
					midX = (dstRight + e.Src.TopLeft.X) / 2
				}
				midY := (a.Y+b.Y)/2 + 30*lane
				route = []*geo.Point{a, geo.NewPoint(midX, midY), b}
			}
		} else if srcRight == e.Dst.TopLeft.X || dstRight == e.Src.TopLeft.X {
			// Touching tables have no facing corridor. Join their outer faces
			// above the row instead of drawing a zero-length or buried shaft.
			gap := 24 + 16*float64(index)
			startX, endX := e.Src.TopLeft.X-gap, dstRight+gap
			if dstRight == e.Src.TopLeft.X {
				startX, endX = srcRight+gap, e.Dst.TopLeft.X-gap
			}
			top := min(e.Src.TopLeft.Y, e.Dst.TopLeft.Y) - gap
			route = []*geo.Point{a, geo.NewPoint(startX, a.Y), geo.NewPoint(startX, top), geo.NewPoint(endX, top), geo.NewPoint(endX, b.Y), b}
		} else {
			// Stacked tables attach at the sides via an exterior lane.
			reach := max(srcRight, dstRight) + 24 + 16*float64(index)
			route = []*geo.Point{a, geo.NewPoint(reach, a.Y), geo.NewPoint(reach, b.Y), b}
		}
	case touchingFaces(e.Src.Box, e.Dst.Box):
		// There is no visible shaft between touching faces. Alternate
		// reciprocal edges on the outer sides of the row or column.
		src, dst := e.Src.Box, e.Dst.Box
		if src.TopLeft.X+src.Width == dst.TopLeft.X || dst.TopLeft.X+dst.Width == src.TopLeft.X {
			gap := 24 + max(16, group.labelHeight+2*label.PADDING)*float64(index/2)
			y := min(src.TopLeft.Y, dst.TopLeft.Y) - gap
			if index%2 != 0 {
				y = max(src.TopLeft.Y+src.Height, dst.TopLeft.Y+dst.Height) + gap
			}
			route = []*geo.Point{a, geo.NewPoint(a.X, y), geo.NewPoint(b.X, y), b}
		} else {
			gap := 24 + max(16, group.labelWidth+2*label.PADDING)*float64(index/2)
			x := max(src.TopLeft.X+src.Width, dst.TopLeft.X+dst.Width) + gap
			if index%2 != 0 {
				x = min(src.TopLeft.X, dst.TopLeft.X) - gap
			}
			route = []*geo.Point{a, geo.NewPoint(x, a.Y), geo.NewPoint(x, b.Y), b}
		}
	case count > 1:
		horizontal := math.Abs(a.X-b.X) >= math.Abs(a.Y-b.Y)
		span := min(e.Src.Width, e.Dst.Width)
		if horizontal {
			span = min(e.Src.Height, e.Dst.Height)
		}
		offset := lane * min(30, .7*span/float64(count-1))
		if horizontal {
			a.Y += offset
			b.Y += offset
		} else {
			a.X += offset
			b.X += offset
		}
	}
	e.Route = route
	start, end := e.TraceToShape(e.Route, 0, len(e.Route)-1)
	e.Route = e.Route[start : end+1]
}

// When two straight shafts are too close for centered captions, put each
// caption on the outside of its own shaft. For a larger group, spread their
// midpoints across the gap and keep the terminal segments aimed at their ports.
func (group *lanes) placeCaption(e *d2graph.Edge) {
	if group.count == 2 {
		if group.used == 0 {
			group.first = e
		} else {
			separatePairCaptions(group.first, e)
		}
		return
	}
	a, b := e.Src.Center(), e.Dst.Center()
	horizontal := math.Abs(a.X-b.X) >= math.Abs(a.Y-b.Y)
	perp := func(p *geo.Point) float64 {
		if horizontal {
			return p.Y
		}
		return p.X
	}
	extent, span := group.labelWidth, min(e.Src.Width, e.Dst.Width)
	if horizontal {
		extent, span = group.labelHeight, min(e.Src.Height, e.Dst.Height)
	}
	if extent+2*label.PADDING <= min(30, .7*span/float64(group.count-1)) {
		return
	}
	lane := float64(group.used) - float64(group.count-1)/2

	if len(e.Route) != 2 && !(len(e.Route) == 3 && (e.SrcTableColumnIndex != nil || e.DstTableColumnIndex != nil)) {
		return
	}
	p, q := e.Route[0], e.Route[len(e.Route)-1]
	mid := p.Interpolate(q, .5)
	mean := (perp(a) + perp(b)) / 2
	if horizontal {
		mid.Y = mean + lane*(extent+2*label.PADDING)
	} else {
		mid.X = mean + lane*(extent+2*label.PADDING)
	}
	e.Route = []*geo.Point{p, p.Interpolate(e.Route[1], .15), mid, q.Interpolate(e.Route[len(e.Route)-2], .15), q}
	length := geo.Route(e.Route).Length()
	if length > 0 {
		e.LabelPosition = go2.Pointer(label.UnlockedMiddle.String())
		e.LabelPercentage = go2.Pointer(geo.Route(e.Route[:3]).Length() / length)
	}
}

// Each arrow can run in either direction. Check the rendered rectangles for
// the two possible sides of each shaft before changing a caption's position.
func separatePairCaptions(a, b *d2graph.Edge) {
	positions := [...]label.Position{label.InsideMiddleCenter, label.OutsideTopCenter, label.OutsideBottomCenter}
	var boxes [2][3]geo.Box
	for i, e := range []*d2graph.Edge{a, b} {
		c := d2exporter.ToConnection(e, nil)
		for j, p := range positions {
			c.LabelPosition = p.String()
			boxes[i][j] = geo.Box{TopLeft: c.GetLabelTopLeft(), Width: float64(c.LabelWidth), Height: float64(c.LabelHeight)}
		}
	}
	if !boxes[0][0].Overlaps(boxes[1][0]) {
		return
	}
	for _, choice := range [][2]int{{1, 2}, {2, 1}, {1, 1}, {2, 2}, {0, 1}, {0, 2}, {1, 0}, {2, 0}} {
		if !boxes[0][choice[0]].Overlaps(boxes[1][choice[1]]) {
			a.LabelPosition = go2.Pointer(positions[choice[0]].String())
			b.LabelPosition = go2.Pointer(positions[choice[1]].String())
			return
		}
	}
}

func touchingFaces(a, b *geo.Box) bool {
	ar, ab := a.TopLeft.X+a.Width, a.TopLeft.Y+a.Height
	br, bb := b.TopLeft.X+b.Width, b.TopLeft.Y+b.Height
	return ((ar == b.TopLeft.X || br == a.TopLeft.X) && a.TopLeft.Y <= bb && b.TopLeft.Y <= ab) ||
		((ab == b.TopLeft.Y || bb == a.TopLeft.Y) && a.TopLeft.X <= br && b.TopLeft.X <= ar)
}

// A detour or loop can extend beyond the outermost cell. Include it in the
// nested grid's content bounds before its container shape and padding are fit.
func (gd *gridDiagram) fitRoutes(edges []*d2graph.Edge) {
	left, top := 0., 0.
	right, bottom := gd.width, gd.height
	for _, e := range edges {
		x1, y1, x2, y2 := routeBounds(e)
		left, top = min(left, x1), min(top, y1)
		right, bottom = max(right, x2), max(bottom, y2)
	}
	gd.width, gd.height = right-left, bottom-top
	if left != 0 || top != 0 {
		gd.shift(-left, -top)
	}
}

// routeBounds includes the rendered captions and icon as well as the path.
func routeBounds(e *d2graph.Edge) (left, top, right, bottom float64) {
	left, top, right, bottom = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, p := range e.Route {
		left, top = min(left, p.X), min(top, p.Y)
		right, bottom = max(right, p.X), max(bottom, p.Y)
	}
	if len(e.Route) < 2 || (e.Label.Value == "" && e.Icon == nil && e.SrcArrowhead == nil && e.DstArrowhead == nil) {
		return
	}
	// Use the renderer's positions for all decorations. Arrowhead labels
	// depend on the shape and size of the arrow as well as the route.
	c := d2exporter.ToConnection(e, nil)
	include := func(p *geo.Point, w, h int) {
		if p != nil {
			left, top = min(left, p.X), min(top, p.Y)
			right, bottom = max(right, p.X+float64(w)), max(bottom, p.Y+float64(h))
		}
	}
	if c.Label != "" && e.LabelPosition != nil {
		include(c.GetLabelTopLeft(), c.LabelWidth, c.LabelHeight)
	}
	if c.Icon != nil {
		include(c.GetIconPosition(), d2target.DEFAULT_ICON_SIZE, d2target.DEFAULT_ICON_SIZE)
	}
	if c.SrcLabel != nil {
		include(c.GetArrowheadLabelPosition(false), c.SrcLabel.LabelWidth, c.SrcLabel.LabelHeight)
	}
	if c.DstLabel != nil {
		include(c.GetArrowheadLabelPosition(true), c.DstLabel.LabelWidth, c.DstLabel.LabelHeight)
	}
	return
}
