package d2ir

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2parser"
)

func TestSpreadArrayReplacesEarlierMap(t *testing.T) {
	const prefix = "vars: {defaults: {node: [a; b]}; reopened: {node: {first}}}\n"
	for _, tc := range []struct {
		name, before, after string
	}{
		{"reopen", "node.old", "node: {fresh}"},
		{"bare keys in new order", "node: {a: old; b; c}", "node: {c; b; a}"},
		{"nested keys", "node: {x: {a; b}}", "node: {x: {b; a}}"},
		{"recreated edge", "node: {a -> b}", "node: {a -> b}"},
		{"several map declarations", "node.old", "node: {x}; node: {y}"},
		{"spread reopens before indexing", "node.old", "$REOPEN; node.child: valid"},
		{"cannot index array", "", "node.child: invalid"},
		{"cannot index before reopening", "", "node.child: invalid; node: {later}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(assignment, reopen string) string {
				t.Helper()
				src := prefix + tc.before + "\n" + assignment + "\n" + strings.ReplaceAll(tc.after, "$REOPEN", reopen)
				ast, err := d2parser.Parse("array.d2", strings.NewReader(src), nil)
				if err != nil {
					t.Fatal(err)
				}
				root, _, err := Compile(ast, nil)
				if err != nil {
					if !strings.Contains(err.Error(), "cannot index into array") {
						t.Fatal(err)
					}
					return err.Error()
				}
				return root.String()
			}
			if got, want := compile("...${defaults}", "...${reopened}"), compile("node: [a; b]", "node: {first}"); got != want {
				t.Fatalf("spread:\n%s\ninline:\n%s", got, want)
			}
		})
	}
}

func TestArraySpreadOrderAndScope(t *testing.T) {
	const vars = `vars: {color: red; empty: []; base: [${color}; green]; theme: [...${base}; ...${empty}]}`
	const use = `app: {vars.color: wrong; class: [first; ...${theme}; middle; ...${theme}; ...${empty}; last]}`
	for _, source := range []string{vars + "\n" + use, use + "\n" + vars} {
		ast, err := d2parser.Parse("array.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		m, _, err := Compile(ast, nil)
		if err != nil {
			t.Fatal(err)
		}
		arr := m.getFieldIndexedMode(d2ast.MakeKeyPath([]string{"app", "class"}).IDA(), true).Composite.(*Array)
		var values []string
		for _, v := range arr.Values {
			values = append(values, v.Primary().Value.ScalarString())
		}
		if got, want := strings.Join(values, "/"), "first/red/green/middle/red/green/last"; got != want {
			t.Errorf("array = %q, want %q", got, want)
		}
	}
}
