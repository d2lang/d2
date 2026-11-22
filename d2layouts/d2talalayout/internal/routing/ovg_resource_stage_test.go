package routing

import (
	"context"
	"math/rand"
	"testing"
)

// The OVG build guard batches its single steps' stage-aggregate charges. Any
// sequence of steps and bulk charges must fail at the same operation with the
// same error, and leave the same counts, as charging every step at once.
func TestOVGBuildGuardBatchedStageMatchesPerStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	random := rand.New(rand.NewSource(7))
	for trial := range 200 {
		stageLimit := uint64(random.Intn(3000))
		ownLimit := uint64(random.Intn(3000))
		stageUsed := uint64(random.Intn(int(stageLimit) + 1))
		newPair := func(batched bool) (*ovgBuildGuard, *routeWorkGuard) {
			stage := &routeWorkGuard{ctx: ctx, done: ctx.Done(), location: "EdgeRouting", used: stageUsed, limit: stageLimit}
			limits := defaultOVGBuildLimits()
			limits.work = ownLimit
			guard, err := newOVGBuildGuard(contextWithRouteAggregateWork(ctx, stage), limits)
			if err != nil {
				t.Fatal(err)
			}
			if !batched {
				guard.stage = nil
			}
			return guard, stage
		}
		batched, batchedStage := newPair(true)
		reference, referenceStage := newPair(false)
		for op := range 400 {
			bulk := random.Intn(10) == 0
			amount := uint64(random.Intn(40))
			var gotErr, wantErr error
			if bulk {
				gotErr, wantErr = batched.reserveWork(amount), reference.reserveWork(amount)
			} else {
				gotErr, wantErr = batched.step(), reference.step()
			}
			if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) {
				t.Fatalf("trial %d op %d: error %v, want %v", trial, op, gotErr, wantErr)
			}
			if batched.work != reference.work || batchedStage.used+batched.pending != referenceStage.used {
				t.Fatalf("trial %d op %d: work %d/%d stage %d+%d/%d", trial, op,
					batched.work, reference.work, batchedStage.used, batched.pending, referenceStage.used)
			}
			if gotErr != nil {
				break
			}
		}
		if err := batched.flushStage(); err != nil {
			t.Fatal(err)
		}
		if batchedStage.used != referenceStage.used {
			t.Fatalf("trial %d: stage %d after flush, want %d", trial, batchedStage.used, referenceStage.used)
		}
	}
}
