package layoutgraph

// existingOverlapSet holds the node pairs that overlapped when a transaction
// was created, in two symmetric relations: near (padded overlap in either
// direction) and exact. Each unordered pair is held once per relation, like
// the map of maps it replaces (map[*Node]map[*Node]struct{} in both
// directions); nodes are numbered in first-insertion order.
//
// Node.overlapSlot is a lookup hint, trusted only when the slot it names holds
// that very node (see snapshotSlots); stale hints fall back to the index map.
type existingOverlapSet struct {
	nodes []*Node
	near  [][]int32
	exact [][]int32
	// nearKeys counts nodes with at least one near pair: the key count of the
	// former near map.
	nearKeys int
	// nearRefs counts directed near references (twice the pair count).
	nearRefs int

	index map[*Node]int32

	// marks is scratch for membership tests against one node's neighbors.
	marks      []uint32
	generation uint32

	// The map form is built on first use by the rare paths that iterate it.
	nearMap, exactMap map[*Node]map[*Node]struct{}

	// Construction scratch (buildExistingOverlapSetInto), kept for reuse.
	placed []transactionSweepNode
	active []*Node
}

// reset empties the set, keeping its storage for reuse.
func (set *existingOverlapSet) reset() {
	clear(set.nodes)
	set.nodes = set.nodes[:0]
	set.near = set.near[:0]
	set.exact = set.exact[:0]
	set.nearKeys = 0
	set.nearRefs = 0
	set.index = nil
	set.nearMap = nil
	set.exactMap = nil
}

// indexOf returns node's number in the set, or -1.
func (set *existingOverlapSet) indexOf(node *Node) int {
	if set == nil || node == nil || len(set.nodes) == 0 {
		return -1
	}
	slot := node.overlapSlot
	if slot >= 0 && int(slot) < len(set.nodes) && set.nodes[slot] == node {
		return int(slot)
	}
	if set.index == nil {
		set.index = make(map[*Node]int32, len(set.nodes))
		for i, recorded := range set.nodes {
			set.index[recorded] = int32(i)
		}
	}
	if index, ok := set.index[node]; ok {
		return int(index)
	}
	return -1
}

// intern returns node's number, adding it when absent. Only construction
// calls it; while the set is being built every added node's hint is current.
func (set *existingOverlapSet) intern(node *Node) int32 {
	slot := node.overlapSlot
	if slot >= 0 && int(slot) < len(set.nodes) && set.nodes[slot] == node {
		return slot
	}
	slot = int32(len(set.nodes))
	node.overlapSlot = slot
	set.nodes = append(set.nodes, node)
	// Reuse a released set's neighbor lists.
	if len(set.near) < cap(set.near) {
		set.near = set.near[:len(set.near)+1]
		set.near[slot] = set.near[slot][:0]
	} else {
		set.near = append(set.near, nil)
	}
	if len(set.exact) < cap(set.exact) {
		set.exact = set.exact[:len(set.exact)+1]
		set.exact[slot] = set.exact[slot][:0]
	} else {
		set.exact = append(set.exact, nil)
	}
	return slot
}

// addPair records the unordered pair {first, second} in the near or exact
// relation. dedupe must be set when g.Nodes repeats a node, the only way the
// construction sweep can produce a pair twice.
func (set *existingOverlapSet) addPair(exact bool, first, second *Node, dedupe bool) {
	i := set.intern(first)
	j := set.intern(second)
	relation := set.near
	if exact {
		relation = set.exact
	}
	if dedupe {
		for _, existing := range relation[i] {
			if existing == j {
				return
			}
		}
	}
	if !exact && len(set.near[i]) == 0 {
		set.nearKeys++
	}
	relation[i] = append(relation[i], j)
	if !exact && len(set.near[j]) == 0 {
		set.nearKeys++
	}
	relation[j] = append(relation[j], i)
	if !exact {
		set.nearRefs += 2
	}
}

// mark stamps the neighbors of node i in one relation and returns the
// stamp; marked(j, stamp) then reports membership of j.
func (set *existingOverlapSet) mark(exact bool, i int) uint32 {
	if len(set.marks) < len(set.nodes) {
		set.marks = make([]uint32, len(set.nodes))
		set.generation = 0
	}
	set.generation++
	if set.generation == 0 {
		clear(set.marks)
		set.generation = 1
	}
	relation := set.near
	if exact {
		relation = set.exact
	}
	for _, j := range relation[i] {
		set.marks[j] = set.generation
	}
	return set.generation
}

func (set *existingOverlapSet) marked(j int, stamp uint32) bool {
	return j >= 0 && set.marks[j] == stamp
}

// nearExceptions answers "is other a near pair of node?" for repeated
// queries about one node.
type nearExceptions struct {
	set   *existingOverlapSet
	stamp uint32
}

// nearExceptionsOf prepares the queries; ok is false when node has no near
// pair (every query would answer false).
func (set *existingOverlapSet) nearExceptionsOf(node *Node) (nearExceptions, bool) {
	i := set.indexOf(node)
	if i < 0 || len(set.near[i]) == 0 {
		return nearExceptions{}, false
	}
	return nearExceptions{set: set, stamp: set.mark(false, i)}, true
}

func (exceptions nearExceptions) contains(other *Node) bool {
	return exceptions.set.marked(exceptions.set.indexOf(other), exceptions.stamp)
}

// existingOverlapSetFromMap converts a caller's pairwise-exception map (any
// shape the former map parameter accepted) into a set with the same near
// membership answers.
func existingOverlapSetFromMap(pairs map[*Node]map[*Node]struct{}) *existingOverlapSet {
	if len(pairs) == 0 {
		return nil
	}
	set := &existingOverlapSet{index: make(map[*Node]int32)}
	intern := func(node *Node) int32 {
		if index, ok := set.index[node]; ok {
			return index
		}
		index := int32(len(set.nodes))
		set.index[node] = index
		set.nodes = append(set.nodes, node)
		set.near = append(set.near, nil)
		set.exact = append(set.exact, nil)
		return index
	}
	// A map may be asymmetric: membership is directional, from the key.
	for node, others := range pairs {
		i := intern(node)
		for other := range others {
			// intern may append a row to set.near: call it before reading
			// set.near (Go leaves the order unspecified; TinyGo reads the
			// slice first and would append into a stale row).
			j := intern(other)
			set.near[i] = append(set.near[i], j)
		}
	}
	return set
}
