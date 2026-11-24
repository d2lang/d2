package d2lib

import (
	"cmp"
	"maps"
	"math"
	"slices"
)

// Look for a clear path when several surviving cells exchange places. Sample
// footprints only in compact groups: large grids still get the linear-time
// shuffle and pop animation without an all-pairs routing cost.
const (
	gridTrackSegments  = 24
	maxRoutedGridCells = 16
)

type gridXY struct{ x, y float64 }

func (p gridXY) distance(q gridXY) float64  { return math.Hypot(p.x-q.x, p.y-q.y) }
func gridMix(a, b gridXY, t float64) gridXY { return gridXY{a.x + (b.x-a.x)*t, a.y + (b.y-a.y)*t} }

type gridShuffleTrack [gridTrackSegments + 1]gridXY

func (p *gridShuffleTrack) at(t float64) gridXY {
	index := t * gridTrackSegments
	i := min(int(index), gridTrackSegments-1)
	return gridMix(p[i], p[i+1], index-float64(i))
}

type shuffleCell struct {
	id            string
	from, to      gridXY
	width, height float64
	motion        *gridMotionItem
	track         gridShuffleTrack
}

func planGridShuffles(old, next map[string]*gridNode, items map[string]*gridMotionItem) {
	groups := make(map[string][]shuffleCell)
	for id, a := range old {
		b := next[id]
		if !a.cell || b == nil || !b.cell || a.shape.Opacity == 0 && b.shape.Opacity == 0 {
			continue
		}
		s, e := a.slot(), b.slot()
		if s.parent != e.parent {
			continue
		}
		w, h := float64(max(a.shape.Width, b.shape.Width)), float64(max(a.shape.Height, b.shape.Height))
		c := shuffleCell{id: id, from: gridXY{float64(s.x), float64(s.y)}, to: gridXY{float64(e.x), float64(e.y)}, width: w, height: h, motion: items[id]}
		// Store only a sentinel for groups above the planner limit.
		if len(groups[s.parent]) <= maxRoutedGridCells {
			groups[s.parent] = append(groups[s.parent], c)
		}
	}
	// Spend a bounded all-pairs budget per transition, in stable grid-ID
	// order. Many independent dense grids otherwise multiply the route search.
	budget := 1024
	for _, id := range slices.Sorted(maps.Keys(groups)) {
		group := groups[id]
		if len(group) < 2 || len(group) > maxRoutedGridCells || len(group)*len(group) > budget {
			continue
		}
		if slices.ContainsFunc(group, func(c shuffleCell) bool { return c.from != c.to && c.motion != nil }) {
			budget -= len(group) * len(group)
			routeGridShuffle(group)
		}
	}
}

func routeGridShuffle(cells []shuffleCell) {
	// Long relocations have fewer clear paths. Break ties by full D2 identity
	// so source declaration order and Go map iteration do not change playback.
	slices.SortFunc(cells, func(a, b shuffleCell) int {
		if c := cmp.Compare(b.from.distance(b.to), a.from.distance(a.to)); c != 0 {
			return c
		}
		return cmp.Compare(a.id, b.id)
	})
	if cells[0].from == cells[0].to {
		return
	}
	left, right, top, bottom := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for i := range cells {
		c := &cells[i]
		for _, p := range []gridXY{c.from, c.to} {
			left = min(left, p.x)
			right = max(right, p.x+c.width)
			top = min(top, p.y)
			bottom = max(bottom, p.y+c.height)
		}
		for j := range c.track {
			t := float64(j) / gridTrackSegments
			p := gridMix(c.from, c.to, t)
			if m := c.motion; m != nil {
				p.x += 4 * t * (1 - t) * m.arcX
				p.y += 4 * t * (1 - t) * m.arcY
			}
			c.track[j] = p
		}
	}
	for pass := 0; pass < 3; pass++ {
		changed := false
		for i := range cells {
			c := &cells[i]
			if c.motion == nil || c.from == c.to {
				continue
			}
			best := gridPathPenalty(cells, i, &c.track)
			if best == 0 {
				continue
			}
			// Minimize time spent occluding other cards first, distance second.
			pathCost := func(p *gridShuffleTrack) float64 {
				var distance float64
				for i := 1; i < len(p); i++ {
					distance += p[i-1].distance(p[i])
				}
				return .001 * distance / max(1, c.width+c.height)
			}
			best += pathCost(&c.track)
			try := func(p gridShuffleTrack) {
				score := gridPathPenalty(cells, i, &p) + pathCost(&p)
				if score+1e-8 < best {
					c.track = p
					best = score
					changed = true
				}
			}
			try(gridTrackThrough([]gridXY{c.from, c.to}, 0))
			// Simple arcs keep a pair exchanging adjacent slots close to home.
			d := c.from.distance(c.to)
			nx, ny := -(c.to.y-c.from.y)/d, (c.to.x-c.from.x)/d
			breadth := math.Abs(nx)*c.width + math.Abs(ny)*c.height
			for _, amp := range []float64{.62, -.62, 1.08, -1.08} {
				var p gridShuffleTrack
				for j := range p {
					t := float64(j) / gridTrackSegments
					q := gridMix(c.from, c.to, t)
					bend := math.Sin(math.Pi*t) * amp * breadth
					p[j] = gridXY{q.x + bend*nx, q.y + bend*ny}
				}
				try(p)
			}
			// Exit on any side, follow the grid boundary, and reenter from
			// any side. Rounded corners connect the straight portions.
			l, r := left-c.width-8, right+8
			t, b := top-c.height-8, bottom+8
			ports := func(p gridXY) [4]gridXY { return [4]gridXY{{p.x, t}, {r, p.y}, {p.x, b}, {l, p.y}} }
			corners := [4]gridXY{{r, t}, {r, b}, {l, b}, {l, t}}
			a, z := ports(c.from), ports(c.to)
			for from := range a {
				for to := range z {
					for _, direction := range []int{-1, 1} {
						if from == to && direction < 0 {
							continue
						}
						path := []gridXY{c.from, a[from]}
						for side := from; side != to; side = (side + direction + 4) % 4 {
							corner := side
							if direction < 0 {
								corner = (side + 3) % 4
							}
							path = append(path, corners[corner])
						}
						path = append(path, z[to], c.to)
						try(gridTrackThrough(path, min(c.width, c.height)*.48))
					}
				}
			}
		}
		if !changed {
			break
		}
	}
	for i := range cells {
		c := &cells[i]
		if c.motion == nil || c.from == c.to {
			continue
		}
		offsets := new(gridShuffleTrack)
		for j, q := range c.track {
			linear := gridMix(c.from, c.to, float64(j)/gridTrackSegments)
			offsets[j] = gridXY{q.x - linear.x, q.y - linear.y}
		}
		c.motion.track = offsets
	}
}

func gridPathPenalty(cells []shuffleCell, i int, path *gridShuffleTrack) float64 {
	c := &cells[i]
	var penalty float64
	for j := range cells {
		if i == j {
			continue
		}
		o := &cells[j]
		var worst, sum float64
		for k := 1; k < gridTrackSegments; k++ {
			p, q := path[k], o.track[k]
			w := min(p.x+c.width, q.x+o.width) - max(p.x, q.x)
			h := min(p.y+c.height, q.y+o.height) - max(p.y, q.y)
			if w > 0 && h > 0 {
				f := w * h / max(1, min(c.width*c.height, o.width*o.height))
				worst = max(worst, f)
				sum += f
			}
		}
		penalty += worst + sum/gridTrackSegments
	}
	return penalty
}

// Sample a rounded polyline by distance, giving every selected path the same
// easing clock without accelerating at a waypoint or cutting across a corner.
func gridTrackThrough(points []gridXY, radius float64) gridShuffleTrack {
	points = slices.Compact(points)
	line := []gridXY{points[0]}
	for i := 1; i+1 < len(points); i++ {
		a, b, c := points[i-1], points[i], points[i+1]
		ab, bc := a.distance(b), b.distance(c)
		r := min(radius, ab*.4, bc*.4)
		start, end := gridMix(b, a, r/ab), gridMix(b, c, r/bc)
		line = append(line, start)
		for j := 1; j <= 4; j++ {
			t := float64(j) / 4
			line = append(line, gridMix(gridMix(start, b, t), gridMix(b, end, t), t))
		}
	}
	line = append(line, points[len(points)-1])
	lengths := make([]float64, len(line))
	for i := 1; i < len(line); i++ {
		lengths[i] = lengths[i-1] + line[i-1].distance(line[i])
	}
	var track gridShuffleTrack
	track[0], track[gridTrackSegments] = points[0], points[len(points)-1]
	k := 1
	for i := 1; i < gridTrackSegments; i++ {
		d := lengths[len(line)-1] * float64(i) / gridTrackSegments
		for k+1 < len(line) && lengths[k] < d {
			k++
		}
		t := (d - lengths[k-1]) / (lengths[k] - lengths[k-1])
		track[i] = gridMix(line[k-1], line[k], t)
	}
	return track
}
