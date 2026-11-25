package placement

import (
	"context"
	"errors"
	"fmt"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// optimizerPoll answers guard.Finish() for guards created on its context
// without walking the context's value chain on every call: the Context
// contract makes Err nil until Done is closed, so with a Done channel only
// every optimizerPollStride-th call looks at it (cancellation is noticed a few
// calls later; an uncancelled run is unaffected). Contexts without a Done
// channel are still asked every time. The zero value defers to guard.Finish().
type optimizerPoll struct {
	ctx   context.Context
	done  <-chan struct{}
	polls uint32
}

const optimizerPollStride = 32

func newOptimizerPoll(ctx context.Context) optimizerPoll {
	return optimizerPoll{ctx: ctx, done: ctx.Done()}
}

// finish is guard.Finish() for a guard whose context is poll.ctx.
func (poll *optimizerPoll) finish(guard *limits.OptimizationWorkGuard) error {
	if poll.ctx == nil {
		return guard.Finish()
	}
	if poll.done != nil {
		poll.polls++
		if poll.polls%optimizerPollStride != 0 {
			return nil
		}
		select {
		case <-poll.done:
		default:
			return nil
		}
	}
	if err := poll.ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", guard.Location(), err)
	}
	return nil
}

// chargeOrReplay charges units that a pure computation counted instead of
// stepping the guard one unit at a time. OptimizationWorkGuard.Add leaves the
// guard untouched when it rejects a charge, so on a resource-limit rejection
// replay runs the legacy stepped computation from the identical guard state and
// its outcome (result, error, and remaining guard state) is returned instead.
// replayed reports whether that happened.
func chargeOrReplay(guard *limits.OptimizationWorkGuard, units uint64, replay func() error) (replayed bool, err error) {
	if units == 0 {
		return false, nil
	}
	if err := guard.Add(units); err != nil {
		if errors.Is(err, limits.ErrOptimizationResourceLimit) {
			return true, replay()
		}
		return false, err
	}
	return false, nil
}
