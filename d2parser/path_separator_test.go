package d2parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestTrailingPathSeparator(t *testing.T) {
	for _, source := range []string{
		"a.", "a.  ", "a.\n", "a.;", "a. # comment", "a.: label", "a. {child}",
		"a. -> b", "a -> b.", "(a -> b.)", "(a -> b).", "(a -> b)[0].style.",
		"x: @theme.", "x: ${name.}", "...${vars.}", "...@file.", "x: [@item.]",
	} {
		t.Run(source, func(t *testing.T) {
			tree, err := d2parser.Parse("test.d2", strings.NewReader(source+"\nnext"), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
				t.Fatalf("expected one separator diagnostic, got %v", err)
			}
			got := parseErr.Errors[0]
			dot := strings.LastIndexByte(source, '.')
			if !strings.Contains(got.Message, "expected a key segment after .") || got.Range.Start.Byte != dot || got.Range.End.Byte != dot+1 {
				t.Fatalf("incorrect separator diagnostic at dot %d: %+v", dot, got)
			}
			last := tree.Nodes[len(tree.Nodes)-1].MapKey
			if last == nil || last.Key == nil || last.Key.StringIDA()[0] != "next" {
				t.Fatalf("lost the following entry: %+v", last)
			}
		})
	}
	for _, source := range []string{"a.", "a.b. ", `"a".`, "a..b"} {
		if _, err := d2parser.ParseKey(source); err == nil {
			t.Errorf("ParseKey(%q) accepted an empty segment", source)
		}
	}
	for _, source := range []string{"a.", "(a -> b)[0]."} {
		if _, err := d2parser.ParseMapKey(source); err == nil {
			t.Errorf("ParseMapKey(%q) accepted an empty segment", source)
		}
	}
	for _, source := range []string{"@theme.", "${name.}"} {
		if _, err := d2parser.ParseValue(source); err == nil {
			t.Errorf("ParseValue(%q) accepted an empty segment", source)
		}
	}
}

func TestScopedConnectionSeparator(t *testing.T) {
	for _, source := range []string{`"svc"(a -> b)[0]`, `'svc'  (a -> b)`, `dept."svc"(a -> b)`} {
		t.Run(source, func(t *testing.T) {
			tree, err := d2parser.Parse("test.d2", strings.NewReader(source+"; next"), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
				t.Fatalf("expected one missing-dot diagnostic, got %v", err)
			}
			got := parseErr.Errors[0]
			if !strings.Contains(got.Message, "expected . before scoped connection") || got.Range.Start.Byte != strings.IndexByte(source, '(') {
				t.Fatalf("incorrect scope diagnostic: %+v", got)
			}
			if len(tree.Nodes) != 2 || len(tree.Nodes[0].MapKey.Edges) != 1 || tree.Nodes[1].MapKey.Key.StringIDA()[0] != "next" {
				t.Fatal("lost the grouped connection or following entry during recovery")
			}
		})
	}
	for _, source := range []string{`"svc".(a -> b)[0].call()`, `'svc' . (a -> b)`, `dept.svc.(get() -> save())`} {
		key, err := d2parser.ParseMapKey(source)
		if err != nil || key.Key == nil || len(key.Edges) != 1 {
			t.Errorf("expected a scoped connection in %q, got %+v, %v", source, key, err)
		}
	}
	for _, source := range []string{`a\.`, `"a."`, `get()`, `foo(a)`} {
		if _, err := d2parser.ParseKey(source); err != nil {
			t.Errorf("valid literal key %q: %v", source, err)
		}
	}
}
