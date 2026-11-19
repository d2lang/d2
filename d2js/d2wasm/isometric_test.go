//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"

	"github.com/d2lang/util-go/go2"
)

func TestIsometricWASMCompileAndRender(t *testing.T) {
	for _, test := range []struct {
		name, source string
		option       *bool
		want         bool
	}{
		{"option", "a -> b", go2.Pointer(true), true},
		{"source", "vars: {d2-config: {isometric: true}}\na -> b", nil, true},
		{"explicit false", "vars: {d2-config: {isometric: true}}\na -> b", go2.Pointer(false), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := CompileRequest{FS: map[string]string{"index": test.source}, Opts: &CompileOptions{RenderOptions: RenderOptions{Isometric: test.option}}}
			compiled, err := Compile(wasmJSONArgument(t, request))
			if err != nil {
				t.Fatal(err)
			}
			result := compiled.(CompileResponse)
			if result.RenderOptions.Isometric == nil || *result.RenderOptions.Isometric != test.want {
				t.Fatalf("resolved isometric = %v, want %v", result.RenderOptions.Isometric, test.want)
			}
			out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &result.RenderOptions}))
			if err != nil {
				t.Fatal(err)
			}
			isometric := strings.Contains(string(out.([]byte)), "d2-isometric")
			if isometric != test.want {
				t.Fatalf("isometric output = %v, want %v", isometric, test.want)
			}
		})
	}
}

func TestIsometricWASMRejectsIncompatibleFormats(t *testing.T) {
	compiled, err := Compile(wasmJSONArgument(t, CompileRequest{FS: map[string]string{"index": "a"}, Opts: &CompileOptions{}}))
	if err != nil {
		t.Fatal(err)
	}
	result := compiled.(CompileResponse)
	for _, test := range []struct {
		name string
		opts RenderOptions
		want string
	}{
		{"sketch", RenderOptions{Sketch: go2.Pointer(true)}, "sketch cannot"},
		{"ASCII", RenderOptions{ASCII: go2.Pointer(true)}, "ASCII cannot"},
		{"appendix", RenderOptions{ForceAppendix: go2.Pointer(true)}, "SVG appendix"},
		{"animation", RenderOptions{AnimateInterval: go2.Pointer(int64(1000))}, "multi-board SVG animation"},
		{"dark theme", RenderOptions{DarkThemeID: go2.Pointer(int64(200))}, "responsive dark themes"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.opts.Isometric = go2.Pointer(true)
			_, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &test.opts}))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func wasmJSONArgument(t *testing.T, value any) []js.Value {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return []js.Value{js.ValueOf(string(encoded))}
}

func TestExplodedWASMTargets(t *testing.T) {
	const source = `vars: {d2-config: {isometric-exploded: true}}
layers: {edition: {layers: {
  upper: {a: {link: _.layers.lower}}
  lower: {b -> c}
}}}`
	compiled, err := Compile(wasmJSONArgument(t, CompileRequest{FS: map[string]string{"index": source}, Opts: &CompileOptions{}}))
	if err != nil {
		t.Fatal(err)
	}
	result := compiled.(CompileResponse)
	if result.RenderOptions.IsometricExploded == nil || !*result.RenderOptions.IsometricExploded {
		t.Fatal("compile lost exploded mode")
	}
	render := func(target *string) string {
		t.Helper()
		opts := result.RenderOptions
		opts.Target = target
		out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &opts}))
		if err != nil {
			t.Fatal(err)
		}
		return string(out.([]byte))
	}
	if render(nil) != render(go2.Pointer("*")) {
		t.Fatal("default exploded target should include layers")
	}
	if render(go2.Pointer("edition.*")) != render(go2.Pointer("layers.edition.*")) {
		t.Fatal("shorthand subtree changed exploded output")
	}
	if render(go2.Pointer("edition.*")) == render(go2.Pointer("edition.upper")) {
		t.Fatal("subtree rendered only one layer")
	}
}
