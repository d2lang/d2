package placement

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
)

func withTestLogger(ctx context.Context, tb testlog.TB) context.Context {
	tb.Helper()
	return log.With(ctx, testlog.New(tb))
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func mustNewTransaction(t *testing.T, graph *layoutgraph.Graph, options layoutgraph.TransactionOptions) *layoutgraph.Transaction {
	t.Helper()
	txn, err := graph.NewRequestTransaction(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	return txn
}

func requireCanceledAt(t *testing.T, err error, location string) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(%v, context.Canceled) = false", err)
	}
	if !strings.Contains(err.Error(), location) {
		t.Fatalf("cancellation error = %v, want operation %q", err, location)
	}
}

type cancelAfterErrChecks struct {
	context.Context
	remaining int
}

func (ctx *cancelAfterErrChecks) Err() error {
	if ctx.remaining == 0 {
		return context.Canceled
	}
	ctx.remaining--
	return nil
}

func captureOptimizerCandidateMovement(node *layoutgraph.Node, guard *limits.OptimizationWorkGuard) (optimizerCandidateMovement, error) {
	before := guard.Used()
	positions, err := captureOptimizerNodePositions([]*layoutgraph.Node{node}, guard)
	if err != nil {
		return optimizerCandidateMovement{}, err
	}
	// Single-root capture charges one Step for each retained position in addition
	// to the descendant walk. Retain the latter's charge so every trial preserves
	// the original work boundary and cancellation polling sequence.
	return optimizerCandidateMovement{
		positions:      positions,
		descendantWork: guard.Used() - before - uint64(len(positions)),
	}, nil
}

// withOptimizerPositionsSwapped owns the rollback point for a speculative swap
// and its scoring callback. The former nested atomic swap captured the same
// geometry twice without any intervening mutation. Reuse this rollback point,
// replaying the second capture's exact work and cancellation checkpoints.
func withOptimizerPositionsSwapped(nodeA, nodeB *layoutgraph.Node, guard *limits.OptimizationWorkGuard, fn func() error) (err error) {
	if optimizerLeaf(nodeA) && optimizerLeaf(nodeB) {
		return withOptimizerLeavesSwapped(nodeA, nodeB, guard, fn)
	}
	before := guard.Used()
	snapshots, err := captureOptimizerNodePositions([]*layoutgraph.Node{nodeA, nodeB}, guard)
	if err != nil {
		return err
	}
	captureWork := guard.Used() - before
	defer restoreNodePositions(snapshots)
	if nodeA == nil || nodeB == nil || nodeA.TopLeft == nil || nodeB.TopLeft == nil {
		return fmt.Errorf("TALA %s cannot swap unpositioned nodes", guard.Location())
	}
	for range captureWork {
		if err := guard.Step(); err != nil {
			return err
		}
	}
	ax, ay := nodeA.TopLeft.X, nodeA.TopLeft.Y
	if err := optimizerMoveNodeAbs(nodeA, nodeB.TopLeft.X, nodeB.TopLeft.Y, guard); err != nil {
		return err
	}
	if err := optimizerMoveNodeAbs(nodeB, ax, ay, guard); err != nil {
		return err
	}
	return fn()
}

// moveNodeToBestGuarded moves node to the best of points (see
// moveNodeToBestStepped, the reference).
func (optim *sizedOptimizer) moveNodeToBestGuarded(ctx context.Context, node *layoutgraph.Node, points []geo.Point, mustImprove bool, guard *limits.OptimizationWorkGuard) (bool, error) {
	return optim.moveNodeToBest(ctx, node, points, mustImprove, false, guard)
}

// overlaps is optimizerScanOverlap(node, (x, y), exceptions) with
// exceptionUnits 0 for nil exceptions and 1 for []*Node{node}: the answer and
// the units the stepped scan charges. ok is false where the scan would stop
// with an error.
func (table *optimizerScanTable) overlaps(x, y float64, exceptionUnits uint64) (overlaps bool, units uint64, ok bool) {
	overlaps, units, others, ok := table.scan(x, y)
	return overlaps, units + exceptionUnits*others, ok
}

// invalidateScores discards the scores kept across optimize calls.
func (optim *sizedOptimizer) invalidateScores() {
	optim.scores.invalidate()
}

func (optim *sizedOptimizer) bestSwapCandidateGuarded(ctx context.Context, node *layoutgraph.Node, guard *limits.OptimizationWorkGuard) (*layoutgraph.Node, error) {
	return optim.bestSwapCandidate(ctx, node, nil, guard)
}
