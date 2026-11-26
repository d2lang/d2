package d2parser_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/d2lang/d2/d2parser"
)

func TestStandaloneParsersConsumeInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parse func(string) error
		valid []string
		// Separate the valid prefix from the first unexpected character.
		invalid [][2]string
	}{
		{"key", func(s string) error { _, err := d2parser.ParseKey(s); return err },
			[]string{" \"a.b\".key\t\r\n", `a\;b`, "two words"},
			[][2]string{{"a ", "-> b"}, {"a", ": b"}, {"a", "; b"}, {"a\n ", "b"}, {"\"svc\"", "(a -> b)"}, {"a ", "#comment"}}},
		{"map key", func(s string) error { _, err := d2parser.ParseMapKey(s); return err },
			[]string{"a: x", "(a -> b)[0].label: yep \n", "node: {a; b}\u2003"},
			[][2]string{{"a", "; b"}, {"(a -> b)[0]", "]"}, {"a\n ", "b"}}},
		{"value", func(s string) error { _, err := d2parser.ParseValue(s); return err },
			[]string{"[one; two]\n", "{one; two}\u2003", "a -> b", "12: 90", "one two"},
			[][2]string{{"\"one\"  ", "β"}, {"[one] ", "extra"}, {"12", "; 90"}, {"{one}\n ", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, s := range tc.valid {
				if err := tc.parse(s); err != nil {
					t.Errorf("valid %s %q: %v", tc.name, s, err)
				}
			}
			for _, parts := range tc.invalid {
				s := parts[0] + parts[1]
				err := tc.parse(s)
				var pe *d2parser.ParseError
				if !errors.As(err, &pe) || len(pe.Errors) != 1 {
					t.Errorf("%q: expected one trailing-input diagnostic, got %v", s, err)
					continue
				}
				pos := len(parts[0])
				_, size := utf8.DecodeRuneInString(parts[1])
				got := pe.Errors[0]
				if !strings.Contains(got.Message, "after "+tc.name) || got.Range.Start.Byte != pos || got.Range.End.Byte != pos+size {
					t.Errorf("%q: wrong diagnostic for first unexpected rune at byte %d: %+v", s, pos, got)
				}
			}
		})
	}
}

func TestLoneExclamationMark(t *testing.T) {
	for _, s := range []string{"!", "! ", "!\n", "previous; !"} {
		m, err := d2parser.Parse("test.d2", strings.NewReader(s), nil)
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		last := m.Nodes[len(m.Nodes)-1].MapKey
		if last == nil || last.Key.StringIDA()[0] != "!" {
			t.Errorf("%q: expected a key named !", s)
		}
	}
	if mk, err := d2parser.ParseMapKey("!"); err != nil || mk.Key.StringIDA()[0] != "!" {
		t.Errorf("ParseMapKey(!) = %v, %v", mk, err)
	}
}
