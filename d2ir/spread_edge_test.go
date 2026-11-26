package d2ir

import (
	"strings"
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

func TestClearEdgeSelectorProperty(t *testing.T) {
	const pair = "a -> b: first {style.opacity: 0.4; style.stroke: blue}; a -> b: second {style.opacity: 0.6}"
	for _, tc := range []struct{ name, source, want string }{
		{"indexed property", pair + "; (a -> b)[0].style.opacity: null", "a -> b: first {style.stroke: blue}; a -> b: second {style.opacity: 0.6}"},
		{"all parallel properties", pair + "; (a -> b)[*].style.opacity: null", "a -> b: first {style.stroke: blue}; a -> b: second"},
		{"whole edge", pair + "; (a -> b)[1]: null", "a -> b: first {style.opacity: 0.4; style.stroke: blue}"},
		{"missing edge", "a; b; (a -> b)[0].style.opacity: null", "a; b"},
		{"property of spread", "vars: {t: {" + pair + "}}; ...${t}; (a -> b)[0].style.opacity: null", "vars: {t: {" + pair + "}}; a -> b: first {style.stroke: blue}; a -> b: second {style.opacity: 0.6}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := compileIndexTestSource(t, tc.source)
			want := compileIndexTestSource(t, tc.want)
			if len(got.Edges) != len(want.Edges) {
				t.Fatalf("got %d edges; want %d", len(got.Edges), len(want.Edges))
			}
			for i, edge := range got.Edges {
				if edge.Primary().Value.ScalarString() != want.Edges[i].Primary().Value.ScalarString() {
					t.Fatalf("wrong edge %d label", i)
				}
				for _, key := range []string{"opacity", "stroke"} {
					read := func(e *Edge) string {
						if f := e.Map().getFieldIndexed(d2ast.FlatUnquotedString("style"), d2ast.FlatUnquotedString(key)); f != nil && f.Primary() != nil {
							return f.Primary().Value.ScalarString()
						}
						return ""
					}
					if value, expected := read(edge), read(want.Edges[i]); value != expected {
						t.Errorf("edge %d %s: got %q, want %q", i, key, value, expected)
					}
				}
			}
		})
	}
}

func TestClearPropertiesOnScopedParallelSpreadEdges(t *testing.T) {
	const links = `a -> b: Alpha {style.opacity: 0.2}; a -> b: Beta {style.opacity: 0.3}; q -> z: Other {style.opacity: 0.7}`
	const prefix = "vars: {t: {" + links + "}}\n"
	const use = "scope: {...${t}}; other: {...${t}}; "
	for _, sel := range []string{
		"scope.(a -> b)[*]", "(scope.a -> scope.b)[*]", "scope.(a -> *)[*]", "scope.(* -> b)[*]",
		"scope.(** -> **)[*]", "(scope.a -> scope.*)[*]", "scope.(a -> *)[0]", "(** -> **)[1]",
	} {
		source := prefix + use + sel + ".style.opacity: null"
		got := compileIndexTestSource(t, source)
		want := compileIndexTestSource(t, strings.ReplaceAll(source, "...${t}", links))
		for _, scope := range []string{"scope", "other"} {
			inScope := func(m *Map) string {
				return m.getFieldIndexed(d2ast.FlatUnquotedString(scope)).Map().String()
			}
			if inScope(got) != inScope(want) {
				t.Errorf("%s, %s: got\n%s\nwant\n%s", sel, scope, inScope(got), inScope(want))
			}
		}
	}
}
