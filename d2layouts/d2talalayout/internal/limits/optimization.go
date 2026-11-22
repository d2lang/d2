package limits

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"math/rand"
	"sync"
)

const (
	// MaxOptimizationWorkUnits bounds one hierarchy or placement optimization.
	MaxOptimizationWorkUnits uint64 = 250_000_000

	optimizationContextCheckStride uint64 = 64
)

// ErrOptimizationResourceLimit reports exhausted or overflowing optimization
// work accounting.
var ErrOptimizationResourceLimit = errors.New("TALA optimization resource limit exceeded")

// OptimizationWorkGuard accounts one complete hierarchy or placement
// optimization. The limit is an explicit ceiling; zero permits cancellation
// checks but no charged work.
type OptimizationWorkGuard struct {
	ctx context.Context
	// done is ctx.Done(), captured once like WorkGuard does, so Check can
	// sample a cancellable context's channel instead of calling Err.
	done     <-chan struct{}
	location string
	used     uint64
	limit    uint64
	// blockEnd is min(limit, used|(stride-1)): the largest total that an Add
	// can reach without exceeding the limit or entering the next polling
	// stride. The limit never changes and used only moves within its stride
	// between refreshes, so it stays exact until addSlow accepts a charge.
	// Zero (a zero-valued guard) sends every Add through addSlow.
	blockEnd uint64
}

// NewOptimizationWorkGuard constructs a guard and observes cancellation before
// any optimization work begins.
func NewOptimizationWorkGuard(ctx context.Context, location string, limit uint64) (*OptimizationWorkGuard, error) {
	if ctx == nil {
		return nil, fmt.Errorf("TALA %s requires a context", location)
	}
	guard := new(OptimizationWorkGuard)
	if err := guard.init(ctx, location, limit); err != nil {
		return nil, err
	}
	return guard, nil
}

// Init sets guard up exactly as NewOptimizationWorkGuard sets up the guard it
// returns (same check, errors and initial cancellation check), for a caller
// that keeps the guard in a variable or reuses one guard value instead of
// allocating one per optimization. Any earlier state is discarded; the
// caller must not use it elsewhere any more. After an error the guard must
// not be used.
func (guard *OptimizationWorkGuard) Init(ctx context.Context, location string, limit uint64) error {
	if ctx == nil {
		*guard = OptimizationWorkGuard{}
		return fmt.Errorf("TALA %s requires a context", location)
	}
	return guard.init(ctx, location, limit)
}

func (guard *OptimizationWorkGuard) init(ctx context.Context, location string, limit uint64) error {
	*guard = OptimizationWorkGuard{ctx: ctx, done: ctx.Done(), location: location, limit: limit}
	guard.refreshBlockEnd()
	return guard.Finish()
}

var optimizationWorkGuardPool = sync.Pool{New: func() any { return new(OptimizationWorkGuard) }}

// AcquireOptimizationWorkGuard is NewOptimizationWorkGuard with the guard
// taken from a pool. The caller calls Release when no reference to the guard
// is used any more.
func AcquireOptimizationWorkGuard(ctx context.Context, location string, limit uint64) (*OptimizationWorkGuard, error) {
	if ctx == nil {
		return nil, fmt.Errorf("TALA %s requires a context", location)
	}
	guard := optimizationWorkGuardPool.Get().(*OptimizationWorkGuard)
	if err := guard.init(ctx, location, limit); err != nil {
		guard.Release()
		return nil, err
	}
	return guard, nil
}

// Release clears a guard from AcquireOptimizationWorkGuard and returns it to
// the pool. The guard must not be used afterwards.
func (guard *OptimizationWorkGuard) Release() {
	*guard = OptimizationWorkGuard{}
	optimizationWorkGuardPool.Put(guard)
}

// Step charges one optimization work unit. It is Add(1) with the fast-path
// test specialized (1-1 < blockEnd-used is used < blockEnd).
func (guard *OptimizationWorkGuard) Step() error {
	if guard.used < guard.blockEnd {
		guard.used++
		return nil
	}
	return guard.addSlow(1)
}

// Add charges amount optimization work units.
//
// The inlined fast path accepts exactly the charges for which the original
// sequence in addSlow returns nil without polling: amount >= 1 (amount-1
// wraps to MaxUint64 for zero) with used+amount <= blockEnd, which also
// rules out arithmetic overflow, exceeding the limit, and crossing a polling
// stride (previous/stride != next/stride). Every other charge runs addSlow.
func (guard *OptimizationWorkGuard) Add(amount uint64) error {
	if amount-1 < guard.blockEnd-guard.used {
		guard.used += amount
		return nil
	}
	return guard.addSlow(amount)
}

func (guard *OptimizationWorkGuard) refreshBlockEnd() {
	guard.blockEnd = min(guard.limit, guard.used|(optimizationContextCheckStride-1))
}

func (guard *OptimizationWorkGuard) addSlow(amount uint64) error {
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
	guard.refreshBlockEnd()
	if previous/optimizationContextCheckStride != next/optimizationContextCheckStride {
		return guard.Check()
	}
	return nil
}

// AddProduct multiplies two work dimensions without allowing arithmetic
// overflow.
//
// The product overflows exactly when its high word is not zero, which is
// the original test a != 0 && b > MaxUint64/a without the division: for
// a != 0, b > floor(MaxUint64/a) holds exactly when a*b > MaxUint64.
func (guard *OptimizationWorkGuard) AddProduct(a, b uint64) error {
	hi, product := bits.Mul64(a, b)
	if hi != 0 {
		return fmt.Errorf("%w: %s work arithmetic overflow", ErrOptimizationResourceLimit, guard.location)
	}
	return guard.Add(product)
}

// AddSort charges the comparison bound for sorting length values.
func (guard *OptimizationWorkGuard) AddSort(length int) error {
	if length <= 1 {
		return guard.Check()
	}
	return guard.AddProduct(uint64(length), uint64(bits.Len64(uint64(length-1))))
}

// Check observes optimization cancellation immediately.
func (guard *OptimizationWorkGuard) Check() error {
	if err := cachedContextErr(guard.ctx, guard.done); err != nil {
		return fmt.Errorf("%s: %w", guard.location, err)
	}
	return nil
}

// Finish observes cancellation at an optimization boundary.
func (guard *OptimizationWorkGuard) Finish() error {
	return guard.Check()
}

// Location returns the operation name used in diagnostics.
func (guard *OptimizationWorkGuard) Location() string {
	return guard.location
}

// Used returns the accepted work units.
func (guard *OptimizationWorkGuard) Used() uint64 {
	return guard.used
}

// Shuffle is math/rand.Shuffle's Fisher-Yates implementation with a guard
// check before every random draw. Its reduction algorithm and draw order are
// intentionally identical so seeded layout output remains stable.
func Shuffle[T any](values []T, random *rand.Rand, guard *OptimizationWorkGuard) error {
	if random == nil {
		return fmt.Errorf("TALA %s shuffle requires a random generator", guard.location)
	}
	i := len(values) - 1
	for ; i > 1<<31-1-1; i-- {
		if err := guard.Step(); err != nil {
			return err
		}
		j := int(random.Int63n(int64(i + 1)))
		values[i], values[j] = values[j], values[i]
	}
	for ; i > 0; i-- {
		j, err := shuffleIndex(random, int32(i+1), guard)
		if err != nil {
			return err
		}
		values[i], values[j] = values[j], values[i]
	}
	return nil
}

func shuffleIndex(random *rand.Rand, n int32, guard *OptimizationWorkGuard) (int32, error) {
	if err := guard.Step(); err != nil {
		return 0, err
	}
	v := random.Uint32()
	product := uint64(v) * uint64(n)
	low := uint32(product)
	if low < uint32(n) {
		threshold := uint32(-n) % uint32(n)
		for low < threshold {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			v = random.Uint32()
			product = uint64(v) * uint64(n)
			low = uint32(product)
		}
	}
	return int32(product >> 32), nil
}
