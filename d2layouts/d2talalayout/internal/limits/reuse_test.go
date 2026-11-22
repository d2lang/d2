package limits

import (
	"context"
	"math"
	"math/rand"
	"testing"
)

// Guards set up by Init (also over a used guard) and by the pooled
// constructors behave exactly like the ones NewWorkGuard and
// NewOptimizationWorkGuard return: same setup errors, same used totals, same
// errors and the same context polls for the same charge sequences.
func TestReusedWorkGuardsMatchNewGuards(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	var reused WorkGuard
	var reusedOpt OptimizationWorkGuard
	for trial := 0; trial < 2000; trial++ {
		limit := int64(rng.Intn(5000))
		if rng.Intn(20) == 0 {
			limit = -1
		}
		cancelAt := 0
		if rng.Intn(3) == 0 {
			cancelAt = 1 + rng.Intn(40)
		}
		withDone := rng.Intn(2) == 0
		newCtx := func() (context.Context, *countingErrContext, context.CancelFunc) {
			counting := &countingErrContext{Context: context.Background(), cancelAt: cancelAt}
			if withDone {
				ctx, cancel := context.WithCancel(context.Background())
				if cancelAt == 1 {
					cancel()
				}
				return ctx, counting, cancel
			}
			return counting, counting, func() {}
		}
		ops := make([]int64, 1+rng.Intn(200))
		for i := range ops {
			switch rng.Intn(4) {
			case 0:
				ops[i] = -1 // Step
			case 1:
				ops[i] = int64(rng.Intn(3000))
			default:
				ops[i] = int64(rng.Intn(40))
			}
		}
		type result struct {
			setup   string
			trace   []string
			used    int64
			optUsed uint64
			polls   int
		}
		run := func(mode int) result {
			ctx, counting, cancel := newCtx()
			defer cancel()
			var res result
			var guard *WorkGuard
			var err error
			switch mode {
			case 0:
				guard, err = NewWorkGuard(ctx, "Reuse", limit)
			case 1:
				// Dirty the reused guard first.
				_ = reused.Add(17)
				err = reused.Init(ctx, "Reuse", limit)
				guard = &reused
			case 2:
				guard, err = AcquireWorkGuard(ctx, "Reuse", limit)
				if guard != nil {
					defer guard.Release()
				}
			}
			res.setup = errString(err)
			if err == nil {
				for _, op := range ops {
					if op < 0 {
						res.trace = append(res.trace, errString(guard.Step()))
					} else {
						res.trace = append(res.trace, errString(guard.Add(op)))
					}
				}
				res.used = guard.Used()
			}
			var opt *OptimizationWorkGuard
			var optErr error
			optLimit := uint64(max(limit, 0))
			switch mode {
			case 0:
				opt, optErr = NewOptimizationWorkGuard(ctx, "ReuseOpt", optLimit)
			case 1:
				_ = reusedOpt.Add(5)
				optErr = reusedOpt.Init(ctx, "ReuseOpt", optLimit)
				opt = &reusedOpt
			case 2:
				opt, optErr = AcquireOptimizationWorkGuard(ctx, "ReuseOpt", optLimit)
				if opt != nil {
					defer opt.Release()
				}
			}
			res.setup += "|" + errString(optErr)
			if optErr == nil {
				for _, op := range ops {
					if op < 0 {
						res.trace = append(res.trace, errString(opt.Step()))
					} else {
						res.trace = append(res.trace, errString(opt.Add(uint64(op))))
					}
				}
				res.optUsed = opt.Used()
			}
			res.polls = counting.calls
			return res
		}
		want := run(0)
		for mode := 1; mode <= 2; mode++ {
			got := run(mode)
			if got.setup != want.setup || got.used != want.used || got.optUsed != want.optUsed || got.polls != want.polls || len(got.trace) != len(want.trace) {
				t.Fatalf("trial %d mode %d: got %+v, want %+v", trial, mode, got, want)
			}
			for i := range want.trace {
				if got.trace[i] != want.trace[i] {
					t.Fatalf("trial %d mode %d op %d: %q, want %q", trial, mode, i, got.trace[i], want.trace[i])
				}
			}
		}
	}
	var guard WorkGuard
	//nolint:staticcheck // a nil context is the case under test
	if err := guard.Init(nil, "Nil", 1); err == nil {
		t.Fatal("Init accepted a nil context")
	}
	if g, err := AcquireWorkGuard(nil, "Nil", 1); err == nil || g != nil { //nolint:staticcheck
		t.Fatal("AcquireWorkGuard accepted a nil context")
	}
	var opt OptimizationWorkGuard
	if err := opt.Init(nil, "Nil", 1); err == nil { //nolint:staticcheck
		t.Fatal("OptimizationWorkGuard.Init accepted a nil context")
	}
}

func TestAcquiredWorkGuardsDoNotAllocate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Warm the pools.
	for range 4 {
		g, _ := AcquireWorkGuard(ctx, "Warm", 10)
		g.Release()
		o, _ := AcquireOptimizationWorkGuard(ctx, "Warm", 10)
		o.Release()
	}
	allocs := testing.AllocsPerRun(200, func() {
		g, err := AcquireWorkGuard(ctx, "Pooled", 100)
		if err != nil {
			t.Fatal(err)
		}
		_ = g.Step()
		g.Release()
		o, err := AcquireOptimizationWorkGuard(ctx, "Pooled", 100)
		if err != nil {
			t.Fatal(err)
		}
		_ = o.Step()
		o.Release()
	})
	if allocs > 0.1 {
		t.Fatalf("pooled guards allocate %.2f objects per use", allocs)
	}
}

// AddProduct's overflow test (the product's high word) is the original
// division test.
func TestAddProductOverflowMatchesDivisionTest(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	values := []uint64{0, 1, 2, 3, math.MaxUint32, math.MaxUint32 + 1, math.MaxUint64, math.MaxUint64 / 2, math.MaxUint64/2 + 1, math.MaxUint64 / 3}
	for range 2000 {
		values = append(values, rng.Uint64()>>uint(rng.Intn(64)))
	}
	for _, a := range values {
		for _, b := range values[:64] {
			for _, pair := range [2][2]uint64{{a, b}, {b, a}} {
				x, y := pair[0], pair[1]
				wantOverflow := x != 0 && y > math.MaxUint64/x
				guard, err := NewOptimizationWorkGuard(context.Background(), "Product", math.MaxUint64)
				if err != nil {
					t.Fatal(err)
				}
				err = guard.AddProduct(x, y)
				gotOverflow := err != nil && guard.Used() == 0
				if gotOverflow != wantOverflow || (!wantOverflow && guard.Used() != x*y) {
					t.Fatalf("AddProduct(%d, %d): err %v used %d, want overflow %v", x, y, err, guard.Used(), wantOverflow)
				}
			}
		}
	}
}
