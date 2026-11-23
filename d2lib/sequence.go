package d2lib

import (
	"reflect"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
)

func hasSequenceShape(g *d2graph.Graph) bool {
	if g.Root.IsSequenceDiagram() {
		return true
	}
	for _, obj := range g.Objects {
		if obj.IsSequenceDiagram() {
			return true
		}
	}
	return false
}

// Steps inherit from their predecessor and scenarios from their parent. Layers
// start independently. Save the comparison on the target so selecting a child
// later (including from serialized D2.js output) plays the same edit.
func inheritSequenceMessages(base, next *d2target.Diagram, graph *d2graph.Graph) {
	if len(next.SequenceTimelines) == 0 {
		// Erasing the last arrow still edits the same stage. Keep its
		// viewport when the preceding board showed a sequence.
		if (len(base.SequenceTimelines) > 0 || base.SequenceContinuation) && hasSequenceShape(graph) {
			next.SequenceContinuation = true
			next.Config = base.Config
		}
		return
	}
	next.SequenceContinuation = true
	if len(base.SequenceTimelines) == 0 {
		return
	}
	previous := make(map[string]*d2target.Connection)
	for _, timeline := range base.SequenceTimelines {
		for _, m := range timeline.Messages {
			previous[m.ConnectionID] = nil
		}
	}
	for i := range base.Connections {
		c := &base.Connections[i]
		if _, isMessage := previous[c.ID]; isMessage {
			previous[c.ID] = c
		}
	}
	for i := range next.Connections {
		c := &next.Connections[i]
		if old := previous[c.ID]; old != nil && !sameSequenceMessage(*old, *c) {
			delete(previous, c.ID)
		}
	}
	for _, timeline := range next.SequenceTimelines {
		for i, m := range timeline.Messages {
			timeline.Messages[i].Unchanged = previous[m.ConnectionID] != nil
		}
	}
}

func sameSequenceMessage(a, b d2target.Connection) bool {
	// New participants, rows or activations can move the route of an
	// otherwise identical message. Compare the resolved message and its styles.
	a.Route, b.Route = nil, nil
	return reflect.DeepEqual(a, b)
}
