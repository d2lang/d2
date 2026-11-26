//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"

	"github.com/d2lang/util-go/go2"
)

// The module leaves out isometric rendering. Compile still resolves the mode as
// the Go library does, and every render that asks for it fails the same way.
func TestIsometricWASMUnavailable(t *testing.T) {
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
			if test.want {
				assertIsometricUnavailable(t, err)
			} else if err != nil {
				t.Fatal(err)
			} else if !strings.Contains(string(out.([]byte)), "<svg") {
				t.Fatalf("flat render is not an SVG")
			}
			_, err = RenderSVG(wasmJSONArgument(t, request))
			if test.want {
				assertIsometricUnavailable(t, err)
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}

	compiled, err := Compile(wasmJSONArgument(t, CompileRequest{FS: map[string]string{"index": "a"}, Opts: &CompileOptions{}}))
	if err != nil {
		t.Fatal(err)
	}
	result := compiled.(CompileResponse)
	for _, opts := range []RenderOptions{
		{ASCII: go2.Pointer(true)},
		{ForceAppendix: go2.Pointer(true)},
		{Target: go2.Pointer("*"), AnimateInterval: go2.Pointer(int64(1000))},
		{DarkThemeID: go2.Pointer(int64(200))},
	} {
		opts.Isometric = go2.Pointer(true)
		_, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &opts}))
		assertIsometricUnavailable(t, err)
	}
}

func assertIsometricUnavailable(t *testing.T, err error) {
	t.Helper()
	wasmErr, ok := err.(*WASMError)
	if !ok || wasmErr.Code != 400 || wasmErr.Message != errIsometric {
		t.Fatalf("error = %#v, want 400 %q", err, errIsometric)
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
