package d2animate

import "github.com/d2lang/d2/d2target"

type layerSequence struct {
	id, count, totalMS int
}

type layerSlot struct {
	*layerSequence
	startMS int
}

// A link opens a new local playback sequence. Its steps, scenarios and
// unlinked boards keep their original order and durations within that layer.
func layerSlots(nav *LayerNavigation, frames []Frame, links []layerLink) []layerSlot {
	sequences := make(map[*d2target.Diagram]*layerSequence)
	initial := &layerSequence{}
	for _, link := range links {
		sequences[link.inner] = &layerSequence{}
	}

	var assign func(*d2target.Diagram, *layerSequence)
	assign = func(board *d2target.Diagram, sequence *layerSequence) {
		if opened := sequences[board]; opened != nil {
			sequence = opened
		}
		sequences[board] = sequence
		for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
			for _, child := range children {
				assign(child, sequence)
			}
		}
	}
	assign(nav.root, initial)

	slots := make([]layerSlot, len(frames))
	sequence := initial
	for i := len(frames) - 1; i >= 0; i-- {
		if owner := sequences[frames[i].Diagram]; !frames[i].TweenSample && owner != nil {
			sequence = owner
		}
		slots[i].layerSequence = sequence
	}
	for i, frame := range frames {
		slot := &slots[i]
		if slot.count == 0 {
			slot.id = i
		}
		slot.startMS = slot.totalMS
		slot.count++
		slot.totalMS += frame.DurationMS
	}
	return slots
}
