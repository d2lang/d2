package placementcost

import (
	"context"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
	"github.com/d2lang/d2/lib/geo"
)

// symmetryFrame holds the buffers of one nodeSymmetry level. They replace a
// pooled bool slice, a pooled node slice with a membership map, a second
// pooled bool slice and an allocated child slice per call.
type symmetryFrame struct {
	used     []bool
	children []*layoutgraph.Node
	deduped  []*layoutgraph.Node
	matched  []bool
	// set mirrors children ∪ deduped once they hold more than
	// symmetrySetThreshold nodes; below that a linear scan is cheaper.
	set   map[*layoutgraph.Node]struct{}
	setOn bool
	// High-water lengths and map use since the workspace was last put back:
	// only these entries can hold node references, so only they are cleared.
	childrenHigh, dedupedHigh int
	setUsed                   bool
}

const maxPooledSymmetryFrameEntries = 4096

// maxPooledSymmetryWorkspaceEntries bounds a pooled workspace as a whole.
const maxPooledSymmetryWorkspaceEntries = 4 * maxPooledSymmetryFrameEntries

// symmetrySetThreshold is a variable only so tests can force either
// membership representation; both give identical answers.
var symmetrySetThreshold = 32

// symmetryWorkspace is a stack of frames for one top-level symmetry
// evaluation (the recursion over container children and neighbors).
type symmetryWorkspace struct {
	frames []*symmetryFrame
	depth  int
	// memoOn enables memo: neighbor symmetry (checkNeighbors false) already
	// computed in the same EdgeLength evaluation. See memoNodeSymmetry.
	memoOn   bool
	memoUsed bool
	memo     map[*layoutgraph.Node]float64
}

var symmetryWorkspacePool = typedpool.New(func() *symmetryWorkspace { return new(symmetryWorkspace) })

func getSymmetryWorkspace() *symmetryWorkspace {
	ws := symmetryWorkspacePool.Get()
	ws.depth = 0
	return ws
}

// retainable reports whether the workspace is small enough to pool: the
// frames' buffers and the memo together within
// maxPooledSymmetryWorkspaceEntries (the frame count follows the symmetry
// recursion's depth, so per-frame bounds alone do not bound it). TinyGo's
// sync.Pool (d2's wasm build) never frees what it holds.
func (ws *symmetryWorkspace) retainable() bool {
	total := len(ws.memo)
	for _, f := range ws.frames {
		total += cap(f.used) + cap(f.children) + cap(f.deduped) + cap(f.matched) + len(f.set)
	}
	return total <= maxPooledSymmetryWorkspaceEntries
}

func putSymmetryWorkspace(ws *symmetryWorkspace) {
	if !ws.retainable() {
		return
	}
	for _, f := range ws.frames {
		if cap(f.children) > maxPooledSymmetryFrameEntries {
			f.children = nil
		} else {
			clear(f.children[:f.childrenHigh])
			f.children = f.children[:0]
		}
		if cap(f.deduped) > maxPooledSymmetryFrameEntries {
			f.deduped = nil
		} else {
			clear(f.deduped[:f.dedupedHigh])
			f.deduped = f.deduped[:0]
		}
		f.childrenHigh, f.dedupedHigh = 0, 0
		if cap(f.used) > maxPooledSymmetryFrameEntries {
			f.used = nil
		}
		if cap(f.matched) > maxPooledSymmetryFrameEntries {
			f.matched = nil
		}
		if f.setUsed {
			if len(f.set) > maxPooledSymmetryFrameEntries {
				f.set = nil
			} else {
				clear(f.set)
			}
			f.setUsed = false
		}
		f.setOn = false
	}
	ws.depth = 0
	if ws.memoUsed {
		if len(ws.memo) > maxPooledSymmetryFrameEntries {
			ws.memo = nil
		} else {
			clear(ws.memo)
		}
		ws.memoUsed = false
	}
	ws.memoOn = false
	symmetryWorkspacePool.Put(ws)
}

func clearedBools(values []bool, length int) []bool {
	if cap(values) < length {
		return make([]bool, length)
	}
	values = values[:length]
	clear(values)
	return values
}

func (ws *symmetryWorkspace) push(abductions int) *symmetryFrame {
	if ws.depth == len(ws.frames) {
		ws.frames = append(ws.frames, new(symmetryFrame))
	}
	f := ws.frames[ws.depth]
	ws.depth++
	f.used = clearedBools(f.used, abductions)
	f.children = f.children[:0]
	f.deduped = f.deduped[:0]
	if f.setOn {
		clear(f.set)
		f.setOn = false
	}
	return f
}

func (ws *symmetryWorkspace) pop() {
	ws.depth--
}

// added reports whether node is in children ∪ deduped, the set the original
// code kept in its "added" map.
func (f *symmetryFrame) added(node *layoutgraph.Node) bool {
	if f.setOn {
		_, ok := f.set[node]
		return ok
	}
	for _, child := range f.children {
		if child == node {
			return true
		}
	}
	for _, adjacent := range f.deduped {
		if adjacent == node {
			return true
		}
	}
	return false
}

func (f *symmetryFrame) addChild(node *layoutgraph.Node) {
	f.children = append(f.children, node)
	f.childrenHigh = max(f.childrenHigh, len(f.children))
	f.noteAdded(node)
}

func (f *symmetryFrame) addAdjacent(node *layoutgraph.Node) {
	f.deduped = append(f.deduped, node)
	f.dedupedHigh = max(f.dedupedHigh, len(f.deduped))
	f.noteAdded(node)
}

func (f *symmetryFrame) noteAdded(node *layoutgraph.Node) {
	if f.setOn {
		f.set[node] = struct{}{}
		return
	}
	if len(f.children)+len(f.deduped) <= symmetrySetThreshold {
		return
	}
	if f.set == nil {
		f.set = make(map[*layoutgraph.Node]struct{}, 2*symmetrySetThreshold)
	}
	for _, child := range f.children {
		f.set[child] = struct{}{}
	}
	for _, adjacent := range f.deduped {
		f.set[adjacent] = struct{}{}
	}
	f.setOn = true
	f.setUsed = true
}

// NodesSymmetry sums local symmetry scores for nodes in order.
func NodesSymmetry(ctx context.Context, nodes layoutgraph.Nodes, edgeAbductions []*layoutgraph.EdgeAbduction) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	p.symmetry = getSymmetryWorkspace()
	defer putSymmetryWorkspace(p.symmetry)
	sum := 0.0
	for i, node := range nodes {
		if err := p.at(i); err != nil {
			return 0, err
		}
		symmetry, err := pollNodeSymmetry(&p, node, edgeAbductions, true)
		if err != nil {
			return 0, err
		}
		sum += symmetry
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	return sum, nil
}

// NodeSymmetry evaluates a node and the neighboring symmetry it contributes to.
func NodeSymmetry(ctx context.Context, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	p.symmetry = getSymmetryWorkspace()
	defer putSymmetryWorkspace(p.symmetry)
	return nodeSymmetryPolled(&p, node, edgeAbductions, true)
}

// memoNodeSymmetry is pollNodeSymmetry, reusing a neighbor symmetry
// (checkNeighbors false) computed earlier in the same EdgeLength evaluation
// when the workspace memoizes. nodeSymmetry is a deterministic function of the
// graph, the abductions and its arguments, none of which change during that
// evaluation, so the reused value is the value the call would compute. The
// memo is only enabled for contexts with a Done channel, which cannot observe
// the skipped polling points (see scoringPoll).
func memoNodeSymmetry(p *scoringPoll, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	if p.sess != nil {
		return p.sess.symmetry(p, node, edgeAbductions, checkNeighbors)
	}
	ws := p.symmetry
	if checkNeighbors || !ws.memoOn {
		return pollNodeSymmetry(p, node, edgeAbductions, checkNeighbors)
	}
	if value, ok := ws.memo[node]; ok {
		return value, nil
	}
	value, err := pollNodeSymmetry(p, node, edgeAbductions, false)
	if err != nil {
		return 0, err
	}
	if ws.memo == nil {
		ws.memo = make(map[*layoutgraph.Node]float64)
	}
	ws.memo[node] = value
	ws.memoUsed = true
	return value, nil
}

// pollNodeSymmetry is nodeSymmetry within an evaluation that already polls.
func pollNodeSymmetry(p *scoringPoll, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	if err := p.check(); err != nil {
		return 0, err
	}
	return nodeSymmetryPolled(p, node, edgeAbductions, checkNeighbors)
}

// nodeSymmetryPolled evaluates node's symmetry in a new workspace frame. The
// frame is popped without defer (the evaluation has too many returns for an
// open-coded defer): a panic leaves the depth raised, which only selects
// which pooled, cleared-on-push frames later levels use, and every workspace
// is reset when it is put back.
func nodeSymmetryPolled(p *scoringPoll, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	frame := p.symmetry.push(len(edgeAbductions))
	value, err := nodeSymmetryInFrame(p, frame, node, edgeAbductions, checkNeighbors)
	p.symmetry.pop()
	return value, err
}

func nodeSymmetryInFrame(p *scoringPoll, frame *symmetryFrame, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	score := 0.0
	maxScore := 0

	usedEdgeAbductions := frame.used

	// We consider container children with protruding edges like neighbors for symmetry, their score matters for the container score
	if node.IsContainer() {
		// Within node placement, we use edge abductions to discern children; otherwise, the AdjacentNodes are correct
		if edgeAbductions == nil {
			for i, child := range node.Graph.Containers[node] {
				if err := p.at(i); err != nil {
					return 0, err
				}
				for j, e := range child.Edges {
					if err := p.at(j); err != nil {
						return 0, err
					}
					adj := child.Adjacent(e)
					// We only want to consider symmetry of child nodes with protrutions
					if !adj.Container.IsDescendantOf(node) {
						frame.addChild(child)
						break
					}
				}
			}
		} else {
			for i, edgeAbduction := range edgeAbductions {
				if err := p.at(i); err != nil {
					return 0, err
				}
				if usedEdgeAbductions[i] {
					continue
				}
				if edgeAbduction.CurrentFrom == node {
					if edgeAbduction.OriginallyFrom != nil {
						usedEdgeAbductions[i] = true
						if frame.added(edgeAbduction.OriginallyFrom) {
							continue
						}
						frame.addChild(edgeAbduction.OriginallyFrom)
					}
				}
				if edgeAbduction.CurrentTo == node {
					if edgeAbduction.OriginallyTo != nil {
						usedEdgeAbductions[i] = true
						if frame.added(edgeAbduction.OriginallyTo) {
							continue
						}
						frame.addChild(edgeAbduction.OriginallyTo)
					}
				}
			}
		}

		for i, nodeReplacement := range frame.children {
			if err := p.at(i); err != nil {
				return 0, err
			}
			symmetry, err := memoNodeSymmetry(p, nodeReplacement, edgeAbductions, checkNeighbors)
			if err != nil {
				return 0, err
			}
			score += symmetry
			maxScore++
		}
	}

	// If a node has multiple edges to node A on one side, and only 1 edge to node B on another symmetric side, count that as symmetrical
	//   ┌───────────┐
	//   ▼           │
	// ┌──┐        ┌─┴┐       ┌──┐
	// │  │◄───────┤  ├──────►│  │
	// └──┘        └──┘       └──┘
	for edgeIndex, e := range node.Edges {
		if err := p.at(edgeIndex); err != nil {
			return 0, err
		}
		adjacentNode := node.Adjacent(e)
		adjacentNodeReplacement := adjacentNode

		// Let's say A and A's container are both connected to B
		// ┌──────────┐
		// │          │
		// │   ┌───┐  │  ┌───┐
		// │   │ A ├──┼─►│B  │
		// │   └───┘  │  └───┘
		// │          │    ▲
		// └──────┬───┘    │
		//        │        │
		//        └────────┘
		// We've already considered A's symmetry above, so we should only count B as an adjacent node if it's not connected to a child
		isConnectedToChild := false
		if node.IsContainer() {
			for i, edgeAbduction := range edgeAbductions {
				if err := p.at(i); err != nil {
					return 0, err
				}
				if (edgeAbduction.CurrentFrom == node) && (edgeAbduction.CurrentTo == adjacentNode) {
					if edgeAbduction.OriginallyFrom != nil {
						isConnectedToChild = true
						break
					}
				}
				if (edgeAbduction.CurrentFrom == adjacentNode) && (edgeAbduction.CurrentTo == node) {
					usedEdgeAbductions[i] = true
					if edgeAbduction.OriginallyTo != nil {
						isConnectedToChild = true
						break
					}
				}
			}
		}
		if isConnectedToChild {
			continue
		}

		// If we're considering symmetry for node B in the above diagram, we'll want to resolve to adjacent node to A, not container for A
		for i, edgeAbduction := range edgeAbductions {
			if err := p.at(i); err != nil {
				return 0, err
			}
			if usedEdgeAbductions[i] {
				continue
			}
			// For symmetry, use cluster vessels
			if edgeAbduction.CurrentFrom != nil && edgeAbduction.CurrentFrom.IsClusterVessel() {
				continue
			}
			if edgeAbduction.CurrentTo != nil && edgeAbduction.CurrentTo.IsClusterVessel() {
				continue
			}
			if node.Graph.IsSequenceVessel(edgeAbduction.CurrentFrom) || node.Graph.IsSequenceVessel(edgeAbduction.CurrentTo) {
				continue
			}
			if (edgeAbduction.CurrentFrom == node) && (edgeAbduction.CurrentTo == adjacentNode) {
				usedEdgeAbductions[i] = true
				if edgeAbduction.OriginallyTo != nil {
					adjacentNodeReplacement = edgeAbduction.OriginallyTo
				}
				break
			}
			if (edgeAbduction.CurrentFrom == adjacentNode) && (edgeAbduction.CurrentTo == node) {
				usedEdgeAbductions[i] = true
				if edgeAbduction.OriginallyFrom != nil {
					adjacentNodeReplacement = edgeAbduction.OriginallyFrom
				}
				break
			}
		}

		// For clusters within containers, the Originally is pointed to the cluster node still
		//              ┌─────────┐
		//              │         │
		//              │         │
		//              │  ┌─┐    │
		// ┌──┐     ┌───┼─►│ │    │
		// │  ├─────┤   │  └─┘    │
		// └──┘     │   │         │
		//          │   │  ┌─┐    │
		//          └───┼─►│ │    │
		//              │  └─┘    │
		//              │         │
		//              └─────────┘
		if adjacentNodeReplacement.Cluster.IsActive() {
			adjacentNodeReplacement = adjacentNodeReplacement.Cluster.Vessel
		} else if adjacentNodeReplacement.Sequence.IsActive() {
			adjacentNodeReplacement = adjacentNodeReplacement.Sequence.Vessel
		}

		if frame.added(adjacentNodeReplacement) {
			continue
		}

		frame.addAdjacent(adjacentNodeReplacement)
	}

	// When we're resolving symmetry for children during node placement, all their edges were abducted to their containers, so here we find those
	for i, edgeAbduction := range edgeAbductions {
		if err := p.at(i); err != nil {
			return 0, err
		}
		var connectedNode *layoutgraph.Node
		if edgeAbduction.OriginallyFrom == node {
			// When the adjacent node is also a container
			if edgeAbduction.OriginallyTo != nil &&
				edgeAbduction.CurrentTo != nil &&
				!edgeAbduction.CurrentTo.IsClusterVessel() &&
				!node.Graph.IsSequenceVessel(edgeAbduction.CurrentTo) {
				connectedNode = edgeAbduction.OriginallyTo
			} else {
				connectedNode = edgeAbduction.CurrentTo
			}
		}
		if edgeAbduction.OriginallyTo == node {
			if edgeAbduction.OriginallyFrom != nil &&
				edgeAbduction.CurrentFrom != nil &&
				!edgeAbduction.CurrentFrom.IsClusterVessel() &&
				!node.Graph.IsSequenceVessel(edgeAbduction.CurrentFrom) {
				connectedNode = edgeAbduction.OriginallyFrom
			} else {
				connectedNode = edgeAbduction.CurrentFrom
			}
		}
		if connectedNode != nil {
			if frame.added(connectedNode) {
				continue
			}

			frame.addAdjacent(connectedNode)
		}
	}

	dedupedAdjacentNodes := frame.deduped
	for i := 0; i < len(dedupedAdjacentNodes); i++ {
		if err := p.at(i); err != nil {
			return 0, err
		}
		d := node.DistanceTo(dedupedAdjacentNodes[i], true)
		if d > 1200 {
			dedupedAdjacentNodes = append(dedupedAdjacentNodes[:i], dedupedAdjacentNodes[i+1:]...)
			i--
		}
	}

	frame.deduped = dedupedAdjacentNodes
	frame.matched = clearedBools(frame.matched, len(dedupedAdjacentNodes))
	matched := frame.matched
	s, err := symmetryScoreInto(p, node, dedupedAdjacentNodes, matched)
	if err != nil {
		return 0, err
	}
	score += s
	maxScore += len(dedupedAdjacentNodes)

	// Node A may not have symmetry itself, but we still don't want it to move because it contributes to B's symmetry
	// So we check neighbors, see that B is symmetrical, which makes A also symmetrical
	// ┌──┐   ┌──┐   ┌──┐
	// │A │◄──┤B ├──►│C │
	// └──┘   └──┘   └──┘
	if checkNeighbors {
		for i, adjacentNode := range dedupedAdjacentNodes {
			if err := p.at(i); err != nil {
				return 0, err
			}
			if matched[i] {
				continue
			}
			adjNodeSymm, err := memoNodeSymmetry(p, adjacentNode, edgeAbductions, false)
			if err != nil {
				return 0, err
			}
			score += adjNodeSymm
		}
	}

	// No adjacent nodes, not a container of a child with protruding edges
	if maxScore == 0 {
		return 0.0, nil
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	return score / float64(maxScore), nil
}

/*
*
// This is ideal. Symmetrical through the center
// ┌──┐   ┌──┐   ┌──┐
// │  │◄──┤  ├──►│  │
// └──┘   └──┘   └──┘
//
// But this is not bad and should still be rewarded
//
//	  ┌──┐
//	┌─┤  ├─┐
//	│ └──┘ │
//	│      │
// ┌▼┐    ┌▼─┐
// │ │    │  │
// └─┘    └──┘
// So we give it a small score
*/
// symmetryScoreInto writes match state into caller-owned storage so the
// optimizer can score symmetry without allocating.
func symmetryScoreInto(p *scoringPoll, node *layoutgraph.Node, neigh []*layoutgraph.Node, matchedSlice []bool) (float64, error) {
	if err := p.check(); err != nil {
		return 0, err
	}
	n := len(neigh)
	if n < 2 {
		return 0, nil
	}
	matchedSlice = matchedSlice[:n]
	clear(matchedSlice)

	score := 0.0

	ownSiblings := node.Graph.Containers[node.Container]
	var otherSiblings []*layoutgraph.Node

	for i, n1 := range neigh {
		if err := p.at(i); err != nil {
			return 0, err
		}
		if matchedSlice[i] {
			continue
		}
		if n1.Container != node.Container {
			otherSiblings = node.Graph.Containers[n1.Container]
		} else {
			otherSiblings = nil
		}

		bestIdx := -1
		best := 0.0

		// When two nodes are considered mirrored for the sake of symmetry, we only want ones under the same container
		// This just doesn't look aesthetically symmetrical, even if it is (in a larger graph at least)
		//                    ┌──────────┐
		//                    │          │
		// ┌──┐        ┌─-┐   │   ┌──┐   │
		// │  │◄───────┤  ├───┼──►│  │   │
		// └──┘        └──┘   │   └──┘   │
		//                    │          │
		//                    └──────────┘
		for j := i + 1; j < n; j++ {
			if err := p.at(j - i - 1); err != nil {
				return 0, err
			}
			if matchedSlice[j] {
				continue
			}
			n2 := neigh[j]
			if n1.Container != n2.Container {
				continue
			}
			// When nodes vary by a lot in size, it also doesn't come off as symmetrical
			area1, area2 := n1.Area(), n2.Area()
			if area1 > 2*area2 || area2 > 2*area1 {
				continue
			}

			ms := math.Inf(-1)
			// The two nodes can be symmetrical across the x or y axis
			for _, isX := range [...]bool{true, false} {
				axis := node.TopLeft.X + node.Width/2
				if !isX {
					axis = node.TopLeft.Y + node.Height/2
				}
				if isMirrored(n1, n2, isX, axis) {
					if node.OverlapsAlongDimension(n1, isX, true) &&
						node.OverlapsAlongDimension(n2, isX, true) {
						ms = 2
					} else {
						ms = 0.5
					}
					// no need to continue, as there's no way n1-n2 is aligned both horizontally and vertically
					break
				}
			}
			// skip obstruction search if the nodes aren't aligned
			if ms < 0 {
				continue
			}

			// We only want to count mirrored objects when they have no obstructions from node to it
			// This won't look symmetrical
			//             ┌─────────┐
			//             │         │
			// ┌───┐     ┌─┴┐  ┌─┐  ┌▼─┐
			// │   ◄─────┤  │  │ │  │  │
			// └───┘     └──┘  │ │  └──┘
			//                 └─┘
			if p.sess != nil {
				p.sess.recordSymmetryScan(node, n1, n2)
			}
			isObstructed, err := obstructed(p, node, n1, n2, ownSiblings, otherSiblings)
			if err != nil {
				return 0, err
			}
			if isObstructed {
				continue
			}

			if ms > best {
				best, bestIdx = ms, j
				if ms == 2 {
					break
				}
			}
		}

		if bestIdx != -1 {
			matchedSlice[i], matchedSlice[bestIdx] = true, true
			score += best
		}
	}

	if err := p.check(); err != nil {
		return 0, err
	}
	return score, nil
}

// obstructed reports whether a sibling in sib1 or sib2 blocks the straight
// line from center's center to a's or b's.
//
// Every test before PassesThrough only skips a sibling and has no side
// effects, so their order does not matter; bounds.excludes is written out
// inline (the same operations on the same values). The centers are the
// points Center() returns, computed where the first PassesThrough needs them
// (b's where the second does) and kept for the rest of the scan: nothing
// moves during it.
func obstructed(p *scoringPoll, center, a, b *layoutgraph.Node, sib1, sib2 []*layoutgraph.Node) (bool, error) {
	bounds := scoringNodeBounds(center).including(scoringNodeBounds(a)).including(scoringNodeBounds(b))
	boundsUsable := bounds.usable
	boundsLeft, boundsTop, boundsRight, boundsBottom := bounds.left, bounds.top, bounds.right, bounds.bottom
	var centerPoint, aPoint, bPoint geo.Point
	haveCenters, haveB := false, false
	check := func(sibs []*layoutgraph.Node) (bool, error) {
		for i, s := range sibs {
			// p.at(i) for i >= 0.
			if i&(scoringCancellationCheckInterval-1) == 0 {
				if err := p.check(); err != nil {
					return false, err
				}
			}
			if s == nil || s.TopLeft == nil {
				continue
			}
			// bounds.excludes(s)
			if boundsUsable {
				x, y := s.TopLeft.X, s.TopLeft.Y
				right, bottom := x+s.Width, y+s.Height
				if (x-x)+(y-y)+(right-right)+(bottom-bottom) == 0 &&
					((x < boundsLeft && right < boundsLeft) ||
						(x > boundsRight && right > boundsRight) ||
						(y < boundsTop && bottom < boundsTop) ||
						(y > boundsBottom && bottom > boundsBottom)) {
					continue
				}
			}
			if s.Graph != center.Graph || s == center || s == a || s == b {
				continue
			}
			if center.IsDescendantOf(s) || a.IsDescendantOf(s) || b.IsDescendantOf(s) {
				continue
			}
			if s.IsDescendantOf(center) || s.IsDescendantOf(a) || s.IsDescendantOf(b) {
				continue
			}
			if !haveCenters {
				centerPoint = geo.Point{X: center.TopLeft.X + center.Width/2, Y: center.TopLeft.Y + center.Height/2}
				aPoint = geo.Point{X: a.TopLeft.X + a.Width/2, Y: a.TopLeft.Y + a.Height/2}
				haveCenters = true
			}
			if s.PassesThrough(&centerPoint, &aPoint) {
				return true, nil
			}
			if !haveB {
				bPoint = geo.Point{X: b.TopLeft.X + b.Width/2, Y: b.TopLeft.Y + b.Height/2}
				haveB = true
			}
			if s.PassesThrough(&centerPoint, &bPoint) {
				return true, nil
			}
		}
		return false, nil
	}
	isObstructed, err := check(sib1)
	if err != nil || isObstructed {
		return isObstructed, err
	}
	return check(sib2)
}

type columnCrossingScratch struct {
	edgeToAbduction map[*layoutgraph.Edge]*layoutgraph.EdgeAbduction
	abductionInput  int
	segments        []layoutgraph.CrossingSegment
}

const maxPooledColumnCrossingEntries = 4096

var columnCrossingScratchPool = typedpool.New(func() *columnCrossingScratch {
	return new(columnCrossingScratch)
})

func putColumnCrossingScratch(scratch *columnCrossingScratch) {
	if scratch.abductionInput > maxPooledColumnCrossingEntries ||
		len(scratch.edgeToAbduction) > maxPooledColumnCrossingEntries ||
		cap(scratch.segments) > maxPooledColumnCrossingEntries {
		return
	}
	clear(scratch.edgeToAbduction)
	scratch.abductionInput = 0
	scratch.segments = scratch.segments[:0]
	columnCrossingScratchPool.Put(scratch)
}

// ColumnCrossingCost scores crossings between labeled table-column edges.
func ColumnCrossingCost(ctx context.Context, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	if !node.IsTable() {
		return 0, nil
	}

	scratch := columnCrossingScratchPool.Get()
	defer putColumnCrossingScratch(scratch)
	clear(scratch.edgeToAbduction)
	scratch.abductionInput = len(edgeAbductions)
	scratch.segments = scratch.segments[:0]
	if len(edgeAbductions) > 0 && scratch.edgeToAbduction == nil {
		scratch.edgeToAbduction = make(map[*layoutgraph.Edge]*layoutgraph.EdgeAbduction, min(len(edgeAbductions), maxPooledColumnCrossingEntries))
	}
	for i, abduction := range edgeAbductions {
		if err := p.at(i); err != nil {
			return 0, err
		}
		scratch.edgeToAbduction[abduction.Edge] = abduction
	}

	for i, e := range node.Edges {
		if err := p.at(i); err != nil {
			return 0, err
		}
		var fromPort, toPort geo.Point
		var hasFromPort, hasToPort bool
		if abduction, exists := scratch.edgeToAbduction[e]; exists {
			fromPort, toPort, hasFromPort, hasToPort, _ = e.FacingTablePortValues(abduction.OriginallyFrom, abduction.OriginallyTo)
		} else {
			fromPort, toPort, hasFromPort, hasToPort, _ = e.FacingTablePortValues(nil, nil)
		}

		if !hasFromPort || !hasToPort {
			continue
		}
		scratch.segments = append(scratch.segments, layoutgraph.CrossingSegment{Start: fromPort, End: toPort})
	}

	crossings, err := layoutgraph.CountSegmentCrossingsContext(ctx, scratch.segments)
	if err != nil {
		return 0, err
	}
	return layoutgraph.CrossingCostWeight * float64(crossings) * node.Graph.CellSize, nil
}

// isMirrored checks if nodeA is mirrored to nodeB along the given axis line
// isXAxis = true means they are mirrored along a vertical line
func isMirrored(nodeA, nodeB *layoutgraph.Node, isXAxis bool, axisVal float64) bool {
	nodeAtlX := nodeA.TopLeft.X
	nodeAtlY := nodeA.TopLeft.Y
	nodeAbrX := nodeA.TopLeft.X + nodeA.Width
	nodeAbrY := nodeA.TopLeft.Y + nodeA.Height

	nodeBtlX := nodeB.TopLeft.X
	nodeBtlY := nodeB.TopLeft.Y
	nodeBbrX := nodeB.TopLeft.X + nodeB.Width
	nodeBbrY := nodeB.TopLeft.Y + nodeB.Height

	symmetryTolerance := symmetryToleranceBand * nodeA.Graph.CellSize
	if isXAxis {
		if nodeA.TopLeft.X == nodeB.TopLeft.X {
			return false
		}

		// The left edge of node A should be on other side of axisVal from right edge of node B
		if nodeA.TopLeft.X > nodeB.TopLeft.X {
			// Axis val must be in between
			if !((axisVal > nodeBbrX) && (axisVal < nodeAtlX)) {
				return false
			}
			// Their distances from the axis line should be roughly equal
			if math.Abs((nodeAtlX-axisVal)-(axisVal-nodeBbrX)) > symmetryTolerance {
				return false
			}
		} else if nodeA.TopLeft.X < nodeB.TopLeft.X {
			if !((axisVal > nodeAbrX) && (axisVal < nodeBtlX)) {
				return false
			}
			if math.Abs((nodeBtlX-axisVal)-(axisVal-nodeAbrX)) > symmetryTolerance {
				return false
			}
		}

		// Centers match
		return math.Abs(((nodeAtlY+nodeAbrY)/2.0)-((nodeBtlY+nodeBbrY)/2.0)) <= symmetryTolerance
	}
	if nodeA.TopLeft.Y == nodeB.TopLeft.Y {
		return false
	}
	if nodeA.TopLeft.Y > nodeB.TopLeft.Y {
		if !((axisVal > nodeBbrY) && (axisVal < nodeAtlY)) {
			return false
		}
		if math.Abs((nodeAtlY-axisVal)-(axisVal-nodeBbrY)) > symmetryTolerance {
			return false
		}
	} else if nodeA.TopLeft.Y < nodeB.TopLeft.Y {
		if !((axisVal > nodeAbrY) && (axisVal < nodeBtlY)) {
			return false
		}
		if math.Abs((nodeBtlY-axisVal)-(axisVal-nodeAbrY)) > symmetryTolerance {
			return false
		}
	}

	// Centers match
	return math.Abs(((nodeAtlX+nodeAbrX)/2.0)-((nodeBtlX+nodeBbrX)/2.0)) <= symmetryTolerance
}
