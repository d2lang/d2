package layoutgraph

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// TestExistingOverlapRegressionCheckMatchesLegacyLoop compares the counted
// regression check with the legacy map-of-maps loop on random overlapping
// layouts: same verdict always, and the same units whenever no regression
// stops the loop early (where the legacy units depend on map order).
func TestExistingOverlapRegressionCheckMatchesLegacyLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	regressions := 0
	for seed := int64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			g := NewGraph()
			count := 3 + rng.Intn(24)
			for i := 0; i < count; i++ {
				node := NewNode(EntityID(i+1), float64(10+rng.Intn(80)), float64(10+rng.Intn(80)))
				node.TopLeft = geo.NewPoint(float64(rng.Intn(300)), float64(rng.Intn(300)))
				g.AddNodeUnchecked(node)
			}
			if rng.Intn(4) == 0 {
				// A repeated node exercises the deduplicating construction.
				g.AddNodeUnchecked(g.Nodes[rng.Intn(count)])
			}
			guard, err := limits.NewWorkGuard(ctx, "RegressionCheckTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{}, guard)
			require.NoError(t, err)

			for _, node := range g.Nodes {
				if rng.Intn(3) == 0 {
					node.TopLeft.X += float64(rng.Intn(61) - 30)
					node.TopLeft.Y += float64(rng.Intn(61) - 30)
				}
			}
			poll := newContextPoll(ctx)
			useDirty, err := txn.collectOverlapValidationNodes(&poll)
			require.NoError(t, err)
			for _, dirty := range []bool{useDirty, false} {
				run := func(legacy bool) (error, int64) {
					runGuard, err := limits.NewWorkGuard(ctx, "RegressionCheckRun", maxTransactionWorkUnits)
					require.NoError(t, err)
					txn.guard = runGuard
					steps := batchedWork(runGuard)
					runPoll := newContextPoll(ctx)
					if legacy {
						err = txn.checkExistingOverlapRegressionsLegacy(&runPoll, &steps, dirty)
					} else {
						err = txn.checkExistingOverlapRegressions(&runPoll, &steps, dirty)
					}
					if flushErr := steps.flush(); err == nil {
						err = flushErr
					}
					return err, runGuard.Used()
				}
				fastErr, fastUnits := run(false)
				legacyErr, legacyUnits := run(true)
				require.Equal(t, legacyErr, fastErr)
				if legacyErr == nil {
					require.Equal(t, legacyUnits, fastUnits)
				} else {
					require.True(t, errors.Is(legacyErr, ErrInvalidCandidate))
					regressions++
				}
			}
			txn.Rollback()
		})
	}
	require.Positive(t, regressions, "fixtures never produced a regression")
}

// TestReleasedTransactionStorageIsReusedExactly releases transactions and
// checks that the next transaction on the same graph, built in the recycled
// storage, captures exactly what a transaction built in fresh storage does,
// and that the work charged is the same.
func TestReleasedTransactionStorageIsReusedExactly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for seed := int64(0); seed < 150; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := randomContainerGraph(rng)
		for round := 0; round < 4; round++ {
			for _, node := range g.Nodes {
				if rng.Intn(3) == 0 {
					node.TopLeft.X += float64(rng.Intn(120) - 60)
				}
			}
			freshGuard, err := limits.NewWorkGuard(ctx, "ReleaseTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			freshState := &GraphState{}
			require.NoError(t, freshState.updateContext(g, freshGuard))
			freshSet, err := buildExistingOverlapSet(g, freshGuard, maxTransactionOverlapReferences)
			require.NoError(t, err)

			guard, err := limits.NewWorkGuard(ctx, "ReleaseTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{AffectContainers: true}, guard)
			require.NoError(t, err)
			// Construction charges two Finish polls and nothing else beyond the
			// capture and the sweep.
			require.Equal(t, freshGuard.Used(), guard.Used())
			state := txn.PriorGraphState
			// Nodes reached only through the container map are recorded in map
			// order, so compare the recorded sets.
			require.Equal(t, freshState.slots.size(), state.slots.size())
			for _, entry := range freshState.slots.entries {
				geometry, ok := state.slots.lookup(entry.node)
				require.True(t, ok)
				require.Equal(t, entry.geometry, geometry)
			}
			require.Equal(t, freshState.originalNodes, state.originalNodes)
			require.Equal(t, freshState.originalGeometry, state.originalGeometry)
			wantNear, wantExact := freshSet.maps()
			gotNear, gotExact := state.existingOverlaps.maps()
			require.Equal(t, overlapPairIDs(wantNear), overlapPairIDs(gotNear))
			require.Equal(t, overlapPairIDs(wantExact), overlapPairIDs(gotExact))
			require.Equal(t, freshSet.nearKeys, state.existingOverlaps.nearKeys)
			require.Equal(t, freshSet.nearRefs, state.existingOverlaps.nearRefs)
			txn.Rollback()
			txn.Release()
		}
	}
}
