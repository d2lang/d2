package d2compiler_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
)

// TestInputsThatPanicked compiles inputs on which the compiler panicked. Most
// were found by fuzzing d2compiler.Compile.
func TestInputsThatPanicked(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text, err string
		// For inputs that compile, when set: the IDs of the objects and
		// edges of the board (the last step, if there are steps, or else the
		// first layer, if there are layers), edges "none" for none.
		objects, edges string
	}{
		{
			// The column's map is inside the table, whose children became
			// its columns, so the reference's scope is the table.
			name: "underscore_from_sql_table_column",
			text: "t: {\n  shape: sql_table\n  a: int\n}\nt.a: {\n  _._.x\n}\n",
		},
		{
			name: "underscore_from_class_field",
			text: "c: {\n  shape: class\n  f: {\n    _._.y\n  }\n}\n",
		},
		{
			name:  "edge_from_sql_table_column",
			text:  "t: {\n  shape: sql_table\n  a: {\n    _._.x -> _._.y\n  }\n}\n",
			edges: "(x -> y)[0]",
		},
		{
			// Each endpoint must name a field; the other endpoint's
			// underscores used up the second one.
			name: "edge_endpoint_of_underscores",
			text: "x: {y: {_._.b -> _}}\n",
			err:  "1:9: field key must contain more than underscores",
		},
		{
			// The edge's common prefix c was made, but the edge failed
			// before its keys gave c any reference.
			name: "keyword_after_common_prefix",
			text: "c.left -> c.t\n",
			err:  "1:3: reserved keywords are prohibited in edges",
		},
		{
			// Both edges have left as an endpoint.
			name: "keyword_in_edge_chain",
			text: "l -> c.left -> c.t\n",
			err:  "1:8: reserved keywords are prohibited in edges\n1:8: reserved keywords are prohibited in edges",
		},
		{
			name: "keyword_common_prefix",
			text: "near.a -> near.b\n",
			err:  `1:1: "near" must be the last part of the key`,
		},
		{
			// The underscores add x to the resolved path, which is longer
			// than the key.
			name: "keyword_after_underscore",
			text: "x: {_.a -> left}\n",
			err:  "1:12: reserved keywords are prohibited in edges",
		},
		{
			name: "keyword_after_two_underscores",
			text: "x: {y: {_._.a -> b.shape}}\n",
			err:  "1:20: reserved keywords are prohibited in edges",
		},
		{
			// label was only part of the edge's keys: no key set it.
			name: "edges_in_label_map",
			text: "label.a -> label.b\n",
			err:  "1:12: unexpected edges in map",
		},
		{
			name: "constraint_with_child",
			text: "a: {constraint.x}\n",
			err:  `1:5: "constraint" keyword can only be used in "sql_table" shapes`,
		},
		{
			name: "step_after_step_without_map",
			text: "steps: {\n  0: x\n  1: {a}\n}\n",
		},
		{
			// Step 2 builds on step 0, the closest step with a map.
			name:    "step_after_steps_without_maps",
			text:    "steps: {\n  0: {a}\n  1: x\n  2: {b}\n}\n",
			objects: "a b",
		},
		{
			// The globs match nothing. a gets a reference from the edge's
			// keys; b, made only for the common prefix a.b, gets none and
			// is dropped, as a.b.*.x -> a.c.*.y has a and no a.b.
			name:    "glob_edge_matching_nothing",
			text:    "q\na.b.*.x -> a.b.*.y\n",
			objects: "q a",
			edges:   "none",
		},
		{
			// a.b, made for the edge, gets its references from the keys
			// after it, and stays.
			name:    "glob_edge_before_keys_under_it",
			text:    "a.b.*.x -> a.b.*.y\na.b.c.x\na.b.c.y\n",
			objects: "a b c x y",
		},
		{
			// A block comment in an array is skipped like a line comment;
			// it was a nil value.
			name:    "block_comment_in_array",
			text:    "d: {class: [a; \"\"\" note \"\"\"; b]}\n",
			objects: "d",
		},
		{
			// The glob's value is compiled in the layer, a board of its own.
			name:    "glob_over_layers",
			text:    "layers: {\n  w: {b}\n  *: {a}\n}\n",
			objects: "b a",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g, _, err := d2compiler.Compile("", strings.NewReader(tc.text), nil)
			if tc.err == "" {
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case len(g.Steps) > 0:
					g = g.Steps[len(g.Steps)-1]
				case len(g.Layers) > 0:
					g = g.Layers[0]
				}
				if tc.objects != "" {
					var ids []string
					for _, obj := range g.Objects {
						ids = append(ids, obj.ID)
					}
					if got := strings.Join(ids, " "); got != tc.objects {
						t.Fatalf("objects %q, want %q", got, tc.objects)
					}
				}
				if tc.edges != "" {
					edges := []string{"none"}
					for _, e := range g.Edges {
						edges = append(edges, e.AbsID())
					}
					if len(edges) > 1 {
						edges = edges[1:]
					}
					if got := strings.Join(edges, " "); got != tc.edges {
						t.Fatalf("edges %q, want %q", got, tc.edges)
					}
				}
				return
			}
			if err == nil || err.Error() != tc.err {
				t.Fatalf("err = %q, want %q", err, tc.err)
			}
		})
	}
}
