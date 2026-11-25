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
	// The crossed grid edges: by position in edges (the OVG's Edges when the
	// index was made) for edges at that position, else in crossedOther.
	edges        []*OVGEdge
	crossedSlots []bool
	crossedOther map[*OVGEdge]struct{}
}

func newCrossingIndexState(ovg *OVG, horizontal, vertical gridLines) *ovgCrossingIndex {
	return &ovgCrossingIndex{
		horizontal:   horizontal,
		vertical:     vertical,
		edges:        ovg.Edges,
		crossedSlots: make([]bool, len(ovg.Edges)),
	}
}

func (index *ovgCrossingIndex) markCrossed(edge *OVGEdge) {
	if slot := ovgEdgeSlot(index.edges, edge); slot >= 0 {
		index.crossedSlots[slot] = true
		return
	}
	if index.crossedOther == nil {
		index.crossedOther = make(map[*OVGEdge]struct{})
	}
	index.crossedOther[edge] = struct{}{}
}

func (index *ovgCrossingIndex) isCrossed(edge *OVGEdge) bool {
	if slot := ovgEdgeSlot(index.edges, edge); slot >= 0 {
		return index.crossedSlots[slot]
	}
	_, ok := index.crossedOther[edge]
	return ok
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

// ovgRouterShared carries what the flavor routers of one OVG can share. The
// routers are built one after another from the same, unchanged OVG, so the
// grid lines of their crossing indexes are identical; they are read-only once
// built and can be read by the concurrent flavors.
type ovgRouterShared struct {
	ovg        *OVG
	edgeCount  int
	horizontal gridLines
	vertical   gridLines
	// units is what indexGridLines charges for building both line sets.
	units uint64
	// classes numbers the OVG's node points (see route_search_index.go).
	classes *ovgPointClasses
}

// routeSortUnits is the work routeSearchWorkGuard.reserveSort charges.
func routeSortUnits(length int) uint64 {
	if length < 2 {
		return 1
	}
	return uint64(length) * uint64(bits.Len(uint(length-1)))
}

// gridLinesUnits is the work indexGridLines charged for building lines.
func gridLinesUnits(lines gridLines) uint64 {
	units := routeSortUnits(len(lines))
	for _, line := range lines {
		units += routeSortUnits(len(line.intervals)) + 2*uint64(len(line.intervals))
	}
	return units
}

// newSharedOVGCrossingIndex is newOVGCrossingIndex for one of several routers
// of the same OVG. The first builds the grid lines as before; the others reuse
// them and are charged the same work in one addition, which fails exactly when
// the building steps would.
func newSharedOVGCrossingIndex(ovg *OVG, shared *ovgRouterShared, guard *routeSearchWorkGuard) (*ovgCrossingIndex, error) {
	if shared == nil {
		return newOVGCrossingIndex(ovg, guard)
	}
	if shared.ovg != ovg || shared.edgeCount != len(ovg.Edges) {
		index, err := newOVGCrossingIndex(ovg, guard)
		if err != nil {
			return nil, err
		}
		*shared = ovgRouterShared{
			ovg:        ovg,
			edgeCount:  len(ovg.Edges),
			horizontal: index.horizontal,
			vertical:   index.vertical,
			units:      gridLinesUnits(index.horizontal) + gridLinesUnits(index.vertical),
		}
		return index, nil
	}
	if err := guard.add(shared.units); err != nil {
		return nil, err
	}
	return newCrossingIndexState(ovg, shared.horizontal, shared.vertical), guard.check()
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
	return newCrossingIndexState(ovg, h, v), nil
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
			index.markCrossed(i.edge)
		}
	})
	if err != nil {
		return err
	}
	return perpendicular.visit(start, end, axis, axis, guard, func(_ float64, i gridInterval) {
		if !i.edge.sharePoints(*segment) {
			index.markCrossed(i.edge)
		}
	})
}
