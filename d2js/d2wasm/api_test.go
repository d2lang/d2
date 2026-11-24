//go:build js && wasm

package d2wasm

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/util-go/go2"
)

func TestRenderRejectsMalformedDiagramBeforeBoardTraversal(t *testing.T) {
	request := RenderRequest{
		Diagram: &d2target.Diagram{Layers: []*d2target.Diagram{nil}},
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	call := wrapWASMCall(Render)
	defer call.Release()
	got := call.Invoke(string(encoded)).String()
	var response WASMResponse
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response %q: %v", got, err)
	}
	if response.Error == nil || response.Error.Code != 400 || !strings.Contains(response.Error.Message, "root.layers[0] is nil") {
		t.Fatalf("error = %#v, want invalid-diagram response", response.Error)
	}
}

func TestRenderAnimationDefaultsMissingPadding(t *testing.T) {
	animateInterval := int64(1_000)
	diagram := d2target.NewDiagram()
	diagram.IsFolderOnly = true
	request := RenderRequest{
		Diagram: diagram,
		Opts:    &RenderOptions{AnimateInterval: &animateInterval},
	}
	response := invokeRenderRequest(t, request)
	if response.Error != nil {
		t.Fatalf("Render() error = %#v", response.Error)
	}
	if response.Data == nil {
		t.Fatal("Render() returned no animated SVG")
	}
}

func TestSourceAnimateInterval(t *testing.T) {
	const source = "vars: {d2-config: {animate-interval: 800ms}}\nroot\nsteps: {next: {second}}"
	for _, tc := range []struct {
		name string
		opts *CompileOptions
		want int64
	}{
		{"source", nil, 800},
		{"override", &CompileOptions{RenderOptions: RenderOptions{AnimateInterval: go2.Pointer(int64(250))}}, 250},
		{"disable", &CompileOptions{RenderOptions: RenderOptions{AnimateInterval: go2.Pointer(int64(0))}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(wasmJSONArgument(t, CompileRequest{FS: map[string]string{"index": source}, Opts: tc.opts}))
			if err != nil {
				t.Fatal(err)
			}
			result := compiled.(CompileResponse)
			if interval := result.RenderOptions.AnimateInterval; interval == nil || *interval != tc.want {
				t.Fatalf("compile returned interval %v, want %d", interval, tc.want)
			}

			opts := result.RenderOptions
			if tc.opts == nil {
				// Render can also read the source setting from the diagram alone.
				opts.AnimateInterval = nil
			}
			if tc.want > 0 {
				opts.Target = go2.Pointer("*")
			}
			out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &opts}))
			if err != nil {
				t.Fatal(err)
			}
			svg := string(out.([]byte))
			if tc.want == 0 {
				if strings.Contains(svg, "d2Transition-") {
					t.Fatal("expected static SVG with an explicit zero interval")
				}
			} else if !strings.Contains(svg, fmt.Sprintf(" %dms infinite", 2*tc.want)) || !strings.Contains(svg, ">second</text>") {
				t.Fatalf("expected two animated boards displayed for %dms each", tc.want)
			}
		})
	}
}

func TestSourceScaleAndAppendix(t *testing.T) {
	const source = "vars: {d2-config: {scale: 0.5; force-appendix: true}}\nservice.tooltip: Receives requests"
	for _, tc := range []struct {
		name     string
		opts     *CompileOptions
		scale    float64
		appendix bool
	}{
		{"source", nil, 0.5, true},
		{"explicit options", &CompileOptions{RenderOptions: RenderOptions{Scale: go2.Pointer(2.0), ForceAppendix: go2.Pointer(false)}}, 2, false},
		{"responsive SVG", &CompileOptions{RenderOptions: RenderOptions{Scale: go2.Pointer(-1.0)}}, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := CompileRequest{FS: map[string]string{"index": source}, Opts: tc.opts}
			compiled, err := Compile(wasmJSONArgument(t, request))
			if err != nil {
				t.Fatal(err)
			}
			result := compiled.(CompileResponse)
			opts := &result.RenderOptions
			if opts.Scale == nil || *opts.Scale != tc.scale || opts.ForceAppendix == nil || *opts.ForceAppendix != tc.appendix {
				t.Fatalf("unexpected compiled render options: %+v", opts)
			}
			if tc.opts == nil {
				opts = nil // Use just the source config in the serialized diagram.
			}
			rendered, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: opts}))
			if err != nil {
				t.Fatal(err)
			}
			direct, err := RenderSVG(wasmJSONArgument(t, request))
			if err != nil {
				t.Fatal(err)
			}
			for _, svg := range []string{string(rendered.([]byte)), direct.(string)} {
				if strings.Contains(svg, `class="appendix"`) != tc.appendix {
					t.Fatalf("expected appendix = %v", tc.appendix)
				}
				var root struct {
					Width   string `xml:"width,attr"`
					ViewBox string `xml:"viewBox,attr"`
				}
				if err := xml.Unmarshal([]byte(svg), &root); err != nil {
					t.Fatal(err)
				}
				var expectedWidth string
				if tc.scale > 0 {
					var width, height int
					if _, err := fmt.Sscanf(root.ViewBox, "0 0 %d %d", &width, &height); err != nil {
						t.Fatal(err)
					}
					expectedWidth = strconv.Itoa(int(math.Ceil(float64(width) * tc.scale)))
				}
				if root.Width != expectedWidth {
					t.Fatalf("SVG width = %q, want %q for viewBox %q at scale %g", root.Width, expectedWidth, root.ViewBox, tc.scale)
				}
			}
		})
	}
}

func TestRenderAnimatedAppendix(t *testing.T) {
	animateInterval := int64(1_000)
	forceAppendix := true
	target := "*"
	compiled, err := Compile(wasmJSONArgument(t, CompileRequest{
		FS: map[string]string{"index": "home\nscenarios: {detail: {back.link: _}}"},
		Opts: &CompileOptions{RenderOptions: RenderOptions{
			AnimateInterval: &animateInterval,
			ForceAppendix:   &forceAppendix,
			Target:          &target,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	result := compiled.(CompileResponse)
	out, err := Render(wasmJSONArgument(t, RenderRequest{Diagram: &result.Diagram, Opts: &result.RenderOptions}))
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out.([]byte))
	if strings.Count(svg, `class="appendix"`) != 1 || !strings.Contains(svg, ">root</text>") {
		t.Fatal("expected one appendix containing the parent-board link")
	}
}

func TestRenderAnimateDurationAfterCompile(t *testing.T) {
	source := `vars: {charge: 100%}
bar: "${charge}" {style.fill: "linear-gradient(to right, green ${charge}, transparent ${charge})"}
steps: {drain: {vars: {charge: 0%}}}`
	compiled, err := Compile(wasmJSONArgument(t, CompileRequest{
		FS: map[string]string{"index": source}, Opts: &CompileOptions{AnimateDuration: go2.Pointer(int64(160))},
	}))
	if err != nil {
		t.Fatal(err)
	}
	diagram := compiled.(CompileResponse).Diagram
	interval := int64(800)
	target := "*"
	out, err := Render(wasmJSONArgument(t, RenderRequest{
		Diagram: &diagram, Opts: &RenderOptions{AnimateInterval: &interval, Target: &target},
	}))
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out.([]byte))
	if !strings.Contains(svg, ">50%</text>") || !strings.Contains(svg, `offset="50%"`) {
		t.Fatal("animated render is missing the intermediate label or gradient stop")
	}
	target = "steps.drain"
	out, err = Render(wasmJSONArgument(t, RenderRequest{
		Diagram: &diagram, Opts: &RenderOptions{Target: &target, AnimateInterval: &interval},
	}))
	if err != nil {
		t.Fatal(err)
	}
	svg = string(out.([]byte))
	if !strings.Contains(svg, ">0%</text>") || strings.Contains(svg, ">50%</text>") {
		t.Fatal("static render should show the authored destination")
	}
}

func TestRenderSketchAnimationAfterCompile(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 120ms; animate-interval: 200ms}}
replica: Replica {style.fill: white}
layers: {
  restore: {
    replica: Replica {style.fill: "#2D936C"}
    steps: {synced: {replica.style.fill: transparent}}
  }
  unrelated: {audit: Unrelated board}
}`
	for _, tc := range []struct {
		name          string
		compileSketch *bool
	}{
		{"sketch at render", nil},
		{"sketch at compile", go2.Pointer(true)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := Compile(wasmJSONArgument(t, CompileRequest{
				FS:   map[string]string{"index": source},
				Opts: &CompileOptions{RenderOptions: RenderOptions{Sketch: tc.compileSketch}},
			}))
			if err != nil {
				t.Fatal(err)
			}
			diagram := compiled.(CompileResponse).Diagram
			for _, view := range []struct {
				target   string
				sketch   bool
				patterns int
			}{
				// Two partial pencil samples for root -> restore and two for restore -> synced.
				{"layers.restore.*", true, 4},
				{"layers.restore.*", false, 0},
				{"layers.restore.steps.synced", true, 0},
			} {
				out, err := Render(wasmJSONArgument(t, RenderRequest{
					Diagram: &diagram,
					Opts:    &RenderOptions{Target: &view.target, Sketch: &view.sketch},
				}))
				if err != nil {
					t.Fatal(err)
				}
				svg := string(out.([]byte))
				if got := strings.Count(svg, `<pattern id="pencil-`); got != view.patterns {
					t.Errorf("target %s, sketch %v: %d pencil patterns, want %d", view.target, view.sketch, got, view.patterns)
				}
				if strings.Contains(svg, ">Unrelated board</text>") {
					t.Errorf("target %s includes unrelated layer", view.target)
				}
			}
		})
	}
}

func TestRenderSVGAnimateDuration(t *testing.T) {
	const source = `vars: {d2-config: {animate-duration: 80ms; animate-interval: 100ms}; charge: 100%}
bar: "${charge}"
steps: {empty: {vars: {charge: 0%}}}`
	for _, tc := range []struct {
		name     string
		duration *int64
		timing   string
		midpoint bool
	}{
		{"source", nil, "200ms steps(1,end) infinite", true},
		{"longer override", go2.Pointer(int64(160)), "260ms steps(1,end) infinite", true},
		{"zero override", go2.Pointer(int64(0)), "200ms infinite", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := RenderSVG(wasmJSONArgument(t, CompileRequest{
				FS: map[string]string{"index": source}, Opts: &CompileOptions{
					RenderOptions: RenderOptions{Target: go2.Pointer("*")}, AnimateDuration: tc.duration,
				},
			}))
			if err != nil {
				t.Fatal(err)
			}
			svg := out.(string)
			if !strings.Contains(svg, tc.timing) || strings.Contains(svg, ">50%</text>") != tc.midpoint {
				t.Errorf("expected timing %q, midpoint = %v", tc.timing, tc.midpoint)
			}
		})
	}
}

func invokeRenderRequest(t *testing.T, request RenderRequest) WASMResponse {
	t.Helper()
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	call := wrapWASMCall(Render)
	defer call.Release()
	got := call.Invoke(string(encoded)).String()
	var response WASMResponse
	if err := json.Unmarshal([]byte(got), &response); err != nil {
		t.Fatalf("invalid response %q: %v", got, err)
	}
	return response
}

func TestDeprecatedRawWASMCallsWarnOnceAndRemainCallable(t *testing.T) {
	t.Run("getObjOrder", func(t *testing.T) {
		getObjOrderDeprecationOnce = sync.Once{}
		warnings := captureWASMWarnings(t)
		call := wrapWASMCall(GetObjOrder)
		defer call.Release()

		got := call.Invoke("a: {\n  b\n}\nc").String()
		var response struct {
			Data struct {
				Order []string `json:"order"`
			} `json:"data"`
			Error *WASMError `json:"error"`
		}
		if err := json.Unmarshal([]byte(got), &response); err != nil {
			t.Fatalf("invalid response %q: %v", got, err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected error: %v", response.Error)
		}
		wantOrder := []string{"a", "c", "a.b"}
		if len(response.Data.Order) != len(wantOrder) {
			t.Fatalf("order = %v, want %v", response.Data.Order, wantOrder)
		}
		for i := range wantOrder {
			if response.Data.Order[i] != wantOrder[i] {
				t.Fatalf("order = %v, want %v", response.Data.Order, wantOrder)
			}
		}

		got = call.Invoke().String()
		assertWASMError(t, got, "missing dsl argument", 400)
		assertSingleWASMWarning(t, *warnings, getObjOrderDeprecation)
	})

	t.Run("getELKGraph", func(t *testing.T) {
		getELKGraphDeprecationOnce = sync.Once{}
		warnings := captureWASMWarnings(t)
		call := wrapWASMCall(GetELKGraph)
		defer call.Release()

		got := call.Invoke(`{"fs":{"index":"x -> y"}}`).String()
		var response struct {
			Data  json.RawMessage `json:"data"`
			Error *WASMError      `json:"error"`
		}
		if err := json.Unmarshal([]byte(got), &response); err != nil {
			t.Fatalf("invalid response %q: %v", got, err)
		}
		if response.Error != nil {
			t.Fatalf("unexpected error: %v", response.Error)
		}
		if len(response.Data) == 0 || string(response.Data) == "null" {
			t.Fatalf("missing ELK graph in response: %s", got)
		}

		got = call.Invoke().String()
		assertWASMError(t, got, "missing JSON argument", 400)
		assertSingleWASMWarning(t, *warnings, getELKGraphDeprecation)
	})
}

func TestDeprecatedRawWASMWarningCannotChangeResponse(t *testing.T) {
	console := js.Global().Get("console")
	if console.Type() != js.TypeObject {
		t.Fatal("JavaScript console is unavailable")
	}
	originalWarn := console.Get("warn")
	warn := js.Global().Call("eval", `(function () { throw new Error("consumer console.warn failed"); })`)
	console.Set("warn", warn)
	t.Cleanup(func() {
		console.Set("warn", originalWarn)
	})

	var once sync.Once
	call := wrapWASMCall(func(args []js.Value) (interface{}, error) {
		warnDeprecatedWASMCall(&once, "deprecated")
		return "unchanged", nil
	})
	defer call.Release()

	got := call.Invoke().String()
	if got != `{"data":"unchanged"}` {
		t.Fatalf("response = %s, want legacy response unchanged", got)
	}
}

func TestRenderRejectsDangerousLegendShapeLinks(t *testing.T) {
	call := wrapWASMCall(Render)
	defer call.Release()

	for _, tc := range []struct {
		name    string
		request string
		object  string
	}{
		{
			name:    "shape",
			request: `{"diagram":{"legend":{"shapes":[{"id":"unsafe-shape","type":"rectangle","label":"Shape","opacity":1,"strokeWidth":2,"fill":"B6","stroke":"B1","link":"javascript:alert(1)"}]}},"options":{}}`,
			object:  `legend shape "unsafe-shape"`,
		},
		{
			name:    "image shape",
			request: `{"diagram":{"legend":{"shapes":[{"id":"unsafe-image","type":"image","label":"Image","opacity":1,"icon":{"Scheme":"https","Host":"example.com","Path":"/image.png"},"link":"data:text/html,<script>alert(1)</script>"}]}},"options":{}}`,
			object:  `legend shape "unsafe-image"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := call.Invoke(tc.request).String()
			var got WASMResponse
			if err := json.Unmarshal([]byte(response), &got); err != nil {
				t.Fatalf("invalid response %q: %v", response, err)
			}
			if got.Error == nil || got.Error.Code != 500 || !strings.Contains(got.Error.Message, tc.object+" uses an unsafe link URL scheme") {
				t.Fatalf("error = %#v, want code 500 containing unsafe-link error for %s", got.Error, tc.object)
			}
		})
	}
}

func captureWASMWarnings(t *testing.T) *[]string {
	t.Helper()
	console := js.Global().Get("console")
	if console.Type() != js.TypeObject {
		t.Fatal("JavaScript console is unavailable")
	}
	originalWarn := console.Get("warn")
	warnings := make([]string, 0, 1)
	warn := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			warnings = append(warnings, args[0].String())
		}
		return nil
	})
	console.Set("warn", warn)
	t.Cleanup(func() {
		console.Set("warn", originalWarn)
		warn.Release()
	})
	return &warnings
}

func assertWASMError(t *testing.T, response, wantMessage string, wantCode int) {
	t.Helper()
	var got WASMResponse
	if err := json.Unmarshal([]byte(response), &got); err != nil {
		t.Fatalf("invalid response %q: %v", response, err)
	}
	if got.Error == nil {
		t.Fatalf("missing error in response: %s", response)
	}
	if got.Error.Message != wantMessage || got.Error.Code != wantCode {
		t.Fatalf("error = %#v, want message %q and code %d", got.Error, wantMessage, wantCode)
	}
}

func assertSingleWASMWarning(t *testing.T, warnings []string, want string) {
	t.Helper()
	if len(warnings) != 1 {
		t.Fatalf("warnings = %q, want exactly one warning", warnings)
	}
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
}
