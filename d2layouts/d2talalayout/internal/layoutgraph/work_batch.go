package layoutgraph

import (
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// contextPoll replaces ctx.Err() polls in hot validation loops. Contexts
// without a Done channel are still asked on every poll, so a context that
// counts its Err calls sees the same calls. For a context with a Done channel
// (Err is nil until Done is closed) only every pollStride-th poll looks at the
// channel: cancellation is noticed at most pollStride polls later, and an
// uncancelled run is unaffected.
type contextPoll struct {
	ctx   context.Context
	done  <-chan struct{}
	polls uint32
}

const pollStride = 32

func newContextPoll(ctx context.Context) contextPoll {
	return contextPoll{ctx: ctx, done: ctx.Done()}
}

func (poll *contextPoll) err() error {
	if poll.done == nil {
		return poll.ctx.Err()
	}
	poll.polls++
	if poll.polls%pollStride != 0 {
		return nil
	}
	return poll.check()
}

// skip accounts for n strided polls that a batched loop did not make one at a
// time: the poll counter advances by n and the channel is looked at once when
// any of those polls would have looked. Only for contexts with a Done channel.
func (poll *contextPoll) skip(n int64) error {
	if n <= 0 {
		return nil
	}
	before := poll.polls
	poll.polls += uint32(n)
	if n >= pollStride || before/pollStride != poll.polls/pollStride {
		return poll.check()
	}
	return nil
}

// check always looks at the context; it is used where work stops between
// caller operations, so a cancellation stops the next one from running.
func (poll *contextPoll) check() error {
	if poll.done == nil {
		return poll.ctx.Err()
	}
	select {
	case <-poll.done:
		return poll.ctx.Err()
	default:
		return nil
	}
}

// batchedSteps replaces a run of guard.Step calls by one guard.Add of the same
// number of units. The units are charged (flush) before every return and
// before any call that could observe the guard, so the guard's total and the
// first error a caller sees are unchanged: a deferred unit run exceeds the
// limit exactly when one of its Steps would have. Guards other than a
// *limits.WorkGuard are stepped directly.
type batchedSteps struct {
	guard   workStepper
	direct  *limits.WorkGuard
	pending int64
}

func newBatchedSteps(guard workStepper) batchedSteps {
	direct, _ := guard.(*limits.WorkGuard)
	return batchedSteps{guard: guard, direct: direct}
}

// batchedWork batches units for a concrete work guard.
func batchedWork(guard *limits.WorkGuard) batchedSteps {
	return batchedSteps{guard: guard, direct: guard}
}

func (steps *batchedSteps) step() error {
	if steps.direct != nil {
		steps.pending++
		return nil
	}
	return steps.guard.Step()
}

func (steps *batchedSteps) add(units int64) error {
	if steps.direct != nil {
		steps.pending += units
		return nil
	}
	for range units {
		if err := steps.guard.Step(); err != nil {
			return err
		}
	}
	return nil
}

func (steps *batchedSteps) flush() error {
	if steps.pending == 0 {
		return nil
	}
	units := steps.pending
	steps.pending = 0
	return steps.direct.Add(units)
}

// flushWith charges pending units and then reports err, so a limit failure
// that the legacy loop would have reached first still wins.
func (steps *batchedSteps) flushWith(err error) error {
	if flushErr := steps.flush(); flushErr != nil {
		return flushErr
	}
	return err
}
