package d2ir

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"

	"github.com/d2lang/util-go/assert"
)

func TestNullSuppliedByMapSpread(t *testing.T) {
	for _, tc := range []struct{ name, template, body string }{
		{"delete color", "node.style.fill: null", "node.style.fill: red; node.style.stroke: green; ...${t}"},
		{"remove empty style", "node.style.fill: null", "node.style.fill: red; ...${t}"},
		{"remove absent color", "node.style.fill: null", "...${t}"},
		{"later color", "node.style.fill: null", "node.style.fill: red; ...${t}; node.style.fill: blue"},
		{"later spread", "node: null", "node.old; ...${t}; ...${fresh}"},
		{"drop old children", "node: null", "node: old {a; b}; ...${t}; node.c"},
		{"bare recreation", "node: null", "node: old; middle; ...${t}; last; node"},
		{"recreate within template", "node: null; node: next {c}", "node: old {a; b}; ...${t}"},
		{"recreate child within template", "node: null; node.new", "node: old {a; b}; ...${t}"},
		{"nested use", "node: null", "node.old; ...${indirect}; node.later"},
		{"repeated use", "node: null", "node.old; ...${t}; node.middle; ...${t}; node.last"},
		{"multiple deletions", "node.a: null; node: null", "node: old {a; b}; ...${t}; node.a: new"},
		{"template null and revival", "node: null; node.new; node.style.fill: null", "node: old {a; b}; ...${t}; node.later"},
		{"remove connected endpoint", "node: null", "node -> other; ...${t}"},
		{"reuse the same edge template", "node: null", "...${edge}; ...${t}; ...${edge}"},
		{"delete then reconnect", "node: null", "node -> other; ...${t}; node -> next"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := "vars: {t: {" + tc.template + "}; fresh: {node.current: new}; edge: {node -> other}; indirect: {...${t}}}\n"
			inline := strings.NewReplacer("...${t}", tc.template, "...${indirect}", tc.template, "...${edge}", "node -> other").Replace(tc.body)
			assert.String(t, compileIndexTestSource(t, vars+inline).String(), compileIndexTestSource(t, vars+tc.body).String())
		})
	}
}

func TestImportedTemplateNull(t *testing.T) {
	const source = `vars: {outside: {node.style.fill: red; node.style.stroke: blue}}
first: @library
cached: @library
...@library
steps: {inherit: {next}}`
	for _, tc := range []struct{ name, library, fill string }{
		{"caller spread before deletion", "...${outside}; ...${del}", ""},
		{"caller spread after deletion", "...${del}; ...${outside}", "red"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ast, err := d2parser.Parse("main.d2", strings.NewReader(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			library := "vars: {del: {node.style.fill: null}}\n" + tc.library
			root, _, err := Compile(ast, &CompileOptions{FS: fstest.MapFS{"library.d2": {Data: []byte(library)}}})
			if err != nil {
				t.Fatal(err)
			}
			for _, scope := range []string{"", "first.", "cached.", "steps.inherit.", "steps.inherit.first.", "steps.inherit.cached."} {
				assertSpreadPrimaries(t, root, map[string]string{scope + "node.style.stroke": "blue"})
				if tc.fill != "" {
					assertSpreadPrimaries(t, root, map[string]string{scope + "node.style.fill": tc.fill})
				} else if nodes, err := root.QueryAll(scope + "node.style.fill"); err != nil || len(nodes) != 0 {
					t.Errorf("%snode.style.fill = %v (%v), want deleted", scope, nodes, err)
				}
			}
		})
	}
}

func TestTemplateNullOnlyAffectsItsAlias(t *testing.T) {
	root := compileIndexTestSource(t, `vars: {original: {style.fill: red; keep: original}; changes: {first.style.fill: null}}
first: ${original}
other: ${original}
...${changes}`)
	assertSpreadPrimaries(t, root, map[string]string{"first.keep": "original", "other.style.fill": "red"})
	assertSpreadPrimaries(t, root.getFieldIndexed(d2ast.FlatUnquotedString("vars")).Map(), map[string]string{"original.style.fill": "red"})
	if nodes, err := root.QueryAll("first.style"); err != nil || len(nodes) != 0 {
		t.Errorf("first.style = %v (%v), want deleted", nodes, err)
	}
}
