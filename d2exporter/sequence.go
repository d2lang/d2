package d2exporter

import (
	"cmp"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

// SequenceTimelines exports the sequence layout's message order after all
// nested diagrams have been put back into the graph and IDs are fully qualified.
func SequenceTimelines(g *d2graph.Graph) []d2target.SequenceTimeline {
	byDiagram := make(map[*d2graph.Object][]*d2graph.Edge)
	for _, edge := range g.Edges {
		if edge.SequenceOrder > 0 {
			seq := edge.Src.OuterSequenceDiagram()
			byDiagram[seq] = append(byDiagram[seq], edge)
		}
	}
	if len(byDiagram) == 0 {
		return nil
	}
	// Collect in authored object order, not map iteration order.
	var timelines []d2target.SequenceTimeline
	if messages := byDiagram[g.Root]; len(messages) > 0 {
		timelines = append(timelines, sequenceTimeline(g.Root, messages))
	}
	for _, obj := range g.Objects {
		if messages := byDiagram[obj]; len(messages) > 0 {
			timelines = append(timelines, sequenceTimeline(obj, messages))
		}
	}
	return timelines
}

func sequenceTimeline(sequence *d2graph.Object, messages []*d2graph.Edge) d2target.SequenceTimeline {
	slices.SortFunc(messages, func(a, b *d2graph.Edge) int {
		return cmp.Compare(a.SequenceOrder, b.SequenceOrder)
	})
	var timeline d2target.SequenceTimeline
	spanIndex := make(map[*d2graph.Object]int)
	recordEndpoint := func(endpoint *d2graph.Object, moment d2target.SequenceMoment) {
		// The direct children are actors. An endpoint nested beneath an
		// actor activates its explicit span and all enclosing spans.
		for span := endpoint; span.Parent != sequence; span = span.Parent {
			if index, ok := spanIndex[span]; ok {
				timeline.Spans[index].End = moment
			} else {
				spanIndex[span] = len(timeline.Spans)
				timeline.Spans = append(timeline.Spans, d2target.SequenceSpan{
					ShapeID: span.AbsID(), Start: moment, End: moment,
				})
			}
		}
	}
	for i, edge := range messages {
		reverse := edge.SrcArrow && !edge.DstArrow
		timeline.Messages = append(timeline.Messages, d2target.SequenceMessage{
			ConnectionID: edge.AbsID(), Reverse: reverse,
		})
		sender, receiver := edge.Src, edge.Dst
		if reverse {
			sender, receiver = receiver, sender
		}
		recordEndpoint(sender, d2target.SequenceMoment{Message: i})
		recordEndpoint(receiver, d2target.SequenceMoment{Message: i, Arrival: true})
	}
	return timeline
}
