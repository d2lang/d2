//go:build scheduler.none

package d2talalayout

const seedsRunConcurrently = false

// attemptSeeds runs the seeds one after another: a TinyGo build without a
// scheduler has no goroutines. A cooperative scheduler with one worker, which
// is what WebAssembly had, runs them in this order too.
func attemptSeeds(seeds []int64, _ int, run localSeedFunc, consider func(localSeedAttempt)) {
	for i, seed := range seeds {
		consider(run(i, seed))
	}
}
