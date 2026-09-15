package layoutgraph

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"
)

// Keep the previous comparator and reflection-based sort as an independent
// ordering oracle. Equal IDs need exact pointer-order parity, not just sorted IDs.
func legacySortNodesByID(nodes []*Node) {
	if len(nodes) < 2 {
		return
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].entityID() < nodes[j].entityID()
	})
}

func TestSortNodesByIDMatchesLegacyPointerOrder(t *testing.T) {
	// Cover both sides of pdqsort's insertion-sort and pivot-selection thresholds,
	// plus larger partitions with repeated IDs and pointers.
	for _, size := range []int{0, 1, 2, 11, 12, 13, 48, 49, 50, 51, 127, 128, 257, 1024} {
		for _, pattern := range []string{"sorted", "reverse", "shuffled", "all equal", "few IDs", "nil zero extrema repeated"} {
			t.Run(fmt.Sprintf("%s/%d", pattern, size), func(t *testing.T) {
				nodes := make([]*Node, size)
				for i := range nodes {
					nodes[i] = &Node{ID: int64(i) - int64(size/2)}
				}
				switch pattern {
				case "reverse":
					slices.Reverse(nodes)
				case "shuffled":
					rng := rand.New(rand.NewSource(int64(size)))
					rng.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
				case "all equal":
					for _, node := range nodes {
						node.ID = 7
					}
				case "few IDs":
					for i, node := range nodes {
						node.ID = int64(i%5) - 2
					}
				case "nil zero extrema repeated":
					zero := &Node{ID: 0}
					minimum, maximum := &Node{ID: math.MinInt64}, &Node{ID: math.MaxInt64}
					values := []*Node{nil, zero, maximum, minimum, {ID: 0}, {ID: -1}, zero, {ID: 1}, minimum, nil, maximum}
					for i := range nodes {
						nodes[i] = values[i%len(values)]
					}
				}
				checkNodeSortPointerOrder(t, nodes)
			})
		}
	}
}

func checkNodeSortPointerOrder(t *testing.T, input []*Node) {
	t.Helper()
	// Sort a subslice with writable capacity beyond its length. Neither sorting
	// implementation may move the surrounding sentinels or change its slice header.
	left, right := &Node{ID: math.MaxInt64}, &Node{ID: math.MinInt64}
	gotBacking := make([]*Node, len(input)+4)
	gotBacking[0], gotBacking[1] = left, nil
	copy(gotBacking[2:], input)
	gotBacking[len(gotBacking)-2], gotBacking[len(gotBacking)-1] = right, left
	wantBacking := slices.Clone(gotBacking)
	got := gotBacking[2 : len(gotBacking)-2]
	want := wantBacking[2 : len(wantBacking)-2]
	beforeLen, beforeCap := len(got), cap(got)
	legacySortNodesByID(want)
	sortNodesByID(got)
	if len(got) != beforeLen || cap(got) != beforeCap {
		t.Fatal("node slice header changed")
	}
	for i := range gotBacking {
		if gotBacking[i] != wantBacking[i] {
			t.Fatalf("pointer at backing index %d: got %p (ID %d), want %p (ID %d)",
				i, gotBacking[i], gotBacking[i].entityID(), wantBacking[i], wantBacking[i].entityID())
		}
	}
}
