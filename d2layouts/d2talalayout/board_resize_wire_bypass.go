package d2talalayout

import (
	"cmp"
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// Replace the section inside an obstacle by the shorter walk around its
// perimeter. All unobstructed runs and the directions at both ports survive.
func bypassResizeBox(route []*geo.Point, obstacle geo.Box) []*geo.Point {
	const margin = 6.
	box := geo.NewBox(geo.NewPoint(obstacle.TopLeft.X-margin, obstacle.TopLeft.Y-margin), obstacle.Width+2*margin, obstacle.Height+2*margin)
	if box.Contains(route[0]) || box.Contains(route[len(route)-1]) {
		return nil
	}
	left, top := box.TopLeft.X, box.TopLeft.Y
	right, bottom := left+box.Width, top+box.Height
	corners := [4]*geo.Point{box.TopLeft, geo.NewPoint(right, top), geo.NewPoint(right, bottom), geo.NewPoint(left, bottom)}
	perimeter := 2 * (box.Width + box.Height)
	offset := func(p *geo.Point) float64 {
		switch {
		case p.Y == top:
			return p.X - left
		case p.X == right:
			return box.Width + p.Y - top
		case p.Y == bottom:
			return box.Width + box.Height + right - p.X
		default:
			return perimeter - (p.Y - top)
		}
	}
	type crossing struct {
		point *geo.Point
		leg   int
	}
	var hits []crossing
	for i := 1; i < len(route); i++ {
		a, b := route[i-1], route[i]
		var near, far *geo.Point
		if a.X == b.X && a.X > left && a.X < right {
			near, far = geo.NewPoint(a.X, top), geo.NewPoint(a.X, bottom)
			if a.Y > b.Y {
				near, far = far, near
			}
		} else if a.Y == b.Y && a.Y > top && a.Y < bottom {
			near, far = geo.NewPoint(left, a.Y), geo.NewPoint(right, a.Y)
			if a.X > b.X {
				near, far = far, near
			}
		} else {
			continue
		}
		for _, p := range []*geo.Point{near, far} {
			if p.OnOrthogonalSegment(a, b) && (len(hits) == 0 || !p.Equals(hits[len(hits)-1].point)) {
				hits = append(hits, crossing{p, i})
			}
		}
	}
	if len(hits) != 2 {
		return nil
	}
	enter, exit := hits[0], hits[1]
	u, v := offset(enter.point), offset(exit.point)
	clockwise := math.Mod(v-u+perimeter, perimeter)
	if clockwise == 0 {
		return nil
	}
	var around []*geo.Point
	for _, p := range corners {
		d := math.Mod(offset(p)-u+perimeter, perimeter)
		if clockwise <= perimeter/2 && d > 0 && d < clockwise || clockwise > perimeter/2 && d > clockwise {
			around = append(around, p)
		}
	}
	xsort.Func(around, func(a, b *geo.Point) int {
		order := cmp.Compare(math.Mod(offset(a)-u+perimeter, perimeter), math.Mod(offset(b)-u+perimeter, perimeter))
		if clockwise > perimeter/2 {
			return -order
		}
		return order
	})
	out := append([]*geo.Point{}, route[:enter.leg]...)
	out = append(out, enter.point)
	out = append(out, around...)
	out = append(out, exit.point)
	out = append(out, route[exit.leg:]...)
	// Coalesce corners lying on an unchanged continuation.
	compact := out[:0]
	for _, p := range out {
		if len(compact) > 0 && p.Equals(compact[len(compact)-1]) {
			continue
		}
		if n := len(compact); n >= 2 && (compact[n-2].X == p.X || compact[n-2].Y == p.Y) && compact[n-1].OnOrthogonalSegment(compact[n-2], p) {
			compact = compact[:n-1]
		}
		compact = append(compact, p)
	}
	return compact
}
