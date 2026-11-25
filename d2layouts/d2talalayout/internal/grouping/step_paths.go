package grouping

import (
	"cmp"
	"math"
	"math/bits"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// A fused row of Step shapes replaces each ordinary transition with the
// right-pointing wedge. Everything else needs to remain an explicit edge.
func implicitStepTransition(e *layoutgraph.Edge) bool {
	ordinaryHead := func(h layoutgraph.Arrowhead) bool {
		return h == "" || h == layoutgraph.NoArrowhead || h == layoutgraph.TriangleArrowhead
	}
	return !e.KeepExplicit && !e.IsLoop() && !e.IsInvisible && !e.IsCurve && !e.HasTableColumn() &&
		e.Label == nil && e.SourceArrowheadLabel == nil && e.TargetArrowheadLabel == nil &&
		e.Style == (layoutgraph.EdgeStyle{}) && ordinaryHead(e.SourceArrowhead) && ordinaryHead(e.TargetArrowhead) &&
		!(e.HasSourceArrow() && e.HasTargetArrow())
}

type stepNetwork struct {
	links    map[*layoutgraph.Node][]*layoutgraph.Edge
	parallel map[*layoutgraph.Edge]bool
	guard    *limits.WorkGuard
}

func activeSequenceNodes(g *layoutgraph.Graph, guard *limits.WorkGuard) (map[*layoutgraph.Node]struct{}, error) {
	active := make(map[*layoutgraph.Node]struct{}, len(g.Nodes))
	for _, n := range g.Nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		active[n] = struct{}{}
	}
	return active, nil
}

func identifySequences(active map[*layoutgraph.Node]struct{}, nodes []*layoutgraph.Node, guard *limits.WorkGuard) ([][]*layoutgraph.Node, error) {
	var steps []*layoutgraph.Node
	for _, n := range nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if _, present := active[n]; present && n.IsSequenceStep() && !n.IsContainer() && n.FixedTopLeft == nil &&
			(n.Sequence == nil || n.Sequence.IsActive()) {
			steps = append(steps, n)
		}
	}
	if len(steps) < 2 {
		return nil, nil
	}
	net := stepNetwork{links: make(map[*layoutgraph.Node][]*layoutgraph.Edge, len(steps)), parallel: map[*layoutgraph.Edge]bool{}, guard: guard}
	for _, n := range steps {
		net.links[n] = nil
	}
	type endpoints struct{ lo, hi layoutgraph.EntityID }
	first := map[endpoints]*layoutgraph.Edge{}
	for _, n := range steps {
		for _, e := range n.Edges {
			if err := guard.Step(); err != nil {
				return nil, err
			}
			if _, present := net.links[e.To]; !present || e.From != n {
				continue
			}
			net.links[n] = append(net.links[n], e)
			net.links[e.To] = append(net.links[e.To], e)
			pair := endpoints{min(n.ID, e.To.ID), max(n.ID, e.To.ID)}
			if prev := first[pair]; prev != nil {
				net.parallel[prev] = true
				net.parallel[e] = true
			} else {
				first[pair] = e
			}
		}
	}

	var sequences [][]*layoutgraph.Node
	seen := map[*layoutgraph.Node]bool{}
	for _, seed := range steps {
		if seen[seed] {
			continue
		}
		component, err := net.collect(seed, seen, false)
		if err != nil {
			return nil, err
		}
		runs, err := net.paths(component)
		if err != nil {
			return nil, err
		}
		sequences = append(sequences, runs...)
	}
	return sequences, nil
}

func (s stepNetwork) collect(start *layoutgraph.Node, seen map[*layoutgraph.Node]bool, split bool) ([]*layoutgraph.Node, error) {
	nodes := []*layoutgraph.Node{start}
	seen[start] = true
	for i := 0; i < len(nodes); i++ {
		node := nodes[i]
		for _, e := range s.links[node] {
			if err := s.guard.Step(); err != nil {
				return nil, err
			}
			next := node.Adjacent(e)
			if !seen[next] && !(split && s.parallel[e]) {
				seen[next] = true
				nodes = append(nodes, next)
			}
		}
	}
	return nodes, nil
}

// Multiple edges can connect otherwise independent rows. Pack those rows only
// if each forms a complete path and the multiple edges link different rows.
// The connecting edges stay explicit.
func (s stepNetwork) paths(component []*layoutgraph.Node) ([][]*layoutgraph.Node, error) {
	if len(component) < 2 {
		return nil, nil
	}
	if len(s.parallel) == 0 {
		path, err := s.orderedPath(component)
		if err != nil || path == nil {
			return nil, err
		}
		return [][]*layoutgraph.Node{path}, nil
	}
	seen := map[*layoutgraph.Node]bool{}
	part := map[*layoutgraph.Node]int{}
	var paths [][]*layoutgraph.Node
	for _, node := range component {
		if err := s.guard.Step(); err != nil {
			return nil, err
		}
		if seen[node] {
			continue
		}
		group, err := s.collect(node, seen, true)
		if err != nil {
			return nil, err
		}
		path, err := s.orderedPath(group)
		if err != nil || path == nil {
			return nil, err
		}
		for _, member := range path {
			part[member] = len(paths)
		}
		paths = append(paths, path)
	}
	for _, n := range component {
		for _, e := range s.links[n] {
			if err := s.guard.Step(); err != nil {
				return nil, err
			}
			if s.parallel[e] && part[e.From] == part[e.To] {
				return nil, nil
			}
		}
	}
	return paths, nil
}

// A fused row runs right. Its members share a height, matching wedges and a
// common upper and lower baseline for any exterior captions.
func compatibleStepGeometry(steps []*layoutgraph.Node, guard *limits.WorkGuard) (bool, error) {
	first := steps[0]
	if dir := first.Graph.Direction(first.Container); dir != geo.NONE && dir != geo.Right {
		return false, nil
	}
	wedge := shape.StepWedgeWidth(sequenceStepWidth(first))
	type span struct{ left, right float64 }
	var captions [2][]span
	x := 0.
	maxHeight, minAuthoredHeight := 0., math.Inf(1)
	for _, n := range steps {
		if err := guard.Step(); err != nil {
			return false, err
		}
		maxHeight = max(maxHeight, n.Height)
		if n.DesiredHeight != nil {
			minAuthoredHeight = min(minAuthoredHeight, n.Height)
		}
		width := sequenceStepWidth(n)
		if maxHeight > minAuthoredHeight || shape.StepWedgeWidth(width) != wedge {
			return false, nil
		}
		side, left, right := n.StepCaptionSpan(width)
		if side != geo.NONE {
			row := 0
			if side == geo.Bottom {
				row = 1
			}
			captions[row] = append(captions[row], span{x + left, x + right})
		}
		x += layoutgraph.SequenceAdvance(width)
	}
	for _, row := range captions {
		if len(row) < 2 {
			continue
		}
		if err := guard.Add(int64(len(row)) * int64(bits.Len(uint(len(row)-1)))); err != nil {
			return false, err
		}
		slices.SortFunc(row, func(a, b span) int { return cmp.Compare(a.left, b.left) })
		for i := 1; i < len(row); i++ {
			if err := guard.Step(); err != nil {
				return false, err
			}
			if row[i].left < row[i-1].right {
				return false, nil
			}
		}
	}
	return true, nil
}

func (s stepNetwork) orderedPath(group []*layoutgraph.Node) ([]*layoutgraph.Node, error) {
	var ends []*layoutgraph.Node
	for _, n := range group {
		degree := 0
		for _, e := range s.links[n] {
			if err := s.guard.Step(); err != nil {
				return nil, err
			}
			if s.parallel[e] {
				continue
			}
			if !implicitStepTransition(e) {
				return nil, nil
			}
			degree++
		}
		if degree == 1 {
			ends = append(ends, n)
		} else if degree != 2 {
			return nil, nil
		}
	}
	if len(ends) != 2 {
		return nil, nil
	}
	for _, start := range ends {
		var path []*layoutgraph.Node
		var previous *layoutgraph.Edge
		for n := start; n != nil; {
			path = append(path, n)
			var next *layoutgraph.Node
			for _, e := range s.links[n] {
				if err := s.guard.Step(); err != nil {
					return nil, err
				}
				if previous == e || s.parallel[e] {
					continue
				}
				if !e.HasArrowTo(n) {
					next = n.Adjacent(e)
					previous = e
				}
				break
			}
			n = next
		}
		if len(path) == len(group) {
			if ok, err := compatibleStepGeometry(path, s.guard); err != nil || !ok {
				return nil, err
			}
			return path, nil
		}
	}
	return nil, nil
}
