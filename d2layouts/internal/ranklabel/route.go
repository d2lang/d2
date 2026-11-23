package ranklabel

import (
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// AvoidNodeLabels repairs a local orthogonal crossing of a container heading
// or an outside label at its final D2 position. It preserves both attachment
// points and the direction of the line entering or leaving each port.
func AvoidNodeLabels(g *d2graph.Graph) {
	var text []*geo.Box
	for _, obj := range g.Objects {
		if obj.HasLabel() && obj.LabelPosition != nil && (obj.IsContainer() || label.FromString(*obj.LabelPosition).IsOutside()) {
			text = append(text, geo.NewBox(obj.GetLabelTopLeft(), float64(obj.LabelDimensions.Width), float64(obj.LabelDimensions.Height)))
		}
	}
	if len(text) == 0 {
		return
	}
	for _, e := range g.Edges {
		if e.IsCurve || len(e.Route) < 2 || (e.Style.Opacity != nil && e.Style.Opacity.Value == "0") {
			continue
		}
		lo, hi := *e.Route[0], *e.Route[0]
		orthogonal := true
		for i := 1; i < len(e.Route); i++ {
			a, b := e.Route[i-1], e.Route[i]
			lo.X, lo.Y = min(lo.X, b.X), min(lo.Y, b.Y)
			hi.X, hi.Y = max(hi.X, b.X), max(hi.Y, b.Y)
			if a.X != b.X && a.Y != b.Y {
				orthogonal = false
				break
			}
		}
		if !orthogonal {
			continue
		}
		bounds := geo.NewBox(&lo, hi.X-lo.X, hi.Y-lo.Y)
		for _, title := range text {
			if !bounds.Overlaps(*title) {
				continue
			}
			for i := 1; i < len(e.Route); i++ {
				if entersBox(e.Route[i-1], e.Route[i], title) {
					for _, padding := range []float64{8, 16} {
						if next := avoidTitle(g, e, i, title, text, padding); next != nil {
							e.Route = next
							break
						}
					}
					break
				}
			}
		}
	}
}

func entersBox(a, b *geo.Point, box *geo.Box) bool {
	return min(a.X, b.X) < box.TopLeft.X+box.Width && max(a.X, b.X) > box.TopLeft.X &&
		min(a.Y, b.Y) < box.TopLeft.Y+box.Height && max(a.Y, b.Y) > box.TopLeft.Y
}

func avoidTitle(g *d2graph.Graph, e *d2graph.Edge, idx int, title *geo.Box, text []*geo.Box, pad float64) geo.Route {
	// Only replace the portion inside this title's clearance rectangle.
	// A rounded SVG corner needs a little space between the track and text.
	rect := geo.NewBox(geo.NewPoint(title.TopLeft.X-pad, title.TopLeft.Y-pad), title.Width+2*pad, title.Height+2*pad)
	i, j := idx-1, idx
	for i > 0 && rect.Contains(e.Route[i]) {
		i--
	}
	for j+1 < len(e.Route) && rect.Contains(e.Route[j]) {
		j++
	}
	if rect.Contains(e.Route[i]) || rect.Contains(e.Route[j]) {
		// The route may deliberately terminate at its own outside label.
		return nil
	}
	// The two boundary intersections nearest the untouched route prefix and
	// suffix delimit this crossing, including any bends inside the title.
	// Every segment is orthogonal, so clamping its outside endpoint to the
	// rectangle keeps fractional port coordinates exactly on the same line.
	boundary := func(from *geo.Point) *geo.Point {
		return geo.NewPoint(max(rect.TopLeft.X, min(from.X, rect.TopLeft.X+rect.Width)),
			max(rect.TopLeft.Y, min(from.Y, rect.TopLeft.Y+rect.Height)))
	}
	a, b := boundary(e.Route[i]), boundary(e.Route[j])
	var obstacles []*geo.Box
	for _, t := range text {
		if t.Overlaps(*rect) {
			obstacles = append(obstacles, t)
		}
	}
	for _, obj := range g.Objects {
		if obj != e.Src && e.Src.IsDescendantOf(obj) || obj != e.Dst && e.Dst.IsDescendantOf(obj) {
			continue
		}
		if obj.Box.Overlaps(*rect) {
			obstacles = append(obstacles, obj.Box)
		}
	}
	// Only tracks meeting the candidate rectangle can block its perimeter.
	var tracks []geo.Segment
	for _, other := range g.Edges {
		if other == e || other.IsCurve || (other.Style.Opacity != nil && other.Style.Opacity.Value == "0") {
			continue
		}
		for k := 1; k < len(other.Route); k++ {
			c, d := other.Route[k-1], other.Route[k]
			if max(c.X, d.X) >= rect.TopLeft.X && min(c.X, d.X) <= rect.TopLeft.X+rect.Width &&
				max(c.Y, d.Y) >= rect.TopLeft.Y && min(c.Y, d.Y) <= rect.TopLeft.Y+rect.Height {
				tracks = append(tracks, geo.Segment{Start: c, End: d})
			}
		}
	}
	clear := func(p, q *geo.Point) bool {
		for _, box := range obstacles {
			if entersBox(p, q, box) {
				return false
			}
		}
		for _, track := range tracks {
			c, d := track.Start, track.End
			if p.X == q.X && c.X == d.X && p.X == c.X && min(max(p.Y, q.Y), max(c.Y, d.Y)) > max(min(p.Y, q.Y), min(c.Y, d.Y)) ||
				p.Y == q.Y && c.Y == d.Y && p.Y == c.Y && min(max(p.X, q.X), max(c.X, d.X)) > max(min(p.X, q.X), min(c.X, d.X)) {
				return false
			}
		}
		return true
	}
	var best geo.Route
	for _, corner := range []geo.Route{
		{a, geo.NewPoint(rect.TopLeft.X, a.Y), geo.NewPoint(rect.TopLeft.X, b.Y), b},
		{a, geo.NewPoint(rect.TopLeft.X+rect.Width, a.Y), geo.NewPoint(rect.TopLeft.X+rect.Width, b.Y), b},
		{a, geo.NewPoint(a.X, rect.TopLeft.Y), geo.NewPoint(b.X, rect.TopLeft.Y), b},
		{a, geo.NewPoint(a.X, rect.TopLeft.Y+rect.Height), geo.NewPoint(b.X, rect.TopLeft.Y+rect.Height), b},
	} {
		corner = slices.CompactFunc(corner, (*geo.Point).Equals)
		valid := true
		for k := 1; k < len(corner); k++ {
			if !clear(corner[k-1], corner[k]) {
				valid = false
				break
			}
		}
		if valid && (best == nil || corner.Length() < best.Length()) {
			best = corner
		}
	}
	if best == nil {
		return nil
	}
	out := append(slices.Clone(e.Route[:i+1]), best...)
	return append(out, e.Route[j:]...)
}
