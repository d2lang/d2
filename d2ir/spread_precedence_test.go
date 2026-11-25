package d2ir

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/util-go/assert"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
)

// A resolved map supplies the values in effect at the spread's use site.
// Compare against writing those same values there directly, including the
// other fields in the map so a winning scalar cannot discard any siblings.
func TestMapSpreadAssignmentOrder(t *testing.T) {
	const defaults = "grid-rows: 1; node: template {style.fill: red; child: retained}; values: [old; items]"
	const changes = "grid-rows: 3; node.style.stroke: orange; values: [new]"
	const override = "grid-rows: 2; node: local; node.style.fill: blue; values: [local; items]"
	const vars = "vars: {defaults: {" + defaults + "}; changes: {" + changes + "}; nested: {...${defaults}; node.style.fill: pink}}\n"
	const file = "grid-rows: 4; node.style.fill: green; values: [file]"
	for _, tc := range []struct {
		name, body string
	}{
		{"spread then explicit", "...${defaults}; " + override},
		{"explicit then spread", override + "; ...${defaults}"},
		{"assignments between spreads", "...${defaults}; " + override + "; ...${changes}; node.style.stroke: purple"},
		{"reference after assignment", "node: earlier; ...${defaults}; node -> other"},
		{"later assignment after early reference", "node -> other; ...${defaults}; node: later"},
		{"nested overlapping scopes", "group.node.style.fill: yellow; group: {\n...${defaults}\n}; group.node.style.fill: green"},
		{"array replaces map", "...${defaults}; node: [a; b]"},
		{"map replaces array", "...${defaults}; values: {item: current}"},
		{"array replaces earlier map", "values: {item: old}; ...${defaults}"},
		{"replace then reopen map", "...${defaults}; node: [a; b]; node: {style.fill: green}"},
		{"later scalar substitution", "...${defaults}; node.style.fill: ${changes.grid-rows}"},
		{"later map substitution", "...${defaults}; node: ${changes.node}"},
		{"later array substitution", "...${defaults}; node: ${changes.values}"},
		{"earlier array substitution", "node: ${changes.values}; ...${defaults}"},
		{"nested spread", "...${nested}; node.style.fill: green"},
		{"earlier glob", "node; nod*.style.fill: blue; ...${defaults}"},
		{"later glob", "node; ...${defaults}; nod*.style.fill: blue"},
		{"earlier glob on new field", "nod*.style.fill: blue; ...${defaults}"},
		{"later glob on new field", "...${defaults}; nod*.style.fill: blue"},
		{"deleted", "...${defaults}; node: null"},
		{"recreated after deletion", "...${defaults}; node: null; node: restored"},
		{"nested child deleted", "...${defaults}; node.style: null; node.style.fill: green"},
		{"spread after deletion", "node: old; node: null; ...${defaults}"},
		{"file after spread", "...${defaults}; ...@library"},
		{"file before spread", "...@library; ...${defaults}"},
		{"cached file after spread", "cache: @library; ...${defaults}; ...@library"},
		{"cached selected import after spread", "cache: @library; ...${defaults}; node.style.fill: @library.node.style.fill"},
		{"cached map import after spread", "cache: @library; ...${defaults}; node: @library.node"},
		{"inherited steps", "...${defaults}; steps: {first: {\n" + override + "\n}; next: {\n...${changes}\n}; last: {\nnode.style.stroke: green\n}}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := func(body string) string {
				t.Helper()
				ast, err := d2parser.Parse("main.d2", strings.NewReader(vars+body), nil)
				if err != nil {
					t.Fatal(err)
				}
				root, _, err := Compile(ast, &CompileOptions{FS: fstest.MapFS{"library.d2": {Data: []byte(file)}}})
				if err != nil {
					t.Fatal(err)
				}
				return root.String()
			}
			inline := strings.NewReplacer("...${defaults}", defaults, "...${changes}", changes, "...${nested}", defaults+"; node.style.fill: pink").Replace(tc.body)
			assert.String(t, run(inline), run(tc.body))
		})
	}
}

func TestMapSpreadWinningAssignmentReference(t *testing.T) {
	for _, tc := range []struct {
		source   string
		wantLine int
	}{
		{"...${defaults}\nn: explicit\nn -> next", 2},
		{"n: explicit\n...${defaults}\nn -> next", 0},
	} {
		root := compileIndexTestSource(t, "vars: {defaults: {n: template}}\n"+tc.source)
		n := root.GetField(d2ast.FlatUnquotedString("n"))
		key := n.LastPrimaryKey()
		if key == nil || key.Range.Start.Line != tc.wantLine {
			t.Fatalf("%q: primary assignment for %v = %v, want source line %d", tc.source, n.Name, key, tc.wantLine+1)
		}
	}
}

func TestMapSpreadKeepsOtherAliasChildren(t *testing.T) {
	const source = `vars: {
	  original: {keep: inherited; overlap: old}
	  changes: {node: new {overlap: changed}}
	}
	node: ${original}
	...${changes}`
	root := compileIndexTestSource(t, source)
	assertSpreadPrimaries(t, root, map[string]string{"node": "new", "node.keep": "inherited", "node.overlap": "changed"})
}

func TestMapSpreadInsideRepeatedImport(t *testing.T) {
	const source = `vars: {defaults: {node: {style.fill: red; child: inherited}}}
first: @library
second: @library
edited: @library
...@library
edited.node.style.fill: local`
	const library = `...${defaults}
node.style.fill: library`
	ast, err := d2parser.Parse("main.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	root, _, err := Compile(ast, &CompileOptions{
		FS: fstest.MapFS{"library.d2": {Data: []byte(library)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSpreadPrimaries(t, root, map[string]string{
		"first.node.style.fill": "library", "second.node.style.fill": "library", "edited.node.style.fill": "local", "node.style.fill": "library",
		"first.node.child": "inherited", "second.node.child": "inherited", "edited.node.child": "inherited", "node.child": "inherited",
	})
}

func assertSpreadPrimaries(t *testing.T, root *Map, expected map[string]string) {
	t.Helper()
	for path, want := range expected {
		nodes, err := root.QueryAll(path)
		if err != nil || len(nodes) != 1 || nodes[0].Primary() == nil || nodes[0].Primary().Value.ScalarString() != want {
			t.Errorf("query %s = %v (%v), want %s", path, nodes, err, want)
		}
	}
}
