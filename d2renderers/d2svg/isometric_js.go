//go:build js && wasm

package d2svg

import (
	"errors"

	"github.com/d2lang/d2/d2target"
)

// D2.js leaves isometric rendering out of its module; the CLI and the Go library
// render it (isometric.go).
func renderIsometric(*d2target.Diagram, *RenderOpts) ([]byte, error) {
	return nil, errors.New("isometric rendering is not available in D2.js")
}
