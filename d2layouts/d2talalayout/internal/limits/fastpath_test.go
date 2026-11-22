package limits

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
)

// The guards' inlined fast paths must be observationally identical to the
// original accounting: the same used totals, the same errors, and the same
// context polls at the same charges. These reference implementations are the
// original code; the tests drive both through identical random sequences.

type referenceWorkGuard struct {
	ctx      context.Context
	done     <-chan struct{}
	location string
	used     int64
	limit    int64
}

func (guard *referenceWorkGuard) Step() error {
	guard.used++
	if guard.used > guard.limit {
		return fmt.Errorf("TALA %s work exceeds limit %d", guard.location, guard.limit)
	}
	return guard.checkAtStride()
}

func (guard *referenceWorkGuard) Add(units int64) error {
	if units < 0 {
		return fmt.Errorf("TALA %s work charge must not be negative", guard.location)
	}
	if units > guard.limit || guard.used > guard.limit-units {
		previous := guard.used
		guard.used = guard.limit + 1
		stride := guard.pollingStride()
		if previous <= guard.limit && previous/stride != guard.limit/stride {
			if err := guard.Check(); err != nil {
				return err
			}
		}
		return fmt.Errorf("TALA %s work exceeds limit %d", guard.location, guard.limit)
	}
	previous := guard.used
	guard.used += units
	stride := guard.pollingStride()
	if guard.used%stride != 0 && previous/stride == guard.used/stride {
		return nil
	}
	return guard.Check()
}

func (guard *referenceWorkGuard) checkAtStride() error {
	if guard.used%guard.pollingStride() != 0 {
		return nil
	}
	return guard.Check()
}

func (guard *referenceWorkGuard) pollingStride() int64 {
	if guard.done == nil {
		return contextCheckStride
	}
	return cancellableContextCheckStride
}

func (guard *referenceWorkGuard) Check() error {
	if err := cachedContextErr(guard.ctx, guard.done); err != nil {
		return fmt.Errorf("%s: %w", guard.location, err)
	}
	return nil
}

type referenceOptimizationWorkGuard struct {
	ctx      context.Context
	location string
	used     uint64
	limit    uint64
}

func (guard *referenceOptimizationWorkGuard) Add(amount uint64) error {
	if amount == 0 {
		return guard.Check()
	}
	if math.MaxUint64-guard.used < amount {
		return fmt.Errorf("%w: %s work arithmetic overflow", ErrOptimizationResourceLimit, guard.location)
	}
	previous := guard.used
	next := previous + amount
	if next > guard.limit {
		return fmt.Errorf("%w: TALA %s work exceeds limit %d", ErrOptimizationResourceLimit, guard.location, guard.limit)
	}
	guard.used = next
	if previous/optimizationContextCheckStride != next/optimizationContextCheckStride {
		return guard.Check()
	}
	return nil
}

func (guard *referenceOptimizationWorkGuard) Check() error {
	if err := guard.ctx.Err(); err != nil {
		return fmt.Errorf("%s: %w", guard.location, err)
	}
	return nil
}

// countingErrContext has no Done channel, so every poll is an observable Err
// call; it reports cancellation from the cancelAt-th call on.
type countingErrContext struct {
	context.Context
	calls, cancelAt int
}

func (ctx *countingErrContext) Err() error {
	ctx.calls++
	if ctx.cancelAt > 0 && ctx.calls >= ctx.cancelAt {
		return context.Canceled
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

func TestWorkGuardFastPathMatchesReference(t *testing.T) {
	random := rand.New(rand.NewSource(7))
	limits := []int64{0, 1, 63, 64, 65, 1023, 1024, 1025, 5000, math.MaxInt64}
	for trial := 0; trial < 400; trial++ {
		limit := limits[trial%len(limits)]
		withDone := trial%2 == 1
		var gotCtx, wantCtx context.Context
		var cancelGot, cancelWant func()
		var gotCount, wantCount *countingErrContext
		if withDone {
			gotCtx, cancelGot = context.WithCancel(context.Background())
			wantCtx, cancelWant = context.WithCancel(context.Background())
			defer cancelGot()
			defer cancelWant()
		} else {
			cancelAt := 2 + random.Intn(200) // the constructor's poll succeeds
			gotCount = &countingErrContext{Context: context.Background(), cancelAt: cancelAt}
			wantCount = &countingErrContext{Context: context.Background(), cancelAt: cancelAt}
			gotCtx, wantCtx = gotCount, wantCount
			cancelGot, cancelWant = func() {}, func() {}
		}
		got, err := NewWorkGuard(gotCtx, "fast", limit)
		if err != nil {
			t.Fatal(err)
		}
		want := &referenceWorkGuard{ctx: wantCtx, done: wantCtx.Done(), location: "fast", limit: limit}
		if err := want.Check(); err != nil {
			t.Fatal(err)
		}
		cancelStep := random.Intn(6000)
		for op := 0; op < 6000; op++ {
			if op == cancelStep {
				cancelGot()
				cancelWant()
			}
			var gotErr, wantErr error
			switch r := random.Intn(100); {
			case r < 90:
				gotErr, wantErr = got.Step(), want.Step()
			case r < 95:
				units := int64(random.Intn(130))
				gotErr, wantErr = got.Add(units), want.Add(units)
			default:
				units := int64(random.Intn(3000))
				gotErr, wantErr = got.Add(units), want.Add(units)
			}
			if errString(gotErr) != errString(wantErr) || got.used != want.used {
				t.Fatalf("trial %d op %d: got (%s, %d), want (%s, %d)", trial, op, errString(gotErr), got.used, errString(wantErr), want.used)
			}
			if gotCount != nil && gotCount.calls != wantCount.calls {
				t.Fatalf("trial %d op %d: %d polls, want %d", trial, op, gotCount.calls, wantCount.calls)
			}
		}
	}
}

func TestOptimizationWorkGuardFastPathMatchesReference(t *testing.T) {
	random := rand.New(rand.NewSource(11))
	limits := []uint64{0, 1, 63, 64, 65, 127, 128, 1000, 100_000, math.MaxUint64 - 5, math.MaxUint64}
	for trial := 0; trial < 400; trial++ {
		limit := limits[trial%len(limits)]
		cancelAt := 0
		if trial%3 != 0 {
			cancelAt = 2 + random.Intn(300) // the constructor's poll succeeds
		}
		gotCtx := &countingErrContext{Context: context.Background(), cancelAt: cancelAt}
		wantCtx := &countingErrContext{Context: context.Background(), cancelAt: cancelAt}
		got, err := NewOptimizationWorkGuard(gotCtx, "fast", limit)
		if err != nil {
			t.Fatal(err)
		}
		want := &referenceOptimizationWorkGuard{ctx: wantCtx, location: "fast", limit: limit}
		if err := want.Check(); err != nil {
			t.Fatal(err)
		}
		for op := 0; op < 4000; op++ {
			var gotErr, wantErr error
			switch r := random.Intn(100); {
			case r < 70:
				gotErr, wantErr = got.Step(), want.Add(1)
			case r < 85:
				amount := uint64(random.Intn(70))
				gotErr, wantErr = got.Add(amount), want.Add(amount)
			case r < 95:
				amount := uint64(random.Intn(5000))
				gotErr, wantErr = got.Add(amount), want.Add(amount)
			case r < 98:
				amount := math.MaxUint64 - uint64(random.Intn(3))
				gotErr, wantErr = got.Add(amount), want.Add(amount)
			default:
				amount := limit - want.used + uint64(random.Intn(3)) - 1
				gotErr, wantErr = got.Add(amount), want.Add(amount)
			}
			if errString(gotErr) != errString(wantErr) || got.used != want.used || gotCtx.calls != wantCtx.calls {
				t.Fatalf("trial %d op %d: got (%s, %d, %d polls), want (%s, %d, %d polls)", trial, op, errString(gotErr), got.used, gotCtx.calls, errString(wantErr), want.used, wantCtx.calls)
			}
		}
	}
}
