package layoutgraph

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// referenceDescendants is the former map-based walk of
// appendDescendantNodesGuarded, kept as the reference for its output and
// unit count.
func referenceDescendants(g *Graph, node *Node, includeClusterNodes bool) ([]*Node, int64) {
	seen := map[*Node]struct{}{}
	var stack []descendantWalkEntry
	var out []*Node
	var units int64
	if node != nil {
		seen[node] = struct{}{}
	}
	push := func(parent *Node) {
		if sequence := g.Sequences[parent]; sequence != nil {
			for _, v := range slices.Backward(sequence.Nodes) {
				units++
				stack = append(stack, descendantWalkEntry{node: v, emit: includeClusterNodes})
			}
		}
		if parent != nil && parent.isClusterVessel {
			if cluster := g.Clusters[parent]; cluster != nil {
				for _, v := range slices.Backward(cluster.Nodes) {
					units++
					stack = append(stack, descendantWalkEntry{node: v, emit: includeClusterNodes})
				}
			}
		}
		if parent == nil || parent.isContainer {
			for _, child := range slices.Backward(g.Containers[parent]) {
				units++
				stack = append(stack, descendantWalkEntry{node: child, emit: true})
			}
		}
	}
	push(node)
	for len(stack) > 0 {
		units++
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current.node == nil {
			continue
		}
		if _, ok := seen[current.node]; ok {
			continue
		}
		seen[current.node] = struct{}{}
		if current.emit {
			out = append(out, current.node)
		}
		push(current.node)
	}
	return out, units
}

// TestDescendantWalkScratchMatchesReference walks small and large
// container trees (above and below the linear seen-set limit), with shared
// children, through one reused scratch, and compares output and units with
// the map-based reference walk; transaction walks are compared too.
func TestDescendantWalkScratchMatchesReference(t *testing.T) {
	ctx := context.Background()
	for seed := int64(0); seed < 120; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			g := NewGraph()
			var containers []*Node
			count := 5 + rng.Intn(200)
			var all []*Node
			for i := 0; i < count; i++ {
				node := NewNode(EntityID(i+1), 10, 10)
				node.TopLeft = geo.NewPoint(float64(i*20), 0)
				var parent *Node
				if len(containers) > 0 && rng.Intn(5) != 0 {
					parent = containers[rng.Intn(len(containers))]
				}
				g.AddNewNodeToContainer(parent, node)
				if rng.Intn(4) == 0 {
					node.isContainer = true
					containers = append(containers, node)
				}
				all = append(all, node)
			}
			// Repeat some children under a second container (a DAG).
			for i := 0; i < count/10; i++ {
				if len(containers) == 0 {
					break
				}
				parent := containers[rng.Intn(len(containers))]
				g.Containers[parent] = append(g.Containers[parent], all[rng.Intn(len(all))])
			}
			guard, err := limits.NewWorkGuard(ctx, "WalkScratchTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{}, guard)
			require.NoError(t, err)
			scratch := descendantWalkPool.Get()
			defer releaseDescendantWalkScratch(scratch)
			for _, start := range append([]*Node{nil}, all...) {
				want, wantUnits := referenceDescendants(g, start, true)
				walkGuard, err := limits.NewWorkGuard(ctx, "WalkScratchRun", maxTransactionWorkUnits)
				require.NoError(t, err)
				got, err := g.appendDescendantNodesGuarded(scratch.descendants[:0], scratch, start, true, walkGuard)
				require.NoError(t, err)
				scratch.descendants = got
				require.Equal(t, len(want), len(got))
				require.True(t, slices.Equal(want, got))
				require.Equal(t, wantUnits, walkGuard.Used())
				var moved []*Node
				g.forEachDescendant(start, true, func(n *Node) { moved = append(moved, n) })
				require.True(t, slices.Equal(want, moved))
				if start != nil {
					generation := txn.beginMarks()
					units := txn.walkDescendants(start, generation)
					require.Equal(t, wantUnits, units)
					require.True(t, slices.Equal(want, txn.descendantNodes))
				}
			}
		})
	}
}

// TestFixedBoundsValuesMatchesFixedBounds compares the value form with
// fixedBounds on random node sets with outside labels, loop offsets and
// fixed nodes.
func TestFixedBoundsValuesMatchesFixedBounds(t *testing.T) {
	positions := []label.Position{label.OutsideTopLeft, label.OutsideBottomRight, label.InsideMiddleCenter, label.OutsideRightMiddle}
	for seed := int64(0); seed < 500; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := NewGraph()
		container := NewNode(1000, 400, 400)
		container.TopLeft = geo.NewPoint(0, 0)
		g.AddNewNodeToContainer(nil, container)
		container.isContainer = true
		var nodes Nodes
		for i := 0; i < rng.Intn(6); i++ {
			node := NewNode(EntityID(i+1), float64(1+rng.Intn(90))+0.5, float64(1+rng.Intn(90))+0.25)
			node.TopLeft = geo.NewPoint(float64(rng.Intn(300))+0.3, float64(rng.Intn(300))-0.7)
			if rng.Intn(3) == 0 {
				node.Label = &Label{Width: float64(rng.Intn(200)), Height: float64(rng.Intn(40)), Position: positions[rng.Intn(len(positions))]}
			}
			if rng.Intn(4) == 0 {
				node.LoopOffsets = map[geo.Orientation]float64{geo.Left: float64(rng.Intn(30)), geo.Bottom: float64(rng.Intn(30))}
			}
			if rng.Intn(5) == 0 {
				node.FixedTopLeft = geo.NewPoint(float64(rng.Intn(50)), float64(rng.Intn(50)))
			}
			g.AddNewNodeToContainer(container, node)
			nodes = append(nodes, node)
		}
		wantTL, wantBR := nodes.fixedBounds()
		gotTL, gotBR, ok := nodes.fixedBoundsValues()
		require.True(t, ok)
		require.Equal(t, *wantTL, gotTL, "seed %d", seed)
		require.Equal(t, *wantBR, gotBR, "seed %d", seed)
	}
}
