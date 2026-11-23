package d2target

import "fmt"

// SequenceTimeline identifies the messages and explicit activation spans of one
// laid-out sequence diagram. Message order is local to that diagram.
type SequenceTimeline struct {
	Messages []SequenceMessage `json:"messages"`
	Spans    []SequenceSpan    `json:"spans,omitempty"`
}

type SequenceMessage struct {
	ConnectionID string `json:"connectionID"`
	// Reverse follows the route from the last point to the first for a <- edge.
	Reverse bool `json:"reverse,omitempty"`
}

type SequenceSpan struct {
	ShapeID string         `json:"shapeID"`
	Start   SequenceMoment `json:"start"`
	End     SequenceMoment `json:"end"`
}

type SequenceMoment struct {
	Message int  `json:"message"` // zero-based index in this timeline
	Arrival bool `json:"arrival,omitempty"`
}

func validateSequenceTimelines(d *Diagram, path string) error {
	if len(d.SequenceTimelines) == 0 {
		return nil
	}
	// A message or activation belongs to exactly one sequence on its board.
	edges := make(map[string]*Connection, len(d.Connections))
	for i := range d.Connections {
		e := &d.Connections[i]
		edges[e.ID] = e
	}
	shapes := make(map[string]bool, len(d.Shapes))
	for _, s := range d.Shapes {
		shapes[s.ID] = true
	}
	for i, timeline := range d.SequenceTimelines {
		if len(timeline.Messages) == 0 {
			return fmt.Errorf("render target %s sequence %d has no messages", path, i)
		}
		for _, m := range timeline.Messages {
			edge := edges[m.ConnectionID]
			if edge == nil {
				return fmt.Errorf("render target %s sequence %d references absent or repeated connection %q", path, i, m.ConnectionID)
			}
			if edge.IsCurve {
				return fmt.Errorf("render target %s sequence %d connection %q must have a polyline route", path, i, m.ConnectionID)
			}
			delete(edges, m.ConnectionID)
		}
		for _, span := range timeline.Spans {
			if !shapes[span.ShapeID] {
				return fmt.Errorf("render target %s sequence %d references absent or repeated span %q", path, i, span.ShapeID)
			}
			delete(shapes, span.ShapeID)
			start, end := span.Start, span.End
			if start.Message < 0 || end.Message >= len(timeline.Messages) || start.Message > end.Message ||
				(start.Message == end.Message && start.Arrival && !end.Arrival) {
				return fmt.Errorf("render target %s sequence %d span %q has invalid start or end", path, i, span.ShapeID)
			}
		}
	}
	return nil
}
