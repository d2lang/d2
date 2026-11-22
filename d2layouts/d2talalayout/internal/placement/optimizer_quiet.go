package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// This file lets the sized optimizer skip a swap search or a transpose whose
// outcome it already knows: the node's last swap search found no candidate,
// or its last transpose failed, in the very graph state the node is in again.
//
// With the score memo on, its epoch and move clock name the graph state:
// every change the optimizer or its owner makes advances the move clock (a
// move) or starts a new epoch (everything else; see optimizerScoreMemo). The
// routing costs are stamped as well. The swap search and transpose are
// deterministic functions of that state: the swap search's shuffle only
// orders its candidates, and when no candidate qualifies in one order none
// does in any order. So in an unchanged state they find nothing again, and
// the layout is the same without them. Skipping them changes only the work
// charged to the budgets; the swap search's random draws are still made, so
// the random stream is unchanged. A failed transpose is recorded only when it
// left the state as it was (the memo kept its epoch: it opened no
// transaction, or its rollback restored every node, cluster and herd fence).

// optimizerStateStamp names a graph state (see above).
type optimizerStateStamp struct {
	version, clock uint64
	turn, crossing uint64
}

// optimizerQuietNode remembers, for one graph node, the states in which its
// last swap search found nothing and its last transpose failed without
// changing anything. Zero stamps match no state (epochs start at 1).
type optimizerQuietNode struct {
	node        *layoutgraph.Node
	noSwap      optimizerStateStamp
	noTranspose optimizerStateStamp
}

// optimizerQuietState is a node's entry with the state it is in now; entry
// is nil when the memo cannot name states (off or suspended).
type optimizerQuietState struct {
	entry *optimizerQuietNode
	stamp optimizerStateStamp
}

// stateStamp returns the current state's stamp; ok is false when the memo is
// off or suspended (its epoch and clock do not track every change then).
func (optim *sizedOptimizer) stateStamp() (stamp optimizerStateStamp, ok bool) {
	memo := &optim.scores
	if !memo.enabled || memo.suspended {
		return optimizerStateStamp{}, false
	}
	costs := optim.g.RoutingCosts()
	return optimizerStateStamp{
		version: memo.version, clock: memo.clock,
		turn: math.Float64bits(costs.Turn), crossing: math.Float64bits(costs.Crossing),
	}, true
}

// quietState returns the entry of node, the graph node at index, with the
// current state.
func (optim *sizedOptimizer) quietState(index int, node *layoutgraph.Node) optimizerQuietState {
	stamp, ok := optim.stateStamp()
	if !ok {
		return optimizerQuietState{}
	}
	if n := len(optim.g.Nodes); len(optim.quiet) != n {
		if cap(optim.quiet) < n {
			optim.quiet = make([]optimizerQuietNode, n)
		} else {
			optim.quiet = optim.quiet[:n]
			clear(optim.quiet)
		}
	}
	entry := &optim.quiet[index]
	if entry.node != node {
		*entry = optimizerQuietNode{node: node}
	}
	return optimizerQuietState{entry: entry, stamp: stamp}
}

// unchanged reports whether the graph is still in the state quiet names.
func (optim *sizedOptimizer) unchanged(quiet optimizerQuietState) bool {
	stamp, ok := optim.stateStamp()
	return ok && quiet.entry != nil && stamp == quiet.stamp
}

// swapCandidateUnlessQuiet is bestSwapCandidateAfterMove, skipped when
// node's last swap search found nothing in the current state.
func (optim *sizedOptimizer) swapCandidateUnlessQuiet(ctx context.Context, node *layoutgraph.Node, quiet optimizerQuietState, guard *limits.OptimizationWorkGuard) (*layoutgraph.Node, error) {
	if quiet.entry != nil && quiet.entry.noSwap == quiet.stamp {
		stats := optim.scores.stats
		if stats == nil || !stats.verify {
			if stats != nil {
				stats.quietSwaps++
			}
			return nil, optim.skipSwapSearch(guard)
		}
		// Tests: search anyway; it must find nothing.
		stats.quietSwaps++
		candidate, err := optim.bestSwapCandidateAfterMove(ctx, node, guard)
		if err == nil && candidate != nil {
			stats.quietMismatch(fmt.Sprintf("node %d: skipped swap search finds %d", node.ID, candidate.ID))
		}
		return candidate, err
	}
	candidate, err := optim.bestSwapCandidateAfterMove(ctx, node, guard)
	if err == nil && candidate == nil && optim.unchanged(quiet) {
		quiet.entry.noSwap = quiet.stamp
	}
	return candidate, err
}

// transposeUnlessQuiet charges and runs transposeTracked, unless node's last
// transpose failed in the current state without changing it.
func (optim *sizedOptimizer) transposeUnlessQuiet(ctx context.Context, node *layoutgraph.Node, quiet optimizerQuietState, guard *limits.OptimizationWorkGuard) (bool, error) {
	skip := quiet.entry != nil && quiet.entry.noTranspose == quiet.stamp
	stats := optim.scores.stats
	if skip {
		if stats != nil {
			stats.quietTransposes++
		}
		if stats == nil || !stats.verify {
			return false, nil
		}
	}
	if err := chargeOptimizerTranspose(optim.g, node, optim.edgeAbductions, guard); err != nil {
		return false, err
	}
	ok, err := optim.transposeTracked(ctx, node)
	if err != nil {
		return false, err
	}
	if err := guard.Step(); err != nil {
		return false, err
	}
	if !ok && optim.unchanged(quiet) {
		quiet.entry.noTranspose = quiet.stamp
	} else if skip {
		// Tests: the skipped transpose must fail and change nothing.
		stats.quietMismatch(fmt.Sprintf("node %d: skipped transpose returns %v or changes the state", node.ID, ok))
	}
	return ok, nil
}

// skipSwapSearch stands for a swap search known to find nothing: it makes
// the search's random draws (the shuffle of the graph's node indices; the
// draws depend only on the length) and drops the scores moveNodeToBest left
// for it.
func (optim *sizedOptimizer) skipSwapSearch(guard *limits.OptimizationWorkGuard) error {
	optim.current = optimizerCurrentScore{}
	n := len(optim.g.Nodes)
	if cap(optim.swapOrder) < n {
		optim.swapOrder = make([]int, n)
	}
	return limits.Shuffle(optim.swapOrder[:n], optim.randGenerator, guard)
}

// releaseQuiet drops the node references.
func (optim *sizedOptimizer) releaseQuiet() {
	clear(optim.quiet)
	optim.quiet = optim.quiet[:0]
}

// quietMismatch records a wrong skip (tests only).
func (stats *scoreMemoStats) quietMismatch(what string) {
	if stats.mismatches == 0 {
		stats.firstMismatch = what
	}
	stats.mismatches++
}
