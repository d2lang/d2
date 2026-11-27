package d2parser_test

import (
	"slices"
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

func TestHashPrefixedValues(t *testing.T) {
	for _, literal := range []string{"#42", "#oncall", "#on-call team", "#工程", "#abcg", "#12345678", "#abcdef triage"} {
		v, err := d2parser.ParseValue(literal)
		if err != nil {
			t.Fatal(err)
		}
		s, ok := v.(*d2ast.UnquotedString)
		if !ok || s.ScalarString() != literal || d2format.Format(s) != literal || s.Range.End.Byte != len(literal) {
			t.Fatalf("expected an unquoted label %q, got %+v", literal, v)
		}
	}

	// The usual substitutions and escapes work within the label. Only its
	// initial hash is literal, even when the trailing comment touches it.
	src := "alert: #${team}\\#ops# note\n"
	m, err := d2parser.Parse("labels.d2", strings.NewReader(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	v := m.Nodes[0].MapKey.Value.UnquotedString
	if len(v.Value) != 3 || *v.Value[0].String != "#" || v.Value[1].Substitution == nil || *v.Value[2].String != "#ops" ||
		len(m.Nodes) != 2 || m.Nodes[1].Comment.Value != "note" {
		t.Fatalf("expected an interpolated label and a comment, got %+v", m.Nodes)
	}
	if got, want := d2format.Format(m), "alert: #${team}\\#ops # note\n"; got != want {
		t.Errorf("formatted hash label = %q, want %q", got, want)
	}
}

func TestFormatFoldedHashLabel(t *testing.T) {
	m, err := d2parser.Parse("label.d2", strings.NewReader("x: #\\\n # note\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := d2format.Format(m), "x: \\# # note\n"; got != want {
		t.Errorf("formatted lone hash = %q, want %q", got, want)
	}
}

func TestHashLabelEndingInEscapedWhitespace(t *testing.T) {
	for _, source := range []string{"#42\\ ", "#ticket\\\u2003", "#literal\\\\  "} {
		src := "x: " + source + "\ny: next\n"
		m, err := d2parser.Parse("label.d2", strings.NewReader(src), nil)
		if err != nil {
			t.Fatal(err)
		}
		formatted := d2format.Format(m)
		m2, err := d2parser.Parse("label.d2", strings.NewReader(formatted), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(m2.Nodes) != 2 || m2.Nodes[0].MapKey.Value.UnquotedString.ScalarString() != m.Nodes[0].MapKey.Value.UnquotedString.ScalarString() {
			t.Fatalf("formatting %q changed the diagram: %q", src, formatted)
		}
	}
}

func TestHashCommentAfterMissingValue(t *testing.T) {
	for _, literal := range []string{"#", "# comment", "#\tcomment", "#\u00a0comment", "##comment"} {
		if _, err := d2parser.ParseMapKey("x: " + literal); err == nil || !strings.Contains(err.Error(), "missing value after colon") {
			t.Errorf("%q: expected missing value diagnostic, got %v", literal, err)
		}
	}
}

func TestHashLabelSourceRange(t *testing.T) {
	for _, literal := range []string{`#${team}`, `#oncall\!`, `#📟${team}`, `#oncall\📟`, "#ticket\\ "} {
		for _, utf16 := range []bool{false, true} {
			const prefix = "alert: "
			m, err := d2parser.Parse("alerts.d2", strings.NewReader(prefix+literal+"  # note\n"), &d2parser.ParseOptions{UTF16Pos: utf16})
			if err != nil {
				t.Fatal(err)
			}
			got := m.Nodes[0].MapKey.Value.UnquotedString.Range
			start := d2ast.Position{}.AdvanceString(prefix, utf16)
			end := start.AdvanceString(literal, utf16)
			if got.Start != start || got.End != end {
				t.Errorf("range of %q (UTF16Pos=%t) = [%v, %v), want [%v, %v)", literal, utf16, got.Start, got.End, start, end)
			}
		}
	}
}

func TestHashLabelWithWildcardsAndVariable(t *testing.T) {
	const label = `#active *${team}x*`
	value, err := d2parser.ParseValue(label)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.(*d2ast.UnquotedString).Pattern, []string{"#active ", "*", "x", "*"}; !slices.Equal(got, want) {
		t.Errorf("wildcard pattern = %q, want %q", got, want)
	}
	if got := d2format.Format(value); got != label {
		t.Errorf("formatted label = %q, want %q", got, label)
	}
}
