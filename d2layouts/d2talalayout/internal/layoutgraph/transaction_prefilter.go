package layoutgraph

import (
	"slices"

	"github.com/d2lang/d2/lib/geo"
)

// placementPrefilter is a transaction's scratch for PlacementRejected,
// indexed by snapshot slot.
type placementPrefilter struct {
	stamps     []uint32
	trialIndex []int32
	generation uint32
	movers     []placementMover
	ancestors  []*Node
}

type placementMover struct {
	node *Node
	// trial is the mover's index in the trial (moving and topLefts).
	trial int
}

// PlacementRejected reports whether Commit is certain to reject a trial whose
// operations do nothing but move each node moving[k] so that its top-left
// becomes topLefts[k], by updating its TopLeft point in place (as Translate
// and MoveAbsWithChildren do for a node without descendants): no other node
// moves, nothing is resized, and no structure, cluster or sequence changes.
// The graph must be in the state the trial's Commit starts from. A false
// answer means "commit to find out"; the method changes nothing.
//
// It answers true only when it finds a node n of moving and a node o of
// g.Nodes outside moving such that, in the trial's final state, Commit's
// post-state overlap check of n reports o:
//   - n and o are distinct g.Nodes entries with finite boxes; n has no
//     descendants and belongs to no cluster or sequence, and n's top-left is
//     not its rollback-point top-left, so n is a changed node and the pair is
//     among the checked candidates;
//   - the graph has no clusters and no sequences, so the cluster and
//     sequence synchronization after each operation moves nothing; without
//     AffectContainers no container changes either, and with it o has no
//     children (only containers are re-wrapped);
//   - neither n's nor o's TopLeft point is shared with another recorded node
//     (the operations and the re-wraps move only the points they own);
//   - o is not an ancestor of n (the exceptions of a node without
//     descendants), the exception index Commit will use names o at o's
//     position, and o is not among n's existing near overlaps;
//   - the pair passes the 500-unit broad phase and overlaps with n's padding
//     (deltaTo at n's new top-left), exactly as hasBadPostStateOverlap tests.
//
// On a graph that satisfies the engine's structural invariants (Validate)
// such a Commit returns ErrInvalidCandidate, unless its work budget or its
// context ends it first, so a caller that skips the trial changes only the
// work charged to its budgets. Malformed geometry or structure (a nil node, a
// non-finite box, an incomplete edge or child, an ancestry cycle) makes the
// answer false; whatever else is malformed is reported by the next Commit
// that runs instead of by this skipped one.
func (t *Transaction) PlacementRejected(moving []*Node, topLefts []geo.Point) bool {
	if t == nil || t.Graph == nil || len(moving) == 0 || len(moving) != len(topLefts) {
		return false
	}
	g := t.Graph
	state := t.PriorGraphState
	if state == nil || state.captureTopology || state.existingOverlaps == nil {
		return false
	}
	if len(g.Clusters) != 0 || len(g.Sequences) != 0 {
		return false
	}
	slots := &state.slots
	if g.prefilter == nil {
		g.prefilter = &placementPrefilter{}
	}
	p := g.prefilter
	if size := slots.size(); len(p.stamps) < size {
		p.stamps = make([]uint32, max(size, 2*len(p.stamps)))
		p.trialIndex = make([]int32, len(p.stamps))
		p.generation = 0
	}
	p.generation++
	if p.generation == 0 {
		clear(p.stamps)
		p.generation = 1
	}
	generation := p.generation
	for k, node := range moving {
		slot := slots.slotOf(node)
		// A node outside the snapshot, or one listed twice (the trial would
		// move it twice), is not predicted.
		if slot < 0 || p.stamps[slot] == generation {
			return false
		}
		p.stamps[slot] = generation
		p.trialIndex[slot] = int32(k)
	}
	isMoving := func(node *Node) bool {
		slot := slots.slotOf(node)
		return slot >= 0 && p.stamps[slot] == generation
	}

	nodes := g.Nodes
	p.movers = p.movers[:0]
	for _, node := range nodes {
		if node == nil {
			// The structural check reports a nil node.
			return false
		}
		if slot := slots.slotOf(node); slot >= 0 && p.stamps[slot] == generation {
			p.movers = append(p.movers, placementMover{node: node, trial: int(p.trialIndex[slot])})
		}
	}
	if len(p.movers) == 0 {
		return false
	}
	// The next dirty-path Commit reuses the exception index when it is ready
	// and g.Nodes has the rollback point's order (see
	// buildPostStateOverlapCandidates); otherwise it rebuilds it from g.Nodes.
	reuseIndex := t.indexOrderReady && state.hasOriginalNodeOrder(g)
	set := state.existingOverlaps
	for _, mover := range p.movers {
		n := mover.node
		if n.TopLeft == nil || n.Cluster != nil || n.Sequence != nil || n.isContainer || n.isClusterVessel ||
			len(g.Containers[n]) != 0 {
			continue
		}
		at := topLefts[mover.trial]
		x, y, width, height := at.X, at.Y, n.Width, n.Height
		right, bottom := x+width, y+height
		// transactionNodeHasFiniteBox at the new top-left.
		if !((x-x)+(y-y)+(width-width)+(height-height)+(right-right)+(bottom-bottom) == 0 &&
			width >= 0 && height >= 0) {
			continue
		}
		prior := slots.entries[slots.slotOf(n)].geometry
		if prior.topLeft.pointer != nil && x == prior.topLeft.value.X && y == prior.topLeft.value.Y &&
			width == prior.width && height == prior.height {
			// Not a changed node: its pairs need not be candidates.
			continue
		}
		ancestors, ok := t.prefilterAncestors(n)
		if !ok {
			return false
		}
		var near []int32
		if index := set.indexOf(n); index >= 0 {
			near = set.near[index]
		}
		atPoint := geo.Point{X: x, Y: y}
		for j, o := range nodes {
			if o == n || o.TopLeft == nil || isMoving(o) {
				continue
			}
			if t.options.AffectContainers && (o.isContainer || len(g.Containers[o]) != 0) {
				continue
			}
			left, top := o.TopLeft.X, o.TopLeft.Y
			oRight, oBottom := left+o.Width, top+o.Height
			if !((left-left)+(top-top)+(o.Width-o.Width)+(o.Height-o.Height)+(oRight-oRight)+(oBottom-oBottom) == 0 &&
				o.Width >= 0 && o.Height >= 0) {
				continue
			}
			// transactionBoxesMayInteract
			const maxSafeDelta = 500.0
			if x > oRight+maxSafeDelta || right+maxSafeDelta < left ||
				y > oBottom+maxSafeDelta || bottom+maxSafeDelta < top {
				continue
			}
			if slices.Contains(ancestors, o) {
				continue
			}
			if reuseIndex && (j >= len(t.indexOrder) || t.indexOrder[j] != o) {
				continue
			}
			if len(near) != 0 {
				if index := set.indexOf(o); index >= 0 && slices.Contains(near, int32(index)) {
					continue
				}
			}
			deltaValue, _, err := n.deltaToCounted(o, &atPoint)
			if err != nil {
				return false
			}
			delta := float64(deltaValue)
			if x < oRight+delta && right+delta > left && y < oBottom+delta && bottom+delta > top {
				if t.ownsTopLeft(n) && t.ownsTopLeft(o) {
					return true
				}
			}
		}
	}
	return false
}

// prefilterAncestors returns node's ancestors as appendAncestorsGuarded walks
// them (the exceptions Commit marks for a node without descendants), or false
// where that walk would fail (a cycle) or is longer than any valid graph's.
// The result is scratch, valid until the next call.
func (t *Transaction) prefilterAncestors(node *Node) ([]*Node, bool) {
	p := t.Graph.prefilter
	ancestors := p.ancestors[:0]
	current := node
	for {
		switch {
		case current.Container != nil:
			current = current.Container
		case current.Cluster != nil:
			current = current.Cluster.Vessel
		case current.Sequence != nil:
			current = current.Sequence.Vessel
		default:
			p.ancestors = ancestors
			return ancestors, true
		}
		if current == nil {
			p.ancestors = ancestors
			return ancestors, true
		}
		if current == node || slices.Contains(ancestors, current) || len(ancestors) >= maxEngineTopologyDepth {
			p.ancestors = ancestors
			return nil, false
		}
		ancestors = append(ancestors, current)
	}
}

// ownsTopLeft reports whether no other recorded node shares node's TopLeft
// point.
func (t *Transaction) ownsTopLeft(node *Node) bool {
	point := node.TopLeft
	entries := t.PriorGraphState.slots.entries
	for i := range entries {
		if other := entries[i].node; other != node && other.TopLeft == point {
			return false
		}
	}
	return true
}
