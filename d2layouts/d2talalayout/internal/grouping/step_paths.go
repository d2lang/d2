package grouping

import (
	"cmp"
	"math"
	"math/bits"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
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

type stepPathCheck func([]*layoutgraph.Node, *limits.WorkGuard) (bool, error)

type stepNetwork struct {
	checkPath stepPathCheck
	links     map[*layoutgraph.Node][]*layoutgraph.Edge
	parallel  map[*layoutgraph.Edge]bool
	guard     *limits.WorkGuard
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

func identifySequences(active map[*layoutgraph.Node]struct{}, nodes []*layoutgraph.Node, guard *limits.WorkGuard, checkPath stepPathCheck) ([][]*layoutgraph.Node, error) {
	var steps []*layoutgraph.Node
	for _, n := range nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if _, present := active[n]; present && n.IsSequenceStep() && !n.IsInvisible && !n.IsContainer() && n.FixedTopLeft == nil &&
			(n.Sequence == nil || n.Sequence.IsActive()) {
			steps = append(steps, n)
		}
	}
	if len(steps) < 2 || !rightwardStepRow(steps[0]) {
		return nil, nil
	}
	net := stepNetwork{checkPath: checkPath, links: make(map[*layoutgraph.Node][]*layoutgraph.Edge, len(steps)), parallel: map[*layoutgraph.Edge]bool{}, guard: guard}
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

func rightwardStepRow(first *layoutgraph.Node) bool {
	dir := first.Graph.Direction(first.Container)
	return dir == geo.NONE || dir == geo.Right
}

// A fused row runs right. Its members share a height, matching wedges and a
// common upper and lower baseline for any exterior captions.
func compatibleStepGeometry(steps []*layoutgraph.Node, guard *limits.WorkGuard) (bool, error) {
	first := steps[0]
	wedge := shape.StepWedgeWidth(sequenceStepWidth(first))
	maxHeight, minAuthoredHeight := 0., math.Inf(1)
	for _, n := range steps {
		if err := guard.Step(); err != nil {
			return false, err
		}
		maxHeight = max(maxHeight, n.Height)
		if n.DesiredHeight != nil {
			minAuthoredHeight = min(minAuthoredHeight, n.Height)
		}
		if maxHeight > minAuthoredHeight || shape.StepWedgeWidth(sequenceStepWidth(n)) != wedge {
			return false, nil
		}
	}

	var decorations []layoutgraph.StepDecoration
	x := 0.
	for i, n := range steps {
		if err := guard.Step(); err != nil {
			return false, err
		}
		width := sequenceStepWidth(n)
		body := geo.Box{TopLeft: &geo.Point{X: x}, Width: width, Height: maxHeight}
		for index, decoration := range n.StepDecorations(&body) {
			fullCoverOnly := decoration.Box.TopLeft == nil
			if fullCoverOnly {
				// With multiline or heavily spaced text we cannot locate individual
				// glyphs reliably. A face covering the *entire* label still hides it.
				l := n.Label
				if index != 0 || l == nil || l.Width <= 0 || l.Height <= 0 {
					continue
				}
				decoration.Side = layoutgraph.StepDecorationSide(l.Position)
				if !decoration.Side.IsHorizontal() || strings.TrimSpace(l.Text) == "" {
					continue
				}
				decoration.Box = geo.Box{TopLeft: l.Position.GetPointOnBox(&body, label.PADDING, l.Width, l.Height), Width: l.Width, Height: l.Height}
			} else {
				decorations = append(decorations, decoration)
			}
			if decoration.Side != geo.Left && decoration.Side != geo.Right {
				continue
			}
			covered, err := stepFaceCovers(steps, i, x, maxHeight, decoration, fullCoverOnly, guard)
			if err != nil || covered {
				return false, err
			}
		}
		x += layoutgraph.SequenceAdvance(width)
	}
	// Sweep the painted rectangles from left to right. Decorations on the
	// same step cannot be separated by keeping a connector explicit.
	if len(decorations) > 1 {
		if err := guard.Add(int64(len(decorations)) * int64(bits.Len(uint(len(decorations)-1)))); err != nil {
			return false, err
		}
		slices.SortFunc(decorations, func(a, b layoutgraph.StepDecoration) int { return cmp.Compare(a.Box.TopLeft.X, b.Box.TopLeft.X) })
		for i, a := range decorations {
			for _, b := range decorations[i+1:] {
				if err := guard.Step(); err != nil {
					return false, err
				}
				if b.Box.TopLeft.X >= a.Box.TopLeft.X+a.Box.Width {
					break
				}
				if a.Owner != b.Owner && a.Box.Overlaps(b.Box) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

func stepFaceCovers(steps []*layoutgraph.Node, i int, x, height float64, decoration layoutgraph.StepDecoration, fullCoverOnly bool, guard *limits.WorkGuard) (bool, error) {
	delta := 1
	if decoration.Side == geo.Left {
		delta = -1
	}
	box := decoration.Box
	for j := i + delta; j >= 0 && j < len(steps); j += delta {
		if err := guard.Step(); err != nil {
			return false, err
		}
		other := steps[j]
		width := sequenceStepWidth(other)
		if delta > 0 {
			x += layoutgraph.SequenceAdvance(sequenceStepWidth(steps[j-1]))
			if x >= box.TopLeft.X+box.Width {
				break
			}
		} else {
			x -= layoutgraph.SequenceAdvance(width)
			if x+width <= box.TopLeft.X {
				break
			}
		}
		if laterOpaqueStep(other.StepPaint, steps[i].StepPaint) && stepCoversDecoration(x, width, height, box, fullCoverOnly) {
			return true, nil
		}
	}
	return false, nil
}

func laterOpaqueStep(face, owner *layoutgraph.StepPaint) bool {
	return face != nil && owner != nil && face.Opaque &&
		(face.ZIndex > owner.ZIndex || face.ZIndex == owner.ZIndex && face.Order > owner.Order)
}

// At each height, the step is a horizontal strip of constant width, shifted
// by the two matching chevrons. Compare only the strip swept through the
// decoration, preserving a fused row when the item fits inside the notch.
func stepCoversDecoration(x, width, height float64, box geo.Box, fullCoverOnly bool) bool {
	top, bottom := max(0, box.TopLeft.Y), min(height, box.TopLeft.Y+box.Height)
	if top >= bottom {
		return false
	}
	wedge := shape.StepWedgeWidth(width)
	offset := func(y float64) float64 { return wedge * (1 - math.Abs(2*y/height-1)) }
	minOffset := min(offset(top), offset(bottom))
	maxOffset := offset(min(bottom, max(top, height/2)))
	if fullCoverOnly {
		// The notch reaches farthest into the box nearest the middle; the
		// opposite flank recedes farthest at one of the vertical extremes.
		// Leave one pixel for export rounding on every side.
		return box.TopLeft.Y >= 1 && box.TopLeft.Y+box.Height <= height-1 &&
			box.TopLeft.X >= x+maxOffset+1 && box.TopLeft.X+box.Width <= x+width-wedge+minOffset-1
	}
	left := x + minOffset
	right := x + width - wedge + maxOffset
	return min(right, box.TopLeft.X+box.Width)-max(left, box.TopLeft.X) >= 1
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
			if ok, err := s.checkPath(path, s.guard); err != nil || !ok {
				return nil, err
			}
			return path, nil
		}
	}
	return nil, nil
}
