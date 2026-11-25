package d2parser_test

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/exp/ebnf"
)

func loadGrammar(t *testing.T) ebnf.Grammar {
	t.Helper()
	grammar := make(ebnf.Grammar)
	for _, name := range []string{"structure.ebnf", "lexical.ebnf"} {
		path := filepath.Join("..", "docs", "grammar", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rules, err := ebnf.Parse(path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		for name, rule := range rules {
			if other := grammar[name]; other != nil {
				t.Fatalf("%s and %s both define %s", other.Pos(), rule.Pos(), name)
			}
			grammar[name] = rule
		}
	}
	if err := ebnf.Verify(grammar, "File"); err != nil {
		t.Fatal(err)
	}
	return grammar
}

// Match the EBNF alone. Each expression returns every byte offset where its
// match could end, so overlapping alternatives need no arbitrary priority.
func grammarRecognizes(t *testing.T, grammar ebnf.Grammar, source string) bool {
	t.Helper()
	type offsets map[int]bool
	type call struct {
		name  string
		start int
	}
	memo := make(map[call]offsets)
	var match func(ebnf.Expression, int) offsets
	match = func(expr ebnf.Expression, start int) offsets {
		switch e := expr.(type) {
		case nil:
			return offsets{start: true}
		case *ebnf.Name:
			key := call{e.String, start}
			if ends, found := memo[key]; found {
				if ends == nil {
					t.Fatalf("%s: left recursion in %s at byte %d is unsupported by the test recognizer", e.Pos(), e.String, start)
				}
				return ends
			}
			memo[key] = nil // In progress at this offset.
			memo[key] = match(grammar[e.String].Expr, start)
			return memo[key]
		case *ebnf.Token:
			if strings.HasPrefix(source[start:], e.String) {
				return offsets{start + len(e.String): true}
			}
		case *ebnf.Range:
			r, size := utf8.DecodeRuneInString(source[start:])
			low, _ := utf8.DecodeRuneInString(e.Begin.String)
			high, _ := utf8.DecodeRuneInString(e.End.String)
			if size > 0 && low <= r && r <= high {
				return offsets{start + size: true}
			}
		case *ebnf.Group:
			return match(e.Body, start)
		case *ebnf.Option:
			ends := offsets{start: true}
			maps.Copy(ends, match(e.Body, start))
			return ends
		case ebnf.Alternative:
			ends := offsets{}
			for _, alt := range e {
				maps.Copy(ends, match(alt, start))
			}
			return ends
		case ebnf.Sequence:
			ends := offsets{start: true}
			for _, term := range e {
				next := offsets{}
				for pos := range ends {
					maps.Copy(next, match(term, pos))
				}
				ends = next
			}
			return ends
		case *ebnf.Repetition:
			ends := offsets{start: true}
			pending := []int{start}
			for i := 0; i < len(pending); i++ {
				for pos := range match(e.Body, pending[i]) {
					if !ends[pos] {
						ends[pos] = true
						pending = append(pending, pos)
					}
				}
			}
			return ends
		default:
			t.Fatalf("unsupported EBNF expression %T", e)
		}
		return offsets{}
	}
	return match(grammar["File"].Name, 0)[len(source)]
}
