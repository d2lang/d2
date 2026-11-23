package d2ir_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

func compileTween(t *testing.T, source string, opts d2ir.CompileOptions) (*d2ir.Map, error) {
	t.Helper()
	ast, err := d2parser.Parse("diagram.d2", strings.NewReader(source), nil)
	if err != nil {
		return nil, err
	}
	m, _, err := d2ir.Compile(ast, &opts)
	return m, err
}

func tweenField(m *d2ir.Map, path ...string) *d2ir.Field {
	keys := make([]d2ast.String, len(path))
	for i, key := range path {
		keys[i] = d2ast.FlatUnquotedString(key)
	}
	return m.GetField(keys...)
}

func tweenText(t *testing.T, m *d2ir.Map, path ...string) string {
	t.Helper()
	field := tweenField(m, path...)
	if field == nil || field.Primary() == nil {
		t.Fatalf("missing scalar %v", path)
	}
	return field.Primary().Value.ScalarString()
}

func TestVariableTween(t *testing.T) {
	source := `
vars: {d2-config: {animate-duration: 4s}; charge: 65%; load: 0.00}
bar: ${charge} {
  style.fill: "linear-gradient(to right, seagreen ${charge}, transparent ${charge})"
}
counter: "load ${load}"
steps: {
  drain: {vars: {charge: 0%; load: 1}}
  done: {status: done}
  refill: {vars: {charge: 100%}}
}
`
	for _, enabled := range []bool{false, true} {
		m, err := compileTween(t, source, d2ir.CompileOptions{AnimateTweens: &enabled})
		if err != nil {
			t.Fatal(err)
		}
		step := tweenField(m, "steps", "drain").Map()
		if tweenText(t, step, "bar") != "0%" || tweenField(step, "vars", "charge").Map() != nil {
			t.Fatal("expected a plain scalar at the destination")
		}
		if tweenField(m, "steps", "done").Map().TweenDurationMS != 0 {
			t.Fatal("later steps must not inherit incoming motion")
		}
		if !enabled {
			if step.TweenDurationMS != 0 || len(step.TweenFrames) != 0 {
				t.Fatal("static compilation should not materialize a timeline")
			}
			continue
		}

		if step.TweenDurationMS != 4000 || len(step.TweenFrames) != 100 {
			t.Fatalf("timeline %d ms, %d frames", step.TweenDurationMS, len(step.TweenFrames))
		}
		first, quarter := step.TweenFrames[0], step.TweenFrames[25]
		if tweenText(t, first, "bar") != "65%" || tweenText(t, quarter, "bar") != "49%" ||
			tweenText(t, quarter, "bar", "style", "fill") != "linear-gradient(to right, seagreen 49%, transparent 49%)" ||
			tweenText(t, quarter, "counter") != "load 0.25" || tweenText(t, step.TweenFrames[50], "counter") != "load 0.50" {
			t.Fatalf("interpolated %q, %q, %q", tweenText(t, quarter, "bar"), tweenText(t, quarter, "bar", "style", "fill"), tweenText(t, quarter, "counter"))
		}
		refill := tweenField(m, "steps", "refill").Map()
		if tweenText(t, refill.TweenFrames[0], "bar") != "0%" {
			t.Fatal("refill should start at the preceding step's endpoint")
		}
	}
}

func TestTweenImportsAndScopes(t *testing.T) {
	opts := d2ir.CompileOptions{
		AnimateTweens: go2.Pointer(true),
		FS: fstest.MapFS{"widget.d2": {Data: []byte(`bar: "imported ${level} ${units}"
local: {
  vars: {level: 77}
  child: ${level}
}
classes: {display: {style.opacity: ${level}}}
steps: {drain: {vars: {level: 0.00}; steps: {next: {done}}}}
`)}},
	}
	m, err := compileTween(t, `vars: {units: watts; d2-config: {animate-duration: 80ms}}
layers: {
  panel: {vars: {level: 1}; ...@widget}
}`, opts)
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "layers", "panel", "steps", "drain").Map()
	if len(step.TweenFrames) != 2 {
		t.Fatalf("frames: %d", len(step.TweenFrames))
	}
	mid := step.TweenFrames[1]
	if tweenText(t, mid, "bar") != "imported 0.50 watts" || tweenText(t, mid, "local", "child") != "77" || tweenText(t, mid, "classes", "display", "style", "opacity") != "0.50" || tweenField(mid, "steps") != nil {
		t.Fatalf("bar=%q child=%q class=%q steps=%v", tweenText(t, mid, "bar"), tweenText(t, mid, "local", "child"), tweenText(t, mid, "classes", "display", "style", "opacity"), tweenField(mid, "steps") != nil)
	}
}

func TestTweenEligibleVariables(t *testing.T) {
	m, err := compileTween(t, `vars: {
  d2-config: {animate-interval: 1s; animate-duration: 80ms}
  a: 1
  b: old
  c: 1
  group: {tween: 0; count: 12}
}
meter: "${group.tween} ${b}"
steps: {change: {vars: {a: 1%; b: new; c: null; d: 5; group.tween: 4}}}
`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "steps", "change").Map()
	if len(step.TweenFrames) != 2 || tweenText(t, step.TweenFrames[1], "meter") != "2 new" ||
		tweenText(t, step.TweenFrames[0], "vars", "a") != "1%" || tweenText(t, step.TweenFrames[0], "vars", "d") != "5" {
		t.Fatal("only compatible, existing scalar variables should interpolate")
	}
}

func TestTweenSampleBudget(t *testing.T) {
	// A four-second tween of several shapes consumes more than one board's
	// substitution budget in aggregate. Each individual sample fits easily.
	var src strings.Builder
	src.WriteString("vars: {d2-config: {animate-duration: 4s}; charge: 65%}\n")
	for _, id := range []string{"battery", "sensor", "radio", "backup"} {
		src.WriteString(id + `: "Battery level ${charge}" {
  width: 260
  height: 58
  style: {
    border-radius: 12
    stroke: "#26573f"
    font-color: "#19352b"
    fill: "linear-gradient(to right, #80d6a8 ${charge}, transparent ${charge})"
  }
}
`)
	}
	src.WriteString("steps: {drain: {vars: {charge: 0%}}}")
	m, err := compileTween(t, src.String(), d2ir.CompileOptions{AnimateTweens: go2.Pointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	mid := tweenField(m, "steps", "drain").Map().TweenFrames[50]
	if !mid.Root() || tweenText(t, mid, "backup") != "Battery level 32%" {
		t.Fatal("expected an independent sampled board with a rounded numeric label")
	}
	_, err = compileTween(t, src.String(), d2ir.CompileOptions{AnimateTweens: go2.Pointer(true), MaxVariableExpansion: 100})
	if err == nil || !strings.Contains(err.Error(), "expansion exceeds limit") {
		t.Fatalf("expected caller expansion limit, got %v", err)
	}
}

func TestTweenDecimalPrecisionBound(t *testing.T) {
	m, err := compileTween(t, `vars: {d2-config: {animate-duration: 80ms}; p: 0e-2147483647%}
meter: ${p}
steps: {up: {vars: {p: 1%}}}`, d2ir.CompileOptions{AnimateTweens: go2.Pointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "steps", "up").Map()
	if got := tweenText(t, step.TweenFrames[1], "meter"); len(got) > 327 || !strings.HasPrefix(got, "0.5") || !strings.HasSuffix(got, "%") {
		t.Fatalf("unexpected midpoint %q", got)
	}
}

func TestTweenDurationPrecedenceAndScope(t *testing.T) {
	const src = `vars: {
		ms: 160ms
		d2-config: {animate-duration: ${ms}; animate-interval: 2s}
		p: 0
		caption: "Progress ${p}"
	}
	meter: ${caption}
	steps: {load: {vars: {p: 100}}; hold: {color: green}}
	layers: {independent: {vars: {p: 40}; lone}}
	`
	for _, tc := range []struct {
		name     string
		opts     d2ir.CompileOptions
		duration int
	}{
		{"source", d2ir.CompileOptions{}, 160},
		{"override", d2ir.CompileOptions{AnimateDuration: go2.Pointer(int64(80))}, 80},
		{"override-zero", d2ir.CompileOptions{AnimateDuration: go2.Pointer(int64(0))}, 0},
		{"explicit-static", d2ir.CompileOptions{AnimateTweens: go2.Pointer(false)}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := compileTween(t, src, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			step := tweenField(m, "steps", "load").Map()
			if step.TweenDurationMS != tc.duration || len(step.TweenFrames) != tc.duration/40 {
				t.Fatalf("got %d frames, %d ms", len(step.TweenFrames), step.TweenDurationMS)
			}
			if tc.duration > 0 && tweenText(t, step.TweenFrames[len(step.TweenFrames)/2], "meter") != "Progress 50" {
				t.Fatal("alias did not follow numeric variable")
			}
			if tweenText(t, step, "meter") != "Progress 100" ||
				len(tweenField(m, "steps", "hold").Map().TweenFrames) != 0 ||
				len(tweenField(m, "layers", "independent").Map().TweenFrames) != 0 {
				t.Fatal("incorrect endpoint, unchanged step, or independent layer")
			}
		})
	}
	for _, value := range []string{"-1", "-1s", "1.5ms", "10us", "soon", "600001", "10m1ms", "[4s]", "4s {extra: yes}"} {
		_, err := compileTween(t, "vars: {d2-config: {animate-duration: "+value+"}}", d2ir.CompileOptions{})
		if err == nil || !strings.Contains(err.Error(), "animate-duration") {
			t.Errorf("invalid duration %q: %v", value, err)
		}
	}
	_, err := compileTween(t, src+"steps.load.vars: {d2-config: {animate-duration: 3s}}", d2ir.CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "root vars.d2-config") {
		t.Fatalf("step duration accepted: %v", err)
	}
}

func TestTweenImportedConfig(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{
		"config.d2": {Data: []byte(`vars: {ms: 80ms; d2-config: {animate-interval: 1s; animate-duration: ${ms}}}`)},
	}}
	m, err := compileTween(t, `...@config
vars: {p: 2}
meter: ${p}
steps: {load: {vars: {p: 4}}}`, opts)
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "steps", "load").Map()
	if len(step.TweenFrames) != 2 || tweenText(t, step.TweenFrames[1], "meter") != "3" {
		t.Fatal("timing did not use resolved imported config")
	}
}

func TestTweenSelectedImportKeepsLexicalBindings(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{
		"module.d2": {Data: []byte(`vars: {unit: watts}
layers: {panel: {
  vars: {p: 0}
  meter: "Reading ${p} ${unit}"
  steps: {load: {vars: {p: 4}}}
}}`)},
	}}
	m, err := compileTween(t, `vars: {d2-config: {animate-interval: 1s; animate-duration: 80ms}}
layers: {sensor: @module.layers.panel}`, opts)
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "layers", "sensor", "steps", "load").Map()
	if len(step.TweenFrames) != 2 || tweenText(t, step.TweenFrames[1], "meter") != "Reading 2 watts" {
		t.Fatal("interpolation lost the imported module's lexical bindings")
	}
}
