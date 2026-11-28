package d2ir_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2ir"
)

func TestScenarioLocalVariableFallback(t *testing.T) {
	const diagram = `vars: {level: 10; detail: {low: 2; high: 8}}
panel: {
  vars.summary: "Load ${level} / ${detail.low} to ${detail.high}"
  meter: ${summary}
  nested: {meter: ${level}}
  pinned: {vars.level: 32; meter: ${level}}
}
reference: "${level}: ${detail.low} to ${detail.high}"
scenarios: {
  busy: {panel.vars: {level: 50; detail.low: 6}}
  normal: {}
}
`
	for _, imported := range []bool{false, true} {
		opts := d2ir.CompileOptions{}
		source := "vars.d2-config: {animate-interval: 1s; animate-duration: 80ms}\n"
		if imported {
			opts.FS = fstest.MapFS{"panel.d2": {Data: []byte("layers.site: {\n" + diagram + "}")}}
			source += "layers.site: @panel.layers.site"
		} else {
			source += diagram
		}
		m, err := compileTween(t, source, opts)
		if err != nil {
			t.Fatal(err)
		}
		if imported {
			m = tweenField(m, "layers", "site").Map()
		}
		normal := tweenField(m, "scenarios", "normal").Map()
		if len(normal.TweenFrames) != 2 {
			t.Fatalf("imported=%t: got %d frames", imported, len(normal.TweenFrames))
		}
		for i, b := range []*d2ir.Map{normal.TweenFrames[0], normal.TweenFrames[1], normal} {
			if got, want := tweenText(t, b, "panel", "meter"), []string{"Load 50 / 6 to 8", "Load 30 / 4 to 8", "Load 10 / 2 to 8"}[i]; got != want {
				t.Errorf("imported=%t frame %d: label %q, want %q", imported, i, got, want)
			}
			if got, want := tweenText(t, b, "panel", "nested", "meter"), []string{"50", "30", "10"}[i]; got != want {
				t.Errorf("frame %d: nested label %q, want %q", i, got, want)
			}
			if tweenText(t, b, "reference") != "10: 2 to 8" || tweenText(t, b, "panel", "pinned", "meter") != "32" {
				t.Fatal("local samples changed an outer or shadowed reference")
			}
		}
		if got := tweenText(t, normal.TweenFrames[1], "panel", "vars", "detail", "high"); got != "8" {
			t.Fatalf("sample-local map lost an inherited member: %s", got)
		}
		if tweenField(normal, "panel", "vars", "level") != nil {
			t.Fatal("local sample modified the scenario endpoint")
		}
	}
}

func TestScenarioLocalFallbackKeepsSeenValueWhileOuterVariableMoves(t *testing.T) {
	const source = `vars: {level: 10; d2-config: {animate-interval: 1s; animate-duration: 80ms}}
reference: ${level}
panel.meter: ${level}
scenarios: {
  fixed: {vars.level: 50; panel.vars.level: 10}
  normal: {}
}`
	root, err := compileTween(t, source, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	normal := tweenField(root, "scenarios", "normal").Map()
	for i, frame := range normal.TweenFrames {
		if got, want := tweenText(t, frame, "reference"), []string{"50", "30"}[i]; got != want {
			t.Errorf("frame %d: reference %q, want %q", i, got, want)
		}
		if got := tweenText(t, frame, "panel", "meter"); got != "10" {
			t.Errorf("frame %d: shadowed meter moved to %q", i, got)
		}
	}
}

func TestScenarioFallbackAcrossIncompatibleVariableTypes(t *testing.T) {
	for _, scalarOrArray := range []string{"45", "[x; y]"} {
		const src = `vars: {counter: 10; d2-config: {animate-interval: 1s; animate-duration: 80ms}; palette.fill: blue}
panel: {
  swatch.style.fill: ${palette.fill}
}
meter: ${counter}
scenarios: {
  busy: {panel.vars.palette.fill: red; vars.counter: 20}
  normal: {panel.vars.palette: VALUE}
}`
		root, err := compileTween(t, strings.ReplaceAll(src, "VALUE", scalarOrArray), d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		normal := tweenField(root, "scenarios", "normal").Map()
		if got := tweenText(t, normal, "panel", "swatch", "style", "fill"); got != "blue" {
			t.Fatalf("changed authored color %s", got)
		}
		if got := tweenText(t, normal.TweenFrames[1], "meter"); got != "15" {
			t.Fatalf("unrelated counter stopped at %s", got)
		}
	}
}

func TestScenarioFallbackPreservesFrozenModuleVariable(t *testing.T) {
	opts := d2ir.CompileOptions{FS: fstest.MapFS{"panel.d2": {Data: []byte(`vars: {level: 10; unit: req; suffix: /s}
layers.site: {
  vars.requests: 20
  count: ${requests}
  plain: ${unit}${suffix} at ${requests}
  quoted: "${requests} ${unit}${suffix}"
  panel.meter: "Load ${level}"
  scenarios: {
    busy: {panel.vars.level: 50; vars.requests: 40}
    normal: {}
  }
}`)}}}
	root, err := compileTween(t, `vars: {level: 90; d2-config: {animate-interval: 1s; animate-duration: 80ms}}
layers.site: @panel.layers.site`, opts)
	if err != nil {
		t.Fatal(err)
	}
	site := tweenField(root, "layers", "site").Map()
	normal := tweenField(site, "scenarios", "normal").Map()
	if len(normal.TweenFrames) != 2 {
		t.Fatalf("%d samples, endpoint %s", len(normal.TweenFrames), tweenText(t, normal, "panel", "meter"))
	}
	for i, b := range []*d2ir.Map{normal.TweenFrames[0], normal.TweenFrames[1], normal} {
		if got := tweenText(t, b, "panel", "meter"); got != "Load 10" {
			t.Errorf("frame %d: module literal changed to %q", i, got)
		}
		n := []string{"40", "30", "20"}[i]
		for _, tc := range []struct{ name, want string }{{"count", n}, {"plain", "req/s at " + n}, {"quoted", n + " req/s"}} {
			if got := tweenText(t, b, tc.name); got != tc.want {
				t.Errorf("frame %d: %s is %q, want %q", i, tc.name, got, tc.want)
			}
		}
	}
}
