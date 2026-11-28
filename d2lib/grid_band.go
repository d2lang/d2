package d2lib

import (
	"sort"

	"github.com/d2lang/d2/lib/animation"
)

type gridBand uint8

const (
	gridBandNone gridBand = iota
	gridBandOpen
	gridBandClose
)

// On insertion, make room before growing the cells. Removal plays the same
// gesture backwards so the neighbors only close over a disappearing band.
func (b gridBand) window(duration int) (start, end int) {
	pop := min(gridPopMS, (duration+1)/2)
	if b == gridBandClose {
		return 0, pop
	}
	return duration - pop, duration
}

func (b gridBand) sample(elapsed, duration int) (travel, scale, opacity float64) {
	if b == gridBandClose {
		elapsed = duration - elapsed
	}
	start, end := gridBandOpen.window(duration)
	// Overlap the end of the slide with the beginning of the pop.
	settled := max(1, start+(end-start+3)/4)
	travel = animation.EaseInOut(gridPhase(float64(elapsed), 0, float64(settled)))
	scale, opacity = gridPop(gridPhase(float64(elapsed), float64(start), float64(end)))
	if b == gridBandClose {
		travel = 1 - travel
	}
	return travel, scale, opacity
}

// Coordinates refer to the laid-out D2 board. This also works for grids using
// automatic dimensions or column-major declaration order.
type gridBandSlot struct{ row, col int }

type gridBandMatrix struct {
	rows, cols int
	slots      map[string]gridBandSlot
}

func gridBandAxis(nodes []*gridNode, vertical bool) (map[string]int, int) {
	type span struct {
		id         string
		start, end int
	}
	spans := make([]span, 0, len(nodes))
	for _, n := range nodes {
		s := n.shape
		start, size := s.Pos.X, s.Width
		if vertical {
			start, size = s.Pos.Y, s.Height
		}
		if size <= 0 {
			return nil, 0
		}
		spans = append(spans, span{s.ID, start, start + size})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	index := make(map[string]int, len(nodes))
	count, end, sharedEnd := 0, 0, 0
	for _, s := range spans {
		if count == 0 || s.start >= end {
			count++
			end, sharedEnd = s.end, s.end
		} else {
			// A track needs a common cross-section through every cell.
			if s.start >= sharedEnd {
				return nil, 0
			}
			end, sharedEnd = max(end, s.end), min(sharedEnd, s.end)
		}
		index[s.id] = count - 1
	}
	return index, count
}

func gridBandLayout(nodes []*gridNode) *gridBandMatrix {
	x, cols := gridBandAxis(nodes, false)
	y, rows := gridBandAxis(nodes, true)
	if rows == 0 || cols == 0 || len(nodes)%cols != 0 || len(nodes)/cols != rows {
		return nil
	}
	slots := make(map[string]gridBandSlot, len(nodes))
	occupied := make(map[gridBandSlot]bool, len(nodes))
	for _, n := range nodes {
		id := n.shape.ID
		slot := gridBandSlot{y[id], x[id]}
		if occupied[slot] {
			return nil
		}
		occupied[slot] = true
		slots[id] = slot
	}
	return &gridBandMatrix{rows, cols, slots}
}

// Every new ID must fill the same complete stripe. Dropping it must restore
// the exact former matrix, including the IDs on either side of the gap.
func hasGridBand(compact, expanded []*gridNode, compactBoard map[string]*gridNode) bool {
	a, b := gridBandLayout(compact), gridBandLayout(expanded)
	if a == nil || b == nil {
		return false
	}
	row := b.rows == a.rows+1 && b.cols == a.cols && b.cols > 1
	column := b.cols == a.cols+1 && b.rows == a.rows && b.rows > 1
	if !row && !column {
		return false
	}
	axis := func(s gridBandSlot) int {
		if row {
			return s.row
		}
		return s.col
	}
	stripe := -1
	for id, s := range b.slots {
		if _, survives := a.slots[id]; survives {
			continue
		}
		if compactBoard[id] != nil || (stripe != -1 && stripe != axis(s)) {
			return false
		}
		stripe = axis(s)
	}
	if stripe == -1 {
		return false
	}
	for id, old := range a.slots {
		newSlot, ok := b.slots[id]
		if !ok || axis(newSlot) == stripe {
			return false
		}
		if axis(newSlot) > stripe {
			if row {
				newSlot.row--
			} else {
				newSlot.col--
			}
		}
		if old != newSlot {
			return false
		}
	}
	// A simple append with stationary neighbors can pop immediately.
	for _, n := range expanded {
		if prev := compactBoard[n.shape.ID]; prev != nil && gridBandBoundsChanged(n, prev) {
			return true
		}
	}
	return false
}

func gridBandBoundsChanged(a, b *gridNode) bool {
	return !sameSlot(a.shape, b.shape)
}

func gridBandGroups(nodes map[string]*gridNode) map[string][]*gridNode {
	groups := make(map[string][]*gridNode)
	for _, n := range nodes {
		if !n.gridCell {
			continue
		}
		parent := ""
		if n.parent != nil {
			parent = n.parent.shape.ID
		}
		groups[parent] = append(groups[parent], n)
	}
	return groups
}

func planGridBands(old, next map[string]*gridNode) map[string]gridBand {
	before, after := gridBandGroups(old), gridBandGroups(next)
	rules := make(map[string]gridBand)
	for id, a := range before {
		b := after[id]
		switch {
		case len(b) >= len(a)+2 && hasGridBand(a, b, old):
			rules[id] = gridBandOpen
		case len(a) >= len(b)+2 && hasGridBand(b, a, next):
			rules[id] = gridBandClose
		}
	}
	if len(rules) == 0 {
		return nil
	}

	plans := make(map[string]gridBand)
	// The larger board contains the band and its surviving neighbors. Only
	// descendants of an actual entering/leaving cell join its pop. Unrelated
	// membership edits nested inside a surviving cell keep their own timing.
	mark := func(nodes, other map[string]*gridNode, direction gridBand) {
		var visit func(*gridNode, gridBand, bool)
		visit = func(n *gridNode, inherited gridBand, inStripe bool) {
			id := n.shape.ID
			own, matched := rules[id]
			band := inherited
			if matched {
				band = own
			}
			prev := other[id]
			if band == direction && ((prev == nil && inStripe) || (prev != nil && gridBandBoundsChanged(n, prev))) {
				plans[id] = direction
			}
			for _, child := range n.children {
				inside := inStripe
				if matched {
					inside = other[child.shape.ID] == nil
				}
				visit(child, band, inside)
			}
		}
		for _, n := range nodes {
			if n.parent == nil {
				band := gridBandNone
				if n.gridCell {
					band = rules[""]
				}
				visit(n, band, band != gridBandNone && other[n.shape.ID] == nil)
			}
		}
	}
	mark(next, old, gridBandOpen)
	mark(old, next, gridBandClose)
	return plans
}
