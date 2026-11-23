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

// ClearOverlaps adjusts a label only when its exported rectangle overlaps a
// shape or an earlier label. Shape tracing, integer export and D2's stroke
// padding can all change the clearance reserved by a native layered engine.
func ClearOverlaps(g *d2graph.Graph) {
	var occupied []*geo.Box
	for _, obj := range g.Objects {
		if !obj.IsContainer() {
			occupied = append(occupied, obj.Box)
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
		if !e.IsCurve && intersects(box, occupied) {
			bestBox, bestPos, bestPct, bestDistance := box, pos, pct, math.Inf(1)
			for _, fraction := range []float64{pct, .5, .4, .6, .3, .7, .2, .8, .1, .9} {
				for _, candidate := range [...]label.Position{label.UnlockedMiddle, label.UnlockedTop, label.UnlockedBottom} {
					next := boxAt(candidate, fraction)
					distance := math.Hypot(next.TopLeft.X-box.TopLeft.X, next.TopLeft.Y-box.TopLeft.Y)
					if distance < bestDistance && !intersects(next, occupied) {
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

func intersects(box *geo.Box, occupied []*geo.Box) bool {
	for _, other := range occupied {
		if box.Overlaps(*other) {
			return true
		}
	}
	return false
}
