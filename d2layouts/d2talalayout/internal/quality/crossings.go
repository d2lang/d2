package quality

import (
	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

type routeCrossings struct {
	scored, interior int64
}

func countNonSharedCrossings(edges []*layoutgraph.Edge, guard *limits.WorkGuard) (int64, error) {
	counts, err := countRouteCrossings(edges, guard, nil)
	return counts.scored, err
}

func countRouteCrossings(edges []*layoutgraph.Edge, guard *limits.WorkGuard, issues *LoopInspection) (routeCrossings, error) {
	var counts routeCrossings
	for i, edge := range edges {
		if err := guard.Step(); err != nil {
			return routeCrossings{}, err
		}
		for _, other := range edges[i+1:] {
			// Charge each pair even when a route has no segments.
			if err := guard.Step(); err != nil {
				return routeCrossings{}, err
			}
			before := counts
			if err := counts.addEdgePair(edge, other, guard); err != nil {
				return routeCrossings{}, err
			}
			if issues != nil {
				issues.add(routeItem(edge), routeItem(other), counts.scored-before.scored, false)
				issues.add(inspectionItem{edge.ID, interiorRoute}, inspectionItem{other.ID, interiorRoute}, counts.interior-before.interior, false)
			}
		}
	}
	return counts, nil
}

func (counts *routeCrossings) addEdgePair(edge, other *layoutgraph.Edge, guard *limits.WorkGuard) error {
	for i := 1; i < len(edge.Points); i++ {
		if err := guard.Step(); err != nil {
			return err
		}
		for j := 1; j < len(other.Points); j++ {
			if err := guard.Step(); err != nil {
				return err
			}
			scored, interior := classifyCrossing(edge.Points[i-1], edge.Points[i], other.Points[j-1], other.Points[j])
			if scored {
				counts.scored++
			}
			if interior && !edge.IsCurve && !other.IsCurve {
				counts.interior++
			}
		}
	}
	return nil
}

func classifyCrossing(a, b, c, d *geo.Point) (scored, interior bool) {
	abc, abd := orientation(a, b, c), orientation(a, b, d)
	if equalSigns(abc, abd) {
		return false, false
	}
	cda, cdb := orientation(c, d, a), orientation(c, d, b)
	if equalSigns(cda, cdb) {
		return false, false
	}
	// Opposite nonzero orientations on both sides put the intersection strictly
	// inside both segments. A T junction or a shared route's departure cannot
	// cancel out a new interior crossing in a spacing trial.
	if abc != 0 && abd != 0 && cda != 0 && cdb != 0 {
		return true, true
	}
	// Preserve the established score at route contacts: when tracing the first
	// route, a bend off the second route's segment end does not add a crossing.
	if a.X == b.X && (a.X == c.X || a.X == d.X) || a.Y == b.Y && (a.Y == c.Y || a.Y == d.Y) {
		return false, false
	}
	return true, false
}

// orientation returns a positive value for a clockwise turn, a negative value
// for a counter-clockwise turn, and zero for collinear points.
func orientation(p, q, r *geo.Point) float64 {
	pqX := q.X - p.X
	pqY := q.Y - p.Y
	prX := r.X - p.X
	prY := r.Y - p.Y
	return pqY*prX - pqX*prY
}

func equalSigns(a, b float64) bool {
	return a > 0 && b > 0 || a == 0 && b == 0 || a < 0 && b < 0
}
