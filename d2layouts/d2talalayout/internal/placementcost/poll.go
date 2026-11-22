package placementcost

import (
	"context"
	"fmt"
)

// scoringSampleInterval is how many polling points a context with a Done
// channel may pass between two samples of that channel.
const scoringSampleInterval = 64

// scoringPoll carries cancellation polling through one scoring evaluation.
//
// Every polling point of the scoring code calls check (or at, for loop
// iterations), exactly where the code used to call ctx.Err().
//
// A context without a Done channel can report cancellation only through Err,
// so check calls Err at every polling point, exactly as before; every
// counting or cancelling test context is of this kind.
//
// A context with a Done channel is canceled by closing that channel (the
// context package publishes Err before closing Done, and Err waits for Done),
// so its polling points only bound the latency of observing cancellation.
// check counts them and samples the channel at every scoringSampleInterval-th
// point, and entry samples it at each top-level scoring call, so a canceled
// evaluation still stops within a bounded amount of work. Results of an
// evaluation that is not canceled do not depend on polling at all.
//
// A scoringPoll belongs to one goroutine.
type scoringPoll struct {
	ctx  context.Context
	done <-chan struct{}
	// pending counts polling points since the last sample (Done mode only).
	pending int
	// checks counts polling points (calls of check) in both modes; the
	// edge-length scorer uses it to replay its preparation's polls exactly.
	checks int
	// symmetry holds the frames of a symmetry evaluation, set by its entry.
	symmetry *symmetryWorkspace
	// sess is set while an EdgeLengthSession evaluates node terms: it records
	// what the evaluation reads and answers symmetry sub-results.
	sess *EdgeLengthSession
}

func newScoringPoll(ctx context.Context) scoringPoll {
	return scoringPoll{ctx: ctx, done: ctx.Done()}
}

func scoringCanceled(err error) error {
	return fmt.Errorf("EdgeLength: %w", err)
}

// entry is the polling point at the start of a top-level scoring call. It
// always observes cancellation.
func (p *scoringPoll) entry() error {
	p.checks++
	if p.done == nil {
		if err := p.ctx.Err(); err != nil {
			return scoringCanceled(err)
		}
		return nil
	}
	return p.sample()
}

// check is one polling point.
func (p *scoringPoll) check() error {
	p.checks++
	if p.done == nil {
		if err := p.ctx.Err(); err != nil {
			return scoringCanceled(err)
		}
		return nil
	}
	p.pending++
	if p.pending < scoringSampleInterval {
		return nil
	}
	return p.sample()
}

func (p *scoringPoll) sample() error {
	p.pending = 0
	select {
	case <-p.done:
		if err := p.ctx.Err(); err != nil {
			return scoringCanceled(err)
		}
	default:
	}
	return nil
}

// at is the polling point of a loop iteration: loops poll on every
// scoringCancellationCheckInterval-th iteration, starting with the first.
func (p *scoringPoll) at(iteration int) error {
	if iteration%scoringCancellationCheckInterval != 0 {
		return nil
	}
	return p.check()
}

// slice polls once per scoringCancellationCheckInterval elements of a slice
// of the given length, as a loop over it would.
func (p *scoringPoll) slice(length int) error {
	for start := 0; start < length; start += scoringCancellationCheckInterval {
		if err := p.check(); err != nil {
			return err
		}
	}
	return nil
}
