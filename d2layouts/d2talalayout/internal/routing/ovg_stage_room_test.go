package routing

import (
	"context"
	"testing"
)

// TestOVGBuildGuardBatchesAgainstCurrentStageRoom charges the routing stage
// aggregate directly between OVG step batches, as RouteGraph does between
// the builds of its subgraphs (they share one build guard), and checks that
// batched steps (step, stepN) exhaust the aggregate at the same unit, with
// the same error and the same counts, as one reserveWork(1) per unit (an earlier version's batch used the room left at the previous
// build, so the aggregate failed up to a polling stride or a whole build
// later).
func TestOVGBuildGuardBatchesAgainstCurrentStageRoom(t *testing.T) {
	type outcome struct {
		err       string
		stageUsed uint64
		work      uint64
	}
	const limit = 10_000
	run := func(mode string, between []uint64, steps int) outcome {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		stage, err := newRouteWorkGuard(ctx, "EdgeRouting", limit)
		if err != nil {
			t.Fatal(err)
		}
		guard, err := newOVGBuildGuard(contextWithRouteAggregateWork(ctx, stage), defaultOVGBuildLimits())
		if err != nil {
			t.Fatal(err)
		}
		fail := func(err error) outcome { return outcome{err.Error(), stage.used, guard.work} }
		for _, charge := range between {
			// The end of a build adds every pending step.
			if err := guard.flushStage(); err != nil {
				return fail(err)
			}
			// Work charged straight to the stage before the next build.
			if err := stage.add(charge); err != nil {
				return fail(err)
			}
			for done := 0; done < steps; {
				n := 1
				var err error
				switch mode {
				case "literal":
					err = guard.reserveWork(1)
				case "step":
					err = guard.step()
				case "stepN":
					n = min(7, steps-done)
					err = guard.stepN(uint64(n))
				}
				if err != nil {
					return fail(err)
				}
				done += n
			}
		}
		if err := guard.flushStage(); err != nil {
			return fail(err)
		}
		return outcome{"", stage.used, guard.work}
	}
	cases := []struct {
		between []uint64
		steps   int
		fails   bool
	}{
		{[]uint64{9_990}, 100, true},
		{[]uint64{100, 9_000, 601}, 100, true},
		{[]uint64{100, 9_000, 600}, 100, false},
		{[]uint64{5_000, 4_000, 700}, 150, true},
		{[]uint64{0, 9_995}, 3, true},
		{[]uint64{0, 9_994}, 3, false},
		{[]uint64{3_000, 3_000, 1_001}, 1_000, true},
		{[]uint64{1, 1, 1}, 1_200, false},
	}
	for _, c := range cases {
		want := run("literal", c.between, c.steps)
		if (want.err != "") != c.fails {
			t.Fatalf("between %v, %d steps: literal outcome %+v", c.between, c.steps, want)
		}
		for _, mode := range []string{"step", "stepN"} {
			if got := run(mode, c.between, c.steps); got != want {
				t.Errorf("between %v, %d steps: %s gives %+v, one reserveWork(1) per unit %+v", c.between, c.steps, mode, got, want)
			}
		}
	}
}
