package d2parser_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestLiteralHyphenAtBoundary(t *testing.T) {
	for _, ending := range []string{"", "\n", ";", " # comment"} {
		m, err := d2parser.Parse("test.d2", strings.NewReader("build-"+ending), nil)
		if err != nil {
			t.Fatal(err)
		}
		k := m.Nodes[0].MapKey.Key
		if k.StringIDA()[0] != "build-" || k.Range.End.Byte != len("build-") {
			t.Errorf("wrong key or source range before %q: %+v", ending, k)
		}
	}
	k, err := d2parser.ParseMapKey("build-.job: ready")
	if err != nil {
		t.Fatal(err)
	}
	if got := k.Key.StringIDA(); len(got) != 2 || got[0] != "build-" || got[1] != "job" || k.Value.UnquotedString.ScalarString() != "ready" {
		t.Fatalf("expected a child named job and its label, got %+v", k)
	}
	k, err = d2parser.ParseMapKey("build-<->db")
	if err != nil {
		t.Fatal(err)
	}
	if len(k.Edges) != 1 || k.Edges[0].Src.StringIDA()[0] != "build-" || k.Edges[0].SrcArrow != "<" || k.Edges[0].DstArrow != ">" {
		t.Fatalf("wrong endpoints or arrowheads: %+v", k)
	}
}

func TestIncompleteSubstitutionAndImport(t *testing.T) {
	for _, tc := range []struct{ source, diagnostic string }{
		{"...$", "invalid text beginning unquoted key"},
		{"x: ${}", "substitution requires a variable name"},
		{"x: [${ }]", "substitution requires a variable name"},
		{"x: @", "import requires a path"},
		{"...@../", "import requires a path"},
	} {
		t.Run(tc.source, func(t *testing.T) {
			_, err := d2parser.Parse("test.d2", strings.NewReader(tc.source), nil)
			if err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Fatalf("expected %q, got %v", tc.diagnostic, err)
			}
		})
	}
	const escaped = `x: amount\$`
	m, err := d2parser.Parse("test.d2", strings.NewReader(escaped), nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Nodes[0].MapKey.Value.UnquotedString.ScalarString() != "amount$" || strings.TrimSpace(d2format.Format(m)) != escaped {
		t.Error("escaped literal dollar was lost")
	}
}
