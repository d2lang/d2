package d2lib

import (
	"context"
	"math"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestWildcardEdgePropertyAcrossSpread(t *testing.T) {
	const edges = `a -> b: wire {style.opacity: 0.2}; a -> c: spare {style.opacity: 0.3}`
	const vars = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}; t: {` + edges + `}}; `
	const use = `box: {...${t}}; `
	const set = `box.(a -> *)[*].style.opacity: 0.8`
	const filter = `box.(a -> *)[*]: {&label: wire; style.opacity: 0.8}`
	for _, tc := range []struct {
		name, source string
		wire, spare  float64
	}{
		{"later selector", use + set, .8, .8},
		{"earlier selector", set + `; ` + use, .2, .3},
		{"later spread", use + set + `; ` + use, .2, .3},
		{"earlier explicit write", `box: {a -> b: initial {style.opacity: 0.5}; ...${t}}; ` + set, .8, .8},
		{"later explicit write", `box: {a -> b: initial; ...${t}}; ` + set + `; box.(a -> b)[0].style.opacity: 0.9`, .9, .8},
		{"filtered property", use + filter, .8, .3},
		{"later filtered property", use + set + `; ` + filter, .8, .8},
		{"different selector order", use + filter + `; box.(a -> *)[*].style.opacity: 0.5`, .5, .5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			d, _, err := Compile(d2log.WithDefault(context.Background()), vars+tc.source, dagreCompileOptions(&calls), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Connections) != 2 {
				t.Fatalf("got %d connections", len(d.Connections))
			}
			for _, e := range d.Connections {
				want := tc.wire
				if e.Label == "spare" {
					want = tc.spare
				}
				if e.Opacity != want {
					t.Errorf("%s opacity %g, want %g", e.Label, e.Opacity, want)
				}
			}
		})
	}
}

func TestAnimatedWildcardEdgePropertyAcrossImport(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
box: @links
other: @links
steps: {
  dim: {box.(a -> *)[*]: {&label: wire; style.opacity: 0.8}}
  hold: {}
}`
	calls := 0
	opts := dagreCompileOptions(&calls)
	opts.FS = fstest.MapFS{"links.d2": {Data: []byte(`vars: {t: {a -> b: wire {style.opacity: 0.2}; a -> c: spare {style.opacity: 0.3}}}; ...${t}`)}}
	d, _, err := Compile(d2log.WithDefault(context.Background()), source, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	check := func(frame *d2target.Diagram, want float64) {
		t.Helper()
		if len(frame.Connections) != 4 {
			t.Fatalf("%s got %d connections", frame.Name, len(frame.Connections))
		}
		for _, e := range frame.Connections {
			expected := .3
			if e.Label == "wire" {
				expected = .2
				if e.Src == "box.a" {
					expected = want
				}
			}
			if math.Abs(e.Opacity-expected) > 1e-9 {
				t.Errorf("%s %s opacity %g, want %g", frame.Name, e.ID, e.Opacity, expected)
			}
		}
	}
	check(d, .2)
	step := d.Steps[0]
	if len(step.TweenFrames) != 4 {
		t.Fatalf("got %d tween frames", len(step.TweenFrames))
	}
	for i, s := range step.TweenFrames {
		check(s, .2+.15*float64(i))
	}
	check(step, .8)
	check(d.Steps[1], .8)
}

func TestInheritedWildcardSpreadProperties(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         []float64
	}{
		{"new property", `vars: {t: {a -> b}}; box: {...${t}}; steps: {first: {box.(a -> *)[*].style.opacity: 0.4; box.(a -> *)[*].style.opacity: 0.7}; hold: {another}}`, []float64{1, .7, .7}},
		{"reopened scope and inherited reset", `vars: {t: {a -> b: wire {style.opacity: 0.3}}}; box: {...${t}}; box: {(a -> *)[*].style.opacity: 0.4}; steps: {reset: {box: {(a -> *)[*].style.opacity: null}}; hold: {another}}`, []float64{.4, 1, 1}},
		{"rule-only template and inherited reset", `vars: {wire: {a -> b}; paint: {(a -> *)[*].style.opacity: 0.52}; reset: {(a -> *)[*].style.opacity: null}}; box: {...${paint}; ...${wire}}; steps: {clear: {box: {...${reset}}}; hold: {another}}`, []float64{.52, 1, 1}},
		{"unrelated spread while rebinding", `vars: {wire: {a -> b {style.opacity: 0.2}}; tiny: {x}}; box: {...${tiny}; ...${wire}; (a -> *)[*].style.opacity: 0.8}; steps: {hold: {another}}`, []float64{.8, .8}},
		{"rule-only template addressing parent", `vars: {paint: {(_.a -> _.b)[*].style.opacity: 0.6}}; a -> b; child: {...${paint}}; steps: {hold: {another}}`, []float64{.6, .6}},
		{"nested property in a shape glob", `vars: {t: {group: {a -> b}}}; ...${t}; gro*: {(a -> *)[*].style.opacity: 0.6}`, []float64{.6}},
		{"global default before explicit template", `vars: {t: {a -> b: wire {style.opacity: 0.2}}}; (*** -> ***)[*].style.opacity: 0.6; steps: {first: {box: {...${t}}}; hold: {another}}`, []float64{-1, .2, .2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			d, _, err := Compile(d2log.WithDefault(context.Background()), tc.source, dagreCompileOptions(&calls), nil)
			if err != nil {
				t.Fatal(err)
			}
			boards := append([]*d2target.Diagram{d}, d.Steps...)
			if len(boards) != len(tc.want) {
				t.Fatalf("got %d boards, want %d", len(boards), len(tc.want))
			}
			for i, b := range boards {
				want := tc.want[i]
				if want < 0 && len(b.Connections) == 0 {
					continue
				}
				if len(b.Connections) != 1 || b.Connections[0].Opacity != want {
					t.Errorf("%s: got %+v, want one edge of opacity %g", b.Name, b.Connections, want)
				}
			}
		})
	}
}

func TestImportedEdgeSetterUsesImportOrder(t *testing.T) {
	const source = `vars: {t: {a -> b: wire {style.opacity: 0.1}}}
first: @library
second: @library
second.box.(a -> *)[*].style.opacity: 0.9`
	calls := 0
	opts := dagreCompileOptions(&calls)
	opts.FS = fstest.MapFS{"library.d2": {Data: []byte(`box: {...${t}}; box.(a -> *)[*].style.opacity: 0.6`)}}
	diagram, _, err := Compile(d2log.WithDefault(context.Background()), source, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Connections) != 2 {
		t.Fatalf("got %d connections", len(diagram.Connections))
	}
	for _, e := range diagram.Connections {
		want := .6
		if e.Src == "second.box.a" {
			want = .9
		}
		if e.Opacity != want {
			t.Errorf("%s opacity %g, want %g", e.ID, e.Opacity, want)
		}
	}
}
