//go:build js && wasm && !scheduler.none

package main

// With a scheduler the program has to stay alive to receive calls.
func main() {
	register()
	select {}
}
