package d2dagrelayout

import (
	"math"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// Dagro ranks whole boxes. Where a D2 link names a column, its actual port
// lies on the left or right of that row, even when the tables are stacked.
func dockRankedTableColumns(g *d2graph.Graph, e *d2graph.Edge) {
	if e.SrcTableColumnIndex == nil && e.DstTableColumnIndex == nil {
		return
	}
	type port struct{ point, exit geo.Point }
	ports := func(obj *d2graph.Object, row *int, end, next *geo.Point) []port {
		if row == nil {
			return []port{{*end, *next}}
		}
		if obj.SQLTable == nil || *row < 0 || *row >= len(obj.SQLTable.Columns) {
			return nil
		}
		h := obj.Height / float64(len(obj.SQLTable.Columns)+1)
		y := obj.TopLeft.Y + (float64(*row)+1.5)*h
		left, right := obj.TopLeft.X, obj.TopLeft.X+obj.Width
		return []port{
			{geo.Point{X: left, Y: y}, geo.Point{X: left - 16, Y: y}},
			{geo.Point{X: right, Y: y}, geo.Point{X: right + 16, Y: y}},
		}
	}
	last := len(e.Route) - 1
	src := ports(e.Src, e.SrcTableColumnIndex, e.Route[0], e.Route[1])
	dst := ports(e.Dst, e.DstTableColumnIndex, e.Route[last], e.Route[last-1])
	var obstacles []*geo.Box
	for _, obj := range g.Objects {
		if obj.HasLabel() && obj.LabelPosition != nil && (obj.IsContainer() || label.FromString(*obj.LabelPosition).IsOutside()) {
			pos := obj.GetLabelTopLeft()
			if pos != nil {
				obstacles = append(obstacles, geo.NewBox(pos, float64(obj.LabelDimensions.Width), float64(obj.LabelDimensions.Height)))
			}
		}
		if (e.Src != obj && e.Src.IsDescendantOf(obj)) || (e.Dst != obj && e.Dst.IsDescendantOf(obj)) {
			continue
		}
		obstacles = append(obstacles, obj.Box)
	}
	var best []geo.Point
	cost := math.Inf(1)
	for _, a := range src {
		for _, b := range dst {
			// The midline gives facing rows a dogleg. An outside vertical track
			// reaches rows in stacked tables without passing through either one.
			score := func(candidate []geo.Point) {
				candidate = compactTableRoute(candidate)
				length := 0.0
				for i := 1; i < len(candidate); i++ {
					p, q := &candidate[i-1], &candidate[i]
					length += math.Hypot(q.X-p.X, q.Y-p.Y)
					for _, obs := range obstacles {
						if obs.Intersects(*geo.NewSegment(p, q), -0.01) {
							length = math.Inf(1)
							break
						}
					}
				}
				// Penalize a bend by half the port's clearance distance.
				length += 8 * float64(len(candidate)-2)
				if length < cost {
					cost, best = length, candidate
				}
			}
			xLane := func(x float64) {
				score([]geo.Point{a.point, a.exit, {X: x, Y: a.exit.Y}, {X: x, Y: b.exit.Y}, b.exit, b.point})
			}
			xLane((a.exit.X + b.exit.X) / 2)
			xLane(a.exit.X)
			xLane(b.exit.X)
			// Nonadjacent peers may have intervening shapes. Keep Dagro's
			// reserved track available in either orientation.
			for _, bend := range e.Route[1:last] {
				xLane(bend.X)
				score([]geo.Point{a.point, a.exit, {X: a.exit.X, Y: bend.Y}, {X: b.exit.X, Y: bend.Y}, b.exit, b.point})
			}
		}
	}
	if best != nil {
		e.Route = make([]*geo.Point, len(best))
		for i := range best {
			e.Route[i] = &best[i]
		}
	}
}

func compactTableRoute(points []geo.Point) []geo.Point {
	out := points[:0]
	for _, p := range points {
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		for len(out) >= 2 {
			a, b := out[len(out)-2], out[len(out)-1]
			if !(a.X == b.X && b.X == p.X || a.Y == b.Y && b.Y == p.Y) {
				break
			}
			out = out[:len(out)-1]
		}
		out = append(out, p)
	}
	return out
}
