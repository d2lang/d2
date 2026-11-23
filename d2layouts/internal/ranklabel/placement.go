// Package ranklabel fits labels to D2's final, shape-clipped edge routes.
package ranklabel

import (
	"math"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// ClearOverlaps keeps edge labels clear of shapes, other labels and other
// orthogonal routes after D2 clips the native routes to their visible shapes.
func ClearOverlaps(g *d2graph.Graph) {
	var occupied []*geo.Box
	for _, obj := range g.Objects {
		if !obj.IsContainer() {
			occupied = append(occupied, obj.Box)
		}
		if obj.HasLabel() && obj.LabelPosition != nil && (obj.IsContainer() || label.FromString(*obj.LabelPosition).IsOutside()) {
			occupied = append(occupied, geo.NewBox(obj.GetLabelTopLeft(), float64(obj.LabelDimensions.Width), float64(obj.LabelDimensions.Height)))
		}
	}
	for _, e := range g.Edges {
		if e.Label.Value == "" || e.LabelPosition == nil || len(e.Route) < 2 {
			continue
		}
		stroke := float64(d2target.BaseConnection().StrokeWidth)
		if e.Style.StrokeWidth != nil {
			stroke, _ = strconv.ParseFloat(e.Style.StrokeWidth.Value, 64)
		}
		w, h := float64(e.LabelDimensions.Width), float64(e.LabelDimensions.Height)
		boxAt := func(pos label.Position, pct float64) *geo.Box {
			point, _ := pos.GetPointOnRoute(e.Route, stroke, pct, w, h)
			return geo.NewBox(point, w, h)
		}
		pct := 0.5
		if e.LabelPercentage != nil {
			pct = *e.LabelPercentage
		}
		pos := label.FromString(*e.LabelPosition)
		box := boxAt(pos, pct)

		if !e.IsCurve && blocked(box, occupied, g.Edges, e) {
			bestBox, bestPos, bestPct, bestDistance := box, pos, pct, math.Inf(1)
			for _, fraction := range []float64{pct, .5, .4, .6, .3, .7, .2, .8, .1, .9} {
				for _, candidate := range [...]label.Position{label.UnlockedMiddle, label.UnlockedTop, label.UnlockedBottom} {
					next := boxAt(candidate, fraction)
					distance := math.Hypot(next.TopLeft.X-box.TopLeft.X, next.TopLeft.Y-box.TopLeft.Y)
					if distance < bestDistance && !blocked(next, occupied, g.Edges, e) {
						bestBox, bestPos, bestPct, bestDistance = next, candidate, fraction, distance
					}
				}
			}
			if bestBox != box {
				box = bestBox
				position := bestPos.String()
				e.LabelPosition, e.LabelPercentage = &position, &bestPct
			}
		}
		occupied = append(occupied, box)
	}
}

func blocked(box *geo.Box, occupied []*geo.Box, edges []*d2graph.Edge, owner *d2graph.Edge) bool {
	for _, other := range occupied {
		if box.Overlaps(*other) {
			return true
		}
	}
	for _, edge := range edges {
		if edge == owner || edge.IsCurve {
			continue
		}
		if opacity := edge.Style.Opacity; opacity != nil {
			if alpha, err := strconv.ParseFloat(opacity.Value, 64); err == nil && alpha == 0 {
				continue
			}
		}
		for i := 1; i < len(edge.Route); i++ {
			a, b := edge.Route[i-1], edge.Route[i]
			if math.Max(a.X, b.X) < box.TopLeft.X || math.Min(a.X, b.X) > box.TopLeft.X+box.Width ||
				math.Max(a.Y, b.Y) < box.TopLeft.Y || math.Min(a.Y, b.Y) > box.TopLeft.Y+box.Height {
				continue
			}
			if box.Contains(a) || box.Intersects(geo.Segment{Start: a, End: b}, 0) {
				return true
			}
		}
	}
	return false
}
