package d2ir_test

import (
	"strings"
	"testing"
	"testing/fstest"

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
vars: {charge: 65%; load: 0.00}
bar: ${charge} {
  style.fill: "linear-gradient(to right, seagreen ${charge}, transparent ${charge})"
}
counter: "load ${load}"
steps: {
  drain: {vars: {charge: 0% {tween: 4s}; load: 1 {tween: 2s}}}
  done: {status: done}
  refill: {vars: {charge: 100% {tween: 4s}}}
}
`
	for _, enabled := range []bool{false, true} {
		m, err := compileTween(t, source, d2ir.CompileOptions{AnimateTweens: enabled})
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
			tweenText(t, quarter, "counter") != "load 0.50" || tweenText(t, step.TweenFrames[50], "counter") != "load 1" {
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
		AnimateTweens: true,
		FS: fstest.MapFS{"widget.d2": {Data: []byte(`bar: "imported ${level} ${units}"
local: {
  vars: {level: 77}
  child: ${level}
}
classes: {display: {style.opacity: ${level}}}
steps: {drain: {vars: {level: 0.00 {tween: 80ms}}; steps: {next: {done}}}}
`)}},
	}
	m, err := compileTween(t, `vars: {units: watts}
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

func TestTweenValidation(t *testing.T) {
	cases := []struct{ source, want string }{
		{`vars: {foo: 1 {tween: 4s}}`, "must be declared in a step"},
		{`steps: {one: {vars: {foo: 0 {tween: 1s}}}}`, "inherited start"},
		{`vars: {foo: 1}; steps: {one: {vars: {foo: 0% {tween: 1s}}}}`, "both numbers or both percentages"},
		{`vars: {foo: 1}; steps: {one: {vars: {foo: text {tween: 1s}}}}`, "both numbers or both percentages"},
		{`vars: {foo: 1}; steps: {one: {vars: {foo: 0 {tween: 0s}}}}`, "whole number of milliseconds"},
		{`vars: {foo: 1}; steps: {one: {vars: {foo: 0 {tween: 7.5ms}}}}`, "whole number of milliseconds"},
		{`vars: {foo: 1}; steps: {one: {vars: {foo: 0 {tween: 11m}}}}`, "whole number of milliseconds"},
	}
	for _, tc := range cases {
		_, err := compileTween(t, tc.source, d2ir.CompileOptions{})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("expected %q for %q, got %v", tc.want, tc.source, err)
		}
	}
	_, err := compileTween(t, `vars: {group: {tween: 1}; scalar: normal {"tween": another}}
a: ${group.tween}
b: ${scalar.tween}
`, d2ir.CompileOptions{AnimateTweens: true})
	if err != nil {
		t.Fatalf("tween as ordinary map key or quoted child: %v", err)
	}
}

func TestTweenSampleBudget(t *testing.T) {
	// A four-second tween of several shapes consumes more than one board's
	// substitution budget in aggregate. Each individual sample fits easily.
	var src strings.Builder
	src.WriteString("vars: {charge: 65%}\n")
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
	src.WriteString("steps: {drain: {vars: {charge: 0% {tween: 4s}}}}")
	m, err := compileTween(t, src.String(), d2ir.CompileOptions{AnimateTweens: true})
	if err != nil {
		t.Fatal(err)
	}
	mid := tweenField(m, "steps", "drain").Map().TweenFrames[50]
	if !mid.Root() || tweenText(t, mid, "backup") != "Battery level 32%" {
		t.Fatal("expected an independent sampled board with a rounded numeric label")
	}
	_, err = compileTween(t, src.String(), d2ir.CompileOptions{AnimateTweens: true, MaxVariableExpansion: 100})
	if err == nil || !strings.Contains(err.Error(), "expansion exceeds limit") {
		t.Fatalf("expected caller expansion limit, got %v", err)
	}
}

func TestTweenDecimalPrecisionBound(t *testing.T) {
	m, err := compileTween(t, `vars: {p: 0e-2147483647%}
meter: ${p}
steps: {up: {vars: {p: 1% {tween: 80ms}}}}`, d2ir.CompileOptions{AnimateTweens: true})
	if err != nil {
		t.Fatal(err)
	}
	step := tweenField(m, "steps", "up").Map()
	if got := tweenText(t, step.TweenFrames[1], "meter"); len(got) > 327 || !strings.HasPrefix(got, "0.5") || !strings.HasSuffix(got, "%") {
		t.Fatalf("unexpected midpoint %q", got)
	}
}
