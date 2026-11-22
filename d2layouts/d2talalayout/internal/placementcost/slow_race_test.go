package placementcost

import (
	"os"
	"testing"
)

// skipSlowUnderRace skips a test that takes about secs seconds under the race
// detector, unless D2_SLOW=1. Without -race the test always runs.
// TestEdgeLengthSessionMatchesEdgeLength takes 3 to 5 s without -race and
// about 92 s with it, on one CPU at a time (Apple M4). With 4 CPUs, as on
// GitHub's runners, it was the longest test of ci.yml's step
// `go test -race ./d2layouts/d2talalayout/...` and made the step take 1.6
// times as long as the original's (135 s against 84 s); skipped, the step
// takes 71 to 72 s against the original's 73 to 80 s (GOMAXPROCS=4). Under
// -race the session code still runs (86% of its statements) in this
// package's other session test and in the swap and equidistance stages of
// the placement, engine and adapter tests.
func skipSlowUnderRace(t *testing.T, secs int) {
	t.Helper()
	if raceEnabled && os.Getenv("D2_SLOW") != "1" {
		t.Skipf("about %d s under -race; set D2_SLOW=1 to run it", secs)
	}
}
