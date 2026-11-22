package placement

import (
	"fmt"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// optimizerMoveScratch is an optimizer's reusable storage for the descendant
// walks, position snapshots and moves of nodes that carry descendants
// (containers, cluster and sequence vessels). The functions below are
// optimizerDescendants, captureOptimizerNodePositions, optimizerMoveNodeAbs
// and withOptimizerPositionsSwapped with the same guard steps, errors and
// results; only where their slices and sets live differs.
//
// Ownership: descendants (the result of descendantsInto) is valid until the
// next descendant walk; swapped holds withOptimizerPositionsSwappedScratch's
// rollback point for the duration of its callback; movement holds a
// candidate movement's snapshot for the duration of one moveNodeToBest.
type optimizerMoveScratch struct {
	seen        map[*layoutgraph.Node]struct{}
	stack       []*layoutgraph.Node
	descendants []*layoutgraph.Node
	captureSeen map[*layoutgraph.Node]struct{}
	all         []*layoutgraph.Node
	moved       []nodePositionSnapshot // optimizerMoveNodeAbs's rollback point
	swapped     []nodePositionSnapshot
	movement    []nodePositionSnapshot
}

// descendantsInto is optimizerDescendants with scratch storage; the result
// is scratch.descendants.
func (scratch *optimizerMoveScratch) descendantsInto(node *layoutgraph.Node, guard *limits.OptimizationWorkGuard) ([]*layoutgraph.Node, error) {
	if node == nil || node.Graph == nil {
		return nil, fmt.Errorf("TALA %s movement requires a node with a graph", guard.Location())
	}
	g := node.Graph
	if scratch.seen == nil {
		scratch.seen = make(map[*layoutgraph.Node]struct{})
	} else {
		clear(scratch.seen)
	}
	seen := scratch.seen
	seen[node] = struct{}{}
	stack := scratch.stack[:0]
	defer func() {
		clear(stack)
		scratch.stack = stack[:0]
	}()
	push := func(child *layoutgraph.Node) error {
		if err := guard.Step(); err != nil {
			return err
		}
		if child == nil {
			return fmt.Errorf("TALA %s found a nil descendant", guard.Location())
		}
		if _, exists := seen[child]; exists {
			return nil
		}
		if len(seen) >= limits.MaxEngineNodes {
			return fmt.Errorf("TALA %s descendant count exceeds limit %d", guard.Location(), limits.MaxEngineNodes)
		}
		seen[child] = struct{}{}
		stack = append(stack, child)
		return nil
	}
	pushChildren := func(parent *layoutgraph.Node) error {
		if sequence := g.Sequences[parent]; sequence != nil {
			if len(sequence.Nodes) > limits.MaxEngineNodes {
				return fmt.Errorf("TALA %s sequence child references exceed node limit %d", guard.Location(), limits.MaxEngineNodes)
			}
			for _, v := range slices.Backward(sequence.Nodes) {
				if err := push(v); err != nil {
					return err
				}
			}
		}
		if parent != nil && parent.IsClusterVessel() {
			if cluster := g.Clusters[parent]; cluster != nil {
				if len(cluster.Nodes) > limits.MaxEngineNodes {
					return fmt.Errorf("TALA %s cluster child references exceed node limit %d", guard.Location(), limits.MaxEngineNodes)
				}
				for _, v := range slices.Backward(cluster.Nodes) {
					if err := push(v); err != nil {
						return err
					}
				}
			}
		}
		if parent == nil || parent.IsContainer() {
			children := g.Containers[parent]
			if len(children) > limits.MaxEngineNodes {
				return fmt.Errorf("TALA %s container child references exceed node limit %d", guard.Location(), limits.MaxEngineNodes)
			}
			for _, c := range slices.Backward(children) {
				if err := push(c); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := pushChildren(node); err != nil {
		return nil, err
	}
	descendants := scratch.descendants[:0]
	for len(stack) > 0 {
		if err := guard.Step(); err != nil {
			scratch.descendants = descendants
			return nil, err
		}
		last := len(stack) - 1
		current := stack[last]
		stack[last] = nil
		stack = stack[:last]
		descendants = append(descendants, current)
		if err := pushChildren(current); err != nil {
			scratch.descendants = descendants
			return nil, err
		}
	}
	scratch.descendants = descendants
	return descendants, nil
}

// captureInto is captureOptimizerNodePositions with scratch storage; the
// snapshots are written to dst (reused from length 0) and returned.
func (scratch *optimizerMoveScratch) captureInto(nodes []*layoutgraph.Node, dst []nodePositionSnapshot, guard *limits.OptimizationWorkGuard) ([]nodePositionSnapshot, error) {
	snapshots := dst[:0]
	if len(nodes) <= 2 {
		leaves := true
		for _, node := range nodes {
			if node == nil {
				return nil, fmt.Errorf("TALA %s found a nil node", guard.Location())
			}
			if node.Graph == nil || node.IsContainer() || node.IsClusterVessel() || node.Graph.Sequences[node] != nil {
				leaves = false
				break
			}
		}
		if leaves {
			for _, node := range nodes {
				duplicate := false
				for _, snapshot := range snapshots {
					if snapshot.node == node {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				if err := guard.Step(); err != nil {
					return nil, err
				}
				snapshots = append(snapshots, nodePositionSnapshot{node: node, topLeft: snapshotPointer(node.TopLeft)})
			}
			return snapshots, nil
		}
	}
	if len(nodes) == 1 {
		descendants, err := scratch.descendantsInto(nodes[0], guard)
		if err != nil {
			return nil, err
		}
		snapshots = slices.Grow(snapshots, len(descendants)+1)[:len(descendants)+1]
		for i := range snapshots {
			if err := guard.Step(); err != nil {
				clear(snapshots)
				return nil, err
			}
			node := nodes[0]
			if i > 0 {
				node = descendants[i-1]
			}
			snapshots[i] = nodePositionSnapshot{node: node, topLeft: snapshotPointer(node.TopLeft)}
		}
		return snapshots, nil
	}
	if scratch.captureSeen == nil {
		scratch.captureSeen = make(map[*layoutgraph.Node]struct{})
	} else {
		clear(scratch.captureSeen)
	}
	seen := scratch.captureSeen
	defer clear(seen)
	for _, node := range nodes {
		if node == nil {
			return nil, fmt.Errorf("TALA %s found a nil node", guard.Location())
		}
		all := append(scratch.all[:0], node)
		descendants, err := scratch.descendantsInto(node, guard)
		if err != nil {
			scratch.all = all
			return nil, err
		}
		all = append(all, descendants...)
		scratch.all = all
		for _, current := range all {
			if _, exists := seen[current]; exists {
				continue
			}
			if len(seen) >= limits.MaxEngineNodes {
				return nil, fmt.Errorf("TALA %s position snapshot exceeds node limit %d", guard.Location(), limits.MaxEngineNodes)
			}
			if err := guard.Step(); err != nil {
				return nil, err
			}
			seen[current] = struct{}{}
			snapshots = append(snapshots, nodePositionSnapshot{node: current, topLeft: snapshotPointer(current.TopLeft)})
		}
	}
	return snapshots, nil
}

// moveAbs is optimizerMoveNodeAbs with scratch storage.
func (scratch *optimizerMoveScratch) moveAbs(node *layoutgraph.Node, x, y float64, guard *limits.OptimizationWorkGuard) error {
	if node == nil || node.TopLeft == nil {
		return fmt.Errorf("TALA %s cannot move an unpositioned node", guard.Location())
	}
	if node.TopLeft.X == x && node.TopLeft.Y == y {
		return guard.Step()
	}
	if node.Graph != nil && !node.IsContainer() && !node.IsClusterVessel() && node.Graph.Sequences[node] == nil {
		node.Translate(x-node.TopLeft.X, y-node.TopLeft.Y)
		return nil
	}
	descendants, err := scratch.descendantsInto(node, guard)
	if err != nil {
		return err
	}
	snapshots := append(scratch.moved[:0], nodePositionSnapshot{node: node, topLeft: snapshotPointer(node.TopLeft)})
	defer func() {
		clear(snapshots)
		scratch.moved = snapshots[:0]
	}()
	for _, child := range descendants {
		if err := guard.Step(); err != nil {
			return err
		}
		if child.TopLeft == nil {
			return fmt.Errorf("TALA %s cannot move an unpositioned descendant", guard.Location())
		}
		snapshots = append(snapshots, nodePositionSnapshot{node: child, topLeft: snapshotPointer(child.TopLeft)})
	}
	complete := false
	defer func() {
		if !complete {
			restoreNodePositions(snapshots)
		}
	}()
	dx, dy := x-node.TopLeft.X, y-node.TopLeft.Y
	node.Translate(dx, dy)
	for _, child := range descendants {
		if err := guard.Step(); err != nil {
			return err
		}
		child.Translate(dx, dy)
	}
	complete = true
	return nil
}

// withPositionsSwapped is withOptimizerPositionsSwapped with scratch storage.
func (scratch *optimizerMoveScratch) withPositionsSwapped(nodeA, nodeB *layoutgraph.Node, guard *limits.OptimizationWorkGuard, fn func() error) (err error) {
	if optimizerLeaf(nodeA) && optimizerLeaf(nodeB) {
		return withOptimizerLeavesSwapped(nodeA, nodeB, guard, fn)
	}
	before := guard.Used()
	pair := [2]*layoutgraph.Node{nodeA, nodeB}
	snapshots, err := scratch.captureInto(pair[:], scratch.swapped, guard)
	if err != nil {
		return err
	}
	scratch.swapped = snapshots
	captureWork := guard.Used() - before
	defer func() {
		restoreNodePositions(snapshots)
		clear(snapshots)
		scratch.swapped = snapshots[:0]
	}()
	if nodeA == nil || nodeB == nil || nodeA.TopLeft == nil || nodeB.TopLeft == nil {
		return fmt.Errorf("TALA %s cannot swap unpositioned nodes", guard.Location())
	}
	for range captureWork {
		if err := guard.Step(); err != nil {
			return err
		}
	}
	ax, ay := nodeA.TopLeft.X, nodeA.TopLeft.Y
	if err := scratch.moveAbs(nodeA, nodeB.TopLeft.X, nodeB.TopLeft.Y, guard); err != nil {
		return err
	}
	if err := scratch.moveAbs(nodeB, ax, ay, guard); err != nil {
		return err
	}
	return fn()
}

// candidateMovement is captureOptimizerCandidateMovement with its snapshot
// in scratch.movement.
func (scratch *optimizerMoveScratch) candidateMovement(node *layoutgraph.Node, guard *limits.OptimizationWorkGuard) (optimizerCandidateMovement, error) {
	before := guard.Used()
	single := [1]*layoutgraph.Node{node}
	positions, err := scratch.captureInto(single[:], scratch.movement, guard)
	if err != nil {
		return optimizerCandidateMovement{}, err
	}
	scratch.movement = positions
	return optimizerCandidateMovement{
		positions:      positions,
		descendantWork: guard.Used() - before - uint64(len(positions)),
	}, nil
}

// descendantsOutside reports whether none of node's descendants (as
// optimizerDescendants finds them) is the node of one of entries, and the
// walk would succeed (no nil child, no limit); it charges nothing.
func (scratch *optimizerMoveScratch) descendantsOutside(node *layoutgraph.Node, entries []optimizerScanEntry) bool {
	g := node.Graph
	if g == nil {
		return false
	}
	if scratch.seen == nil {
		scratch.seen = make(map[*layoutgraph.Node]struct{})
	} else {
		clear(scratch.seen)
	}
	seen := scratch.seen
	defer clear(seen)
	seen[node] = struct{}{}
	stack := scratch.stack[:0]
	defer func() {
		clear(stack)
		scratch.stack = stack[:0]
	}()
	ok := true
	push := func(children []*layoutgraph.Node) {
		if len(children) > limits.MaxEngineNodes {
			ok = false
			return
		}
		for _, child := range children {
			if child == nil {
				ok = false
				return
			}
			if _, exists := seen[child]; exists {
				continue
			}
			if len(seen) >= limits.MaxEngineNodes {
				ok = false
				return
			}
			seen[child] = struct{}{}
			stack = append(stack, child)
		}
	}
	pushChildren := func(parent *layoutgraph.Node) {
		if sequence := g.Sequences[parent]; sequence != nil {
			push(sequence.Nodes)
		}
		if ok && parent != nil && parent.IsClusterVessel() {
			if cluster := g.Clusters[parent]; cluster != nil {
				push(cluster.Nodes)
			}
		}
		if ok && (parent == nil || parent.IsContainer()) {
			push(g.Containers[parent])
		}
	}
	pushChildren(node)
	for ok && len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack[last] = nil
		stack = stack[:last]
		pushChildren(current)
	}
	if !ok {
		return false
	}
	for i := range entries {
		other := entries[i].node
		if other == node {
			continue
		}
		if _, descendant := seen[other]; descendant {
			return false
		}
	}
	return true
}
