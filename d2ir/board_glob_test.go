package d2ir_test

import (
	"strings"
	"testing"

	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/mapfs"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
)

// TestBoardGlobs covers globs that select layers, scenarios or steps, or
// reach into them. A glob over boards applies to every board it selects, and
// once. want gives the shape (see boardShape) of every layer, scenario and
// step, named by its path, and may give the root board's ("").
func TestBoardGlobs(t *testing.T) {
	t.Parallel()

	tca := []struct {
		name  string
		text  string
		files map[string]string
		want  map[string]string
		// Query to the number of nodes it matches.
		count map[string]int
	}{
		{
			name: "star-map/scenarios",
			text: `scenarios: {w: {b}; v: {c}; *: {a}}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "star-map/layers",
			text: `layers: {w: {b}; v: {c}; *: {a}}`,
			want: map[string]string{
				"layers.w": "b a",
				"layers.v": "c a",
			},
		},
		{
			// v was made a copy of w before the glob gave w an a.
			name: "star-map/steps",
			text: `steps: {w: {b}; v: {c}; *: {a}}`,
			want: map[string]string{
				"steps.w": "b a",
				"steps.v": "b c a",
			},
		},
		{
			name: "star-map/three-scenarios",
			text: `scenarios: {w: {b}; v: {c}; u: {d}; *: {a}}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
				"scenarios.u": "d a",
			},
		},
		{
			name: "star-map/dotted",
			text: `scenarios: {w: {b}; v: {c}; *.a}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "star-map/pattern",
			text: `scenarios: {w1: {b}; w2: {c}; x: {d}; w*: {a}}`,
			want: map[string]string{
				"scenarios.w1": "b a",
				"scenarios.w2": "c a",
				"scenarios.x":  "d",
			},
		},
		{
			name: "star-map/existing-object",
			text: `layers: {w: {b}; v: {b}; *.b.style.fill: red}`,
			want: map[string]string{
				"layers.w": "b{style{fill:red}}",
				"layers.v": "b{style{fill:red}}",
			},
		},
		{
			name: "star-map/objects",
			text: `layers: {w: {b}; v: {b}; *.*.style.fill: red}`,
			want: map[string]string{
				"layers.w": "b{style{fill:red}}",
				"layers.v": "b{style{fill:red}}",
			},
		},
		{
			name: "star-map/value-style",
			text: `scenarios: {w: {b}; v: {b}; *: {b.style.fill: red}}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "outside/layers",
			text: `layers: {w: {b}; v: {c}}
layers.*.a`,
			want: map[string]string{
				"layers.w": "b a",
				"layers.v": "c a",
			},
		},
		{
			name: "outside/scenarios",
			text: `scenarios: {w: {b}; v: {c}}
scenarios.*.a`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "outside/steps",
			text: `steps: {w: {b}; v: {c}}
steps.*.a`,
			want: map[string]string{
				"steps.w": "b a",
				"steps.v": "b c a",
			},
		},
		{
			name: "outside/map",
			text: `scenarios: {w: {b}; v: {c}}
scenarios.*: {a}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "outside/scenarios-style",
			text: `scenarios: {w: {b}; v: {b}}
scenarios.*.b.style.fill: red`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "outside/layers-style",
			text: `layers: {w: {b}; v: {b}}
layers.*.b.style.fill: red`,
			want: map[string]string{
				"layers.w": "b{style{fill:red}}",
				"layers.v": "b{style{fill:red}}",
			},
		},
		{
			name: "outside/objects",
			text: `scenarios: {w: {b}; v: {b}}
scenarios.*.*.style.fill: red`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "outside/inherited",
			text: `x
scenarios: {w: {b}; v: {c}}
scenarios.*.x.style.fill: red`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x{style{fill:red}} b",
				"scenarios.v": "x{style{fill:red}} c",
			},
		},
		{
			// The glob selects the boards, not the objects in each board.
			name: "before/scenarios",
			text: `scenarios: {*: {a}; w: {b}; v: {c}}`,
			want: map[string]string{
				"scenarios.w": "a b",
				"scenarios.v": "a c",
			},
		},
		{
			name: "before/layers",
			text: `layers: {*: {a}; w: {b}; v: {c}}`,
			want: map[string]string{
				"layers.w": "a b",
				"layers.v": "a c",
			},
		},
		{
			name: "before/steps",
			text: `steps: {*: {a}; w: {b}; v: {c}}`,
			want: map[string]string{
				"steps.w": "a b",
				"steps.v": "a b c",
			},
		},
		{
			name: "outside-before/scenarios",
			text: `scenarios.*.a
scenarios: {w: {b}; v: {c}}`,
			want: map[string]string{
				"scenarios.w": "a b",
				"scenarios.v": "a c",
			},
		},
		{
			name: "outside-before/layers",
			text: `layers.*.a
layers: {w: {b}; v: {c}}`,
			want: map[string]string{
				"layers.w": "a b",
				"layers.v": "a c",
			},
		},
		{
			name: "outside-before/steps",
			text: `steps.*.a
steps: {w: {b}; v: {c}}`,
			want: map[string]string{
				"steps.w": "a b",
				"steps.v": "a b c",
			},
		},
		{
			name: "outside-before/style",
			text: `scenarios.*.b.style.fill: red
scenarios: {w: {b}; v: {b}}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "between/scenarios",
			text: `scenarios: {w: {b}; *: {a}; v: {c}}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "a c",
			},
		},
		{
			name: "between/layers",
			text: `layers: {w: {b}; *: {a}; v: {c}}`,
			want: map[string]string{
				"layers.w": "b a",
				"layers.v": "a c",
			},
		},
		{
			name: "outside-reopened",
			text: `scenarios: {w: {b}}
scenarios.*.a
scenarios: {v: {c}}`,
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "a c",
			},
		},
		{
			name: "no-map/scenarios",
			text: `x
scenarios: {w; v: {c}; *: {a}}`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x a",
				"scenarios.v": "x c a",
			},
		},
		{
			name: "no-map/empty",
			text: `x
scenarios: {w: {}; v: {}; *: {a}}`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x a",
				"scenarios.v": "x a",
			},
		},
		{
			name: "no-map/layers",
			text: `layers: {w; v; *: {a}}`,
			want: map[string]string{
				"layers.w": "a",
				"layers.v": "a",
			},
		},
		{
			name: "no-map/steps",
			text: `x
steps: {w; v: {c}; *: {a}}`,
			want: map[string]string{
				"":        "x",
				"steps.w": "x a",
				"steps.v": "x c a",
			},
		},
		{
			name: "nested/layers",
			text: `layers: {l: {layers: {w: {b}; v: {c}; *: {x}}}}`,
			want: map[string]string{
				"layers.l":          "",
				"layers.l.layers.w": "b x",
				"layers.l.layers.v": "c x",
			},
		},
		{
			name: "nested/outside",
			text: `layers: {l: {layers: {w: {b}; v: {c}}}}
layers.l.layers.*.x`,
			want: map[string]string{
				"layers.l":          "",
				"layers.l.layers.w": "b x",
				"layers.l.layers.v": "c x",
			},
		},
		{
			name: "nested/outside-both",
			text: `layers: {l: {layers: {w: {b}; v: {c}}}; m: {layers: {u: {d}}}}
layers.*.layers.*.x`,
			want: map[string]string{
				"layers.l":          "",
				"layers.l.layers.w": "b x",
				"layers.l.layers.v": "c x",
				"layers.m":          "",
				"layers.m.layers.u": "d x",
			},
		},
		{
			name: "nested/one-each",
			text: `layers: {l: {layers: {w: {b}}}; m: {layers: {u: {d}}}}
layers.*.layers.*.x`,
			want: map[string]string{
				"layers.l":          "",
				"layers.l.layers.w": "b x",
				"layers.m":          "",
				"layers.m.layers.u": "d x",
			},
		},
		{
			name: "nested/layer-scenarios",
			text: `layers: {l: {y; scenarios: {w: {b}; v: {c}; *: {x}}}}`,
			want: map[string]string{
				"layers.l":             "y",
				"layers.l.scenarios.w": "y b x",
				"layers.l.scenarios.v": "y c x",
			},
		},
		{
			name: "nested/scenario-steps",
			text: `scenarios: {s: {y; steps: {w: {b}; v: {c}; *: {x}}}}`,
			want: map[string]string{
				"scenarios.s":         "y",
				"scenarios.s.steps.w": "y b x",
				"scenarios.s.steps.v": "y b c x",
			},
		},
		{
			name: "double-glob/scenarios",
			text: `scenarios: {w: {b}; v: {b}; **.style.fill: red}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "double-glob/outside",
			text: `scenarios: {w: {b}; v: {b}}
scenarios.**.style.fill: red`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "double-glob/outside-before",
			text: `scenarios.**.style.fill: red
scenarios: {w: {b}; v: {b}}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "double-glob/create",
			text: `scenarios: {w: {b}; v: {b}; **: {k}}`,
			want: map[string]string{
				"scenarios.w": "b{k}",
				"scenarios.v": "b{k}",
			},
		},
		{
			// This was an error, as v.b got an empty style map.
			name: "double-glob/before",
			text: `scenarios: {**.style.fill: red; w: {b}; v: {b}}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "double-glob/before-shape",
			text: `scenarios: {**.shape: circle; w: {b}; v: {b}}`,
			want: map[string]string{
				"scenarios.w": "b{shape:circle}",
				"scenarios.v": "b{shape:circle}",
			},
		},
		{
			name: "double-glob/before-layers",
			text: `layers: {**.style.fill: red; w: {b}; v: {b}}`,
			want: map[string]string{
				"layers.w": "b{style{fill:red}}",
				"layers.v": "b{style{fill:red}}",
			},
		},
		{
			name: "star-dot/before",
			text: `scenarios: {*.b.style.fill: red; w: {b}; v: {b}}`,
			want: map[string]string{
				"scenarios.w": "b{style{fill:red}}",
				"scenarios.v": "b{style{fill:red}}",
			},
		},
		{
			name: "edges/map",
			text: `scenarios: {w: {b; c}; v: {b; c}; *: {b -> c}}`,
			want: map[string]string{
				"scenarios.w": "b c (b -> c)[0]",
				"scenarios.v": "b c (b -> c)[0]",
			},
		},
		{
			name: "edges/outside-map",
			text: `scenarios: {w: {x}; v: {y}}
scenarios.*: {a -> b}`,
			want: map[string]string{
				"scenarios.w": "x a b (a -> b)[0]",
				"scenarios.v": "y a b (a -> b)[0]",
			},
		},
		{
			name: "edges/glob-in-map",
			text: `scenarios: {w: {a; b}; v: {c; d}; *: {* -> *}}`,
			want: map[string]string{
				"scenarios.w": "a b (a -> b)[0] (b -> a)[0]",
				"scenarios.v": "c d (c -> d)[0] (d -> c)[0]",
			},
		},
		{
			name: "edges/selector",
			text: `scenarios: {w: {a -> b}; v: {a -> b}}
scenarios.*.(a -> b)[0].style.stroke: red`,
			want: map[string]string{
				"scenarios.w": "a b (a -> b)[0]{style{stroke:red}}",
				"scenarios.v": "a b (a -> b)[0]{style{stroke:red}}",
			},
		},
		{
			name: "edges/glob-selector",
			text: `scenarios: {w: {a -> b}; v: {a -> b}}
scenarios.*.(* -> *)[*].style.stroke: red`,
			want: map[string]string{
				"scenarios.w": "a b (a -> b)[0]{style{stroke:red}}",
				"scenarios.v": "a b (a -> b)[0]{style{stroke:red}}",
			},
		},
		{
			name: "edges/glob-selector-in-map",
			text: `layers: {w: {a -> b}; v: {a -> b}; *: {(* -> *)[*].style.stroke: red}}`,
			want: map[string]string{
				"layers.w": "a b (a -> b)[0]{style{stroke:red}}",
				"layers.v": "a b (a -> b)[0]{style{stroke:red}}",
			},
		},
		{
			name: "edges/glob-selector-distinct",
			text: `layers: {w: {a -> b}; v: {c -> d}}
layers.*.(* -> *)[*].style.stroke: red`,
			want: map[string]string{
				"layers.w": "a b (a -> b)[0]{style{stroke:red}}",
				"layers.v": "c d (c -> d)[0]{style{stroke:red}}",
			},
		},
		{
			name: "imports/spread",
			text: `scenarios: {...@lib-boards; *: {a}}`,
			files: map[string]string{
				"lib-boards.d2": `w: {b}
v: {c}`,
			},
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "imports/value",
			text: `scenarios: @lib-boards
scenarios.*.a`,
			files: map[string]string{
				"lib-boards.d2": `w: {b}
v: {c}`,
			},
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "imports/file-with-glob",
			text: `...@lib-scen-with-glob`,
			files: map[string]string{
				"lib-scen-with-glob.d2": `scenarios: {w: {b}; v: {c}; *: {a}}`,
			},
			want: map[string]string{
				"scenarios.w": "b a",
				"scenarios.v": "c a",
			},
		},
		{
			name: "imports/layers-spread",
			text: `...@lib-layers
layers.*.a`,
			files: map[string]string{
				"lib-layers.d2": `layers: {w: {b}; v: {c}}`,
			},
			want: map[string]string{
				"layers.w": "b a",
				"layers.v": "c a",
			},
		},
		{
			name: "imports/layers-value",
			text: `layers: @lib-boards
layers.*.a`,
			files: map[string]string{
				"lib-boards.d2": `w: {b}
v: {c}`,
			},
			want: map[string]string{
				"layers.w": "b a",
				"layers.v": "c a",
			},
		},
		{
			name: "filters/label",
			text: `layers: {w: {label: W; b}; v: {label: V; c}; *: {&label: V; a}}`,
			want: map[string]string{
				"layers.w": "label:W b",
				"layers.v": "label:V c a",
			},
		},
		{
			name: "filters/not-label",
			text: `layers: {w: {label: W; b}; v: {label: V; c}; u: {label: U; d}; *: {!&label: W; a}}`,
			want: map[string]string{
				"layers.w": "label:W b",
				"layers.v": "label:V c a",
				"layers.u": "label:U d a",
			},
		},
		{
			name: "filters/outside",
			text: `scenarios: {w: {label: W; b}; v: {label: V; c}}
scenarios.*: {&label: V; a}`,
			want: map[string]string{
				"scenarios.w": "label:W b",
				"scenarios.v": "label:V c a",
			},
		},
		{
			name: "filters/second-glob",
			text: `layers: {w: {label: W; b}; v: {label: V; c}; *: {&label: V; a}; *: {&label: W; e}}`,
			want: map[string]string{
				"layers.w": "label:W b e",
				"layers.v": "label:V c a",
			},
		},
		{
			// Each pair of objects in different boards is skipped, as for **.
			name: "cross-board-edges/scenarios",
			text: `scenarios: {w: {a; b}; v: {a; b}}
scenarios.*.a -> scenarios.*.b`,
			want: map[string]string{
				"scenarios.w": "a b (a -> b)[0]",
				"scenarios.v": "a b (a -> b)[0]",
			},
		},
		{
			name: "cross-board-edges/new-objects",
			text: `scenarios: {w: {b}; v: {c}}
scenarios.*.x -> scenarios.*.y`,
			want: map[string]string{
				"scenarios.w": "b x y (x -> y)[0]",
				"scenarios.v": "c x y (x -> y)[0]",
			},
		},
		{
			name: "cross-board-edges/layers",
			text: `layers: {w: {a; b}; v: {a; b}}
layers.*.a -> layers.*.b`,
			want: map[string]string{
				"layers.w": "a b (a -> b)[0]",
				"layers.v": "a b (a -> b)[0]",
			},
		},
		{
			// Every pair is in different boards: nothing to connect.
			name: "cross-board-edges/only-across",
			text: `x
scenarios: {w: {a}}
scenarios.*.a -> x`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x a",
			},
		},
		{
			// Each step's copy of ** applies to its objects as they are created,
			// so a and b come before c.
			name: "order/steps",
			text: `steps: {**: {a -> b}; s1: {x; x.c}}
steps.s1.x.d`,
			want: map[string]string{
				"steps.s1": "x{a b c{a b (a -> b)[0]} d (a -> b)[0]}",
			},
		},
		{
			name: "order/scenarios",
			text: `scenarios: {**: {a -> b}; w: {x; x.c}; v: {y}}
scenarios.w.x.d`,
			want: map[string]string{
				"scenarios.w": "x{a b c{a b (a -> b)[0]} d (a -> b)[0]}",
				"scenarios.v": "y{a b (a -> b)[0]}",
			},
		},
		{
			name: "order/inherited",
			text: `p
steps: {**: {k}; s1: {x: {y}}; s2: {z}}`,
			want: map[string]string{
				"":         "p",
				"steps.s1": "p{k} x{k y{k}}",
				"steps.s2": "p{k} x{k y{k}} z{k}",
			},
		},
		{
			// A glob passing through a step with no map yet gives it its base
			// first.
			name: "base/steps",
			text: `x
steps.*.a
steps: {s1: {y}}`,
			want: map[string]string{
				"":         "x",
				"steps.s1": "x a y",
			},
		},
		{
			name: "base/scenarios",
			text: `x
scenarios.*.a
scenarios: {w: {y}}`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x a y",
			},
		},
		{
			name: "base/later-step",
			text: `steps.*: {a -> b}
steps.*.x: {c -> d}
steps: {s1: {y}}
steps: {s2: {z}}`,
			want: map[string]string{
				"steps.s1": "a b x{c d (c -> d)[0]} y (a -> b)[0]",
				"steps.s2": "a b x{c d (c -> d)[0]} y z (a -> b)[0]",
			},
		},
		{
			name: "base/glob-after-board",
			text: `x
scenarios: {w; v}
scenarios.*.a`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x a",
				"scenarios.v": "x a",
			},
		},
		{
			name: "base/literal-board",
			text: `x
scenarios: {w}
scenarios.w.*.style.fill: red`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x{style{fill:red}}",
			},
		},
		{
			// What a step's copy of a glob applies to does not count for the
			// board.
			name: "step-applications/root",
			text: `x
*.a
steps: {s1: {y}}
y`,
			want: map[string]string{
				"":         "x{a} y{a}",
				"steps.s1": "x{a} y{a}",
			},
		},
		{
			name: "step-applications/scenario",
			text: `x
*.a
steps: {s1: {y}}
scenarios: {w: {y}}`,
			want: map[string]string{
				"":            "x{a}",
				"scenarios.w": "x{a} y{a}",
				"steps.s1":    "x{a} y{a}",
			},
		},
		{
			// Steps compiled again start their copies of a glob declared since
			// with nothing applied.
			name: "step-applications/compiled-again",
			text: `steps: {s1: {x}; s2: {y}}
*.k
steps: {*: {z}}`,
			want: map[string]string{
				"steps.s1": "x{k} z{k}",
				"steps.s2": "x{k} y{k} z{k}",
			},
		},
		{
			// s2 starts as a copy of s1, which already has the edge.
			name: "once/steps-before",
			text: `steps: {*: {a -> b}; s1: {x}; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "a b x (a -> b)[0]",
				"steps.s2": "a b x y (a -> b)[0]",
			},
		},
		{
			name: "once/steps-outside",
			text: `steps.*: {a -> b}
steps: {s1: {x}; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "a b x (a -> b)[0]",
				"steps.s2": "a b x y (a -> b)[0]",
			},
		},
		{
			name: "once/steps-after",
			text: `steps: {s1: {x}; s2: {y}; *: {a -> b}}`,
			want: map[string]string{
				"steps.s1": "x a b (a -> b)[0]",
				"steps.s2": "x y a b (a -> b)[0]",
			},
		},
		{
			name: "once/steps-double-glob",
			text: `steps: {**: {a -> b}; s1: {x}; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "x{a b (a -> b)[0]}",
				"steps.s2": "x{a b (a -> b)[0]} y{a b (a -> b)[0]}",
			},
		},
		{
			name: "once/steps-star-path",
			text: `steps: {*.x: {a -> b}; s1: {x}; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "x{a b (a -> b)[0]}",
				"steps.s2": "x{a b (a -> b)[0]} y",
			},
		},
		{
			name: "once/scenarios-double-glob",
			text: `scenarios: {**: {a -> b}; w: {x}}`,
			want: map[string]string{
				"scenarios.w": "x{a b (a -> b)[0]}",
			},
		},
		{
			name: "once/scenario-compiled-again",
			text: `*: {a -> b}
scenarios: {s1: {y}; s2: {y}}
scenarios.*: {z}`,
			want: map[string]string{
				"scenarios.s1": "y{a b (a -> b)[0]} z{a b (a -> b)[0]}",
				"scenarios.s2": "y{a b (a -> b)[0]} z{a b (a -> b)[0]}",
			},
		},
		{
			name: "once/scenario-reopened",
			text: `*: {a -> b}
scenarios: {s1: {y}}
scenarios: {s1: {z}}`,
			want: map[string]string{
				"scenarios.s1": "y{a b (a -> b)[0]} z{a b (a -> b)[0]}",
			},
		},
		{
			name: "once/step-compiled-again",
			text: `* -> hub
x
steps: {s1: {y}; s2: {z}}
steps.*: {w}`,
			want: map[string]string{
				"":         "hub x (x -> hub)[0]",
				"steps.s1": "hub x y w (x -> hub)[0] (y -> hub)[0] (w -> hub)[0]",
				"steps.s2": "hub x y z w (x -> hub)[0] (y -> hub)[0] (z -> hub)[0] (w -> hub)[0]",
			},
		},
		{
			name: "once/inherited",
			text: `x
z
* -> z
scenarios: {w: {q}}
steps: {s1: {q}; s2: {r}}`,
			want: map[string]string{
				"":            "x z (x -> z)[0]",
				"scenarios.w": "x z q (x -> z)[0] (q -> z)[0]",
				"steps.s1":    "x z q (x -> z)[0] (q -> z)[0]",
				"steps.s2":    "x z q r (x -> z)[0] (q -> z)[0] (r -> z)[0]",
			},
		},
		{
			name: "once/import",
			text: `x
*: @f
p`,
			files: map[string]string{
				"f.d2": `p
q`,
			},
			want: map[string]string{
				"": "x{p q} p{p q}",
			},
		},
		{
			// n starts as a copy of w, which already has the edge.
			name: "nested/inherited-scenario",
			text: `scenarios.*: {a0 -> b0}
scenarios: {w: {scenarios: {n: {x}}}}`,
			want: map[string]string{
				"scenarios.w":             "a0 b0 (a0 -> b0)[0]",
				"scenarios.w.scenarios.n": "a0 b0 x (a0 -> b0)[0]",
			},
		},
		{
			// ** in a board group's map applies inside the boards of its boards
			// too, once.
			name: "nested/double-glob-steps-in-scenario",
			text: `scenarios: {**: {k}; w: {x; steps: {s1: {y}; s2: {z}}}}`,
			want: map[string]string{
				"scenarios.w":          "x{k}",
				"scenarios.w.steps.s1": "x{k} y{k}",
				"scenarios.w.steps.s2": "x{k} y{k} z{k}",
			},
		},
		{
			name: "nested/double-glob-scenario-in-step",
			text: `steps: {**: {a -> b}; s1: {x; scenarios: {alt: {y}}}; s2: {z}}`,
			want: map[string]string{
				"steps.s1":               "x{a b (a -> b)[0]}",
				"steps.s1.scenarios.alt": "x{a b (a -> b)[0]} y{a b (a -> b)[0]}",
				"steps.s2":               "x{a b (a -> b)[0]} z{a b (a -> b)[0]}",
			},
		},
		{
			name: "nested/step-path",
			text: `steps.*.x: {a -> b}
steps: {s1: {x; steps: {t1: {y}}}}`,
			want: map[string]string{
				"steps.s1":          "x{a b (a -> b)[0]}",
				"steps.s1.steps.t1": "x{a b (a -> b)[0]} y",
			},
		},
		{
			// A board declared by a dotted key starts from the globs of its
			// base.
			name: "once/dotted-nested-scenario",
			text: `* -> t0
steps: {s1: {y}}
steps.s1.scenarios: {s2: {x}}`,
			want: map[string]string{
				"":                      "t0",
				"steps.s1":              "t0 y (y -> t0)[0]",
				"steps.s1.scenarios.s2": "t0 y x (y -> t0)[0] (x -> t0)[0]",
			},
		},
		{
			name: "once/dotted-nested-step",
			text: `steps.*: {a -> b}
steps.s1.steps: {s3: {y}}
steps.*: {c -> d}`,
			want: map[string]string{
				"steps.s1":          "a b c d (a -> b)[0] (c -> d)[0]",
				"steps.s1.steps.s3": "a b y (a -> b)[0]",
			},
		},
		{
			// Boards made by dotted keys.
			name:  "dotted-boards",
			text:  "x\nscenarios.w.b\nscenarios.v.c\nscenarios.*.a",
			count: map[string]int{"scenarios.w.a": 1, "scenarios.v.a": 1},
		},
		{
			// The import is compiled in a tree of its own. Its p is not the
			// importer's p, which gets the import too.
			name:  "once/import-triple",
			text:  "x\n***: @f\np",
			files: map[string]string{"f.d2": "p\nq"},
			count: map[string]int{"x.p": 1, "x.q": 1, "p.p": 1, "p.q": 1},
		},
		{
			// A connection glob in a board group's map, with an endpoint that
			// would name a board there, applies in each board instead.
			name: "in-boards/edge-selector",
			text: `steps: {(* -> *)[*].style.stroke: red; s1: {a -> b}; s2: {c -> d}}`,
			want: map[string]string{
				"steps.s1": "a b (a -> b)[0]{style{stroke:red}}",
				"steps.s2": "a b c d (a -> b)[0]{style{stroke:red}} (c -> d)[0]{style{stroke:red}}",
			},
		},
		{
			// This was an error: cannot create edges between boards.
			name: "in-boards/star",
			text: `x
scenarios: {* -> hub; w: {y}; v: {z}}`,
			want: map[string]string{
				"":            "x",
				"scenarios.w": "x y hub (x -> hub)[0] (y -> hub)[0]",
				"scenarios.v": "x z hub (x -> hub)[0] (z -> hub)[0]",
			},
		},
		{
			// No step named hub.
			name: "in-boards/double-glob",
			text: `steps: {** -> hub; s1: {x}; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "x hub (x -> hub)[0]",
				"steps.s2": "x hub y (x -> hub)[0] (y -> hub)[0]",
			},
		},
		{
			name: "in-boards/layers",
			text: `layers: {* -> hub; l1: {x}; l2: {y}}`,
			want: map[string]string{
				"layers.l1": "x hub (x -> hub)[0]",
				"layers.l2": "y hub (y -> hub)[0]",
			},
		},
		{
			name: "in-boards/pattern",
			text: `scenarios: {a* -> b*; w: {a1; b1}}`,
			want: map[string]string{
				"scenarios.w": "a1 b1 (a1 -> b1)[0]",
			},
		},
		{
			name: "in-boards/literal-source",
			text: `scenarios: {a -> *; w: {a; b}; v: {a; c}}`,
			want: map[string]string{
				"scenarios.w": "a b (a -> b)[0]",
				"scenarios.v": "a c (a -> c)[0]",
			},
		},
		{
			// Boards declared before the glob get it too.
			name: "in-boards/after-boards",
			text: `steps: {s1: {a -> b}; s2: {c -> d}; (* -> *)[*].style.stroke: red}`,
			want: map[string]string{
				"steps.s1": "a b (a -> b)[0]{style{stroke:red}}",
				"steps.s2": "a b c d (a -> b)[0]{style{stroke:red}} (c -> d)[0]{style{stroke:red}}",
			},
		},
		{
			// s2 starts as a copy of s1, which already has x -> hub.
			name: "in-boards/between-steps",
			text: `steps: {s1: {x}; * -> hub; s2: {y}}`,
			want: map[string]string{
				"steps.s1": "x hub (x -> hub)[0]",
				"steps.s2": "x hub y (x -> hub)[0] (y -> hub)[0]",
			},
		},
		{
			// As for a glob declared after the board's own keys.
			name: "in-boards/later-declaration",
			text: `steps: {s1: {a -> b: {style.stroke: blue}}; (* -> *)[*].style.stroke: red}`,
			want: map[string]string{
				"steps.s1": "a b (a -> b)[0]{style{stroke:red}}",
			},
		},
		{
			// The scenarios and steps of a board get the glob, as they get
			// one declared in the board. Its layers do not.
			name: "in-boards/nested",
			text: `scenarios: {(* -> *)[*].style.stroke: red; w: {a -> b; steps: {s1: {c -> d}}; layers: {l: {e -> f}}}}`,
			want: map[string]string{
				"scenarios.w":          "a b (a -> b)[0]{style{stroke:red}}",
				"scenarios.w.steps.s1": "a b c d (a -> b)[0]{style{stroke:red}} (c -> d)[0]{style{stroke:red}}",
				"scenarios.w.layers.l": "e f (e -> f)[0]",
			},
		},
		{
			name: "in-boards/nested-after",
			text: `scenarios: {w: {x; steps: {s1: {y}}}; ** -> hub}`,
			want: map[string]string{
				"scenarios.w":          "x hub (x -> hub)[0]",
				"scenarios.w.steps.s1": "x y hub (x -> hub)[0] (y -> hub)[0]",
			},
		},
		{
			name: "in-boards/import",
			text: `...@lib-steps`,
			files: map[string]string{
				"lib-steps.d2": `steps: {(* -> *)[*].style.stroke: red; s1: {a -> b}}`,
			},
			want: map[string]string{
				"steps.s1": "a b (a -> b)[0]{style{stroke:red}}",
			},
		},
		{
			// Each endpoint has two elements: the first selects the boards.
			name: "group-map/connection",
			text: `scenarios: {*.x -> *.y; w: {x; y}; v: {x; y}}`,
			want: map[string]string{
				"scenarios.w": "x y (x -> y)[0]",
				"scenarios.v": "x y (x -> y)[0]",
			},
		},
		{
			// * selects the scenario, whose own fill it sets.
			name: "group-map/board-style",
			text: `scenarios: {*.style.fill: red; w: {b}}`,
			want: map[string]string{
				"scenarios.w": "style{fill:red} b",
			},
		},
		{
			// v and u start as copies of w, which has the glob's edge.
			name: "no-map/steps-after",
			text: `steps: {w; v; u; *: {a -> b}}`,
			want: map[string]string{
				"steps.w": "a b (a -> b)[0]",
				"steps.v": "a b (a -> b)[0]",
				"steps.u": "a b (a -> b)[0]",
			},
		},
		{
			name: "no-map/steps-outside",
			text: `x
steps: {w; v}
steps.*: {a -> b}`,
			want: map[string]string{
				"":        "x",
				"steps.w": "x a b (a -> b)[0]",
				"steps.v": "x a b (a -> b)[0]",
			},
		},
		{
			name: "no-map/steps-before",
			text: `steps: {*: {a -> b}; w; v}`,
			want: map[string]string{
				"steps.w": "a b (a -> b)[0]",
				"steps.v": "a b (a -> b)[0]",
			},
		},
		{
			name: "no-map/steps-after-map",
			text: `steps: {w; v: {y}; u; *: {a -> b}}`,
			want: map[string]string{
				"steps.w": "a b (a -> b)[0]",
				"steps.v": "y a b (a -> b)[0]",
				"steps.u": "y a b (a -> b)[0]",
			},
		},
		{
			name: "no-map/steps-connection",
			text: `steps: {1; 2}
steps.*.x -> steps.*.y`,
			want: map[string]string{
				"steps.1": "x y (x -> y)[0]",
				"steps.2": "x y (x -> y)[0]",
			},
		},
		{
			name: "no-map/steps-path",
			text: `steps: {w; v; u; *.style.fill: red}`,
			want: map[string]string{
				"steps.w": "style{fill:red}",
				"steps.v": "style{fill:red}",
				"steps.u": "style{fill:red}",
			},
		},
		{
			// A failing filter leaves the scenario to its own value, which
			// starts from its base.
			name: "filters/failing",
			text: `r
scenarios: {*: {&label: Z; k}; w: {x}}`,
			want: map[string]string{
				"":            "r",
				"scenarios.w": "r x",
			},
		},
		{
			// The step's copy of *** recognizes what it applied to in the
			// board the step is a copy of.
			name: "once/triple-inherited",
			text: `***: {u -> v}
a
steps: {s1: {b}}`,
			want: map[string]string{
				"":         "a{u v (u -> v)[0]}",
				"steps.s1": "a{u v (u -> v)[0]} b{u v (u -> v)[0]}",
			},
		},
		{
			// The glob does not apply to what its own import added. This was
			// a cyclic import error.
			name: "once/glob-own-import",
			text: `x.**: @n
x.z
...@n`,
			files: map[string]string{"n.d2": "p\n**.label: L"},
			want: map[string]string{
				"": "x{z{p}} p{label:L}",
			},
		},
	}
	for _, tc := range tca {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"index.d2": tc.text}
			for k, v := range tc.files {
				files[k] = v
			}
			ast, err := d2parser.Parse("index.d2", strings.NewReader(tc.text), nil)
			assert.Success(t, err)
			fs, err := mapfs.New(files)
			assert.Success(t, err)
			t.Cleanup(func() { fs.Close() })
			m, _, err := d2ir.Compile(ast, &d2ir.CompileOptions{FS: fs})
			assert.Success(t, err)
			for path, want := range tc.want {
				if got := boardShape(t, m, path); got != want {
					t.Errorf("board %q:\ngot  %s\nwant %s", path, got, want)
				}
			}
			if len(tc.want) > 0 {
				for _, path := range boardPaths(m, "")[1:] {
					if _, ok := tc.want[path]; !ok {
						t.Errorf("unexpected board %q: %s", path, boardShape(t, m, path))
					}
				}
			}
			for query, want := range tc.count {
				na, err := m.QueryAll(query)
				assert.Success(t, err)
				if len(na) != want {
					t.Errorf("%s: got %d, want %d", query, len(na), want)
				}
			}
		})
	}
}

// boardShape returns the fields and edges of the board at path, which is
// dotted, in order. A field with a primary value is name:value and one with
// children is name{...}, as is an edge with children. The board's own layers,
// scenarios and steps are left out. A board without a map is "<no map>".
func boardShape(t *testing.T, m *d2ir.Map, path string) string {
	t.Helper()
	if path != "" {
		for _, name := range strings.Split(path, ".") {
			if m == nil {
				t.Fatalf("no board %q", path)
			}
			f := m.GetField(d2ast.FlatUnquotedString(name))
			if f == nil {
				t.Fatalf("no board %q", path)
			}
			m = f.Map()
		}
		if m == nil {
			return "<no map>"
		}
	}
	return shape(m)
}

// boardPaths returns the root board ("") and the path of every layer, scenario
// and step under m, which is the board at prefix.
func boardPaths(m *d2ir.Map, prefix string) []string {
	paths := []string{prefix}
	if m == nil {
		return paths
	}
	for _, group := range m.Fields {
		if _, ok := d2ast.BoardKeywords[d2ast.Keyword(group.Name)]; !ok || group.Map() == nil {
			continue
		}
		for _, b := range group.Map().Fields {
			path := group.Name.ScalarString() + "." + b.Name.ScalarString()
			if prefix != "" {
				path = prefix + "." + path
			}
			paths = append(paths, boardPaths(b.Map(), path)...)
		}
	}
	return paths
}

func shape(m *d2ir.Map) string {
	if m == nil {
		return ""
	}
	var parts []string
	for _, f := range m.Fields {
		if _, ok := d2ast.BoardKeywords[d2ast.Keyword(f.Name)]; ok {
			continue
		}
		s := f.Name.ScalarString()
		if f.Primary() != nil {
			s += ":" + f.Primary().Value.ScalarString()
		}
		if children := shape(f.Map()); children != "" {
			s += "{" + children + "}"
		}
		parts = append(parts, s)
	}
	for _, e := range m.Edges {
		s := e.IDString().ScalarString()
		if children := shape(e.Map()); children != "" {
			s += "{" + children + "}"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}
