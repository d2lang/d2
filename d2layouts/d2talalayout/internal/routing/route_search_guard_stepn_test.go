package routing

import (
	"context"
	"testing"
)

// stepN must leave a guard in exactly the state n step calls leave it in,
// including after exhaustion, because the straight-line fallback keeps using
// an exhausted flavor guard.
func TestRouteSearchWorkGuardStepNMatchesSteps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, limit := range []uint64{0, 1, 5, 1023, 1024, 1025, 3000} {
		for _, used := range []uint64{0, 1, 4, 1022, 1023, 1024, 2047, 2999, 3000} {
			if used > limit {
				continue
			}
			for _, nextCheck := range []uint64{0, used, used + 1, used + 1024, 1024, 2048} {
				for _, n := range []int{0, 1, 2, 3, 1023, 1024, 1025, 4000} {
					literal := &routeSearchWorkGuard{ctx: ctx, done: ctx.Done(), flavor: Default, used: used, limit: limit, nextCheck: nextCheck}
					bulk := *literal
					var literalErr error
					for range n {
						if literalErr = literal.step(); literalErr != nil {
							break
						}
					}
					bulkErr := bulk.stepN(n)
					if (literalErr == nil) != (bulkErr == nil) ||
						(literalErr != nil && literalErr.Error() != bulkErr.Error()) {
						t.Fatalf("limit %d used %d next %d n %d: errors %v vs %v", limit, used, nextCheck, n, literalErr, bulkErr)
					}
					if literal.used != bulk.used || literal.nextCheck != bulk.nextCheck {
						t.Fatalf("limit %d used %d next %d n %d: state (%d, %d) vs (%d, %d)",
							limit, used, nextCheck, n, literal.used, literal.nextCheck, bulk.used, bulk.nextCheck)
					}
				}
			}
		}
	}
}
