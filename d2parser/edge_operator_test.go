package d2parser_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestSingleHyphenConnection(t *testing.T) {
	for _, input := range []string{
		`"build"-server`,
		`'build' - server`,
		`("build" - server)[0]`,
		`client -> "build" - server`,
		"\"build\" -\\\n  server",
	} {
		t.Run(input, func(t *testing.T) {
			_, err := d2parser.Parse("input.d2", strings.NewReader(input), nil)
			var parseErr *d2parser.ParseError
			if !errors.As(err, &parseErr) || len(parseErr.Errors) != 1 {
				t.Fatalf("expected one connection error, got %v", err)
			}
			diagnostic := parseErr.Errors[0]
			dash := strings.LastIndexByte(input, '-')
			if !strings.Contains(diagnostic.Message, "use --") || diagnostic.Range.Start.Byte != dash || diagnostic.Range.End.Byte != dash+1 {
				t.Fatalf("expected a correction at the single hyphen, got %+v", diagnostic)
			}
		})
	}
}

func TestHyphenatedKey(t *testing.T) {
	for _, input := range []string{"build-server", "build - server"} {
		key, err := d2parser.ParseMapKey(input)
		if err != nil {
			t.Fatal(err)
		}
		if key.Key == nil || len(key.Key.Path) != 1 || key.Key.StringIDA()[0] != input || len(key.Edges) != 0 {
			t.Errorf("%q should be one key, got %+v", input, key)
		}
	}
}

func TestConnectionShaft(t *testing.T) {
	for _, tc := range []struct {
		input, srcArrow, dstArrow string
	}{
		{"build--server", "", ""},
		{"build ------ server", "", ""},
		{"build -> server", "", ">"},
		{"build <- server", "<", ""},
		{"build <-> server", "<", ">"},
		{"build -* server", "", "*"},
		{`("build" *- server)[0]`, "*", ""},
		{"build --\\\n  -> server", "", ">"},
		{"\"build\" -\\\n  - server", "", ""},
		{"\"build\" -\\\n  > server", "", ">"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			key, err := d2parser.ParseMapKey(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if len(key.Edges) != 1 {
				t.Fatalf("expected one edge, got %+v", key)
			}
			edge := key.Edges[0]
			if edge.SrcArrow != tc.srcArrow || edge.DstArrow != tc.dstArrow ||
				edge.Src.StringIDA()[0] != "build" || edge.Dst.StringIDA()[0] != "server" {
				t.Fatalf("incorrect arrowheads or endpoints in %+v", edge)
			}
		})
	}
}
