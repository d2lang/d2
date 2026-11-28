package d2lib

import (
	"image"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
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
			if len(plan.positions) == 0 || e.IsCurve || e.HasStaticIcon() || e.IconAnimation != nil || e.Language != "" || slices.Contains(old.Route, nil) || slices.Contains(e.Route, nil) {
				return nil
			}
			pair := &gridEdgePair{
				before:    gridEdgeLayout{edge: old, src: before[old.Src], dst: before[old.Dst], srcRow: -1, dstRow: -1},
				after:     gridEdgeLayout{edge: e, src: after[e.Src], dst: after[e.Dst], srcRow: -1, dstRow: -1},
				dstAnchor: e.Dst,
			}
			pair.before.straight = pair.before.hasStraightPorts()
			pair.after.straight = pair.after.hasStraightPorts()
			if !pair.before.straight || !pair.after.straight {
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
	// Start the pen as the contents settle only when their remaining paths
	// already clear every new frame. Nested borders still follow the outer one.
	gather := 0.
	if len(plan.positions) > 0 {
		gather = boardTracePickup
		for n := range panels {
			if !plan.canTraceDuringGather(n, wrapped, after) {
				gather = boardGatherEnd
				break
			}
		}
	}
	for n := range panels {
		depth := 0
		for parent := n.Parent; parent != nil; parent = parent.Parent {
			if panels[parent] {
				depth++
			}
		}
		stagger := min(.24, float64(depth)*.12)
		if len(plan.positions) > 0 {
			stagger /= 2
		}
		start := gather + stagger
		plan.start[n.AbsID()] = start
	}
	return plan
}

const (
	boardGatherEnd   = .56
	boardTracePickup = .29
)

// A straight journey between the pickup and final positions stays inside
// their bounding rectangle. Keep that rectangle clear of the new border and
// its title before allowing the pen to start. Complex captions wait until rest.
func (o *boardOutline) canTraceDuringGather(panel *d2graph.Object, wrapped map[*d2graph.Object]*d2graph.Object, shapes map[string]*d2target.Shape) bool {
	s := shapes[panel.AbsID()]
	border := image.Rect(s.Pos.X, s.Pos.Y, s.Pos.X+s.Width, s.Pos.Y+s.Height)
	pad := int(math.Ceil(s.RenderedStrokeWidth()/2)) + 1
	inside, outside := border.Inset(pad), border.Inset(-pad)
	var title image.Rectangle
	if s.Label != "" {
		switch label.FromString(s.LabelPosition) {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
			inside.Min.Y += s.LabelHeight + label.PADDING
			title = image.Rect(outside.Min.X, outside.Min.Y, outside.Max.X, inside.Min.Y)
		case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
			inside.Max.Y -= s.LabelHeight + label.PADDING
			title = image.Rect(outside.Min.X, inside.Max.Y, outside.Max.X, outside.Max.Y)
		default:
			return false
		}
	}
	travel := boardGlide(boardTracePickup / boardGatherEnd)
	pickup := func(child *d2target.Shape) image.Point {
		if from, moving := o.positions[child.ID]; moving {
			return image.Pt(gridLerpInt(from.X, child.Pos.X, travel), gridLerpInt(from.Y, child.Pos.Y, travel))
		}
		return image.Pt(child.Pos.X, child.Pos.Y)
	}
	members := make(map[string]bool, len(wrapped))
	for n := range wrapped {
		id := n.AbsID()
		members[id] = n.IsDescendantOf(panel)
		if _, moving := o.positions[id]; !moving {
			continue
		}
		child := shapes[id]
		pos := label.FromString(child.LabelPosition)
		if child.Label != "" && (pos.IsOutside() || pos.IsBorder()) {
			return false
		}
		start := pickup(child)
		final := image.Rect(child.Pos.X, child.Pos.Y, child.Pos.X+child.Width, child.Pos.Y+child.Height)
		sweep := final.Union(image.Rect(0, 0, child.Width, child.Height).Add(start)).Inset(-int(math.Ceil(child.RenderedStrokeWidth() / 2)))
		if members[id] {
			if !sweep.In(inside) {
				return false
			}
		} else if sweep.Overlaps(outside) {
			return false
		}
	}
	if o.edges != nil {
		for _, pair := range o.edges.pairs {
			e := pair.after.edge
			if e.Label != "" || e.SrcLabel != nil || e.DstLabel != nil {
				return false
			}
			sweep := gatherWireSweep(pair.after, pickup)
			if sweep.Overlaps(title) ||
				members[e.Src] && members[e.Dst] && !sweep.In(inside) ||
				!members[e.Src] && !members[e.Dst] && sweep.Overlaps(outside) {
				return false
			}
		}
	}
	return true
}

// Direct ports clip the center line. Its swept bounds contain every
// intermediate shaft even when both endpoints move independently.
func gatherWireSweep(wire gridEdgeLayout, pickup func(*d2target.Shape) image.Point) image.Rectangle {
	var sweep image.Rectangle
	for _, end := range [2]*d2target.Shape{wire.src, wire.dst} {
		start, last := pickup(end), image.Pt(end.Pos.X, end.Pos.Y)
		center := image.Rect(end.Width/2, end.Height/2, end.Width/2+1, end.Height/2+1)
		sweep = sweep.Union(center.Add(start)).Union(center.Add(last))
	}
	return sweep.Inset(-int(math.Ceil(boardArrowReach(wire.edge) + wire.edge.RenderedStrokeWidth()/2 + 1)))
}

// Both pen tips cover w+h: half the top, a side, and half the bottom.
// Let the stroke gather speed early and pass each sharp turn more slowly.
// The local adjustment has unit slope at both ends and never stops the nib.
func boardOutlineStroke(t, w, h float64) float64 {
	corner := w / (2 * (w + h))
	radius := min(.10, corner, .5-corner)
	distance := boardCorner(boardStroke(t), corner, radius)
	return boardCorner(distance, 1-corner, radius)
}

func (o *boardOutline) apply(frame *d2target.Diagram, progress float64) {
	travel := boardGlide(gridPhase(progress, 0, boardGatherEnd))
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
		phase := gridPhase(progress, start, .94)
		if phase == 1 {
			continue
		}
		traced := boardOutlineStroke(phase, float64(s.Width), float64(s.Height))
		s.OutlineProgress = &traced
		// The tint and title enter with the stroke while the child objects
		// retain their own independent visibility.
		s.Opacity *= boardGlide(gridPhase(phase, 0, .52))
	}
	if o.edges != nil && travel < 1 {
		frame.Connections = o.edges.sample(frame.Connections, frame.Shapes, travel, 1, 0, nil)
	}
}
