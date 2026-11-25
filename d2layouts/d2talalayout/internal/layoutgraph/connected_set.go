package layoutgraph

import "github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"

// connectedOwners stores the usual single parent without a slice allocation.
// Additional runtime ownership views are retained, rather than assuming that
// Node.Container fully describes graph-owned container/cluster membership.
type connectedOwners struct {
	first map[*Node]*Node
	more  map[*Node][]*Node
}

func (owners *connectedOwners) add(child, parent *Node) {
	if owners.first[child] == nil {
		owners.first[child] = parent
		return
	}
	if owners.more == nil {
		owners.more = make(map[*Node][]*Node)
	}
	owners.more[child] = append(owners.more[child], parent)
}

// connectedSetScratch is ConnectedNodeSet's working storage, pooled because
// gap reduction asks for the set of every node it visits. Every map and
// slice is empty when the scratch leaves the pool.
type connectedSetScratch struct {
	excluded         map[*Node]bool
	excludedChildren map[*Node]bool
	owners           connectedOwners
	seen             map[*Node]bool
	queue            []*Node
}

var connectedSetPool = typedpool.New(func() *connectedSetScratch {
	return &connectedSetScratch{
		excluded:         make(map[*Node]bool),
		excludedChildren: make(map[*Node]bool),
		owners:           connectedOwners{first: make(map[*Node]*Node)},
		seen:             make(map[*Node]bool),
	}
})

const maxPooledConnectedSet = 4096

// retainable reports whether every map and slice of the scratch is within
// maxPooledConnectedSet (TinyGo's sync.Pool never frees what it holds).
func (scratch *connectedSetScratch) retainable() bool {
	const limit = maxPooledConnectedSet
	return len(scratch.seen) <= limit && len(scratch.owners.first) <= limit && len(scratch.owners.more) <= limit &&
		len(scratch.excluded) <= limit && len(scratch.excludedChildren) <= limit && cap(scratch.queue) <= limit
}

func (scratch *connectedSetScratch) release() {
	if !scratch.retainable() {
		return
	}
	clear(scratch.excluded)
	clear(scratch.excludedChildren)
	clear(scratch.owners.first)
	clear(scratch.owners.more)
	clear(scratch.seen)
	// The whole capacity: ConnectedNodeSet stores its queue back only when it
	// returns normally, so after a panic the entries it appended lie beyond
	// the stored length.
	clear(scratch.queue[:cap(scratch.queue)])
	scratch.queue = scratch.queue[:0]
	connectedSetPool.Put(scratch)
}

// ConnectedNodeSet returns the same reachable set as ConnectedNodes, with no
// traversal-order guarantee. Use it only when all members are treated equally;
// order-sensitive nearest-node tie breaking must continue using ConnectedNodes.
func (node *Node) ConnectedNodeSet(excludedNodes []*Node, graph *Graph) []*Node {
	// Tiny graphs and flat graphs have too little ownership scanning to amortize
	// an index. Keep their existing traversal.
	if len(graph.Nodes) < 32 || len(graph.Clusters) == 0 && len(graph.Containers) <= 1 {
		return node.connectedNodes(excludedNodes, graph)
	}
	scratch := connectedSetPool.Get()
	defer scratch.release()
	excluded := scratch.excluded
	for _, n := range excludedNodes {
		excluded[n] = true
	}
	excludedChildren := scratch.excludedChildren
	owners := &scratch.owners
	for container, children := range graph.Containers {
		if container == nil {
			continue
		}
		blocked := false
		for _, child := range children {
			if excluded[child] {
				blocked = true
				break
			}
		}
		excludedChildren[container] = blocked
		if blocked || excluded[container] {
			continue
		}
		for _, child := range children {
			owners.add(child, container)
		}
	}
	for vessel, cluster := range graph.Clusters {
		for _, member := range cluster.Nodes {
			if !member.isContainer || excludedChildren[member] {
				continue
			}
			for _, child := range graph.Containers[member] {
				owners.add(child, vessel)
			}
		}
	}
	nodes := make([]*Node, 0)
	queue := append(scratch.queue[:0], node)
	seen := scratch.seen
	seen[node] = true
	add := func(n *Node) {
		if !seen[n] {
			seen[n] = true
			queue = append(queue, n)
		}
	}
	for head := 0; head < len(queue); head++ {
		current := queue[head]
		blocked := false
		for _, e := range excludedNodes {
			if current.isDescendantOf(e) || e.isDescendantOf(current) && !node.isDescendantOf(current) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		nodes = append(nodes, current)
		for _, edge := range current.Edges {
			adjacent := current.adjacent(edge)
			if !excluded[adjacent] {
				add(adjacent)
			}
		}
		if current.isClusterVessel {
			for _, member := range graph.Clusters[current].Nodes {
				if member.isContainer {
					for _, child := range graph.Containers[member] {
						add(child)
					}
				}
			}
		}
		if owner := owners.first[current]; owner != nil {
			add(owner)
		}
		for _, owner := range owners.more[current] {
			add(owner)
		}
		if current != nil && !excluded[current] && !excludedChildren[current] {
			for _, child := range graph.Containers[current] {
				add(child)
			}
		}
	}
	scratch.queue = queue
	return nodes
}
