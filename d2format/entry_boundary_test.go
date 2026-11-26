package d2format_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestArrayCommentBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, source, want string }{
		{"bare-item", `x: [one; """ note """two]`, `x: [one; """ note """; two]`},
		{"quoted-item", `x: ["one"; """ note """; two]`, `x: ["one"; """ note """; two]`},
		{"last-item", `x: [one; """ note """]`, `x: [one; """ note """]`},
		{"multiline", "x: [\n  one; \"\"\" note \"\"\"\n  two\n]", "x: [\n  one\n  \"\"\" note \"\"\"\n  two\n]"},
		{"line-comment", "x: [one # note\n two]", "x: [\n  one # note\n  two\n]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source
			for range 2 {
				m, err := d2parser.Parse("array.d2", strings.NewReader(source), nil)
				if err != nil {
					t.Fatal(err)
				}
				source = d2format.Format(m)
				if source != tc.want+"\n" {
					t.Fatalf("expected %q, got %q", tc.want+"\n", source)
				}
			}
		})
	}
}

func TestStarSourceEdgeGroup(t *testing.T) {
	for _, tc := range []struct{ source, want string }{
		{`("api" *- "db"): writes`, `("api" *- "db"): writes`},
		{`("api" *-> db -> queue)`, `("api" *-> db -> queue)`},
		{`(api -> "db" *- queue)`, `api -> "db" *- queue`},
	} {
		t.Run(tc.source, func(t *testing.T) {
			source := tc.source
			for range 2 {
				key, err := d2parser.ParseMapKey(source)
				if err != nil {
					t.Fatal(err)
				}
				source = d2format.Format(key)
				if source != tc.want {
					t.Fatalf("expected %q, got %q", tc.want, source)
				}
				for _, edge := range key.Edges {
					single := d2format.Format(edge)
					k, err := d2parser.ParseMapKey(single)
					if err != nil || len(k.Edges) != 1 || k.Edges[0].SrcArrow != edge.SrcArrow || k.Edges[0].DstArrow != edge.DstArrow {
						t.Fatalf("lost arrowheads formatting a single connection %q: %v", single, err)
					}
				}
			}
		})
	}
}
