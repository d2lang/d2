package d2parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestEmptyFilter(t *testing.T) {
	for _, marker := range []string{"&", "!&"} {
		for _, tc := range []struct{ name, before, after string }{
			{"eof", "api; ", ""},
			{"semicolon", "*: {", "; label: selected}"},
			{"comment", "*: {\n  ", " # missing key\n  label: selected\n}"},
			{"newline", "", "\napi"},
			{"closing-brace", "box: {", " }"},
		} {
			t.Run(marker+"/"+tc.name, func(t *testing.T) {
				source := tc.before + marker + tc.after
				_, err := d2parser.Parse("test.d2", strings.NewReader(source), nil)
				var parseErr *d2parser.ParseError
				if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
					t.Fatalf("expected one diagnostic, got %v", err)
				}
				e := parseErr.Errors[0]
				if !strings.HasSuffix(e.Message, ": filter requires a key or connection") ||
					e.Range.Start.Byte != len(tc.before) || e.Range.End.Byte != len(tc.before)+len(marker) {
					t.Fatalf("expected diagnostic on %q, got %+v", marker, e)
				}
			})
		}
	}
}

func TestFilterKeyOrConnection(t *testing.T) {
	for _, marker := range []string{"&", "!&"} {
		for _, operand := range []string{"label: selected", "(worker -> db)[*]"} {
			k, err := d2parser.ParseMapKey(marker + " " + operand)
			if err != nil {
				t.Fatal(err)
			}
			if k.Ampersand != (marker == "&") || k.NotAmpersand != (marker == "!&") {
				t.Fatalf("lost filter marker %q for %q", marker, operand)
			}
			if operand[0] == '(' {
				if len(k.Edges) != 1 || k.EdgeIndex == nil || !k.EdgeIndex.Glob {
					t.Fatalf("lost filtered connection: %+v", k)
				}
			} else if k.Key.StringIDA()[0] != "label" {
				t.Fatalf("lost filter key: %+v", k)
			}
		}
	}
	for _, source := range []string{`"&": R&D`, `\&: R&D`} {
		k, err := d2parser.ParseMapKey(source)
		if err != nil {
			t.Fatal(err)
		}
		if k.Ampersand || k.NotAmpersand || k.Key.StringIDA()[0] != "&" || k.Value.UnquotedString.ScalarString() != "R&D" {
			t.Fatalf("expected literal ampersands in %q, got %+v", source, k)
		}
	}
}
