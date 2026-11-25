package d2grid

import (
	"context"
	"math"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/util-go/go2"
)

// RouteEdges is the fallback router for placed cells and cross-graph edges.
// Parallel and reciprocal connections share a set of distinct attachment lanes.
func RouteEdges(ctx context.Context, _ *d2graph.Graph, edges []*d2graph.Edge) error {
	type pair struct{ src, dst *d2graph.Object }
	type lanes struct{ count, used int }
	groups := make(map[pair]*lanes, len(edges))
	for _, e := range edges {
		key := pair{e.Src, e.Dst}
		group := groups[key]
		if group == nil {
			group = &lanes{}
			groups[key], groups[pair{e.Dst, e.Src}] = group, group
		}
		group.count++
	}
	for _, e := range edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := groups[pair{e.Src, e.Dst}]
		routeEdge(e, group.used, group.count)
		group.used++
		if e.Label.Value != "" {
			e.LabelPosition = go2.Pointer(label.InsideMiddleCenter.String())
		}
	}
	return nil
}

func tablePortY(obj *d2graph.Object, column *int) float64 {
	rowHeight := obj.Height / float64(len(obj.SQLTable.Columns)+1)
	return obj.TopLeft.Y + (1.5+float64(*column))*rowHeight
}

func routeEdge(e *d2graph.Edge, index, count int) {
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
		gap := 24 + 16*float64(index/2)
		src, dst := e.Src.Box, e.Dst.Box
		if src.TopLeft.X+src.Width == dst.TopLeft.X || dst.TopLeft.X+dst.Width == src.TopLeft.X {
			y := min(src.TopLeft.Y, dst.TopLeft.Y) - gap
			if index%2 != 0 {
				y = max(src.TopLeft.Y+src.Height, dst.TopLeft.Y+dst.Height) + gap
			}
			route = []*geo.Point{a, geo.NewPoint(a.X, y), geo.NewPoint(b.X, y), b}
		} else {
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
		for _, p := range e.Route {
			left, top = min(left, p.X), min(top, p.Y)
			right, bottom = max(right, p.X), max(bottom, p.Y)
		}
		if e.LabelPosition == nil || e.Label.Value == "" || len(e.Route) < 2 {
			continue
		}
		width, height := float64(e.LabelDimensions.Width), float64(e.LabelDimensions.Height)
		percentage, stroke := 0., 2.
		if e.LabelPercentage != nil {
			percentage = *e.LabelPercentage
		}
		if e.Style.StrokeWidth != nil {
			stroke, _ = strconv.ParseFloat(e.Style.StrokeWidth.Value, 64)
		}
		p, _ := label.FromString(*e.LabelPosition).GetPointOnRoute(e.Route, stroke, percentage, width, height)
		if p != nil {
			left, top = min(left, p.X), min(top, p.Y)
			right, bottom = max(right, p.X+width), max(bottom, p.Y+height)
		}
	}
	gd.width, gd.height = right-left, bottom-top
	if left != 0 || top != 0 {
		gd.shift(-left, -top)
	}
}
