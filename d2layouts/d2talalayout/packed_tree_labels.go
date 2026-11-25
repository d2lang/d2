package d2talalayout

import (
	"math"
	"slices"
	"strconv"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Measure connector text at its final exported position, after routing and packing.
// Both the source and target endpoint can carry an annotation.
func packedTreeCaptionBoxes(g *layoutgraph.Graph) (endpoints, other []geo.Box, ok bool) {
	if g.IsRootHierarchy || len(g.Nodes) > 16 || len(g.Edges) < 2 || len(g.Edges) > 16 {
		return nil, nil, false
	}
	for _, e := range g.Edges {
		if e.IsInvisible {
			continue
		}
		if e.IsCurve || len(e.Points) < 2 || len(e.Points) > 128 {
			return nil, nil, false
		}
		for _, l := range []*layoutgraph.Label{e.Label, e.SourceArrowheadLabel, e.TargetArrowheadLabel} {
			if l != nil && (len(l.Text) > 1024 || !plainCaptionSpacing(l.Text)) {
				return nil, nil, false
			}
		}
		if e.Label == nil && e.SourceArrowheadLabel == nil && e.TargetArrowheadLabel == nil {
			continue
		}
		// Match the polyline written by the patch and D2 exporter, including
		// decimal and float32 rounding of each coordinate.
		var route geo.Route
		for i, point := range e.Points {
			if i > 0 && point.Equals(e.Points[i-1]) {
				continue
			}
			p := point.Copy()
			p.TruncateDecimals()
			p.TruncateFloat32()
			route = append(route, p)
		}
		if len(route) < 2 {
			return nil, nil, false
		}
		for target, l := range []*layoutgraph.Label{e.SourceArrowheadLabel, e.TargetArrowheadLabel} {
			if l != nil && l.Width >= 1 && l.Height >= 1 {
				tl := e.ArrowheadLabelTopLeft(route, target == 1, l)
				endpoints = append(endpoints, *geo.NewBox(tl, math.Trunc(l.Width), math.Trunc(l.Height)))
			}
		}
		if l := e.Label; l != nil {
			conn := d2target.BaseConnection()
			if w := e.Style.StrokeWidth; w != nil {
				conn.StrokeWidth, _ = strconv.Atoi(w.Value)
			}
			pos, pct := l.Position, e.LabelPercentage
			if e.ReversedForLayout {
				pos, pct = pos.Mirrored(), 1-pct
				slices.Reverse(route)
			}
			w, h := math.Trunc(l.Width), math.Trunc(l.Height)
			p, _ := pos.GetPointOnRoute(route, conn.RenderedStrokeWidth(), float64(float32(pct)), w, h)
			other = append(other, *geo.NewBox(p, w, h))
		}
	}
	return endpoints, other, true
}
