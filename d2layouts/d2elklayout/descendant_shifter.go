package d2elklayout

import "github.com/d2lang/d2/d2graph"

// descendantShifter applies the same geometry updates as
// d2graph.Object.ShiftDescendants while avoiding its repeated whole-graph edge
// scans. ELK performs several sequential margin corrections per object, so the
// shifter deliberately keeps each correction separate: ShiftStart/ShiftEdge may
// simplify a route after each individual horizontal or vertical move.
//
// It reads the states of the objects reached from the graph's root through
// ChildrenArray (see walkObjects): each one's preorder range and incident
// edges; edges' seen stamps are in their states.
type descendantShifter struct {
	root      *d2graph.Object
	rootRange subtreeRange
	objects   map[*d2graph.Object]*elkObject
	edges     map[*d2graph.Edge]*elkEdgeState

	candidates []*d2graph.Edge
	generation uint64
}

type subtreeRange struct {
	start, end int
}

// newDescendantShifterWith shifts with the states of objects (every object
// reached from g.Root, with its subtree range and incident edges) and edges
// (every edge of g), and the root's range.
func newDescendantShifterWith(g *d2graph.Graph, rootRange subtreeRange,
	objects map[*d2graph.Object]*elkObject, edges map[*d2graph.Edge]*elkEdgeState) *descendantShifter {
	return &descendantShifter{root: g.Root, rootRange: rootRange, objects: objects, edges: edges}
}

// rangeOf is the preorder range of obj, if the walk reached obj (the root
// included).
func (s *descendantShifter) rangeOf(obj *d2graph.Object) (subtreeRange, bool) {
	if obj == s.root {
		return s.rootRange, true
	}
	if state := s.objects[obj]; state != nil {
		return state.subtree, true
	}
	return subtreeRange{}, false
}

func (s *descendantShifter) inSubtree(root, obj *d2graph.Object) bool {
	rootRange, rootOK := s.rangeOf(root)
	objRange, objOK := s.rangeOf(obj)
	return rootOK && objOK && rootRange.start <= objRange.start && objRange.start < rootRange.end
}

// incident is the incident edge list of obj, if the walk reached it (only
// such objects are shifted).
func (s *descendantShifter) incident(obj *d2graph.Object) []*d2graph.Edge {
	if state := s.objects[obj]; state != nil {
		return state.incident
	}
	return nil
}

func (s *descendantShifter) shift(root *d2graph.Object, dx, dy float64) {
	if dx == 0 && dy == 0 {
		return
	}

	s.generation++
	if s.generation == 0 {
		for _, state := range s.edges {
			state.seen = 0
		}
		s.generation = 1
	}
	generation := s.generation
	candidates := s.candidates[:0]
	addIncident := func(obj *d2graph.Object) {
		for _, edge := range s.incident(obj) {
			state := s.edges[edge]
			if state.seen == generation {
				continue
			}
			state.seen = generation
			candidates = append(candidates, edge)
		}
	}

	// IsDescendantOf, used by the original implementation's first pass,
	// includes the object itself. Include root's incident edges so root self
	// loops and root-to-descendant edges preserve that behavior.
	addIncident(root)
	root.IterDescendants(func(_ *d2graph.Object, descendant *d2graph.Object) {
		descendant.TopLeft.X += dx
		descendant.TopLeft.Y += dy
		addIncident(descendant)
	})
	s.candidates = candidates

	for _, edge := range candidates {
		srcInSubtree := s.inSubtree(root, edge.Src)
		dstInSubtree := s.inSubtree(root, edge.Dst)
		if srcInSubtree && dstInSubtree {
			for _, point := range edge.Route {
				point.X += dx
				point.Y += dy
			}
			continue
		}

		// The original second pass visits strict descendants only. An edge
		// from root to an outside object therefore remains unchanged.
		if srcInSubtree && edge.Src != root {
			shiftEdgeStart(edge, dx, dy)
		} else if dstInSubtree && edge.Dst != root {
			shiftEdgeEnd(edge, dx, dy)
		}
	}
}

func shiftEdgeStart(edge *d2graph.Edge, dx, dy float64) {
	if dx == 0 {
		edge.ShiftStart(dy, false)
	} else if dy == 0 {
		edge.ShiftStart(dx, true)
	} else {
		edge.Route[0].X += dx
		edge.Route[0].Y += dy
	}
}

func shiftEdgeEnd(edge *d2graph.Edge, dx, dy float64) {
	if dx == 0 {
		edge.ShiftEnd(dy, false)
	} else if dy == 0 {
		edge.ShiftEnd(dx, true)
	} else {
		edge.Route[len(edge.Route)-1].X += dx
		edge.Route[len(edge.Route)-1].Y += dy
	}
}
