package routing

import (
	"math/bits"
	"slices"
)

// A router only adds routes. Mark the fixed OVG edges crossed by a new route
// segment once so every later search can query that edge in constant time.
type ovgCrossingIndex struct {
	ovg          *OVG
	xAxes, yAxes []float64
	crossed      map[*OVGEdge]struct{}
}

func newOVGCrossingIndex(ovg *OVG, guard *routeSearchWorkGuard) (*ovgCrossingIndex, error) {
	sortedAxes := func(lines map[float64][]*OVGEdge) ([]float64, error) {
		if err := guard.reserveSort(len(lines)); err != nil {
			return nil, err
		}
		axes := make([]float64, 0, len(lines))
		for axis := range lines {
			if err := guard.step(); err != nil {
				return nil, err
			}
			axes = append(axes, axis)
		}
		slices.Sort(axes)
		return axes, guard.check()
	}
	xAxes, err := sortedAxes(ovg.VerticalEdges)
	if err != nil {
		return nil, err
	}
	yAxes, err := sortedAxes(ovg.HorizontalEdges)
	if err != nil {
		return nil, err
	}
	return &ovgCrossingIndex{ovg: ovg, xAxes: xAxes, yAxes: yAxes, crossed: make(map[*OVGEdge]struct{})}, nil
}

func (index *ovgCrossingIndex) add(segment *OVGEdge, guard *routeSearchWorkGuard) error {
	vertical := segment.isVertical()
	if !vertical && !segment.isHorizontal() {
		return nil
	}
	axis, start, end := segment.From.Y, segment.From.X, segment.To.X
	parallel, perpendicular, axes := index.ovg.HorizontalEdges, index.ovg.VerticalEdges, index.xAxes
	if vertical {
		axis, start, end = segment.From.X, segment.From.Y, segment.To.Y
		parallel, perpendicular, axes = index.ovg.VerticalEdges, index.ovg.HorizontalEdges, index.yAxes
	}
	lo, hi := min(start, end), max(start, end)
	for _, edge := range parallel[axis] {
		if err := guard.step(); err != nil {
			return err
		}
		a, b := edge.From.X, edge.To.X
		if vertical {
			a, b = edge.From.Y, edge.To.Y
		}
		if lo < max(a, b) && min(a, b) < hi && !edge.sharePoints(*segment) {
			index.crossed[edge] = struct{}{}
		}
	}

	if err := guard.add(uint64(bits.Len(uint(len(axes))))); err != nil {
		return err
	}
	first, _ := slices.BinarySearch(axes, lo)
	for _, coordinate := range axes[first:] {
		if err := guard.step(); err != nil {
			return err
		}
		if coordinate > hi {
			break
		}
		for _, edge := range perpendicular[coordinate] {
			if err := guard.step(); err != nil {
				return err
			}
			a, b := edge.From.Y, edge.To.Y
			if vertical {
				a, b = edge.From.X, edge.To.X
			}
			if min(a, b) <= axis && axis <= max(a, b) && !edge.sharePoints(*segment) {
				index.crossed[edge] = struct{}{}
			}
		}
	}
	return guard.check()
}
