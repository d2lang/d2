package grammartest

import (
	"maps"
	"strings"
	"testing"
	"unicode/utf8"

	"golang.org/x/exp/ebnf"
)

// Recognizes reports whether the entire source matches the grammar's File rule.
func Recognizes(t testing.TB, grammar ebnf.Grammar, source string) bool {
	t.Helper()
	source = decodeSource(t, source)
	return longestFilePrefix(t, grammar, source) == len(source)
}

// longestFilePrefix returns the longest complete File prefix in UTF-8 bytes,
// or -1 if no prefix is accepted. This is a match boundary, not a diagnostic.
//
// Every input offset is a possible starting position for the expression.
// Matching a term against the whole frontier lets overlapping repetitions
// share work without changing which parses succeed.
func longestFilePrefix(t testing.TB, grammar ebnf.Grammar, source string) int {
	t.Helper()
	type offsets map[int]bool

	type call struct {
		name  string
		start int
	}
	memo := make(map[call]offsets)
	var match func(ebnf.Expression, offsets) offsets
	match = func(expr ebnf.Expression, starts offsets) offsets {
		switch e := expr.(type) {
		case nil:
			return starts
		case *ebnf.Name:
			result := offsets{}
			for start := range starts {
				key := call{e.String, start}
				ends, found := memo[key]
				if !found {
					memo[key] = nil
					ends = match(grammar[e.String].Expr, offsets{start: true})
					memo[key] = ends
				} else if ends == nil {
					t.Fatalf("%s: left recursion in %s at byte %d is unsupported by the test recognizer", e.Pos(), e.String, start)
				}
				maps.Copy(result, ends)
			}
			return result
		case *ebnf.Token:
			ends := offsets{}
			for start := range starts {
				if strings.HasPrefix(source[start:], e.String) {
					ends[start+len(e.String)] = true
				}
			}
			return ends
		case *ebnf.Range:
			ends := offsets{}
			low, _ := utf8.DecodeRuneInString(e.Begin.String)
			high, _ := utf8.DecodeRuneInString(e.End.String)
			for start := range starts {
				r, size := utf8.DecodeRuneInString(source[start:])
				if size > 0 && low <= r && r <= high {
					ends[start+size] = true
				}
			}
			return ends
		case *ebnf.Group:
			return match(e.Body, starts)
		case *ebnf.Option:
			ends := maps.Clone(starts)
			maps.Copy(ends, match(e.Body, starts))
			return ends
		case ebnf.Alternative:
			ends := offsets{}
			for _, alt := range e {
				maps.Copy(ends, match(alt, starts))
			}
			return ends
		case ebnf.Sequence:
			ends := starts
			for _, term := range e {
				ends = match(term, ends)
				if len(ends) == 0 {
					break
				}
			}
			return ends
		case *ebnf.Repetition:
			ends := maps.Clone(starts)
			for frontier := starts; len(frontier) > 0; {
				next := offsets{}
				for pos := range match(e.Body, frontier) {
					if !ends[pos] {
						next[pos] = true
						ends[pos] = true
					}
				}
				frontier = next
			}
			return ends
		default:
			t.Fatalf("unsupported EBNF expression %T", e)
			return nil
		}
	}
	last := -1
	for end := range match(grammar["File"].Name, offsets{0: true}) {
		last = max(last, end)
	}
	return last
}
