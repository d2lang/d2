package d2ir

import (
	"testing"

	"github.com/d2lang/d2/d2ast"
)

func TestSpreadEdgeValues(t *testing.T) {
	const vars = "vars: {t: {a -> b: template {style.stroke: red; style.opacity: 0.5}}; link: edge {style.opacity: 0.5; style.stroke: orange}}\n"
	for _, tc := range []struct{ name, src, label, stroke string }{
		{"later assignments", "a -> b; ...${t}; (a -> b)[0]: explicit {style.stroke: blue}", "explicit", "blue"},
		{"later spread", "a -> b: explicit {style.stroke: blue}; ...${t}", "template", "red"},
		{"aliased edge style override", "a -> b: ${link}; (a -> b)[0].style.stroke: teal", "edge", "teal"},
		{"only label overridden", "a -> b; ...${t}; (a -> b)[0]: local", "local", "red"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := compileIndexTestSource(t, vars+tc.src)
			if len(m.Edges) != 1 {
				t.Fatalf("got %d edges, want 1", len(m.Edges))
			}
			e := m.Edges[0]
			if e.Primary().Value.ScalarString() != tc.label {
				t.Fatalf("edge label = %s, want %s", e.Primary().Value.ScalarString(), tc.label)
			}
			style := e.Map().GetField(d2ast.FlatUnquotedString("style")).Map()
			for key, want := range map[string]string{"stroke": tc.stroke, "opacity": "0.5"} {
				if got := style.GetField(d2ast.FlatUnquotedString(key)).Primary().Value.ScalarString(); got != want {
					t.Errorf("style.%s = %s, want %s", key, got, want)
				}
			}
		})
	}
}

func TestSpreadDeletionScope(t *testing.T) {
	const vars = "vars: {t: {a -> b; a: {style.fill: red; style.stroke: green}}}\n"
	for _, tc := range []struct{ name, source, expected string }{
		{"delete edge from spread", "...${t}; (a -> b)[0]: null", "a: {style.fill: red; style.stroke: green}; b"},
		{"delete parent edge", "...${t}; child: {(_.a -> _.b)[0]: null}", "a: {style.fill: red; style.stroke: green}; b; child: {}"},
		{"delete nested edge", "scope: {...${t}}; (scope.a -> scope.b)[0]: null", "scope: {a: {style.fill: red; style.stroke: green}; b}"},
		{"delete endpoint from spread", "...${t}; a: null", "b"},
		{"delete style in later step", "...${t}; steps: {s: {a.style.fill: null}}", "a -> b; a: {style.fill: red; style.stroke: green}; steps: {s: {a.style.fill: null}}"},
		{"delete nested reserved key", "scope: {...${t}}; scope.a.style.fill: null", "scope: {a -> b; a.style.stroke: green}"},
		{"recreate independently", "...${t}; a: null; a -> c", "b; a -> c"},
		{"later spread restores field", "...${t}; a: null; ...${t}", "b; a -> b; a: {style.fill: red; style.stroke: green}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := compileIndexTestSource(t, vars+tc.expected).String()
			if got := compileIndexTestSource(t, vars+tc.source).String(); got != want {
				t.Fatalf("spread produced\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestMapSpreadGlobDeletionOrder(t *testing.T) {
	const defaults = "node: {style.fill: red; style.stroke: green}"
	const vars = "vars: {t: {" + defaults + "}}\n"
	const clear = "node*.style.fill: null"
	for _, tc := range []struct{ name, spread, inline string }{
		{"earlier lazy null", clear + "; ...${t}", clear + "; " + defaults},
		{"later lazy null", "...${t}; " + clear, defaults + "; " + clear},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := compileIndexTestSource(t, vars+tc.inline).String()
			got := compileIndexTestSource(t, vars+tc.spread).String()
			if got != want {
				t.Fatalf("spread produced\n%s\nwant inline result\n%s", got, want)
			}
		})
	}
}
