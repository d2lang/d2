package d2lib

import (
	"slices"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// Panel IDs map to their stagger within the incoming step. The outer frame
// starts first, and a newly introduced nested frame follows shortly after.
type boardOutline struct {
	start       map[string]float64
	needsReveal bool                      // New leaves or wires also need their own entrance.
	positions   map[string]d2target.Point // Matched old leaves gathered into a frame.
	edges       *gridMotionEdges
}

func planBoardOutline(prev, next *d2target.Diagram, previousGraph, nextGraph *d2graph.Graph) *boardOutline {
	if prev == nil || next == nil || previousGraph == nil || nextGraph == nil || !next.LayoutContinuity ||
		len(previousGraph.Objects) > 256 || len(nextGraph.Objects) > 256 || len(prev.Shapes) > 256 || len(next.Shapes) > 256 || len(prev.Connections) > 256 || len(next.Connections) > 256 {
		return nil
	}
	before, after := gridEdgeShapeMap(prev.Shapes), gridEdgeShapeMap(next.Shapes)
	oldByName := make(map[string]*d2graph.Object, len(prev.Shapes))
	for _, n := range previousGraph.Objects {
		id := strings.ToLower(n.ID)
		if _, seen := oldByName[id]; seen {
			oldByName[id] = nil
		} else {
			oldByName[id] = n
		}
	}
	byName := make(map[string]int, len(next.Shapes))
	for _, n := range nextGraph.Objects {
		byName[strings.ToLower(n.ID)]++
	}

	oldForNew := make(map[string]string, len(next.Shapes))
	plan := &boardOutline{start: make(map[string]float64)}
	panels := make(map[*d2graph.Object]bool)
	wrapped := make(map[*d2graph.Object]*d2graph.Object)
	for _, n := range nextGraph.Objects {
		id := n.AbsID()
		s := after[id]
		if s == nil {
			return nil
		}
		o := before[id]
		if o == nil && n.IsContainer() {
			if !s.CanDrawOutline() {
				return nil
			}
			panels[n] = true
			continue
		}
		if o == nil {
			key := strings.ToLower(n.ID)
			peer := oldByName[key]
			// A changed scoped ID is a wrapped survivor only if the local name
			// is unique on both sides and its visible identity still matches.
			if peer != nil && !peer.IsContainer() && byName[key] == 1 && after[peer.AbsID()] == nil {
				candidate := before[peer.AbsID()]
				if candidate != nil && candidate.Label == s.Label && candidate.Type == s.Type {
					o = candidate
					wrapped[n] = peer
				}
			}
		}
		if o == nil {
			plan.needsReveal = true
			continue
		}
		if o.Width != s.Width || o.Height != s.Height || o.Type != s.Type {
			return nil
		}
		if o.Pos != s.Pos {
			// Only the contents entering the new boundary travel. Leave incidental
			// reflows to the regular board change.
			if o.ID == s.ID || !s.CanDrawOutline() || s.Icon != nil || s.Language != "" {
				return nil
			}
			if plan.positions == nil {
				plan.positions = make(map[string]d2target.Point)
			}
			plan.positions[id] = o.Pos
		}
		oldForNew[id] = o.ID
	}
	// Every old drawable object must survive. Removing or transferring an old
	// container would give a scope change the appearance of a newly drawn box.
	if len(panels) == 0 || len(oldForNew) != len(before) {
		return nil
	}

	// The new path must insert a boundary around this node in its original
	// scope. A separate service transfer is a different edit.
	for node, prior := range wrapped {
		parent := node.Parent
		if !panels[parent] {
			return nil
		}
		for panels[parent] {
			parent = parent.Parent
		}
		if parent == nil || prior.Parent == nil || parent.AbsID() != prior.Parent.AbsID() {
			return nil
		}
	}

	if len(plan.positions) > 0 {
		// The leaf starts outside the new boundary. Make sure its straight
		// journey is clear of other bodies before introducing the frame.
		for i := range next.Shapes {
			a := &next.Shapes[i]
			if oldForNew[a.ID] == "" {
				continue
			}
			for j := range next.Shapes[:i] {
				b := &next.Shapes[j]
				if oldForNew[b.ID] == "" {
					continue
				}
				_, movingA := plan.positions[a.ID]
				_, movingB := plan.positions[b.ID]
				if !movingA && !movingB {
					continue
				}
				if motionBoxesCross(before[oldForNew[a.ID]], a, before[oldForNew[b.ID]], b) {
					return nil
				}
			}
		}
	}
	// Full edge IDs include their scope. Compare endpoints through the same
	// identity mapping for a wrap, preserving the order of parallel edges.
	type wire struct {
		src, dst string
		a, b     d2target.Arrowhead
	}
	oldWires := make(map[wire][]*d2target.Connection)
	for i := range prev.Connections {
		e := &prev.Connections[i]
		k := wire{e.Src, e.Dst, e.SrcArrow, e.DstArrow}
		oldWires[k] = append(oldWires[k], e)
	}
	matched := 0
	for i := range next.Connections {
		e := &next.Connections[i]
		k := wire{oldForNew[e.Src], oldForNew[e.Dst], e.SrcArrow, e.DstArrow}
		edges := oldWires[k]
		if len(edges) == 0 {
			plan.needsReveal = true
			continue
		}
		old := edges[0]
		if old.IsCurve != e.IsCurve || old.Label != e.Label {
			return nil
		}
		if !slices.EqualFunc(old.Route, e.Route, func(a, b *geo.Point) bool { return a != nil && b != nil && *a == *b }) {
			if len(plan.positions) == 0 || e.IsCurve || e.Icon != nil || e.Language != "" || slices.Contains(old.Route, nil) || slices.Contains(e.Route, nil) {
				return nil
			}
			pair := &gridEdgePair{
				before: gridEdgeLayout{old, before[old.Src], before[old.Dst]},
				after:  gridEdgeLayout{e, after[e.Src], after[e.Dst]}, dstAnchor: e.Dst,
			}
			if !pair.before.direct() || !pair.after.direct() {
				return nil
			}
			if plan.edges == nil {
				plan.edges = &gridMotionEdges{pairs: make(map[string]*gridEdgePair)}
			}
			plan.edges.pairs[e.ID] = pair
		}
		oldWires[k] = edges[1:]
		matched++
	}
	if matched != len(prev.Connections) || len(wrapped) > 0 && plan.needsReveal {
		return nil
	}
	for n := range panels {
		depth := 0
		for parent := n.Parent; parent != nil; parent = parent.Parent {
			if panels[parent] {
				depth++
			}
		}
		start := min(.24, float64(depth)*.12)
		if len(plan.positions) > 0 {
			start = .5 + start/2
		}
		plan.start[n.AbsID()] = start
	}
	return plan
}

func (o *boardOutline) apply(frame *d2target.Diagram, progress float64) {
	travel := gridEase(gridPhase(progress, 0, .5))
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if previous, moving := o.positions[s.ID]; moving && travel < 1 {
			s.Pos.X = gridLerpInt(previous.X, s.Pos.X, travel)
			s.Pos.Y = gridLerpInt(previous.Y, s.Pos.Y, travel)
		}
		start, ok := o.start[s.ID]
		if !ok || !s.CanDrawOutline() {
			continue
		}
		phase := gridPhase(progress, start, .90)
		if phase == 1 {
			continue
		}
		traced := gridEase(phase)
		s.OutlineProgress = &traced
		// The tint and title enter with the stroke while the child objects
		// retain their own independent visibility.
		s.Opacity *= gridEase(gridPhase(phase, 0, .48))
	}
	if o.edges != nil && travel < 1 {
		frame.Connections = o.edges.sample(frame.Connections, frame.Shapes, travel, 1, 0, nil)
	}
}
