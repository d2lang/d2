package placementcost

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
)

func withTestLogger(ctx context.Context, tb testlog.TB) context.Context {
	tb.Helper()
	return log.With(ctx, testlog.New(tb))
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

func computeSymmetryScore(ctx context.Context, node *layoutgraph.Node, neighbors []*layoutgraph.Node) (float64, map[*layoutgraph.Node]struct{}, error) {
	if err := ctx.Err(); err != nil {
		return 0, nil, fmt.Errorf("EdgeLength: %w", err)
	}
	if len(neighbors) < 2 {
		return 0, nil, nil
	}

	matchedFlags := make([]bool, len(neighbors))
	score, err := computeSymmetryScoreInto(ctx, node, neighbors, matchedFlags)
	if err != nil {
		return 0, nil, err
	}
	matched := make(map[*layoutgraph.Node]struct{}, len(neighbors)/2)
	for index, ok := range matchedFlags {
		if err := scoringCancellationError(ctx, index); err != nil {
			return 0, nil, err
		}
		if ok {
			matched[neighbors[index]] = struct{}{}
		}
	}
	return score, matched, nil
}

func scoringCancellationError(ctx context.Context, iteration int) error {
	if iteration%scoringCancellationCheckInterval != 0 {
		return nil
	}
	return checkScoringCancellation(ctx)
}

// Keep the interval check small enough to inline into the scoring loops. The
// context call and error formatting are only needed at a polling boundary.
func checkScoringCancellation(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("EdgeLength: %w", err)
	}
	return nil
}

// nodeSymmetry is the internal entry point with a context.
func nodeSymmetry(ctx context.Context, node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, checkNeighbors bool) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	p.symmetry = getSymmetryWorkspace()
	defer putSymmetryWorkspace(p.symmetry)
	return nodeSymmetryPolled(&p, node, edgeAbductions, checkNeighbors)
}

// computeSymmetryScoreInto writes match state into caller-owned storage so the
// optimizer can score symmetry without allocating.
func computeSymmetryScoreInto(ctx context.Context, node *layoutgraph.Node, neigh []*layoutgraph.Node, matchedSlice []bool) (float64, error) {
	p := newScoringPoll(ctx)
	return symmetryScoreInto(&p, node, neigh, matchedSlice)
}
