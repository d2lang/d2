//go:build !scheduler.none

package routing

import "sync"

const flavorsRunConcurrently = true

// startRouteFlavors runs route(0) to route(n-1) concurrently and calls done
// once all have returned. It does not wait for them.
func startRouteFlavors(n int, route func(i int), done func()) {
	var workers sync.WaitGroup
	for i := range n {
		workers.Go(func() {
			route(i)
		})
	}
	go func() {
		workers.Wait()
		done()
	}()
}
