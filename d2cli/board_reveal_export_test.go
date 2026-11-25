package d2cli

import (
	"bytes"
	"image/gif"
	"testing"
)

func TestLinkedConnectionArrivalExports(t *testing.T) {
	const source = `vars: {d2-config: {layout-engine: tala; animate-interval: 600ms; animate-duration: 400ms}}
a: Hello
steps: {append: {a -> b: Details {link: https://example.com/details; tooltip: More details}}}`
	dir := t.TempDir()
	svg, err := imageCLI(t, dir, source, "--stdout-format=svg", "-")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(svg, []byte(`href="https://example.com/details"`)) {
		t.Fatal("settled caption lost its link")
	}
	raw, err := imageCLI(t, dir, source, "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	result, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil || len(result.Image) < 3 {
		t.Fatalf("expected an animated GIF: %v", err)
	}
}
