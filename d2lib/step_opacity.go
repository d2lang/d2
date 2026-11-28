package d2lib

import (
	"slices"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/animation"
)

type opacityRange struct{ start, end float64 }

func (r opacityRange) changesVisibility() bool { return r.start == 0 || r.end == 0 }

func (r opacityRange) sample(t float64) float64 {
	if r.changesVisibility() {
		t = animation.EaseInOut(t)
	}
	return (1-t)*r.start + t*r.end
}

type stepOpacity struct {
	from          *d2target.Diagram
	shapes, edges map[string]opacityRange
}

// Opacity can be implicit, inherited through a class, or reset with null.
// The exported endpoints already include the compiler's class and variable
// resolution. Plan fades to or from zero here so every way of showing or
// hiding an object uses the same timing.
func planStepOpacity(prev, next *d2target.Diagram, oldGraph, newGraph *d2graph.Graph) *stepOpacity {
	first := next
	if len(next.TweenFrames) != 0 {
		first = next.TweenFrames[0]
	}
	shapes := stepOpacityRanges(prev.Shapes, next.Shapes, first.Shapes, nil, func(s *d2target.Shape) (string, float64) {
		return s.ID, s.Opacity
	})
	var edges map[string]opacityRange
	hasOpacity := func(edges []d2target.Connection) bool {
		for i := range edges {
			if edges[i].Opacity != 1 {
				return true
			}
		}
		return false
	}
	if hasOpacity(prev.Connections) || hasOpacity(next.Connections) {
		oldIDs, newIDs := opacityEdgeIDs(oldGraph), opacityEdgeIDs(newGraph)
		renamed := make(map[string]string, len(oldIDs))
		for origin, oldID := range oldIDs {
			if id := newIDs[origin]; oldID != "" && id != "" {
				renamed[oldID] = id
			}
		}
		edges = stepOpacityRanges(prev.Connections, next.Connections, first.Connections, renamed, func(e *d2target.Connection) (string, float64) {
			return e.ID, e.Opacity
		})
	}
	if len(shapes) == 0 && len(edges) == 0 {
		return nil
	}
	return &stepOpacity{from: prev, shapes: shapes, edges: edges}
}

// Match moving icons by the same declarations as opacity tweens. Keep the
// common case implicit in JSON and record only renumbered or replaced IDs.
func matchStepIconStreams(oldGraph, nextGraph *d2graph.Graph, board *d2target.Diagram) {
	if !slices.ContainsFunc(board.Connections, func(e d2target.Connection) bool { return e.IconAnimation != nil }) {
		return
	}
	oldIDs, nextIDs := opacityEdgeIDs(oldGraph), opacityEdgeIDs(nextGraph)
	previousByID := make(map[string]string, len(nextIDs))
	for origin, next := range nextIDs {
		previousByID[next] = oldIDs[origin]
	}
	occupied := make(map[string]bool, len(oldGraph.Edges))
	for _, e := range oldGraph.Edges {
		occupied[e.AbsID()] = true
	}
	for i := range board.Connections {
		e := &board.Connections[i]
		if e.IconAnimation == nil {
			continue
		}
		old := previousByID[e.ID]
		if old != e.ID && (old != "" || occupied[e.ID]) {
			e.IconAnimation.PreviousID = &old
		}
	}
}

// An edge's rendered index can change after a parallel edge is deleted.
// A declaration expanded by a wildcard can also create edges between several
// pairs of nodes. Match the declaration and its endpoints together.
type opacityEdgeOrigin struct {
	declaration *d2ast.Edge
	src, dst    string
}

func opacityEdgeIDs(g *d2graph.Graph) map[opacityEdgeOrigin]string {
	ids := make(map[opacityEdgeOrigin]string)
	for _, e := range g.Edges {
		if len(e.References) == 0 || e.References[0].Edge == nil {
			continue
		}
		key := opacityEdgeOrigin{e.References[0].Edge, e.Src.AbsID(), e.Dst.AbsID()}
		if _, duplicate := ids[key]; duplicate {
			ids[key] = ""
		} else {
			ids[key] = e.AbsID()
		}
	}
	return ids
}

func stepOpacityRanges[T any](before, after, first []T, renamed map[string]string, read func(*T) (string, float64)) map[string]opacityRange {
	readBefore := func(v *T) (string, float64) {
		id, opacity := read(v)
		if renamed != nil {
			id = renamed[id]
		}
		return id, opacity
	}
	var starts map[string]float64
	var changes map[string]opacityRange
	for i := range after {
		id, end := read(&after[i])
		var start float64
		matched := false
		if i < len(before) {
			oldID, value := readBefore(&before[i])
			start, matched = value, oldID == id
		}
		if !matched {
			// Most boards keep declaration order. Only index a board when
			// it actually inserts, removes or reorders its objects.
			if starts == nil {
				starts = make(map[string]float64, len(before))
				for j := range before {
					oldID, value := readBefore(&before[j])
					starts[oldID] = value
				}
			}
			start, matched = starts[id]
		}
		if matched && start != end {
			if changes == nil {
				changes = make(map[string]opacityRange)
			}
			changes[id] = opacityRange{start: start, end: end}
		}
	}
	if len(changes) > 0 {
		for i := range first {
			id, value := read(&first[i])
			if change, ok := changes[id]; ok && value == change.start && !change.changesVisibility() {
				// Keep the authored paint samples for changes between two
				// visible opacity values.
				delete(changes, id)
			}
		}
	}
	return changes
}

func (o *stepOpacity) sample(base *d2target.Diagram, t float64) *d2target.Diagram {
	frame := *base
	if len(o.shapes) != 0 {
		frame.Shapes = slices.Clone(base.Shapes)
		for i := range frame.Shapes {
			s := &frame.Shapes[i]
			if r, ok := o.shapes[s.ID]; ok {
				s.Opacity = r.sample(t)
			}
		}
	}
	if len(o.edges) != 0 {
		frame.Connections = slices.Clone(base.Connections)
		for i := range frame.Connections {
			e := &frame.Connections[i]
			if r, ok := o.edges[e.ID]; ok {
				e.Opacity = r.sample(t)
			}
		}
	}
	return &frame
}
