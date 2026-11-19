//go:build !js || !wasm

package codestyle

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestWASMPalettesMatchChroma(t *testing.T) {
	for _, name := range []string{Light, Dark} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(name + ".xml")
			if err != nil {
				t.Fatal(err)
			}
			palette, err := chroma.NewXMLStyle(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(palette, styles.Registry[name]) {
				t.Fatal("palette differs from the installed Chroma version; run go generate in this package")
			}
		})
	}
}
