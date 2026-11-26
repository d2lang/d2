package d2parser_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
)

func TestHexColorValues(t *testing.T) {
	for _, literal := range []string{"#000", "#AbC", "#12AbF0"} {
		v, err := d2parser.ParseValue(literal)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := v.(*d2ast.UnquotedString)
		if !ok || s.ScalarString() != literal || d2format.Format(s) != literal || s.Range.End.Byte != len(literal) {
			t.Fatalf("expected unquoted %q and its full source range, got %+v", literal, v)
		}
	}
	for _, source := range []string{`\#abc`, `"#abc"`, `'#abc'`} {
		v, err := d2parser.ParseValue(source)
		if err != nil || v.(d2ast.String).ScalarString() != "#abc" || d2format.Format(v) != source {
			t.Fatalf("changed spelling or meaning of %q: %v, %v", source, v, err)
		}
	}

	source := "swatch: #Aa23fF {style.fill: #Aa23fF; style.stroke: #abc} #abc\nlist: [\n  #abc\n  item #abc\n]\n"
	for range 2 {
		m, err := d2parser.Parse("colors.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		swatch := m.Nodes[0].MapKey
		if swatch.Primary.UnquotedString.ScalarString() != "#Aa23fF" ||
			swatch.Value.Map.Nodes[0].MapKey.Value.UnquotedString.ScalarString() != "#Aa23fF" ||
			swatch.Value.Map.Nodes[1].MapKey.Value.UnquotedString.ScalarString() != "#abc" || m.Nodes[1].Comment.Value != "abc" {
			t.Fatalf("unexpected colors or trailing comment: %+v", m.Nodes)
		}
		items := m.Nodes[2].MapKey.Value.Array.Nodes
		if len(items) != 3 || items[0].Comment.Value != "abc" || items[1].UnquotedString.ScalarString() != "item" || items[2].Comment.Value != "abc" {
			t.Fatalf("unexpected array entries: %+v", items)
		}
		source = d2format.Format(m)
	}
}

func TestInvalidHexColorValues(t *testing.T) {
	for _, literal := range []string{"#", "# comment", "#abcg", "#12", "#1234", "#12345", "#1234567", "#12345678", "#abcdefg"} {
		if _, err := d2parser.ParseMapKey("color: " + literal); err == nil {
			t.Errorf("expected an error for %q", literal)
		}
	}
}
