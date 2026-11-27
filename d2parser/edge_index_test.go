package d2parser_test

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestEdgeIndex(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   int
		glob   bool
	}{
		{"0", 0, false},
		{" \t0012\u00a0", 12, false},
		{strconv.Itoa(math.MaxInt), math.MaxInt, false},
		{" \t* ", 0, true},
	} {
		t.Run(tc.source, func(t *testing.T) {
			key, err := d2parser.ParseMapKey("(a -> b)[" + tc.source + "].label")
			if err != nil {
				t.Fatal(err)
			}
			index := key.EdgeIndex
			if index == nil || index.Glob != tc.glob || (index.Int == nil) != tc.glob {
				t.Fatalf("unexpected edge index: %+v", index)
			}
			if index.Int != nil && *index.Int != tc.want {
				t.Fatalf("got edge index %d, want %d", *index.Int, tc.want)
			}
		})
	}
}

func TestInvalidEdgeIndex(t *testing.T) {
	overflow := strconv.FormatUint(uint64(math.MaxInt)+1, 10)
	for _, token := range []string{"", "1 2", "1\t2", "١", "1２", "+0", "-0", "1x2", "* 1", "1 *", overflow} {
		t.Run(token, func(t *testing.T) {
			source := "(a -> b)[" + token + "].label: ok; next"
			tree, err := d2parser.Parse("input.d2", strings.NewReader(source), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
				t.Fatalf("expected one index diagnostic, got %v", err)
			}
			got := parseErr.Errors[0]
			message := "edge index must be a nonnegative decimal integer or *"
			if token == overflow {
				message = "edge index is too large"
			}
			if !strings.Contains(got.Message, message) || got.Range.Start.Byte != strings.IndexByte(source, '[') || got.Range.End.Byte != strings.IndexByte(source, ']')+1 {
				t.Fatalf("incorrect index diagnostic: %+v", got)
			}
			if len(tree.Nodes) != 2 || tree.Nodes[1].MapKey.Key.StringIDA()[0] != "next" {
				t.Fatal("did not recover at the next entry")
			}
			key := tree.Nodes[0].MapKey
			if key.EdgeIndex != nil || key.EdgeKey == nil || key.EdgeKey.StringIDA()[0] != "label" {
				t.Fatalf("invalid index or missing property in partial tree: %+v", key)
			}
		})
	}
}

func TestUnterminatedEdgeIndex(t *testing.T) {
	for _, suffix := range []string{"", "  ", "12", "*", "0\nnext", "1; next"} {
		t.Run(suffix, func(t *testing.T) {
			source := "(a -> b)[" + suffix
			tree, err := d2parser.Parse("input.d2", strings.NewReader(source), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 || !strings.Contains(err.Error(), "unterminated edge index") {
				t.Fatalf("expected an unterminated index diagnostic, got %v", err)
			}
			if tree.Nodes[0].MapKey.EdgeIndex != nil {
				t.Fatal("incomplete selector retained in partial tree")
			}
			if strings.HasSuffix(suffix, "next") && (len(tree.Nodes) != 2 || tree.Nodes[1].MapKey.Key.StringIDA()[0] != "next") {
				t.Fatal("did not recover at the next entry")
			}
		})
	}
}
