//go:build scheduler.none

package routing

const flavorsRunConcurrently = false

// startRouteFlavors routes the flavors one after another: a TinyGo build
// without a scheduler has no goroutines. Each result fits in the channel the
// caller made, so nothing here has to wait for the collector.
func startRouteFlavors(n int, route func(i int), done func()) {
	for i := range n {
		route(i)
	}
	done()
}
