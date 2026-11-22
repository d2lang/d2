package layoutgraph

import (
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestSnapshotSlotsLookupIgnoresStaleHints checks that a slot set answers
// exactly like a map keyed by the recorded nodes, whatever other sets, value
// copies, or restores have written into the nodes' slot hints.
func TestSnapshotSlotsLookupIgnoresStaleHints(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	nodes := make([]*Node, 64)
	for i := range nodes {
		nodes[i] = NewNode(EntityID(i+1), float64(10+i), float64(20+i))
		nodes[i].TopLeft = geo.NewPoint(float64(i), float64(2*i))
	}
	for trial := 0; trial < 200; trial++ {
		var sets []*snapshotSlots
		var want []map[*Node]int
		for range 3 {
			slots := &snapshotSlots{}
			slots.reset()
			reference := map[*Node]int{}
			for _, node := range nodes {
				if rng.Intn(3) == 0 {
					continue
				}
				if slots.recordedDuringCapture(node) {
					t.Fatalf("node %d reported recorded before it was added", node.ID)
				}
				reference[node] = slots.size()
				slots.add(node)
				if !slots.recordedDuringCapture(node) {
					t.Fatalf("node %d not reported recorded after it was added", node.ID)
				}
			}
			sets = append(sets, slots)
			want = append(want, reference)
		}
		// Scramble some hints the way restores and other snapshots do.
		for _, node := range nodes {
			switch rng.Intn(4) {
			case 0:
				node.snapSlot = int32(rng.Intn(80)) - 8
			case 1:
				node.snapSlot = 0
			}
		}
		copied := *nodes[rng.Intn(len(nodes))]
		for i, slots := range sets {
			for _, node := range nodes {
				slot, ok := want[i][node]
				if !ok {
					slot = -1
				}
				if got := slots.slotOf(node); got != slot {
					t.Fatalf("trial %d set %d node %d: slot %d, want %d", trial, i, node.ID, got, slot)
				}
				geometry, found := slots.lookup(node)
				if found != ok || (ok && (geometry.width != node.Width || geometry.topLeft.pointer != node.TopLeft)) {
					t.Fatalf("trial %d set %d node %d: lookup mismatch", trial, i, node.ID)
				}
			}
			if got := slots.slotOf(&copied); got != -1 {
				t.Fatalf("value copy of node %d resolved to slot %d", copied.ID, got)
			}
			if got := slots.slotOf(nil); got != -1 {
				t.Fatalf("nil resolved to slot %d", got)
			}
		}
	}
}
