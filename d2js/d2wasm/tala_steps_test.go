//go:build js && wasm

package d2wasm

import (
	"bytes"
	"testing"

	"github.com/d2lang/util-go/go2"
)

// Compile and Render can be separate worker calls with a JSON round trip in between.
func TestTALAStepMotionRoundTrip(t *testing.T) {
	const config = "vars: {d2-config: {layout-engine: tala; animate-interval: 300ms; animate-duration: 200ms}}\n"
	for _, tc := range []struct{ name, source, markup string }{
		{"SQL row", "catalog: {shape: sql_table; catalog_id: int; title: text}\nsteps: {next: {catalog.pages: int}}", `overflow="hidden"`},
		{"sequence reply", "shape: sequence_diagram\na: Source; b: Catalog\na -> b: index\nsteps: {next: {a <- b: ok {style.stroke-dash: 4}}}", "stroke-dashoffset:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := CompileRequest{
				FS:   map[string]string{"index": config + tc.source},
				Opts: &CompileOptions{RenderOptions: RenderOptions{Target: go2.Pointer("*")}},
			}
			compiled, err := Compile(wasmJSONArgument(t, req))
			if err != nil {
				t.Fatal(err)
			}
			result := compiled.(CompileResponse)
			if len(result.Diagram.Steps) != 1 || !result.Diagram.Steps[0].LayoutContinuity || len(result.Diagram.Steps[0].TweenFrames) < 2 {
				t.Fatal("missing intermediate TALA step")
			}
			out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &result.RenderOptions}))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(out.([]byte), []byte(tc.markup)) {
				t.Errorf("missing sampled SVG markup %q", tc.markup)
			}
			direct, err := RenderSVG(wasmJSONArgument(t, req))
			if err != nil {
				t.Fatal(err)
			}
			if direct.(string) != string(out.([]byte)) {
				t.Fatal("separate compile and render calls changed the animation")
			}
		})
	}
}
