package placement

import (
	"os"
	"testing"
)

// skipSlowUnderRace skips a test that takes about secs seconds under the race
// detector, unless D2_SLOW=1. Without -race each of these tests takes 4 to 6 s
// and always runs. With -race the package took 338 s run alone (-p 1, Apple
// M4, load 9 to 12), 278 s of it in the five tests that call this, over the 5 minutes a package's tests should take with -race; within d2's ci.yml step
// `go test -race ./d2layouts/d2talalayout/... -timeout=15m` (-p 4, load 15 to
// 30) it took 487 s, which made the step 1.8 times as long as the original's
// (285 s). With them skipped the package takes 59 s with -race.
// Under -race, Place still runs in the package's other tests and in the
// engine's and the adapter's tests.
func skipSlowUnderRace(t *testing.T, secs int) {
	t.Helper()
	if raceEnabled && os.Getenv("D2_SLOW") != "1" {
		t.Skipf("about %d s under -race; set D2_SLOW=1 to run it", secs)
	}
}
