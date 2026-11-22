package placement

import (
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// gapNoopReplay lets gapNormalization skip the outer commit of a node whose
// gap-reduction operation left the graph exactly as it found it. That covers
// every node without a connected node ahead (the operation returns at once,
// so it is not even run) and every node whose candidate moves were all
// rejected or rolled back; together most nodes of a large graph.
//
// gapNormalization runs the operation before the outer Commit, which then
// validates the result with an operation that changes nothing; Commit would
// otherwise run the operation first thing after charging the same one step.
// After an operation that changed nothing, the outer Commit, UpdateState and
// Clear mutate nothing but what the commit itself does: container re-wrapping,
// cluster and sequence syncing, the work guard and the rollback point. When
// such a real commit is accepted and leaves every tracked node box, cluster
// arrangement, tree orientation and the node order exactly as they were
// (compared bit for bit, top-left pointers included), it ran at a fixed point.
// Once two consecutive real commits have done so with equal work, the second
// ran with exactly the state and rollback point that every following commit
// after an unchanged-state operation would see, so each of those would charge
// the same units, be accepted and change nothing again. A replay charges those
// units to the same guard at once (WorkGuard.Add fails exactly when the steps
// would, with the same error and final count) and records the acceptance.
// Every other outcome resets the streak.
type gapNoopReplay struct {
	// disabled commits every iteration for real.
	disabled bool
	guard    *limits.WorkGuard
	streak   int
	units    int64
	replayed int
	before   graphGeometryFingerprint
	after    graphGeometryFingerprint
}

func (replay *gapNoopReplay) enabled() bool {
	return !replay.disabled && replay.guard != nil
}

// replayable reports whether an outer commit after an operation that left
// the state unchanged can be replayed.
func (replay *gapNoopReplay) replayable() bool {
	return replay.enabled() && replay.streak >= 2
}

// replay charges one replayed outer commit.
func (replay *gapNoopReplay) replay() error {
	replay.replayed++
	return replay.guard.Add(replay.units)
}

// begin records the state at the start of a real iteration.
func (replay *gapNoopReplay) begin(g *layoutgraph.Graph) {
	if replay.enabled() {
		replay.before.capture(g)
	}
}

// unchangedSinceBegin reports whether the state is still the one begin
// recorded.
func (replay *gapNoopReplay) unchangedSinceBegin(g *layoutgraph.Graph) bool {
	if !replay.enabled() {
		return false
	}
	replay.after.capture(g)
	return replay.before.equal(&replay.after)
}

// finish records a real outer commit that charged units. eligible means its
// operation left the state unchanged and the commit was accepted.
func (replay *gapNoopReplay) finish(g *layoutgraph.Graph, eligible bool, units int64) {
	if !replay.enabled() || !eligible || units <= 0 {
		replay.reset()
		return
	}
	replay.after.capture(g)
	if !replay.before.equal(&replay.after) {
		replay.reset()
		return
	}
	if replay.streak > 0 && units != replay.units {
		// Two fixed-point commits at the same state charge the same work;
		// anything else means the state was not a fixed point after all.
		replay.streak = 0
	}
	replay.streak++
	replay.units = units
}

func (replay *gapNoopReplay) reset() {
	replay.streak = 0
	replay.units = 0
}

// graphGeometryFingerprint is everything a geometry transaction snapshots and
// restores (see layoutgraph.GeometryImage).
type graphGeometryFingerprint struct {
	image layoutgraph.GeometryImage
}

func (fingerprint *graphGeometryFingerprint) capture(g *layoutgraph.Graph) {
	fingerprint.image.Capture(g)
}

func (fingerprint *graphGeometryFingerprint) equal(other *graphGeometryFingerprint) bool {
	return fingerprint.image.Equal(&other.image)
}
