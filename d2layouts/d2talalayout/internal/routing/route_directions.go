package routing

type routeAxis struct {
	coordinate float64
	vertical   bool
}

type directedInterval struct{ from, to float64 }
type routeDirectionIndex map[routeAxis][]directedInterval

// Preserve segment order within each line. An endpoint-only contact encountered
// before another collinear segment has an explicit tie break in the router.
func (router *ovgEdgeRouter) hasOpposingSegment(route *Route, from, to *OVGNode) (bool, error) {
	index := router.routeDirections[route]
	if index == nil {
		index = make(routeDirectionIndex)
		// The segment leaving the source center participates in this check.
		// The last segment entering the destination center does not.
		for i := 0; i < len(route.OVGNodes)-2; i++ {
			if err := router.work.step(); err != nil {
				return false, err
			}
			a, b := route.OVGNodes[i], route.OVGNodes[i+1]
			if a.Y == b.Y && a.X != b.X {
				key := routeAxis{coordinate: a.Y}
				index[key] = append(index[key], directedInterval{a.X, b.X})
			} else if a.X == b.X && a.Y != b.Y {
				key := routeAxis{coordinate: a.X, vertical: true}
				index[key] = append(index[key], directedInterval{a.Y, b.Y})
			}
		}
		router.routeDirections[route] = index
	}
	key := routeAxis{coordinate: from.Y}
	a, b := from.X, to.X
	if from.Y != to.Y {
		if from.X != to.X {
			return false, nil
		}
		key = routeAxis{coordinate: from.X, vertical: true}
		a, b = from.Y, to.Y
	}
	for _, segment := range index[key] {
		if err := router.work.step(); err != nil {
			return false, err
		}
		if intersects, opposing := segment.opposes(a, b); intersects {
			return opposing, nil
		}
	}
	return false, nil
}

// A contact that leaves a route segment at one of its ends stops the scan.
// Crossings in the same direction leave later segments eligible.
func (segment directedInterval) opposes(from, to float64) (stop, opposing bool) {
	lo, hi := min(segment.from, segment.to), max(segment.from, segment.to)
	if max(from, to) < lo || hi < min(from, to) {
		return false, false
	}
	if (from == lo || from == hi) && (to < lo || to > hi) ||
		(to == lo || to == hi) && (from < lo || from > hi) {
		return true, false
	}
	opposing = segment.to > segment.from && to < from || segment.to < segment.from && to > from
	return opposing, opposing
}
