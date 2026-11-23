package hierarchy

import (
	"cmp"
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// Align an unbranched path across ranks when all its nodes can
// share a centerline without passing a sibling or reducing its clearance.
// Branches and table row ports retain independent routes.
func alignRankChains(children []*layoutgraph.Node, levels map[*layoutgraph.Node]int, horizontal bool, gap float64, guard *limits.WorkGuard) error {
	if len(children) < 2 {
		return nil
	}
	type neighbors struct{ before, after []*layoutgraph.Node }
	adj := make(map[*layoutgraph.Node]neighbors)
	for _, n := range children {
		level, ok := levels[n]
		if !ok || n.IsContainer() || n.IsTable() {
			continue
		}
		var sides neighbors
		for _, e := range n.Edges {
			if err := guard.Step(); err != nil {
				return err
			}
			other := e.From
			if other == n {
				other = e.To
			}
			otherLevel, ok := levels[other]
			if !ok || otherLevel == level || e.IsInvisible {
				continue
			}
			side := &sides.after
			if otherLevel < level {
				side = &sides.before
			}
			// Two distinct neighbors suffice to exclude a branching side.
			if len(*side) < 2 && !slices.Contains(*side, other) {
				*side = append(*side, other)
			}
		}
		adj[n] = sides
	}
	next := make(map[*layoutgraph.Node]*layoutgraph.Node)
	prev := make(map[*layoutgraph.Node]*layoutgraph.Node)
	for _, n := range children {
		out := adj[n].after
		if len(out) != 1 {
			continue
		}
		other := out[0]
		if len(adj[other].before) == 1 {
			next[n], prev[other] = other, n
		}
	}
	if len(next) == 0 {
		return nil
	}
	// Cache the visible node bounds, including margins such as outside labels.
	// Only their transverse positions can change during this pass.
	type bounds struct{ low, high, near, far float64 }
	boxes := make(map[*layoutgraph.Node]bounds, len(children))
	for _, n := range children {
		tl, br := n.BoundingBox(layoutgraph.Nodes{n})
		boxes[n] = bounds{
			low: axisValue(horizontal, tl.X, tl.Y), high: axisValue(horizontal, br.X, br.Y),
			near: axisValue(!horizontal, tl.X, tl.Y), far: axisValue(!horizontal, br.X, br.Y),
		}
	}
	// A chain's feasible median is in the closed span of its seed centers:
	// each sibling clearance interval contains the node's current center.
	// Index the entire possible transverse sweep once, so it remains valid
	// after any earlier chain has moved.
	type sweep struct {
		node                 *layoutgraph.Node
		near, far, prefixFar float64
	}
	sweeps := make([]sweep, 0, len(children))
	type candidate struct {
		nodes             []*layoutgraph.Node
		median, low, high float64
	}
	var candidates []candidate
	heads := make(map[*layoutgraph.Node]*layoutgraph.Node, len(next))
	for _, start := range children {
		if prev[start] != nil || next[start] == nil {
			continue
		}
		var chain []*layoutgraph.Node
		var centers []float64
		for n := start; n != nil; n = next[n] {
			heads[n] = start
			chain = append(chain, n)
			centers = append(centers, rankCenter(n, !horizontal))
		}
		slices.Sort(centers)
		lo, hi := centers[0], centers[len(centers)-1]
		for _, n := range chain {
			box := boxes[n]
			center := rankCenter(n, !horizontal)
			sweeps = append(sweeps, sweep{node: n, near: box.near + lo - center, far: box.far + hi - center})
		}
		if lo != hi {
			candidates = append(candidates, candidate{chain, (centers[(len(centers)-1)/2] + centers[len(centers)/2]) / 2, lo, hi})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	for _, n := range children {
		if heads[n] == nil {
			box := boxes[n]
			sweeps = append(sweeps, sweep{node: n, near: box.near, far: box.far})
		}
	}
	slices.SortStableFunc(sweeps, func(a, b sweep) int { return cmp.Compare(a.near, b.near) })
	running := math.Inf(-1)
	for i := range sweeps {
		running = max(running, sweeps[i].far)
		sweeps[i].prefixFar = running
	}
	overlapping := func(near, far float64) []sweep {
		first := sort.Search(len(sweeps), func(i int) bool { return sweeps[i].prefixFar >= near })
		end := sort.Search(len(sweeps), func(i int) bool { return sweeps[i].near > far })
		return sweeps[first:end]
	}
	// Heavily overlapping swept intervals can defeat the index. Alignment
	// is optional refinement; keep every mandatory rank feasible and bound
	// its candidate checks to a linear budget for this sibling set.
	remaining := 64 * len(children)
	for _, c := range candidates {
		chain := c.nodes
		start := chain[0]
		low, high := c.low, c.high
		for _, n := range chain {
			center := rankCenter(n, !horizontal)
			if n.FixedTopLeft != nil {
				low, high = max(low, center), min(high, center)
			}
			box := boxes[n]
			// Only a sibling whose sweep overlaps this node's possible extent
			// can force the median away from the proposed interval.
			possible := overlapping(c.low+box.near-center-gap, c.high+box.far-center+gap)
			if len(possible) > remaining {
				return guard.Check()
			}
			remaining -= len(possible)
			for _, entry := range possible {
				sibling := entry.node
				if err := guard.Step(); err != nil {
					return err
				}
				if sibling == n {
					continue
				}
				other := boxes[sibling]
				if other.high+crossingSpacing <= box.low || box.high+crossingSpacing <= other.low {
					continue
				}
				if rankCenter(sibling, !horizontal) < center {
					low = max(low, other.far+gap+center-box.near)
				} else {
					high = min(high, other.near-gap+center-box.far)
				}
			}
		}
		if low > high {
			continue
		}
		// The feasible median minimizes transverse displacement from the seed.
		target := max(low, min(high, c.median))
		// A long edge may span occupied levels, including in disconnected
		// components whose ranks have a different physical origin.
		head, tail := rankCenter(start, horizontal), rankCenter(chain[len(chain)-1], horizontal)
		clear := true
		possible := overlapping(target-crossingSpacing, target+crossingSpacing)
		if len(possible) > remaining {
			return guard.Check()
		}
		remaining -= len(possible)
		for _, entry := range possible {
			obstacle := entry.node
			if err := guard.Step(); err != nil {
				return err
			}
			box := boxes[obstacle]
			if box.high <= head || box.low >= tail || target < box.near-crossingSpacing || target > box.far+crossingSpacing {
				continue
			}
			if heads[obstacle] != start {
				clear = false
				break
			}
		}
		if !clear {
			continue
		}
		for _, n := range chain {
			delta := target - rankCenter(n, !horizontal)
			if delta == 0 {
				continue
			}
			box := boxes[n]
			box.near += delta
			box.far += delta
			boxes[n] = box
			if horizontal {
				n.MoveWithChildren(0, delta)
			} else {
				n.MoveWithChildren(delta, 0)
			}
		}
	}
	return nil
}

// A flat edge between two descendants projects onto two siblings at their
// lowest common container. Three or more peers in a path must follow that
// path's order, or a transition will double back around the middle peer.
type flatRankNeighbor struct {
	class   layoutgraph.EntityID
	blocked bool
	peers   []*layoutgraph.Node
}
type flatRankPaths map[*layoutgraph.Node]flatRankNeighbor

func (paths flatRankPaths) link(n, peer *layoutgraph.Node, class layoutgraph.EntityID) {
	row, ok := paths[n]
	if !ok {
		row.class = class
	}
	if row.class != class || n.FixedTopLeft != nil {
		row.blocked = true
	}
	if !row.blocked && !slices.Contains(row.peers, peer) {
		if len(row.peers) == 2 {
			row.blocked = true
		} else {
			row.peers = append(row.peers, peer)
		}
	}
	paths[n] = row
}

func (paths flatRankPaths) order(ordered []*layoutgraph.Node) {
	if len(ordered) < 3 || len(paths) == 0 {
		return
	}
	index := make(map[*layoutgraph.Node]int, len(ordered))
	for i, n := range ordered {
		index[n] = i
	}
	seen := make(map[*layoutgraph.Node]bool)
	for _, start := range ordered {
		row := paths[start]
		if row.blocked || len(row.peers) != 1 || seen[start] {
			continue
		}
		var path []*layoutgraph.Node
		lo, hi := index[start], index[start]
		var previous *layoutgraph.Node
		n := start
		for n != nil {
			row := paths[n]
			if row.blocked || seen[n] {
				break
			}
			seen[n] = true
			path = append(path, n)
			lo, hi = min(lo, index[n]), max(hi, index[n])
			var next *layoutgraph.Node
			for _, peer := range row.peers {
				if peer != previous {
					next = peer
					break
				}
			}
			previous, n = n, next
		}
		// Only permute a contiguous run of peers. Foreign lanes, pins, forks,
		// and cycles keep the order the native hierarchy chose for them.
		if n == nil && len(path) >= 3 && hi-lo+1 == len(path) {
			copy(ordered[lo:hi+1], path)
		}
	}
}
