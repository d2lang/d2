package d2parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestInvalidEdgeGroup(t *testing.T) {
	for _, tc := range []struct {
		source, message string
	}{
		{"(a -> b", "edge groups must be terminated with )"},
		{"scope.(a -> b  ", "edge groups must be terminated with )"},
		{"(a -> b\nnext", "edge groups must be terminated with )"},
		{"(x)", "edge groups must contain a connection"},
		{"()", "edge groups must contain a connection"},
		{"scope.(child): label", "edge groups must contain a connection"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			tree, err := d2parser.Parse("input.d2", strings.NewReader(tc.source), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
				t.Fatalf("expected one group diagnostic, got %v", err)
			}
			got := parseErr.Errors[0]
			if !strings.Contains(got.Message, tc.message) || got.Range.Start.Byte != strings.IndexByte(tc.source, '(') {
				t.Fatalf("expected %q at the opening parenthesis, got %+v", tc.message, got)
			}
			if strings.HasSuffix(tc.source, "\nnext") {
				if len(tree.Nodes) != 2 || tree.Nodes[1].MapKey.Key.StringIDA()[0] != "next" {
					t.Fatal("expected the parser to recover at the next line")
				}
			}
		})
	}
}

func TestGroupedEdgeParentheses(t *testing.T) {
	tree, err := d2parser.Parse("input.d2", strings.NewReader(`(get() -> save()); (a -> b)[0].call(): ok; "(x)"`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree.Nodes) != 3 {
		t.Fatalf("expected three entries, got %d", len(tree.Nodes))
	}
	key := tree.Nodes[0].MapKey
	if len(key.Edges) != 1 || key.Edges[0].Src.StringIDA()[0] != "get()" || key.Edges[0].Dst.StringIDA()[0] != "save()" {
		t.Fatalf("incorrect connection endpoints: %+v", key.Edges)
	}
	key = tree.Nodes[1].MapKey
	if key.EdgeIndex == nil || key.EdgeIndex.Int == nil || *key.EdgeIndex.Int != 0 ||
		key.EdgeKey == nil || key.EdgeKey.StringIDA()[0] != "call()" || key.Value.UnquotedString.ScalarString() != "ok" {
		t.Fatalf("incorrect indexed property: %+v", key)
	}
	key = tree.Nodes[2].MapKey
	if len(key.Edges) != 0 || key.Key == nil || key.Key.StringIDA()[0] != "(x)" {
		t.Fatalf("incorrect quoted key: %+v", key)
	}
}
