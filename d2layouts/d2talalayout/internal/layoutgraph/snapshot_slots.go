package layoutgraph

// snapshotSlots is a GraphState's deduplicated node set: every recorded node
// has a dense slot, in recording order, with its geometry at capture time.
//
// A recorded node remembers its slot in Node.snapSlot. That field is only a
// hint: another snapshot may have recorded the node since, a topology rollback
// may have restored an older value, and a cloned node carries its source's
// value. A hint is therefore trusted only when the slot it names holds that
// very node (slots hold distinct nodes, so the answer is then exact);
// otherwise the lookup falls back to an index map built on first use.
type snapshotSlots struct {
	entries []snapshotEntry
	// index answers lookups whose hint is stale; nil until first needed.
	index map[*Node]int32
}

type snapshotEntry struct {
	node     *Node
	geometry graphGeometrySnapshot
}

// reset empties the set while keeping its storage.
func (slots *snapshotSlots) reset() {
	clear(slots.entries)
	slots.entries = slots.entries[:0]
	slots.index = nil
}

// size is the number of recorded nodes.
func (slots *snapshotSlots) size() int {
	return len(slots.entries)
}

// recordedDuringCapture reports whether node was recorded by the capture in
// progress. While a capture runs, every node it records gets its hint
// written, so the hint alone is exact.
func (slots *snapshotSlots) recordedDuringCapture(node *Node) bool {
	slot := node.snapSlot
	return slot >= 0 && int(slot) < len(slots.entries) && slots.entries[slot].node == node
}

// add records node with its current geometry. The caller has checked that it
// is not recorded yet.
func (slots *snapshotSlots) add(node *Node) {
	node.snapSlot = int32(len(slots.entries))
	slots.entries = append(slots.entries, snapshotEntry{
		node: node,
		geometry: graphGeometrySnapshot{
			topLeft: snapshotPointer(node.TopLeft),
			width:   node.Width,
			height:  node.Height,
		},
	})
}

// slotOf returns node's slot, or -1 when it was not recorded.
func (slots *snapshotSlots) slotOf(node *Node) int {
	if node == nil {
		return -1
	}
	slot := node.snapSlot
	if slot >= 0 && int(slot) < len(slots.entries) && slots.entries[slot].node == node {
		return int(slot)
	}
	if slots.index == nil {
		slots.index = make(map[*Node]int32, len(slots.entries))
		for i := range slots.entries {
			slots.index[slots.entries[i].node] = int32(i)
		}
	}
	if index, ok := slots.index[node]; ok {
		return int(index)
	}
	return -1
}

// lookup returns node's recorded geometry, like a map lookup.
func (slots *snapshotSlots) lookup(node *Node) (graphGeometrySnapshot, bool) {
	slot := slots.slotOf(node)
	if slot < 0 {
		return graphGeometrySnapshot{}, false
	}
	return slots.entries[slot].geometry, true
}
