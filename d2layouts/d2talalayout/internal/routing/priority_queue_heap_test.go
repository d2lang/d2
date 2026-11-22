package routing

import (
	"math"
	"math/rand"
	"testing"
)

// referenceHeap is the swap-based binary heap the queue replaced (entries
// referenced by pointer, keys read through them), kept as the reference for
// heap positions.
type referenceHeap struct {
	items []*referenceEntry
}

type referenceEntry struct {
	priority float64
	order    uint64
	index    int
}

func (h *referenceHeap) less(i, j int) bool {
	a, b := h.items[i], h.items[j]
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	return a.order < b.order
}

func (h *referenceHeap) swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.items[i].index = i
	h.items[j].index = j
}

func (h *referenceHeap) siftUp(index int) {
	for index > 0 {
		parent := (index - 1) / 2
		if !h.less(index, parent) {
			return
		}
		h.swap(index, parent)
		index = parent
	}
}

func (h *referenceHeap) siftDown(index int) {
	for {
		left := index*2 + 1
		if left >= len(h.items) {
			return
		}
		smallest := left
		right := left + 1
		if right < len(h.items) && h.less(right, left) {
			smallest = right
		}
		if !h.less(smallest, index) {
			return
		}
		h.swap(index, smallest)
		index = smallest
	}
}

func (h *referenceHeap) push(priority float64, order uint64) *referenceEntry {
	entry := &referenceEntry{priority: priority, order: order, index: len(h.items)}
	h.items = append(h.items, entry)
	h.siftUp(entry.index)
	return entry
}

func (h *referenceHeap) pop() *referenceEntry {
	min := h.items[0]
	last := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	min.index = -1
	if len(h.items) > 0 {
		h.items[0] = last
		last.index = 0
		h.siftDown(0)
	}
	return min
}

// The queue must keep exactly the reference heap's layout after every
// operation (decrease charges by heap position), with ties, NaN priorities
// (not a strict weak order) and infinities.
func TestPriorityQueueMatchesSwapHeap(t *testing.T) {
	random := rand.New(rand.NewSource(29))
	priority := func() float64 {
		switch random.Intn(20) {
		case 0:
			return math.NaN()
		case 1:
			return math.Inf(1)
		case 2:
			return math.Inf(-1)
		default:
			return float64(random.Intn(30))
		}
	}
	for trial := range 300 {
		var queue priorityQueue
		var ref referenceHeap
		var entries []*priorityQueueEntry
		var refs []*referenceEntry
		for op := range 400 {
			switch r := random.Intn(10); {
			case r < 5 || queue.empty():
				p := priority()
				entry, err := queue.push(p, nil, false, nil)
				if err != nil {
					t.Fatal(err)
				}
				entries = append(entries, entry)
				refs = append(refs, ref.push(p, uint64(len(refs))))
			case r < 7:
				k := random.Intn(len(entries))
				if entries[k].index < 0 {
					continue
				}
				p := entries[k].priority - float64(1+random.Intn(5))
				if math.IsNaN(entries[k].priority) || !(p < entries[k].priority) {
					continue
				}
				if err := queue.decrease(entries[k], p, nil); err != nil {
					t.Fatal(err)
				}
				refs[k].priority = p
				ref.siftUp(refs[k].index)
			default:
				got, err := queue.pop(nil)
				if err != nil {
					t.Fatal(err)
				}
				want := ref.pop()
				if got.order != want.order {
					t.Fatalf("trial %d op %d: popped order %d, want %d", trial, op, got.order, want.order)
				}
			}
			if len(queue.items) != len(ref.items) {
				t.Fatalf("trial %d op %d: %d items, want %d", trial, op, len(queue.items), len(ref.items))
			}
			for i := range ref.items {
				item := queue.items[i]
				if item.order != ref.items[i].order || int(item.entry.index) != i || item.entry.order != item.order ||
					math.Float64bits(item.priority) != math.Float64bits(item.entry.priority) {
					t.Fatalf("trial %d op %d: heap position %d differs from the swap heap", trial, op, i)
				}
			}
		}
	}
}
