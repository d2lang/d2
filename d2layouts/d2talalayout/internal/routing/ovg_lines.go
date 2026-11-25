package routing

import (
	"cmp"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
)

// ovgLineGroups lists connectNodes' lines in the order it connects them: the
// positions the line map's sorted keys gave, and each line's nodes in
// ovg.Nodes order (the order the map's appends gave).
type ovgLineGroups struct {
	positions []float64
	// line i's nodes are nodes[starts[i]:starts[i+1]]; a NaN line (its map
	// lookup found nothing) is passed as nil.
	starts []int
	nodes  []*OVGNode
}

func (lines *ovgLineGroups) line(i int) []*OVGNode {
	start, end := lines.starts[i], lines.starts[i+1]
	if start == end || math.IsNaN(lines.positions[i]) {
		return nil
	}
	// Full slice expression: a line can never grow into the next one.
	return lines.nodes[start:end:end]
}

// ovgLine is one map key while grouping: its last assigned coordinate (the
// key bits a float64-keyed map keeps after its last assignment), node count
// and output cursor.
type ovgLine struct {
	position float64
	count    int
	cursor   int
}

// ovgLineScratch holds connectNodes' grouping buffers; it is pooled across
// OVG builds.
type ovgLineScratch struct {
	eligible             []*OVGNode
	lineOfNode           []int32
	lineIndex            map[float64]int32
	lines                []ovgLine
	order                []int32
	horizontal, vertical ovgLineGroups
}

var ovgLineScratchPool = typedpool.New(func() *ovgLineScratch {
	return &ovgLineScratch{lineIndex: map[float64]int32{}}
})

// retainable reports whether the scratch is small enough to pool (see
// maxPooledRouteSearchNodes).
func (scratch *ovgLineScratch) retainable() bool {
	const limit = maxPooledRouteSearchNodes
	return cap(scratch.eligible) <= limit && cap(scratch.lineOfNode) <= limit &&
		len(scratch.lineIndex) <= limit && cap(scratch.lines) <= limit && cap(scratch.order) <= limit &&
		scratch.horizontal.storage() <= limit && scratch.vertical.storage() <= limit
}

func (groups *ovgLineGroups) storage() int {
	return max(cap(groups.positions), cap(groups.starts), cap(groups.nodes))
}

func (scratch *ovgLineScratch) release() {
	if !scratch.retainable() {
		return
	}
	clear(scratch.eligible[:cap(scratch.eligible)])
	clear(scratch.horizontal.nodes[:cap(scratch.horizontal.nodes)])
	clear(scratch.vertical.nodes[:cap(scratch.vertical.nodes)])
	scratch.eligible = scratch.eligible[:0]
	clear(scratch.lineIndex)
	ovgLineScratchPool.Put(scratch)
}

// groupLines groups the eligible nodes into horizontal lines (by Y) and
// vertical lines (by X) as connectNodes' float64-keyed maps did: it counts
// nodes per key with a key -> line index map (same == semantics: -0 and +0
// share a line, every NaN is a new key) and places them in one buffer, so no
// per-line slice grows.
func (scratch *ovgLineScratch) groupLines(eligible []*OVGNode) (*ovgLineGroups, *ovgLineGroups) {
	scratch.groupCounted(&scratch.horizontal, eligible, true)
	scratch.groupCounted(&scratch.vertical, eligible, false)
	return &scratch.horizontal, &scratch.vertical
}

// groupCounted fills lines for one axis. Lines are ordered as slices.Sort
// orders the map's keys (ascending, NaN first; distinct non-NaN keys never
// compare equal, and NaN lines are all empty, so ties cannot matter).
func (scratch *ovgLineScratch) groupCounted(lines *ovgLineGroups, eligible []*OVGNode, byY bool) {
	clear(scratch.lineIndex)
	if cap(scratch.lineOfNode) < len(eligible) {
		scratch.lineOfNode = make([]int32, len(eligible))
	}
	lineOfNode := scratch.lineOfNode[:len(eligible)]
	known := scratch.lines[:0]
	for i, node := range eligible {
		coordinate := node.X
		if byY {
			coordinate = node.Y
		}
		index, ok := scratch.lineIndex[coordinate]
		if !ok {
			index = int32(len(known))
			scratch.lineIndex[coordinate] = index
			known = append(known, ovgLine{})
		}
		known[index].position = coordinate
		known[index].count++
		lineOfNode[i] = index
	}
	scratch.lines = known

	order := scratch.order[:0]
	for i := range known {
		order = append(order, int32(i))
	}
	slices.SortFunc(order, func(a, b int32) int {
		x, y := known[a].position, known[b].position
		switch {
		case x < y || (math.IsNaN(x) && !math.IsNaN(y)):
			return -1
		case y < x || (math.IsNaN(y) && !math.IsNaN(x)):
			return 1
		}
		return cmp.Compare(a, b)
	})
	scratch.order = order

	positions := lines.positions[:0]
	starts := append(lines.starts[:0], 0)
	next := 0
	for _, index := range order {
		line := &known[index]
		positions = append(positions, line.position)
		line.cursor = next
		next += line.count
		starts = append(starts, next)
	}
	if cap(lines.nodes) < len(eligible) {
		lines.nodes = make([]*OVGNode, len(eligible))
	}
	nodes := lines.nodes[:len(eligible)]
	for i, node := range eligible {
		line := &known[lineOfNode[i]]
		nodes[line.cursor] = node
		line.cursor++
	}
	lines.positions, lines.starts, lines.nodes = positions, starts, nodes
}

// sortSliceExact sorts s exactly as sort.Slice(s, func(i, j int) bool {
// return less(s[i], s[j]) }) does: slices.SortFunc runs the same pattern-
// defeating quicksort (both are generated from one template) and consults
// its comparison only as cmp(a, b) < 0, so every comparison, swap and the
// resulting order are the same, without sort.Slice's reflection swapper.
func sortSliceExact[T any](s []T, less func(a, b T) bool) {
	slices.SortFunc(s, func(a, b T) int {
		if less(a, b) {
			return -1
		}
		return 1
	})
}
