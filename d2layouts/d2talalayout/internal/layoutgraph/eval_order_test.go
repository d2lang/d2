package layoutgraph

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// These tests cover code whose result depended on Go's unspecified order of
// evaluation between a slice operand and a call that grows the slice
// (x[f()], s[i] = append(s[i], f())). gc runs the call first, TinyGo (d2's
// wasm build) reads the slice first. They pass under both compilers with
// the fixes; without them they fail under TinyGo (index out of range in the
// container memo, a lost pair in the overlap set), e.g. with
// tinygo test -run EvaluationOrder ./d2layouts/d2talalayout/internal/layoutgraph

func evalOrderContainerGraph(containers int) (*Graph, []*Node) {
	g := NewGraph()
	id := EntityID(1)
	var out []*Node
	for c := 0; c < containers; c++ {
		container := NewNode(id, 10, 10)
		id++
		container.TopLeft = geo.NewPoint(float64(c*1000), 0)
		g.AddNewNodeToContainer(nil, container)
		container.isContainer = true
		for i := 0; i < 2; i++ {
			child := NewNode(id, 40, 30)
			id++
			child.TopLeft = geo.NewPoint(float64(c*1000+60*i+20), 40)
			g.AddNewNodeToContainer(container, child)
		}
		out = append(out, container)
	}
	return g, out
}

func setGeometry(node *Node, geometry memoGeometry) {
	node.TopLeft = geo.NewPoint(math.Float64frombits(geometry.x), math.Float64frombits(geometry.y))
	node.Width = math.Float64frombits(geometry.w)
	node.Height = math.Float64frombits(geometry.h)
}

// TestEvaluationOrderContainerMemoWrapAddsEntries wraps containers that have
// no memo entry yet while the entry slice is full, so entryFor reallocates it
// (TinyGo indexed the old slice, and every
// TALA seed failed on graphs with containers).
func TestEvaluationOrderContainerMemoWrapAddsEntries(t *testing.T) {
	for _, preset := range []int{0, 1, 3} {
		_, containers := evalOrderContainerGraph(5)
		var m containerMemo
		// Entries of other nodes, filling the slice to its capacity.
		for i := 0; i < preset; i++ {
			m.entryFor(NewNode(EntityID(1000+i), 1, 1))
		}
		m.entries = slices.Clip(m.entries)
		for i, container := range containers {
			if i == 0 && len(m.entries) != cap(m.entries) {
				t.Fatalf("preset %d: entries not full (%d of %d)", preset, len(m.entries), cap(m.entries))
			}
			// The legacy computation on the same inputs.
			before := geometryOf(container)
			container.wrapChildren()
			want := geometryOf(container)
			setGeometry(container, before)

			m.wrap(container)
			slot := container.memoSlot
			if slot < 0 || int(slot) >= len(m.entries) || m.entries[slot].node != container {
				t.Fatalf("preset %d container %d: no memo entry after wrap (slot %d, %d entries)", preset, i, slot, len(m.entries))
			}
			if len(m.entries) != preset+i+1 {
				t.Fatalf("preset %d container %d: %d entries, want %d", preset, i, len(m.entries), preset+i+1)
			}
			entry := &m.entries[slot]
			if !entry.wrapValid || len(entry.wrapNodes) != 3 || entry.wrapNodes[0] != container {
				t.Fatalf("preset %d container %d: wrap not recorded", preset, i)
			}
			if got := geometryOf(container); got != want {
				t.Fatalf("preset %d container %d: wrapped geometry %v, legacy %v", preset, i, got, want)
			}
			// The same inputs again are answered from the entry.
			setGeometry(container, before)
			hits := m.hits.wrap
			m.wrap(container)
			if m.hits.wrap != hits+1 || geometryOf(container) != want {
				t.Fatalf("preset %d container %d: repeated wrap not answered from the memo", preset, i)
			}
		}
	}
}

// TestEvaluationOrderOverlapSetFromMapKeepsEveryPair converts pairwise maps
// whose inner sets name nodes not interned yet, so intern grows set.near
// while a row is appended to.
func TestEvaluationOrderOverlapSetFromMapKeepsEveryPair(t *testing.T) {
	for _, n := range []int{2, 3, 5, 9, 17, 40} {
		nodes := make([]*Node, n)
		for i := range nodes {
			nodes[i] = NewNode(EntityID(i+1), 10, 10)
		}
		pairs := map[*Node]map[*Node]struct{}{}
		for i, node := range nodes {
			inner := map[*Node]struct{}{}
			for j := range nodes {
				if j != i && (j*7+i*3)%5 != 0 {
					inner[nodes[j]] = struct{}{}
				}
			}
			pairs[node] = inner
		}
		set := existingOverlapSetFromMap(pairs)
		if set == nil {
			t.Fatalf("n=%d: nil set", n)
		}
		if len(set.near) != len(set.nodes) {
			t.Fatalf("n=%d: %d rows for %d nodes", n, len(set.near), len(set.nodes))
		}
		for node, others := range pairs {
			i := set.indexOf(node)
			if i < 0 {
				t.Fatalf("n=%d: node %d not interned", n, node.ID)
			}
			if len(set.near[i]) != len(others) {
				t.Fatalf("n=%d: node %d has %d near entries, want %d", n, node.ID, len(set.near[i]), len(others))
			}
			for other := range others {
				j := set.indexOf(other)
				if j < 0 || !slices.Contains(set.near[i], int32(j)) {
					t.Fatalf("n=%d: pair %d -> %d lost", n, node.ID, other.ID)
				}
			}
		}
	}
}
