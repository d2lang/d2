package gmext_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestWASMDoesNotLinkGoldmarkExtension guards d2lang/d2#2956. Importing
// github.com/yuin/goldmark/extension compiles its linkify regexps at package
// init, which overflows the JS stack of a Safari 27 Web Worker and leaves
// d2.js without an API.
func TestWASMDoesNotLinkGoldmarkExtension(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go list")
	}
	cmd := exec.Command("go", "list", "-deps", "github.com/d2lang/d2/d2js")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, pkg := range strings.Fields(string(out)) {
		if pkg == "github.com/yuin/goldmark/extension" {
			t.Fatalf("d2js links %s; use lib/textmeasure/internal/gmext instead", pkg)
		}
	}
}
