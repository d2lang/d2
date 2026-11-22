package routing

import (
	"fmt"
	"math/bits"
)

const priorityQueueEntryChunkSize = 256

// priorityQueue is an indexed binary min-heap. Entries keep their heap index
// so Dijkstra's algorithm can lower a queued distance without a map lookup.
// Equal priorities are popped in insertion order.
//
// The heap stores each entry's key (priority, order) next to its pointer so
// comparisons read the heap array only, and sifts move a hole instead of
// swapping: every comparison, every resulting position and therefore every
// work charge (decrease charges by the binary-heap index) are those of the
// swap-based binary heap.
type priorityQueue struct {
	items       []priorityQueueItem
	entryChunks [][]priorityQueueEntry
	entryCursor int
	nextOrder   uint64
}

type priorityQueueItem struct {
	priority float64
	order    uint64
	entry    *priorityQueueEntry
}

type priorityQueueEntry struct {
	node     *OVGNode
	priority float64
	order    uint64
	// index is the entry's heap position, -1 once popped. A queue cannot
	// hold 2^31 entries: they would need over 100 GB.
	index        int32
	isHorizontal bool
}

// reset empties the queue while retaining storage. Entries returned before a
// reset must not be used afterward.
func (q *priorityQueue) reset() {
	clear(q.items)
	q.items = q.items[:0]
	q.entryCursor = 0
	q.nextOrder = 0
}

// release resets the queue for reuse by another router and drops every
// entry's references (allocateEntry overwrites an entry before use anyway),
// keeping the chunks.
func (q *priorityQueue) release() {
	for _, chunk := range q.entryChunks {
		clear(chunk)
	}
	q.reset()
}

// storage is the larger of the queue's item capacity and its entry storage.
func (q *priorityQueue) storage() int {
	return max(cap(q.items), len(q.entryChunks)*priorityQueueEntryChunkSize)
}

func (q *priorityQueue) empty() bool {
	return len(q.items) == 0
}

func (q *priorityQueue) push(priority float64, node *OVGNode, isHorizontal bool, guard workBudget) (*priorityQueueEntry, error) {
	if err := reservePriorityQueueWork(guard, len(q.items)+1, 1); err != nil {
		return nil, err
	}

	entry := q.allocateEntry(priority)
	entry.node = node
	entry.isHorizontal = isHorizontal
	entry.index = int32(len(q.items))
	entry.order = q.nextOrder
	q.nextOrder++
	q.items = append(q.items, priorityQueueItem{priority: priority, order: entry.order, entry: entry})
	q.siftUp(len(q.items) - 1)
	return entry, nil
}

func (q *priorityQueue) pop(guard workBudget) (*priorityQueueEntry, error) {
	if q.empty() {
		return nil, fmt.Errorf("cannot dequeue minimum of empty priority queue")
	}
	if err := reservePriorityQueueWork(guard, len(q.items), 2); err != nil {
		return nil, err
	}

	min := q.items[0].entry
	lastIndex := len(q.items) - 1
	last := q.items[lastIndex]
	q.items[lastIndex] = priorityQueueItem{}
	q.items = q.items[:lastIndex]
	min.index = -1

	if lastIndex != 0 {
		q.items[0] = last
		last.entry.index = 0
		q.siftDown(0)
	}
	return min, nil
}

func (q *priorityQueue) decrease(entry *priorityQueueEntry, priority float64, guard workBudget) error {
	if q.empty() {
		return fmt.Errorf("cannot decrease priority in an empty priority queue")
	}
	if entry == nil {
		return fmt.Errorf("cannot decrease priority: entry is nil")
	}
	if entry.index < 0 || int(entry.index) >= len(q.items) || q.items[entry.index].entry != entry {
		return fmt.Errorf("cannot decrease priority: entry is not in the priority queue")
	}
	if priority >= entry.priority {
		return fmt.Errorf("new priority %v is not less than old priority %v", priority, entry.priority)
	}
	if err := reservePriorityQueueWork(guard, int(entry.index)+1, 1); err != nil {
		return err
	}

	entry.priority = priority
	q.items[entry.index].priority = priority
	q.siftUp(int(entry.index))
	return nil
}

func reservePriorityQueueWork(guard workBudget, upperBound, unitsPerLevel int) error {
	if guard == nil {
		return nil
	}
	return guard.add(uint64(bits.Len(uint(upperBound)) * unitsPerLevel))
}

func (q *priorityQueue) allocateEntry(priority float64) *priorityQueueEntry {
	chunkIndex := q.entryCursor / priorityQueueEntryChunkSize
	offset := q.entryCursor % priorityQueueEntryChunkSize
	if chunkIndex == len(q.entryChunks) {
		q.entryChunks = append(q.entryChunks, make([]priorityQueueEntry, priorityQueueEntryChunkSize))
	}
	q.entryCursor++

	entry := &q.entryChunks[chunkIndex][offset]
	*entry = priorityQueueEntry{priority: priority, index: -1}
	return entry
}

// siftUp moves the item at index up: the swap-based sift's comparisons (the
// moving item against each parent), with the hole moved instead of swapping.
func (q *priorityQueue) siftUp(index int) {
	item := q.items[index]
	for index > 0 {
		parent := (index - 1) / 2
		if !priorityQueueItemLess(&item, &q.items[parent]) {
			break
		}
		q.items[index] = q.items[parent]
		q.items[index].entry.index = int32(index)
		index = parent
	}
	q.items[index] = item
	item.entry.index = int32(index)
}

// siftDown moves the item at index down: the swap-based sift's comparisons
// (right child against left child, then the smaller child against the moving
// item), with the hole moved instead of swapping.
func (q *priorityQueue) siftDown(index int) {
	item := q.items[index]
	n := len(q.items)
	for {
		left := index*2 + 1
		if left >= n {
			break
		}
		smallest := left
		right := left + 1
		if right < n && priorityQueueItemLess(&q.items[right], &q.items[left]) {
			smallest = right
		}
		if !priorityQueueItemLess(&q.items[smallest], &item) {
			break
		}
		q.items[index] = q.items[smallest]
		q.items[index].entry.index = int32(index)
		index = smallest
	}
	q.items[index] = item
	item.entry.index = int32(index)
}

// priorityQueueItemLess orders heap items by priority, then insertion order
// (an item's priority and order always equal its entry's).
func priorityQueueItemLess(a, b *priorityQueueItem) bool {
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	// Preserve insertion order for equal-cost states. Route selection should not
	// depend on the binary heap's internal shape.
	return a.order < b.order
}
