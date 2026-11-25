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
)

// Budget accounting must not depend on Go's map iteration order: when a budget binds, the units decide whether a
// layout succeeds. These tests repeat the same transactions on identical
// graphs, whose maps iterate in different random orders, and require the
// same units for every commit, rejected ones included.

// TestCommitUnitsDoNotDependOnMapOrder: the stepped container check (it
// stops at the first bad container) and the existing-overlap regression
// check (it stops at the first regression) walked maps.
func TestCommitUnitsDoNotDependOnMapOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rejected, compared := 0, 0
	for seed := int64(0); seed < 120; seed++ {
		for _, options := range []TransactionOptions{
			{AffectContainers: true},
			{AffectContainers: true, StableStructure: true},
		} {
			const sides = 3
			graphs := make([]*Graph, sides)
			txns := make([]*Transaction, sides)
			for i := range graphs {
				if seed%2 == 0 {
					graphs[i] = randomContainerGraph(rand.New(rand.NewSource(seed)))
				} else {
					graphs[i] = spreadContainerGraph(rand.New(rand.NewSource(seed)))
				}
				guard, err := limits.NewWorkGuard(ctx, "OrderTest", maxTransactionWorkUnits)
				require.NoError(t, err)
				txns[i], err = graphs[i].newTransactionWithOptionsContext(ctx, options, guard)
				require.NoError(t, err)
			}
			rng := rand.New(rand.NewSource(seed*104729 + 1))
			for step := 0; step < 30; step++ {
				index := rng.Intn(len(graphs[0].Nodes))
				dx := float64(rng.Intn(400) - 200)
				dy := float64(rng.Intn(400) - 200)
				var errs [sides]string
				var used [sides]int64
				for i := range graphs {
					guard, err := limits.NewWorkGuard(ctx, "OrderTest", maxTransactionWorkUnits)
					require.NoError(t, err)
					txns[i].guard = guard
					node := graphs[i].Nodes[index]
					txns[i].AddOp(func() error {
						node.MoveWithChildren(dx, dy)
						return nil
					})
					if err := txns[i].Commit(ctx); err != nil {
						errs[i] = err.Error()
					}
					used[i] = guard.Used()
					txns[i].Clear()
					if errs[i] == "" {
						require.NoError(t, txns[i].UpdateState())
					}
				}
				for i := 1; i < sides; i++ {
					require.Equal(t, errs[0], errs[i], "seed %d step %d: errors", seed, step)
					require.Equal(t, used[0], used[i], "seed %d step %d (error %q): units", seed, step, errs[0])
				}
				compared++
				if errs[0] != "" {
					rejected++
				}
			}
			for _, txn := range txns {
				txn.Release()
			}
		}
	}
	require.Positive(t, rejected)
	t.Logf("%d commits compared, %d rejected", compared, rejected)
}

// TestContainerCheckOrder: the stepped container check visits the nil key
// first, then containers by entity ID, then by geometry.
func TestContainerCheckOrder(t *testing.T) {
	g := NewGraph()
	var containers []*Node
	for _, id := range []EntityID{9, 3, 7, 3, 1} {
		container := NewNode(id, 10, 10)
		container.TopLeft = geo.NewPoint(float64(len(containers)*100), 0)
		container.isContainer = true
		g.AddNewNodeToContainer(nil, container)
		child := NewNode(100+id, 1, 1)
		child.TopLeft = geo.NewPoint(float64(len(containers)*100+1), 1)
		g.AddNewNodeToContainer(container, child)
		containers = append(containers, container)
	}
	txn := &Transaction{Graph: g}
	for round := 0; round < 10; round++ {
		order := txn.containerCheckOrder()
		require.Len(t, order, len(g.Containers))
		require.Nil(t, order[0])
		require.True(t, slices.IsSortedFunc(order, compareNodeKeys), "order %v", order)
		ids := make([]EntityID, 0, len(order))
		for _, node := range order[1:] {
			ids = append(ids, node.ID)
		}
		require.Equal(t, []EntityID{1, 3, 3, 7, 9}, ids)
		// Equal IDs: by geometry.
		require.Same(t, containers[1], order[2], fmt.Sprintf("round %d", round))
		require.Same(t, containers[3], order[3])
	}
}

// TestExistingOverlapRegressionStepsDoNotDependOnMapOrder: a commit that
// turns one of many padded overlaps into an exact overlap stops there; the
// units charged up to it must not depend on map order.
func TestExistingOverlapRegressionStepsDoNotDependOnMapOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	build := func() (*Graph, []*Node) {
		g := NewGraph()
		var nodes []*Node
		// Pairs of nodes 3 apart: overlapping with padding, not exactly.
		for i := 0; i < 12; i++ {
			a := NewNode(EntityID(2*i+1), 40, 40)
			a.TopLeft = geo.NewPoint(float64(i*300), 0)
			b := NewNode(EntityID(2*i+2), 40, 40)
			b.TopLeft = geo.NewPoint(float64(i*300+43), 0)
			g.AddNewNodeToContainer(nil, a)
			g.AddNewNodeToContainer(nil, b)
			nodes = append(nodes, a, b)
		}
		return g, nodes
	}
	var want []int64
	for run := 0; run < 12; run++ {
		var used []int64
		for _, background := range []bool{false, true} {
			g, nodes := build()
			commitCtx := ctx
			if background {
				commitCtx = context.Background() // no Done channel: the stepped check itself
			}
			guard, err := limits.NewWorkGuard(commitCtx, "OrderTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			txn, err := g.newTransactionWithOptionsContext(commitCtx, TransactionOptions{}, guard)
			require.NoError(t, err)
			require.Positive(t, txn.PriorGraphState.existingOverlaps.nearKeys)
			// Every pair's second node moves closer to its partner, still
			// apart; the eighth pair's moves onto it: one regression, found
			// after a number of other pairs that depended on map order.
			txn.AddOp(func() error {
				for i := 1; i < len(nodes); i += 2 {
					nodes[i].TopLeft.X--
				}
				nodes[15].TopLeft.X -= 10
				return nil
			})
			err = txn.Commit(commitCtx)
			require.ErrorIs(t, err, ErrInvalidCandidate)
			used = append(used, guard.Used())
			txn.Release()
		}
		if want == nil {
			want = used
		}
		require.Equal(t, want, used, "run %d", run)
		require.Equal(t, used[0], used[1], "run %d: the batched and the stepped check", run)
	}
}
