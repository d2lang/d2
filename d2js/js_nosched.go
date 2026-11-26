//go:build js && wasm && scheduler.none

package main

// Without a scheduler D2 is built as a reactor (-buildmode=c-shared): the
// host runs the initializers and then calls in, and main never runs.
func init() {
	register()
}

func main() {}
