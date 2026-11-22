package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// compactionSchedule runs the compactions of one placeNodesOrthogonally
// call (one level), skipping a compaction known to change nothing: the last
// compaction with the same options left the level as it found it, and the
// level is still in that state.
//
// A compaction is a deterministic function of its options and of the
// level's state that placement changes: the geometry of the level's nodes
// and of their container, cluster and sequence members, the cluster
// arrangements, the herd fences and the routing costs, which
// optimizerStateImage records bit for bit (with the level's node order).
// Topology, labels and sizes of other entities do not change while a level
// is placed (the score memo relies on the same). So in a recorded state it
// changes nothing again, and skipping it changes only the work charged to
// its budgets. A compaction that changed the state, failed, or found a node
// it cannot image is not recorded.
type compactionSchedule struct {
	entries [compactionScheduleEntries]compactionScheduleEntry
	next    int
	// before images the state before the compaction that is running.
	before optimizerStateImage
	// stats counts the skipped compactions and, with verify, has them run
	// and checked (tests only; from the context, see scoreMemoStats).
	stats *scoreMemoStats
}

// compactionScheduleEntries covers the option sets that recur within a
// level: the sizeless phase's two axes and the sized phase's two axes once
// its factor has reached 1.
const compactionScheduleEntries = 4

type compactionScheduleEntry struct {
	key   compactionScheduleKey
	image optimizerStateImage
	valid bool
}

type compactionScheduleKey struct {
	axis          layoutAxis
	factor        uint64
	includeSizes  bool
	transition    bool
	abductions    bool
	moveWorkLimit uint64
}

// skipKnownNoOpCompactions lets compactionSchedule skip compactions known to
// change nothing (tests compare both ways).
var skipKnownNoOpCompactions = true

// run is compaction(ctx, g, options) for the level g; options.edgeAbductions
// must be nil or the same slice in every call of one schedule.
func (schedule *compactionSchedule) run(ctx context.Context, g *layoutgraph.Graph, options compactionOptions) error {
	if !skipKnownNoOpCompactions {
		return compaction(ctx, g, options)
	}
	key := compactionScheduleKey{
		axis: options.axis, factor: math.Float64bits(options.factor),
		includeSizes: options.includeSizes, transition: options.transition,
		abductions: options.edgeAbductions != nil, moveWorkLimit: options.moveWorkLimit,
	}
	for i := range schedule.entries {
		entry := &schedule.entries[i]
		if entry.valid && entry.key == key && entry.image.unchanged(g) {
			if stats := schedule.stats; stats != nil {
				stats.compactionSkips++
				if stats.verify {
					// Tests: run it anyway; it must change nothing.
					if err := compaction(ctx, g, options); err != nil {
						return err
					}
					if !entry.image.unchanged(g) {
						stats.quietMismatch(fmt.Sprintf("a skipped %v compaction (factor %v) changes the level", options.axis, options.factor))
					}
					return nil
				}
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("Compaction: %w", err)
			}
			return nil
		}
	}
	before := &schedule.before
	before.captureGraph(g, 1, 0)
	if err := compaction(ctx, g, options); err != nil {
		return err
	}
	if before.version == 0 || !before.unchanged(g) {
		return nil
	}
	// Record the state: reuse the entry of the same options, else the next.
	slot := -1
	for i := range schedule.entries {
		if schedule.entries[i].valid && schedule.entries[i].key == key {
			slot = i
			break
		}
	}
	if slot < 0 {
		slot = schedule.next
		schedule.next = (schedule.next + 1) % len(schedule.entries)
	}
	entry := &schedule.entries[slot]
	entry.key, entry.valid = key, true
	entry.image, schedule.before = schedule.before, entry.image
	return nil
}

// release drops the node references.
func (schedule *compactionSchedule) release() {
	for i := range schedule.entries {
		schedule.entries[i].image.release()
		clear(schedule.entries[i].image.seen)
		schedule.entries[i].valid = false
	}
	schedule.before.release()
}
