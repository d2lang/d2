//go:build !scheduler.none

package d2talalayout

import "sync"

const seedsRunConcurrently = true

// attemptSeeds runs every seed on up to concurrency goroutines and passes each
// attempt to consider, on the caller's goroutine, in the order they finish.
func attemptSeeds(seeds []int64, concurrency int, run localSeedFunc, consider func(localSeedAttempt)) {
	jobs := make(chan int, len(seeds))
	for i := range seeds {
		jobs <- i
	}
	close(jobs)

	results := make(chan localSeedAttempt)
	var workers sync.WaitGroup
	for range concurrency {
		workers.Go(func() {
			for i := range jobs {
				results <- run(i, seeds[i])
			}
		})
	}
	go func() {
		workers.Wait()
		close(results)
	}()
	for attempt := range results {
		consider(attempt)
	}
}
