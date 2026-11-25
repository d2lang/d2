package layoutgraph

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
)

// slotMarks stamps nodes with a generation. Nodes recorded in the
// transaction's current snapshot are stamped by slot; any other node (one an
// operation added after the capture) in a map.
type slotMarks struct {
	marks      []uint32
	extra      map[*Node]uint32
	generation uint32
}

// begin starts a new set of marks for a snapshot with size slots.
func (m *slotMarks) begin(size int) uint32 {
	if len(m.marks) < size {
		m.marks = make([]uint32, max(size, 2*len(m.marks)))
	}
	m.generation++
	if m.generation == 0 {
		clear(m.marks)
		clear(m.extra)
		m.generation = 1
	}
	return m.generation
}

func (m *slotMarks) mark(slots *snapshotSlots, node *Node, generation uint32) {
	if slot := slots.slotOf(node); slot >= 0 {
		m.marks[slot] = generation
		return
	}
	if m.extra == nil {
		m.extra = make(map[*Node]uint32)
	}
	m.extra[node] = generation
}

func (m *slotMarks) has(slots *snapshotSlots, node *Node, generation uint32) bool {
	if slot := slots.slotOf(node); slot >= 0 {
		return m.marks[slot] == generation
	}
	return m.extra[node] == generation
}

// hasSlot is has for a node whose slot (or -1) is already known.
func (m *slotMarks) hasSlot(slot int32, node *Node, generation uint32) bool {
	if slot >= 0 {
		return m.marks[slot] == generation
	}
	return m.extra[node] == generation
}

// pushDescendantChildren pushes parent's children in reverse traversal order
// (so the walk keeps the container-before-cluster-before-sequence preorder of
// appendDescendantNodesGuarded) and returns the number pushed: one unit each.
func (t *Transaction) pushDescendantChildren(stack []transactionPendingDescendant, parent *Node) ([]transactionPendingDescendant, int64) {
	g := t.Graph
	var pushed int64
	// A lookup in an empty map finds nothing, so it is skipped.
	if len(g.Sequences) != 0 {
		if sequence := g.Sequences[parent]; sequence != nil {
			for i := len(sequence.Nodes) - 1; i >= 0; i-- {
				stack = append(stack, transactionPendingDescendant{node: sequence.Nodes[i], emit: true})
			}
			pushed += int64(len(sequence.Nodes))
		}
	}
	if parent != nil && parent.isClusterVessel {
		if cluster := g.Clusters[parent]; cluster != nil {
			for i := len(cluster.Nodes) - 1; i >= 0; i-- {
				stack = append(stack, transactionPendingDescendant{node: cluster.Nodes[i], emit: true})
			}
			pushed += int64(len(cluster.Nodes))
		}
	}
	if parent == nil || parent.isContainer {
		children := g.Containers[parent]
		for i := len(children) - 1; i >= 0; i-- {
			stack = append(stack, transactionPendingDescendant{node: children[i], emit: true})
		}
		pushed += int64(len(children))
	}
	return stack, pushed
}

// walkDescendants is the descendant walk of appendDescendantNodesGuarded and
// markDescendantOverlapExceptions with includeClusterNodes set (every pushed
// entry is emitted): the same pushes and pops in the same order. Every
// visited node, node itself included, is marked with generation in t.marks,
// which serves as the walk's seen set. The descendants are left in
// t.descendantNodes in walk order. It returns the walk's units, one per push
// and one per pop, without charging them.
func (t *Transaction) walkDescendants(node *Node, generation uint32) int64 {
	slots := &t.PriorGraphState.slots
	t.descendantNodes = t.descendantNodes[:0]
	if node != nil {
		t.marks.mark(slots, node, generation)
	}
	stack, units := t.pushDescendantChildren(t.descendantStack[:0], node)
	for len(stack) > 0 {
		units++
		last := len(stack) - 1
		current := stack[last].node
		stack = stack[:last]
		if current == nil || t.marks.has(slots, current, generation) {
			continue
		}
		t.marks.mark(slots, current, generation)
		t.descendantNodes = append(t.descendantNodes, current)
		var pushed int64
		stack, pushed = t.pushDescendantChildren(stack, current)
		units += pushed
	}
	t.descendantStack = stack
	return units
}

// containerPass caches, for one pass of container checks (nothing moves
// while they run), each g.Nodes entry's box sums, snapshot slot and
// existing-overlap number.
type containerPass struct {
	ready   bool
	boxes   []transactionBox
	hasBox  []bool
	slots   []int32
	overlap []int32
}

// beginContainerPass invalidates the pass cache; Commit calls it after
// repositioning the containers, before checking them.
func (t *Transaction) beginContainerPass() {
	t.ensureCommitScratch()
	t.pass.ready = false
}

func (t *Transaction) preparePass() *containerPass {
	pass := &t.pass
	if pass.ready {
		return pass
	}
	nodes := t.Graph.Nodes
	count := len(nodes)
	if cap(pass.boxes) < count {
		pass.boxes = make([]transactionBox, count)
		pass.hasBox = make([]bool, count)
		pass.slots = make([]int32, count)
		pass.overlap = make([]int32, count)
	}
	pass.boxes = pass.boxes[:count]
	pass.hasBox = pass.hasBox[:count]
	pass.slots = pass.slots[:count]
	pass.overlap = pass.overlap[:count]
	slots := &t.PriorGraphState.slots
	set := t.PriorGraphState.existingOverlaps
	for i, node := range nodes {
		pass.hasBox[i] = false
		pass.slots[i] = -1
		pass.overlap[i] = -1
		if node == nil {
			continue
		}
		pass.slots[i] = int32(slots.slotOf(node))
		pass.overlap[i] = int32(set.indexOf(node))
		if node.TopLeft != nil {
			pass.hasBox[i] = true
			pass.boxes[i] = transactionBox{
				left: node.TopLeft.X, top: node.TopLeft.Y,
				right: node.TopLeft.X + node.Width, bottom: node.TopLeft.Y + node.Height,
			}
		}
	}
	pass.ready = true
	return pass
}

// beginMarks starts a mark set over the current snapshot.
func (t *Transaction) beginMarks() uint32 {
	t.ensureCommitScratch()
	return t.marks.begin(t.PriorGraphState.slots.size())
}

// containerBad is t.Graph.isBadStateWithOrigins(container, t.PriorGraphState,
// t.options.IgnoreContainerEscape, origins, t.guard) with the overlap part in
// containerOverlapBad.
func (t *Transaction) containerBad(container *Node, origins *fixedOriginCache) (bool, error) {
	steps := batchedWork(t.guard)
	bad, skipOverlapCheck, err := t.Graph.structurallyBadCountedWithOrigins(container, t.PriorGraphState, t.options.IgnoreContainerEscape, nil, false, origins, &steps)
	if flushErr := steps.flush(); flushErr != nil {
		return false, flushErr
	}
	if err != nil || bad {
		return bad, err
	}
	if skipOverlapCheck {
		return false, t.guard.Finish()
	}
	return t.containerOverlapBad(container)
}

// containerOverlapBad is hasBadOverlapStateContext(node, t.PriorGraphState,
// t.guard) followed by its doesOverlapWithDimensionsContext scan, with the
// exceptions (descendants and ancestors) kept as marks rather than a list or
// set. The walks, the exception units (one per listed exception), the scan
// order and its units, and every Finish are those of the original.
func (t *Transaction) containerOverlapBad(node *Node) (bool, error) {
	if node == nil || node.TopLeft == nil {
		return true, invariant.New("transaction overlap check received an incomplete node")
	}
	if node.Cluster != nil {
		return false, nil
	}
	g := t.Graph
	slots := &t.PriorGraphState.slots
	generation := t.beginMarks()
	var exceptionCount int64
	if node.isContainer || node.isClusterVessel || g.Sequences[node] != nil {
		if units := t.walkDescendants(node, generation); units != 0 {
			if err := t.guard.Add(units); err != nil {
				return false, err
			}
		}
		if err := t.guard.Finish(); err != nil {
			return false, err
		}
		exceptionCount += int64(len(t.descendantNodes))
	}
	if node.Container != nil || node.Cluster != nil || node.Sequence != nil {
		if t.walkScratch == nil {
			t.walkScratch = &descendantWalkScratch{seen: make(map[*Node]struct{})}
		}
		ancestors, err := g.appendAncestorsGuarded(t.ancestorScratch[:0], t.walkScratch, node, t.guard)
		t.walkScratch.resetVisited()
		t.ancestorScratch = ancestors
		if err != nil {
			clear(ancestors)
			return false, err
		}
		for _, ancestor := range ancestors {
			t.marks.mark(slots, ancestor, generation)
		}
		exceptionCount += int64(len(ancestors))
		clear(ancestors)
	}

	p := node.TopLeft
	right := p.X + node.Width
	bottom := p.Y + node.Height
	steps := batchedWork(t.guard)
	if exceptionCount > 0 {
		if err := steps.add(exceptionCount); err != nil {
			return false, err
		}
	}
	pass := t.preparePass()
	set := t.PriorGraphState.existingOverlaps
	var pairwiseStamp uint32
	hasPairwise := false
	if i := set.indexOf(node); i >= 0 && len(set.near[i]) != 0 {
		pairwiseStamp = set.mark(false, i)
		hasPairwise = true
	}
	// The pass boxes hold the same sums (x+width, y+height) the comparisons
	// below were written with. Each visited entry costs one unit; they are
	// added up where the scan stops (all units are batched until a flush).
	for index, otherNode := range g.Nodes {
		if otherNode == nil {
			if err := steps.add(int64(index + 1)); err != nil {
				return false, err
			}
			return false, steps.flushWith(invariant.New("overlap check encountered a nil graph node"))
		}
		if otherNode == node || !pass.hasBox[index] {
			continue
		}
		other := pass.boxes[index]
		const maxSafeDelta = 500.0
		if p.X > other.right+maxSafeDelta ||
			right+maxSafeDelta < other.left ||
			p.Y > other.bottom+maxSafeDelta ||
			bottom+maxSafeDelta < other.top {
			continue
		}
		if hasPairwise && set.marked(int(pass.overlap[index]), pairwiseStamp) {
			continue
		}
		if exceptionCount > 0 && t.marks.hasSlot(pass.slots[index], otherNode, generation) {
			continue
		}
		deltaValue, units, err := node.deltaToCounted(otherNode, p)
		if addErr := steps.add(units); addErr != nil {
			return false, addErr
		}
		if err != nil {
			if addErr := steps.add(int64(index + 1)); addErr != nil {
				return false, addErr
			}
			return false, steps.flushWith(err)
		}
		delta := float64(deltaValue)
		if (p.X < (other.right + delta)) && ((right + delta) > other.left) &&
			(p.Y < (other.bottom + delta)) && ((bottom + delta) > other.top) {
			if err := steps.add(int64(index + 1)); err != nil {
				return false, err
			}
			if err := steps.flush(); err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if err := steps.add(int64(len(g.Nodes))); err != nil {
		return false, err
	}
	if err := steps.flush(); err != nil {
		return false, err
	}
	return false, t.guard.Finish()
}
