//go:build slowtests

package d2dagrelayout

// A test binary built with -tags slowtests runs the tests that D2_SLOW=1
// enables (slowTests). Under TinyGo the tag is the only way to run them:
// `tinygo test -target=wasm` builds the test flags into the js/wasm program
// but gives it no environment variables, so os.Getenv("D2_SLOW") is empty
// there whatever the shell sets.
func init() { slowTests = true }
