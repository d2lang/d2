package d2lib

import (
	"math"
	"reflect"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

// The sequence layout already knows where the actors, activation bars and
// messages belong. Trace newly sent messages if the prior exchange still fits
// at its authored positions. Appending a message can extend a lifeline or bar.
type sequenceTrace struct {
	actors      map[string]int                  // original x for a horizontally displaced actor
	spans       map[string]int                  // original height for a growing activation
	lines       map[string]*d2target.Connection // previous vertical lifelines
	messages    map[string]*sequenceMessage
	prepare     float64
	shapeWidths traceShapeWidths // stroke widths of stationary sequence actors
}

type sequenceMessage struct {
	target  *d2target.Connection
	wire    *boardTrace
	caption d2target.Point
}

func planSequenceTrace(prev, next *d2target.Diagram, prevGraph, nextGraph *d2graph.Graph) *sequenceTrace {
	if prevGraph == nil || nextGraph == nil || prevGraph.Root.IsSequenceDiagram() != nextGraph.Root.IsSequenceDiagram() || !slices.ContainsFunc(nextGraph.Edges, func(e *d2graph.Edge) bool {
		return e.ZIndex == d2sequence.MESSAGE_Z_INDEX && e.Src.OuterSequenceDiagram() != nil
	}) {
		return nil
	}
	// An unrelated style or structural change should use its own transition.
	if !sameBoardCanvas(prev, next) {
		return nil
	}
	m := &sequenceTrace{
		actors:   make(map[string]int),
		spans:    make(map[string]int),
		lines:    make(map[string]*d2target.Connection),
		messages: make(map[string]*sequenceMessage),
	}
	objects := make(map[string]*d2graph.Object)
	for _, o := range nextGraph.Objects {
		objects[o.AbsID()] = o
	}
	beforeShapes := gridEdgeShapeMap(prev.Shapes)
	for i := range next.Shapes {
		s := &next.Shapes[i]
		old := beforeShapes[s.ID]
		delete(beforeShapes, s.ID)
		if old != nil && reflect.DeepEqual(old, s) {
			continue
		}
		o := objects[s.ID]
		if o == nil || o.OuterSequenceDiagram() == nil {
			return nil
		}
		// A bar can be introduced by the first message to that activation.
		if s.ZIndex == d2sequence.SPAN_Z_INDEX && s.Type == d2target.ShapeRectangle && s.Label == "" {
			if old == nil {
				m.spans[s.ID] = 0
				continue
			}
			a := *old
			a.Height = s.Height
			if s.Height >= old.Height && reflect.DeepEqual(a, *s) {
				m.spans[s.ID] = old.Height
				continue
			}
		}
		// A long first request can widen the gap before a previously idle
		// participant. Its lifeline travels with it before the message leaves.
		if old != nil && o.Parent.IsSequenceDiagram() {
			a := *old
			a.Pos.X = s.Pos.X
			if reflect.DeepEqual(a, *s) {
				m.actors[s.ID] = old.Pos.X
				continue
			}
		}
		return nil
	}
	if len(beforeShapes) > 0 {
		return nil
	}
	oldEdges := make(map[string]*d2target.Connection, len(prev.Connections))
	for i := range prev.Connections {
		e := &prev.Connections[i]
		oldEdges[e.ID] = e
	}
	graphEdges := make(map[string]*d2graph.Edge, len(nextGraph.Edges))
	for _, e := range nextGraph.Edges {
		graphEdges[e.AbsID()] = e
	}
	var ordered []*sequenceMessage
	for i := range next.Connections {
		e := &next.Connections[i]
		old := oldEdges[e.ID]
		delete(oldEdges, e.ID)
		if old != nil && reflect.DeepEqual(old, e) {
			continue
		}
		g := graphEdges[e.ID]
		if g == nil || g.Src.OuterSequenceDiagram() == nil {
			return nil
		}
		if d2sequence.IsLifelineEnd(g.Dst) {
			if old == nil || len(old.Route) != 2 || len(e.Route) != 2 {
				return nil
			}
			a := *old
			a.Route = e.Route
			if !reflect.DeepEqual(a, *e) || old.Route[0].X != old.Route[1].X || e.Route[0].X != e.Route[1].X ||
				old.Route[0].Y != e.Route[0].Y || old.Route[1].Y > e.Route[1].Y {
				return nil
			}
			m.lines[e.ID] = old
			continue
		}
		if g.ZIndex != d2sequence.MESSAGE_Z_INDEX || g.Dst.OuterSequenceDiagram() != g.Src.OuterSequenceDiagram() || e.Opacity <= 0 {
			return nil
		}
		if old != nil {
			a := *old
			a.Opacity = e.Opacity
			if old.Opacity != 0 || !reflect.DeepEqual(a, *e) {
				return nil
			}
		}
		msg := newSequenceMessage(e)
		if msg == nil {
			return nil
		}
		m.messages[e.ID] = msg
		ordered = append(ordered, msg)
	}
	if len(oldEdges) > 0 || len(ordered) == 0 {
		return nil
	}
	if len(m.spans) > 0 || len(m.lines) > 0 || len(m.actors) > 0 {
		m.prepare = .42
	}
	// A batch of messages follows the visual reading order of the sequence.
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].target.Route[0].Y < ordered[j].target.Route[0].Y })
	for i, msg := range ordered {
		msg.wire.measure(msg.target, next.Shapes, &m.shapeWidths)
		msg.wire.start = m.prepare + (1-m.prepare)*float64(i)/float64(len(ordered))
		msg.wire.end = m.prepare + (1-m.prepare)*float64(i+1)/float64(len(ordered))
	}
	return m
}

func newSequenceMessage(e *d2target.Connection) *sequenceMessage {
	// End labels and icons are positioned on the moving path. Keep their
	// authored appearance when there is no unambiguous moving arrowhead.
	if (e.SrcArrow == d2target.NoArrowhead) == (e.DstArrow == d2target.NoArrowhead) ||
		e.SrcLabel != nil || e.DstLabel != nil || e.HasStaticIcon() || e.IconAnimation != nil || e.Tooltip != "" || e.Link != "" ||
		e.IsCurve || len(e.Route) < 2 || slices.Contains(e.Route, nil) {
		return nil
	}
	wire := newBoardTrace(e, e.SrcArrow != d2target.NoArrowhead, 0, 1)
	if wire == nil {
		return nil
	}
	m := &sequenceMessage{target: e, wire: wire}
	if e.Label != "" {
		at := e.GetLabelTopLeft()
		if at == nil {
			return nil
		}
		m.caption = d2target.Point{X: int(math.Round(at.X)), Y: int(math.Round(at.Y))}
	}
	return m
}

func (m *sequenceTrace) apply(frame *d2target.Diagram, t float64) {
	growth := 1.
	if m.prepare > 0 {
		growth = boardStroke(gridPhase(t, 0, m.prepare))
	}
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if x, ok := m.actors[s.ID]; ok {
			s.Pos.X = gridLerpInt(x, s.Pos.X, growth)
		}
		if h, ok := m.spans[s.ID]; ok {
			s.Height = gridLerpInt(h, s.Height, growth)
			if s.Height == 0 {
				s.Opacity = 0
			}
		}
	}
	result := make([]d2target.Connection, 0, len(frame.Connections))
	for _, e := range frame.Connections {
		if line := m.lines[e.ID]; line != nil && growth < 1 {
			e.Route = []*geo.Point{line.Route[0].Interpolate(e.Route[0], growth), line.Route[1].Interpolate(e.Route[1], growth)}
		}
		if msg := m.messages[e.ID]; msg != nil {
			w := msg.wire
			u := gridPhase(t, w.start, w.end)
			// A predeclared message may also carry a generic opacity tween.
			// Its shaft paints at the authored opacity as it traverses the route.
			e = *msg.target
			if u < 1 {
				fraction := w.travel(u)
				if fraction*w.lengths[len(w.lengths)-1] < w.minDraw {
					continue
				}
				if !w.applyPrefix(&e, fraction) {
					continue
				}
				w.pinDashes(&e, frame.Shapes, &m.shapeWidths)
				if e.Label != "" {
					e.MotionLabel = &d2target.MotionLabel{Point: msg.caption, Opacity: gridPhase(u, .45, .92)}
				}
			}
		}
		result = append(result, e)
	}
	frame.Connections = result
}
