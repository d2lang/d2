package layoutgraph

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
)

// containerMemo serves a StableStructure transaction with AffectContainers.
// Every operation of such a transaction re-wraps every container
// (repositionContainers) and then checks every container (containerBad), but
// a typical operation moves a few nodes, so most containers see exactly the
// inputs they saw before. Under the StableStructure contract (operations and
// the caller's code between commits change only node geometry and cluster
// arrangement) both computations are deterministic functions of geometry:
//
//   - wrapChildren(C) writes C's TopLeft values, Width and Height from C's
//     own geometry and its children's. A wrap whose inputs equal the last
//     recorded wrap's inputs bit for bit writes the recorded output.
//   - containerBad(C), when it finds C good, charges units that depend on
//     C's geometry, its container's and children's geometry (containment) and
//     the geometry of the g.Nodes entries within C's 500-unit broad phase.
//     The memo keeps each container's units from the last pass in which every
//     container was good, together with the geometry of every g.Nodes entry
//     at that pass (ref). A container whose own, parent and child geometry is
//     unchanged and whose broad phase no changed entry touches (before or
//     after its change) would find the same nodes with the same geometry, so
//     it is good with the same units. Other containers are checked anew
//     without charging (containerBadCounted).
//
// When every container is good, the pass charges the legacy loop's units in
// one Add (they do not depend on the map order of g.Containers). Otherwise
// nothing has been charged and the legacy loop runs (its map order and early
// stop are then as arbitrary as before).
type containerMemo struct {
	entries []containerMemoEntry

	refNodes []*Node
	refGeom  []memoGeometry
	refValid bool

	dirty   []int32
	pending []containerCheckRecord
	// pendingChildren holds the pending records' child geometry.
	pendingChildren []memoGeometry
	counter         unitCounter
	// pass numbers container passes; a good pass invalidates the checks of
	// containers it did not check (they would not follow ref).
	pass uint32
	// hits counts how checks and wraps were answered (tests read it).
	hits struct{ clean, rescan, full, wrap int }
}

// memoGeometry is a node's geometry as raw bits (placed is TopLeft != nil).
type memoGeometry struct {
	x, y, w, h uint64
	placed     bool
}

func geometryOf(node *Node) memoGeometry {
	if node == nil || node.TopLeft == nil {
		return memoGeometry{}
	}
	return memoGeometry{
		x: math.Float64bits(node.TopLeft.X), y: math.Float64bits(node.TopLeft.Y),
		w: math.Float64bits(node.Width), h: math.Float64bits(node.Height),
		placed: true,
	}
}

type containerMemoEntry struct {
	node *Node

	// The last wrap: wrapNodes[0] is the container, then its children in
	// order, each with the geometry it had (wrapIn); wrapOut is the
	// container's geometry afterwards.
	wrapValid bool
	wrapNodes []*Node
	wrapIn    []memoGeometry
	wrapOut   memoGeometry

	// The last check found the container good, at the container geometry
	// checkGeom, container-of geometry parentGeom and child geometry
	// checkChildren, with geometry ref around it (it is valid only while ref
	// is that of the pass it ran in or was carried over). Its units were
	// checkBase plus checkEdgeUnits (the container's edge count, deltaTo's
	// units) for each of the checkEvaluated pairs it evaluated.
	checkValid     bool
	checkedPass    uint32
	checkGeom      memoGeometry
	parentGeom     memoGeometry
	checkChildren  []memoGeometry
	checkBase      int64
	checkEvaluated int64
	checkEdgeUnits int64
}

type containerCheckRecord struct {
	entry      int32
	geometry   memoGeometry
	parent     memoGeometry
	base       int64
	evaluated  int64
	edgeUnits  int64
	childStart int32
	childEnd   int32
}

type unitCounter struct{ units int64 }

func (c *unitCounter) Step() error   { c.units++; return nil }
func (c *unitCounter) Finish() error { return nil }

// reset empties the memo for a new transaction, keeping storage.
func (m *containerMemo) reset() {
	for i := range m.entries {
		entry := &m.entries[i]
		entry.node = nil
		entry.wrapValid = false
		entry.checkValid = false
		clear(entry.wrapNodes)
		entry.wrapNodes = entry.wrapNodes[:0]
		entry.wrapIn = entry.wrapIn[:0]
		entry.checkChildren = entry.checkChildren[:0]
	}
	m.entries = m.entries[:0]
	clear(m.refNodes)
	m.refNodes = m.refNodes[:0]
	m.refGeom = m.refGeom[:0]
	m.refValid = false
	m.dirty = m.dirty[:0]
	m.pending = m.pending[:0]
	m.pendingChildren = m.pendingChildren[:0]
	m.pass = 0
}

// entryFor returns node's entry index, adding one when absent. Node.memoSlot
// is a hint verified by the entry it names.
func (m *containerMemo) entryFor(node *Node) int32 {
	slot := node.memoSlot
	if slot >= 0 && int(slot) < len(m.entries) && m.entries[slot].node == node {
		return slot
	}
	for i := range m.entries {
		if m.entries[i].node == node {
			node.memoSlot = int32(i)
			return int32(i)
		}
	}
	slot = int32(len(m.entries))
	if len(m.entries) < cap(m.entries) {
		m.entries = m.entries[:len(m.entries)+1]
	} else {
		m.entries = append(m.entries, containerMemoEntry{})
	}
	entry := &m.entries[slot]
	entry.node = node
	entry.wrapValid = false
	entry.checkValid = false
	node.memoSlot = slot
	return slot
}

// wrap is container.wrapChildren() answered from the last wrap when its
// inputs are unchanged.
func (m *containerMemo) wrap(container *Node) {
	if !container.isContainer {
		return
	}
	children := container.Graph.Containers[container]
	// entryFor may append to m.entries, so it runs in its own statement: in
	// m.entries[m.entryFor(container)] Go leaves unspecified whether the slice
	// is read before or after the call, and TinyGo reads it first (the old,
	// shorter slice: index out of range).
	slot := m.entryFor(container)
	entry := &m.entries[slot]
	if entry.wrapValid && len(entry.wrapNodes) == len(children)+1 && entry.wrapNodes[0] == container &&
		entry.wrapIn[0] == geometryOf(container) {
		same := true
		for i, child := range children {
			if entry.wrapNodes[i+1] != child || entry.wrapIn[i+1] != geometryOf(child) {
				same = false
				break
			}
		}
		if same {
			m.hits.wrap++
			out := entry.wrapOut
			container.TopLeft.X = math.Float64frombits(out.x)
			container.TopLeft.Y = math.Float64frombits(out.y)
			container.Width = math.Float64frombits(out.w)
			container.Height = math.Float64frombits(out.h)
			return
		}
	}
	// Record the inputs; a child without a position is never memoized (its
	// legacy path fails or behaves as before on every call).
	entry.wrapValid = false
	entry.wrapNodes = append(entry.wrapNodes[:0], container)
	entry.wrapIn = append(entry.wrapIn[:0], geometryOf(container))
	recordable := container.TopLeft != nil
	for _, child := range children {
		if child == nil || child.TopLeft == nil {
			recordable = false
		}
		entry.wrapNodes = append(entry.wrapNodes, child)
		entry.wrapIn = append(entry.wrapIn, geometryOf(child))
	}
	container.wrapChildren()
	if recordable && container.TopLeft != nil {
		entry.wrapOut = geometryOf(container)
		entry.wrapValid = true
	}
}

// outsideBroadPhase is containerOverlapBad's broad-phase rejection of a box
// (left, top, right, bottom sums as preparePass computes them) for a
// container at p with the given right and bottom.
func outsideBroadPhase(px, py, right, bottom float64, other memoGeometry) bool {
	if !other.placed {
		return true
	}
	x := math.Float64frombits(other.x)
	y := math.Float64frombits(other.y)
	otherRight := x + math.Float64frombits(other.w)
	otherBottom := y + math.Float64frombits(other.h)
	const maxSafeDelta = 500.0
	return px > otherRight+maxSafeDelta ||
		right+maxSafeDelta < x ||
		py > otherBottom+maxSafeDelta ||
		bottom+maxSafeDelta < y
}

// checkContainersMemo is the container loop of one Commit operation answered
// from the memo. handled is false when the legacy loop must run instead
// (nothing has been charged then).
func (t *Transaction) checkContainersMemo(poll *contextPoll, origins *fixedOriginCache) (handled bool, err error) {
	m := t.memo
	g := t.Graph
	nodes := g.Nodes

	// The changed g.Nodes entries since ref (all of them without a ref).
	refUsable := m.refValid && len(m.refNodes) == len(nodes)
	if refUsable {
		for i, node := range nodes {
			if m.refNodes[i] != node {
				refUsable = false
				break
			}
		}
	}
	m.dirty = m.dirty[:0]
	if refUsable {
		for i, node := range nodes {
			if geometryOf(node) != m.refGeom[i] {
				m.dirty = append(m.dirty, int32(i))
			}
		}
	}

	m.pending = m.pending[:0]
	m.pendingChildren = m.pendingChildren[:0]
	m.pass++
	var keyUnits, checkUnits int64
	for container := range g.Containers {
		keyUnits++
		if container == nil || container.TopLeft == nil {
			continue
		}
		index := m.entryFor(container)
		entry := &m.entries[index]
		geometry := geometryOf(container)
		parent := geometryOf(container.container())
		record := containerCheckRecord{entry: index, geometry: geometry, parent: parent}
		if refUsable && entry.checkValid && entry.checkGeom == geometry && entry.parentGeom == parent &&
			t.sameCheckChildren(entry, container) {
			px := math.Float64frombits(geometry.x)
			py := math.Float64frombits(geometry.y)
			right := px + math.Float64frombits(geometry.w)
			bottom := py + math.Float64frombits(geometry.h)
			clean := true
			for _, i := range m.dirty {
				if !outsideBroadPhase(px, py, right, bottom, m.refGeom[i]) ||
					!outsideBroadPhase(px, py, right, bottom, geometryOf(nodes[i])) {
					clean = false
					break
				}
			}
			if clean {
				m.hits.clean++
				checkUnits += entry.checkBase + entry.checkEdgeUnits*entry.checkEvaluated
				entry.checkedPass = m.pass
				continue
			}
			// The container and its structural inputs are unchanged; only
			// changed entries in its broad phase can change the scan.
			bad, evaluated, badErr := t.containerRescan(entry, container)
			if badErr != nil || bad {
				return false, nil
			}
			m.hits.rescan++
			record.base = entry.checkBase
			record.edgeUnits = entry.checkEdgeUnits
			record.evaluated = evaluated
		} else {
			m.hits.full++
			bad, base, evaluated, edgeUnits, badErr := t.containerBadCounted(container, origins)
			if badErr != nil || bad {
				return false, nil
			}
			record.base = base
			record.edgeUnits = edgeUnits
			record.evaluated = evaluated
		}
		checkUnits += record.base + record.edgeUnits*record.evaluated
		record.childStart = int32(len(m.pendingChildren))
		if t.structuralChildrenChecked(container) {
			for _, child := range t.Graph.Containers[container] {
				m.pendingChildren = append(m.pendingChildren, geometryOf(child))
			}
		}
		record.childEnd = int32(len(m.pendingChildren))
		m.pending = append(m.pending, record)
	}

	// Every container is good: charge the loop's units (one per key, then
	// each positioned container's check) and poll as its strided polls would.
	steps := batchedWork(t.guard)
	if err := steps.add(keyUnits + checkUnits); err != nil {
		return true, err
	}
	if err := steps.flush(); err != nil {
		return true, err
	}
	if err := poll.skip(keyUnits); err != nil {
		return true, err
	}
	if err := t.guard.Finish(); err != nil {
		return true, err
	}

	for _, record := range m.pending {
		entry := &m.entries[record.entry]
		entry.checkedPass = m.pass
		entry.checkValid = true
		entry.checkGeom = record.geometry
		entry.parentGeom = record.parent
		entry.checkBase = record.base
		entry.checkEvaluated = record.evaluated
		entry.checkEdgeUnits = record.edgeUnits
		entry.checkChildren = append(entry.checkChildren[:0], m.pendingChildren[record.childStart:record.childEnd]...)
	}
	for i := range m.entries {
		if m.entries[i].checkedPass != m.pass {
			m.entries[i].checkValid = false
		}
	}
	if !refUsable {
		m.refNodes = append(m.refNodes[:0], nodes...)
		m.refGeom = m.refGeom[:0]
		for _, node := range nodes {
			m.refGeom = append(m.refGeom, geometryOf(node))
		}
	} else {
		for _, i := range m.dirty {
			m.refGeom[i] = geometryOf(nodes[i])
		}
	}
	m.refValid = true
	return true, nil
}

// structuralChildrenChecked reports whether containerBad's structural check
// reads container's children's geometry.
func (t *Transaction) structuralChildrenChecked(container *Node) bool {
	return !t.options.IgnoreContainerEscape && container.isContainer && container.Cluster == nil
}

// sameCheckChildren reports whether the child geometry the structural check
// reads is what the entry's check saw.
func (t *Transaction) sameCheckChildren(entry *containerMemoEntry, container *Node) bool {
	if !t.structuralChildrenChecked(container) {
		return true
	}
	children := t.Graph.Containers[container]
	if len(entry.checkChildren) != len(children) {
		return false
	}
	for i, child := range children {
		if entry.checkChildren[i] != geometryOf(child) {
			return false
		}
	}
	return true
}

// containerBadCounted is containerBad without charging: it returns the
// verdict and the units containerBad would charge, split into base and
// edgeUnits per evaluated pair (deltaToCounted charges one unit per edge of
// the container, so edgeUnits is its edge count), or an error (which the
// caller answers by running containerBad itself).
func (t *Transaction) containerBadCounted(container *Node, origins *fixedOriginCache) (bad bool, base, evaluated, edgeUnits int64, err error) {
	counter := &t.memo.counter
	counter.units = 0
	steps := newBatchedSteps(counter)
	bad, skipOverlapCheck, err := t.Graph.structurallyBadCountedWithOrigins(container, t.PriorGraphState, t.options.IgnoreContainerEscape, nil, false, origins, &steps)
	if err != nil || bad {
		return bad, counter.units, 0, 0, err
	}
	if skipOverlapCheck {
		return false, counter.units, 0, 0, nil
	}
	return t.containerOverlapBadCounted(container, counter)
}

// markContainerExceptions performs containerOverlapBad's exception walks for
// node (descendants, then ancestors), marking them with the returned
// generation, and returns their units and the exception count.
func (t *Transaction) markContainerExceptions(node *Node, counter *unitCounter) (generation uint32, exceptionCount int64, err error) {
	g := t.Graph
	slots := &t.PriorGraphState.slots
	generation = t.beginMarks()
	if node.isContainer || node.isClusterVessel || g.Sequences[node] != nil {
		counter.units += t.walkDescendants(node, generation)
		exceptionCount += int64(len(t.descendantNodes))
	}
	if node.Container != nil || node.Cluster != nil || node.Sequence != nil {
		if t.walkScratch == nil {
			t.walkScratch = &descendantWalkScratch{seen: make(map[*Node]struct{})}
		}
		ancestors, err := g.appendAncestorsGuarded(t.ancestorScratch[:0], t.walkScratch, node, counter)
		t.walkScratch.resetVisited()
		t.ancestorScratch = ancestors
		if err != nil {
			clear(ancestors)
			return generation, exceptionCount, err
		}
		for _, ancestor := range ancestors {
			t.marks.mark(slots, ancestor, generation)
		}
		exceptionCount += int64(len(ancestors))
		clear(ancestors)
	}
	return generation, exceptionCount, nil
}

// containerOverlapBadCounted is containerOverlapBad counting its units in
// counter instead of charging them (see containerBadCounted).
func (t *Transaction) containerOverlapBadCounted(node *Node, counter *unitCounter) (bad bool, base, evaluated, edgeUnits int64, err error) {
	if node == nil || node.TopLeft == nil {
		return true, counter.units, 0, 0, invariant.New("transaction overlap check received an incomplete node")
	}
	if node.Cluster != nil {
		return false, counter.units, 0, 0, nil
	}
	g := t.Graph
	generation, exceptionCount, err := t.markContainerExceptions(node, counter)
	if err != nil {
		return false, counter.units, 0, 0, err
	}
	counter.units += exceptionCount

	edgeUnits = int64(len(node.Edges))
	p := node.TopLeft
	right := p.X + node.Width
	bottom := p.Y + node.Height
	pass := t.preparePass()
	set := t.PriorGraphState.existingOverlaps
	var pairwiseStamp uint32
	hasPairwise := false
	if i := set.indexOf(node); i >= 0 && len(set.near[i]) != 0 {
		pairwiseStamp = set.mark(false, i)
		hasPairwise = true
	}
	for index, otherNode := range g.Nodes {
		if otherNode == nil {
			return false, counter.units, 0, 0, invariant.New("overlap check encountered a nil graph node")
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
		if err != nil || units != edgeUnits {
			return false, counter.units, 0, 0, invariant.New("transaction memo: spacing check failed")
		}
		evaluated++
		delta := float64(deltaValue)
		if (p.X < (other.right + delta)) && ((right + delta) > other.left) &&
			(p.Y < (other.bottom + delta)) && ((bottom + delta) > other.top) {
			return true, counter.units, evaluated, edgeUnits, nil
		}
	}
	return false, counter.units + int64(len(g.Nodes)), evaluated, edgeUnits, nil
}

// containerRescan answers containerOverlapBad for a container whose own,
// container-of and child geometry equal its memo entry's while ref changed
// only at the m.dirty entries: every other g.Nodes entry has its ref
// geometry, so its broad-phase, exception and overlap outcome is the one the
// entry's good check saw (no overlap). The changed entries are re-evaluated:
// the pair count moves by the ones evaluated now minus the ones evaluated at
// ref, and any overlap now makes the container bad. Units of the walks are
// part of the entry's base and are not charged here.
func (t *Transaction) containerRescan(entry *containerMemoEntry, node *Node) (bad bool, evaluated int64, err error) {
	if node.Cluster != nil || entry.checkEdgeUnits != int64(len(node.Edges)) {
		return false, 0, invariant.New("transaction memo: container changed")
	}
	m := t.memo
	g := t.Graph
	counter := &m.counter
	counter.units = 0
	generation, exceptionCount, err := t.markContainerExceptions(node, counter)
	if err != nil {
		return false, 0, err
	}
	slots := &t.PriorGraphState.slots
	set := t.PriorGraphState.existingOverlaps
	var pairwiseStamp uint32
	hasPairwise := false
	if i := set.indexOf(node); i >= 0 && len(set.near[i]) != 0 {
		pairwiseStamp = set.mark(false, i)
		hasPairwise = true
	}
	p := node.TopLeft
	right := p.X + node.Width
	bottom := p.Y + node.Height
	excepted := func(other *Node) bool {
		if hasPairwise && set.marked(set.indexOf(other), pairwiseStamp) {
			return true
		}
		return exceptionCount > 0 && t.marks.has(slots, other, generation)
	}
	evaluated = entry.checkEvaluated
	for _, i := range m.dirty {
		other := g.Nodes[i]
		if other == nil {
			return false, 0, invariant.New("overlap check encountered a nil graph node")
		}
		if other == node {
			continue
		}
		if !outsideBroadPhase(p.X, p.Y, right, bottom, m.refGeom[i]) && !excepted(other) {
			evaluated--
		}
		if other.TopLeft == nil {
			continue
		}
		left, top := other.TopLeft.X, other.TopLeft.Y
		otherRight, otherBottom := left+other.Width, top+other.Height
		const maxSafeDelta = 500.0
		if p.X > otherRight+maxSafeDelta ||
			right+maxSafeDelta < left ||
			p.Y > otherBottom+maxSafeDelta ||
			bottom+maxSafeDelta < top {
			continue
		}
		if excepted(other) {
			continue
		}
		deltaValue, units, err := node.deltaToCounted(other, p)
		if err != nil || units != entry.checkEdgeUnits {
			return false, 0, invariant.New("transaction memo: spacing check failed")
		}
		evaluated++
		delta := float64(deltaValue)
		if (p.X < (otherRight + delta)) && ((right + delta) > left) &&
			(p.Y < (otherBottom + delta)) && ((bottom + delta) > top) {
			return true, evaluated, nil
		}
	}
	return false, evaluated, nil
}
