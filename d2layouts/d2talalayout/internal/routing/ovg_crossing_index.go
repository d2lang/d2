package routing

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
	"sort"
)

// Grid edges stay fixed as routes are added. Each coordinate has a sorted list
// of intervals. Prefix maxima let us skip intervals ending before the query,
// including on lines where long center-to-port edges overlap shorter grid edges.
type gridInterval struct {
	edge               *OVGEdge
	start, end, maxEnd float64
}

type gridLine struct {
	coordinate float64
	intervals  []gridInterval
}

type gridLines []gridLine

type ovgCrossingIndex struct {
	horizontal, vertical gridLines
	crossed              map[*OVGEdge]struct{}
}

func indexGridLines(lines map[float64][]*OVGEdge, vertical bool, guard *routeSearchWorkGuard) (gridLines, error) {
	if err := guard.reserveSort(len(lines)); err != nil {
		return nil, err
	}
	index := make(gridLines, 0, len(lines))
	for coordinate, edges := range lines {
		if err := guard.reserveSort(len(edges)); err != nil {
			return nil, err
		}
		intervals := make([]gridInterval, len(edges))
		for i, edge := range edges {
			if err := guard.step(); err != nil {
				return nil, err
			}
			a, b := edge.From.X, edge.To.X
			if vertical {
				a, b = edge.From.Y, edge.To.Y
			}
			intervals[i] = gridInterval{edge: edge, start: min(a, b), end: max(a, b)}
		}
		slices.SortFunc(intervals, func(a, b gridInterval) int { return cmp.Compare(a.start, b.start) })
		maxEnd := math.Inf(-1)
		for i := range intervals {
			if err := guard.step(); err != nil {
				return nil, err
			}
			maxEnd = max(maxEnd, intervals[i].end)
			intervals[i].maxEnd = maxEnd
		}
		index = append(index, gridLine{coordinate, intervals})
	}
	slices.SortFunc(index, func(a, b gridLine) int { return cmp.Compare(a.coordinate, b.coordinate) })
	return index, guard.check()
}

func newOVGCrossingIndex(ovg *OVG, guard *routeSearchWorkGuard) (*ovgCrossingIndex, error) {
	h, err := indexGridLines(ovg.HorizontalEdges, false, guard)
	if err != nil {
		return nil, err
	}
	v, err := indexGridLines(ovg.VerticalEdges, true, guard)
	if err != nil {
		return nil, err
	}
	return &ovgCrossingIndex{horizontal: h, vertical: v, crossed: make(map[*OVGEdge]struct{})}, nil
}

// visit reports all grid edges in the closed coordinate and interval ranges.
func (lines gridLines) visit(first, last, start, end float64, guard *routeSearchWorkGuard, match func(float64, gridInterval)) error {
	if err := guard.add(uint64(bits.Len(uint(len(lines))))); err != nil {
		return err
	}
	i := sort.Search(len(lines), func(i int) bool { return lines[i].coordinate >= first })
	for _, line := range lines[i:] {
		if err := guard.step(); err != nil {
			return err
		}
		if line.coordinate > last {
			break
		}
		intervals := line.intervals
		if err := guard.add(uint64(bits.Len(uint(len(intervals))))); err != nil {
			return err
		}
		j := sort.Search(len(intervals), func(j int) bool { return intervals[j].maxEnd >= start })
		for _, interval := range intervals[j:] {
			if err := guard.step(); err != nil {
				return err
			}
			if interval.start > end {
				break
			}
			if interval.end >= start {
				match(line.coordinate, interval)
			}
		}
	}
	return guard.check()
}

func (index *ovgCrossingIndex) add(segment *OVGEdge, guard *routeSearchWorkGuard) error {
	axis, a, b := segment.From.Y, segment.From.X, segment.To.X
	parallel, perpendicular := index.horizontal, index.vertical
	if segment.isVertical() {
		axis, a, b = segment.From.X, segment.From.Y, segment.To.Y
		parallel, perpendicular = perpendicular, parallel
	} else if !segment.isHorizontal() {
		return nil
	}
	start, end := min(a, b), max(a, b)
	err := parallel.visit(axis, axis, start, end, guard, func(_ float64, i gridInterval) {
		if start < i.end && i.start < end && !i.edge.sharePoints(*segment) {
			index.crossed[i.edge] = struct{}{}
		}
	})
	if err != nil {
		return err
	}
	return perpendicular.visit(start, end, axis, axis, guard, func(_ float64, i gridInterval) {
		if !i.edge.sharePoints(*segment) {
			index.crossed[i.edge] = struct{}{}
		}
	})
}
