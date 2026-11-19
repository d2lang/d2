//go:build !js || !wasm

package codestyle

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func Get(name string) *chroma.Style {
	return styles.Get(name)
}
