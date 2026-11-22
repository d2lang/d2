package d2dagrelayout

import (
	"cmp"
	"math"
	"slices"
)

// axisOrder keeps the objects of g.Objects sorted by their coordinate along
// the shift axis (X when the cross-rank shifts are horizontal, Y otherwise),
// so that checkBelow can find the objects whose coordinate lies in its window
// by binary search instead of testing every object.
//
// It finds exactly the objects the full scan queues, because of how
// coordinates change. Within one shift an object's axis coordinate changes
// only when the object itself is shifted, by the shift's distance, and a
// shifted object is seen, which checkBelow never queues; growing a container
// changes only its size. So for every object checkBelow can queue, key equals
// the live coordinate. When a shift ends, the keys of the objects it shifted
// grow by its distance, with the same rounding as their coordinates, and keep
// their order among themselves (rounding is monotonic), so merging them back
// into the others restores the sort in linear time.
//
// That reasoning needs every object in g.Objects to own its TopLeft and to
// appear once, and no object outside g.Objects to be shifted (one could share
// a TopLeft); newCrossRankShifter scans every object otherwise.
type axisOrder struct {
	key   []float64 // by position in g.Objects
	order []int32   // positions with a non-NaN key, by increasing key
	tmp   []int32
	cands []int32
}

// regularForAxisOrder reports whether axisOrder may be used for the graph.
func regularForAxisOrder(ix *objectIndex) bool {
	if ix.distinct != len(ix.g.Objects) || len(ix.topLefts) != ix.distinct {
		return false
	}
	for k := 0; k < ix.distinct; k++ {
		if ix.objs[k].TopLeft != &ix.topLefts[k] {
			return false
		}
	}
	for k := ix.distinct; k < len(ix.objs); k++ {
		if int32(k) != ix.root && ix.objs[k] != nil {
			return false
		}
	}
	for i := range ix.edgeSrc {
		if ix.edgeSrc[i] == ix.root || ix.edgeDst[i] == ix.root {
			return false
		}
	}
	return true
}

func (s *crossRankShifter) axisCoordinate(i int) float64 {
	if s.isHorizontal {
		return s.scan[i].tl.X
	}
	return s.scan[i].tl.Y
}

func (s *crossRankShifter) buildAxisOrder() {
	n := len(s.scan)
	// The scratch's axis storage: key is written for every object here,
	// order is rebuilt from empty, tmp and cands are lists used from empty.
	a := &s.ix.sc.axis
	a.key = grow(a.key, n)
	a.order = a.order[:0]
	for i := range s.scan {
		a.key[i] = s.axisCoordinate(i)
		if !math.IsNaN(a.key[i]) {
			a.order = append(a.order, int32(i))
		}
	}
	slices.SortStableFunc(a.order, func(x, y int32) int {
		return cmp.Compare(a.key[x], a.key[y])
	})
	s.axis = a
}

// queueInWindow queues, in g.Objects order, the objects that are not seen,
// whose axis coordinate lies strictly between lo and hi, and that pass
// crossTest.
func (s *crossRankShifter) queueInWindow(lo, hi float64, crossTest func(i int) bool) {
	a := s.axis
	key, order := a.key, a.order
	j, end := 0, len(order)
	for j < end {
		h := int(uint(j+end) >> 1)
		if key[order[h]] > lo {
			end = h
		} else {
			j = h + 1
		}
	}
	cands := a.cands[:0]
	for ; j < len(order); j++ {
		i := order[j]
		if !(key[i] < hi) {
			break
		}
		if s.marks[i].seen == s.epoch {
			continue
		}
		if crossTest(int(i)) {
			cands = append(cands, i)
		}
	}
	slices.Sort(cands)
	for _, i := range cands {
		s.enqueue(i)
	}
	a.cands = cands
}

// moveShifted moves the keys of the objects the finished shift moved.
func (s *crossRankShifter) moveShifted() {
	a := s.axis
	if a == nil {
		return
	}
	moved, rebuild := false, false
	for i := range a.key {
		if s.marks[i].shifted == s.epoch {
			old := a.key[i]
			a.key[i] = old + s.distance
			moved = true
			if math.IsNaN(a.key[i]) && !math.IsNaN(old) {
				rebuild = true
			}
		}
	}
	if !moved {
		return
	}
	if rebuild {
		s.buildAxisOrder()
		return
	}
	shifted := a.tmp[:0]
	kept := 0
	for _, i := range a.order {
		if s.marks[i].shifted == s.epoch {
			shifted = append(shifted, i)
		} else {
			a.order[kept] = i
			kept++
		}
	}
	// Merge from the back: order[:kept] and shifted are both sorted.
	ia, ib := kept-1, len(shifted)-1
	for out := len(a.order) - 1; ib >= 0; out-- {
		if ia >= 0 && a.key[a.order[ia]] > a.key[shifted[ib]] {
			a.order[out] = a.order[ia]
			ia--
		} else {
			a.order[out] = shifted[ib]
			ib--
		}
	}
	a.tmp = shifted
}
