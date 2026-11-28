package d2exporter

import (
	"cmp"
	"math"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/xsort"
)

type sequenceEvent struct {
	y          float64
	note       *d2graph.Object
	message    *d2graph.Edge
	connection *d2target.Connection
}

// The sequence layout has placed the authored events in reading order. The
// scopes in their references retain group membership even for a note whose
// graph parent is an actor or a span inside that actor.
func setSequencePlayback(g *d2graph.Graph, d *d2target.Diagram) {
	var events map[*d2graph.Object][]sequenceEvent
	enable := func(obj *d2graph.Object) {
		if !obj.IsSequenceDiagram() || obj.Style.Animated == nil {
			return
		}
		if yes, _ := strconv.ParseBool(obj.Style.Animated.Value); yes {
			if events == nil {
				events = make(map[*d2graph.Object][]sequenceEvent)
			}
			events[obj] = nil
		}
	}
	enable(g.Root)
	for _, obj := range g.Objects {
		enable(obj)
	}
	if len(events) == 0 {
		return
	}
	shapes := make(map[*d2graph.Object]*d2target.Shape)
	for i, obj := range g.Objects {
		switch obj.ZIndex {
		case d2sequence.SPAN_Z_INDEX, d2sequence.GROUP_Z_INDEX, d2sequence.NOTE_Z_INDEX:
			owner := obj.OuterSequenceDiagram()
			if _, enabled := events[owner]; !enabled {
				continue
			}
			s := &d.Shapes[i]
			shapes[obj] = s
			if obj.ZIndex == d2sequence.NOTE_Z_INDEX && s.Opacity > 0 {
				events[owner] = append(events[owner], sequenceEvent{y: obj.TopLeft.Y, note: obj})
			}
		}
	}
	for i, edge := range g.Edges {
		c := &d.Connections[i]
		if edge.ZIndex != d2sequence.MESSAGE_Z_INDEX || c.Opacity <= 0 || len(c.Route) == 0 {
			continue
		}
		owner := edge.Src.OuterSequenceDiagram()
		if _, enabled := events[owner]; enabled && owner == edge.Dst.OuterSequenceDiagram() {
			events[owner] = append(events[owner], sequenceEvent{y: edge.Route[0].Y, message: edge, connection: c})
		}
	}
	for owner, timeline := range events {
		xsort.StableFunc(timeline, func(a, b sequenceEvent) int { return cmp.Compare(a.y, b.y) })
		for i, event := range timeline {
			step := i + 1
			// Frames and activations can nest. Extend each ancestor enough
			// to enclose this event and its child frame's padding.
			grow := func(obj *d2graph.Object, role int, bottom float64, padding float64, arrival bool) {
				for ; obj != nil && obj != owner; obj = obj.Parent {
					s := shapes[obj]
					if s == nil || obj.ZIndex != role {
						continue
					}
					bottom += padding
					extendSequenceShape(s, step, bottom, arrival)
				}
			}
			if event.note != nil {
				n := event.note
				shapes[n].SequenceStep = step
				bottom := n.TopLeft.Y + n.Height
				grow(n.Parent, d2sequence.SPAN_Z_INDEX, bottom, d2sequence.SPAN_MESSAGE_PAD, false)
				// placeGroup pads a note by half a message row.
				bottom += d2sequence.MIN_MESSAGE_DISTANCE/2 - d2sequence.GROUP_CONTAINER_PADDING
				for _, ref := range n.References {
					if !ref.Key.HasGlob() {
						grow(ref.ScopeObj, d2sequence.GROUP_Z_INDEX, bottom, d2sequence.GROUP_CONTAINER_PADDING, false)
					}
				}
				continue
			}
			c, e := event.connection, event.message
			c.SequenceStep = step
			srcY, dstY := c.Route[0].Y, c.Route[len(c.Route)-1].Y
			grow(e.Src, d2sequence.SPAN_Z_INDEX, srcY, d2sequence.SPAN_MESSAGE_PAD, e.SrcArrow && !e.DstArrow)
			grow(e.Dst, d2sequence.SPAN_Z_INDEX, dstY, d2sequence.SPAN_MESSAGE_PAD, e.DstArrow && !e.SrcArrow)
			bottom := max(srcY, dstY) + max(float64(c.LabelHeight)/2, d2sequence.MIN_MESSAGE_DISTANCE/2-d2sequence.GROUP_CONTAINER_PADDING)
			for _, ref := range e.References {
				grow(ref.ScopeObj, d2sequence.GROUP_Z_INDEX, bottom, d2sequence.GROUP_CONTAINER_PADDING, false)
			}
		}
	}
	for obj, s := range shapes {
		// A frame or activation attached exclusively to hidden messages
		// still belongs in the completed drawing. Give it no separate beat.
		if s.SequenceStep == 0 && obj.ZIndex != d2sequence.NOTE_Z_INDEX {
			if last := len(events[obj.OuterSequenceDiagram()]); last > 0 {
				s.SequenceStep, s.SequenceAtArrival = last, true
			}
		}
		// Finish at the authored bounds, including the original layout's padding.
		switch len(s.SequenceGrowth) {
		case 0:
		case 1:
			s.SequenceGrowth = nil
		default:
			s.SequenceGrowth[len(s.SequenceGrowth)-1].Height = s.Height
		}
	}
}

func extendSequenceShape(s *d2target.Shape, step int, bottom float64, arrival bool) {
	height := min(s.Height, max(1, int(math.Ceil(bottom))-s.Pos.Y))
	if s.SequenceStep == 0 {
		s.SequenceStep, s.SequenceAtArrival = step, arrival
	} else if step == s.SequenceStep && !arrival {
		// A shared ancestor of sender and receiver starts with the sender.
		s.SequenceAtArrival = false
	}
	last := len(s.SequenceGrowth) - 1
	if last >= 0 {
		if s.SequenceGrowth[last].Height >= height {
			return
		}
		if s.SequenceGrowth[last].Step == step {
			s.SequenceGrowth[last].Height = height
			return
		}
	}
	s.SequenceGrowth = append(s.SequenceGrowth, d2target.SequenceExtent{Step: step, Height: height})
}
