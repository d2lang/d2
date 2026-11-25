// Package grammartest checks parser and end-to-end test fixtures against the
// syntax reference. Its callers are tests in top-level D2 packages.
package grammartest

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/exp/ebnf"
	"golang.org/x/text/encoding/unicode"

	"github.com/d2lang/d2/d2parser"
)

// Load returns the shared, read-only grammar for this test binary.
func Load(t testing.TB) ebnf.Grammar {
	t.Helper()
	grammar, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return grammar
}

var load = sync.OnceValues(func() (ebnf.Grammar, error) {
	grammar := make(ebnf.Grammar)
	for _, filename := range []string{"structure.ebnf", "lexical.ebnf"} {
		path := filepath.Join("..", "docs", "grammar", filename)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rules, err := ebnf.Parse(path, bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		for name, rule := range rules {
			if other := grammar[name]; other != nil {
				return nil, fmt.Errorf("%s and %s both define %s", other.Pos(), rule.Pos(), name)
			}
			grammar[name] = rule
		}
	}
	return grammar, ebnf.Verify(grammar, "File")
})

// Check requires a valid fixture to pass both the Go parser and the written EBNF.
func Check(t testing.TB, grammar ebnf.Grammar, source string) {
	t.Helper()
	if _, err := d2parser.Parse(t.Name()+".d2", strings.NewReader(source), nil); err != nil {
		t.Fatalf("Go parser rejected fixture: %v", err)
	}
	CheckSyntax(t, grammar, source)
}

// CheckSyntax reports where the longest complete File prefix ends on a mismatch.
func CheckSyntax(t testing.TB, grammar ebnf.Grammar, source string) {
	t.Helper()
	source = decodeSource(t, source)
	end := longestFilePrefix(t, grammar, source)
	if end < 0 {
		t.Fatal("EBNF File did not match any prefix of the source")
	}
	if end < len(source) {
		t.Fatalf("EBNF File matched only %d of %d bytes; remaining input starts %q", end, len(source), source[end:min(end+80, len(source))])
	}
}

// The EBNF describes text after the parser's UTF-16LE BOM decoding step.
func decodeSource(t testing.TB, source string) string {
	t.Helper()
	if !strings.HasPrefix(source, "\xff\xfe") {
		return source
	}
	decoded, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().String(source)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
