package labeling

import (
	"errors"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/label"
)

type edgeLabelLocation struct {
	position   label.Position
	percentage float64
}

func locationOf(edge *layoutgraph.Edge) edgeLabelLocation {
	return edgeLabelLocation{edge.Label.Position, edge.LabelPercentage}
}

func (loc edgeLabelLocation) apply(edge *layoutgraph.Edge) {
	edge.Label.Position = loc.position
	edge.LabelPercentage = loc.percentage
}

type edgeLabelPlacement struct {
	edgeLabelLocation
	box       *layoutgraph.Node
	score     float64
	conflicts int
	blocker   *layoutgraph.Node
}

// Only positions clear of all shapes are eligible. During repair look for a
// position blocked by a single label, then a collision-free home for that label.
type edgeLabelAlternatives struct {
	conflicts int
	choices   []edgeLabelPlacement
}

func placeMovableEdgeLabels(g *layoutgraph.Graph, edges []*layoutgraph.Edge, obstacles, shared []*layoutgraph.Node, guard *labelPlacementWorkGuard) error {
	edges, err := sortLabelPlacementEdges(edges, guard)
	if err != nil {
		return err
	}
	fixed := len(obstacles)
	hints := make([]edgeLabelLocation, len(edges))
	var collisions []int
	for i, edge := range edges {
		if err := guard.step(); err != nil {
			return err
		}
		hints[i] = locationOf(edge)
		best, err := findBestEdgeLabelPosition(edge, g, obstacles, shared, nil, guard)
		if err != nil {
			return err
		}
		obstacles = append(obstacles, best.box)
		best.apply(edge)
		if best.conflicts != 0 {
			collisions = append(collisions, i)
		}
	}
	if len(collisions) == 0 {
		return guard.check()
	}
	// Greedy placement is complete. Spend at most 1% of the normal work budget
	// resolving visible collisions, always staying inside the caller's limit.
	exhausted := errors.New("edge label repair budget exhausted")
	repair := *guard
	repair.limit = repair.used + min(guard.limit-guard.used, maxLabelPlacementWorkUnits/100)
	repair.exhaustion = exhausted
	search := edgeLabelRepair{
		graph: g, edges: edges, hints: hints,
		obstacles: obstacles, shared: shared, fixed: fixed, guard: &repair,
	}
	for _, i := range collisions {
		err := search.resolve(i)
		if err == exhausted {
			break
		}
		if err != nil {
			return err
		}
	}
	guard.used = repair.used
	return guard.check()
}

type edgeLabelRepair struct {
	graph             *layoutgraph.Graph
	edges             []*layoutgraph.Edge
	hints             []edgeLabelLocation
	obstacles, shared []*layoutgraph.Node
	fixed             int // The remaining obstacle boxes correspond to edges, in placement order.
	guard             *labelPlacementWorkGuard
}

// Enumerate with the moving labels removed and all other final obstacles in
// place. An automatically chosen percentage must not pin the repair search.
func (r *edgeLabelRepair) choices(index, conflicts int, partner *layoutgraph.Node) (edgeLabelPlacement, []edgeLabelPlacement, error) {
	edge := r.edges[index]
	defer locationOf(edge).apply(edge)
	r.hints[index].apply(edge)
	own := r.obstacles[r.fixed+index]
	obstacles := make([]*layoutgraph.Node, 0, len(r.obstacles)-1)
	for _, obstacle := range r.obstacles {
		if err := r.guard.step(); err != nil {
			return edgeLabelPlacement{}, nil, err
		}
		if obstacle != own && obstacle != partner {
			obstacles = append(obstacles, obstacle)
		}
	}
	alternatives := edgeLabelAlternatives{conflicts: conflicts}
	best, err := findBestEdgeLabelPosition(edge, r.graph, obstacles, r.shared, &alternatives, r.guard)
	return best, alternatives.choices, err
}

func (r *edgeLabelRepair) resolve(index int) error {
	best, choices, err := r.choices(index, 1, nil)
	if err != nil {
		return err
	}
	if best.conflicts == 0 {
		best.apply(r.edges[index])
		r.obstacles[r.fixed+index] = best.box
		return nil
	}
	if len(choices) == 0 {
		return nil
	}
	blocked := make(map[*layoutgraph.Node][]edgeLabelPlacement)
	for _, c := range choices {
		blocked[c.blocker] = append(blocked[c.blocker], c)
	}
	// Reconsider the most recently placed blocker first. Fixed labels never
	// enter this loop. Commit both new locations only when both are clear.
	for j := len(r.edges) - 1; j >= 0; j-- {
		if err := r.guard.step(); err != nil {
			return err
		}
		options := blocked[r.obstacles[r.fixed+j]]
		if len(options) == 0 {
			continue
		}
		_, other, err := r.choices(j, 0, r.obstacles[r.fixed+index])
		if err != nil {
			return err
		}
		a, b, err := bestClearLabelPair(options, other, r.guard)
		if err != nil {
			return err
		}
		if a.box != nil {
			a.apply(r.edges[index])
			b.apply(r.edges[j])
			r.obstacles[r.fixed+index], r.obstacles[r.fixed+j] = a.box, b.box
			return nil
		}
	}
	return nil
}

func bestClearLabelPair(choices, other []edgeLabelPlacement, guard *labelPlacementWorkGuard) (a, b edgeLabelPlacement, err error) {
	bestScore := math.Inf(1)
	for _, prev := range other {
		for _, next := range choices {
			if err := guard.step(); err != nil {
				return a, b, err
			}
			if prev.box.DoesOverlapExact(next.box) {
				continue
			}
			score := prev.score + next.score
			if prev.box.LabelBoxesOverlap(next.box, label.PADDING) {
				score++
			}
			if score < bestScore {
				bestScore, a, b = score, next, prev
			}
		}
	}
	return a, b, nil
}
