package d2parser_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/internal/grammartest"
)

func TestGrammarConformance(t *testing.T) {
	grammar := grammartest.Load(t)
	t.Run("edge-productions", func(t *testing.T) {
		// Check each production directly so an alternative through File cannot mask a mismatch.
		for _, tc := range []struct {
			rule, source string
			want         bool
		}{
			{"EdgeChain", "build-server", false},
			{"EdgeChain", `"build" - server`, false},
			{"EdgeChain", "build--server", true},
			{"EdgeChain", "build->server", true},
			{"EdgeGroup", "(a -> b)[ 12 ]", true},
			{"EdgeGroup", "(a -> b", false},
			{"EdgeGroup", "(a)", false},
			{"EdgeGroup", "(a -> b)[1 2]", false},
			{"EdgeGroup", "(a -> b)[٣]", false},
		} {
			fromRule := maps.Clone(grammar)
			fromRule["File"] = grammar[tc.rule]
			if got := grammartest.Recognizes(t, fromRule, tc.source); got != tc.want {
				t.Errorf("%s recognition of %q = %t, want %t", tc.rule, tc.source, got, tc.want)
			}
		}
	})
	names := map[string]bool{}
	for _, c := range readConformanceCases(t) {
		if c.Name == "" || names[c.Name] || c.Source == nil || (c.Nodes != nil) == c.Reject {
			t.Fatalf("invalid conformance case %q: require a unique name, source, and either nodes or reject", c.Name)
		}
		names[c.Name] = true
		t.Run(c.Name, func(t *testing.T) {
			if c.LexicalConstraint != "" && (!c.Reject || grammar[c.LexicalConstraint] == nil) {
				t.Fatalf("lexicalConstraint %q must name a production for a rejected case", c.LexicalConstraint)
			}
			wantRecognition := !c.Reject || c.LexicalConstraint != ""
			if got := grammartest.Recognizes(t, grammar, *c.Source); got != wantRecognition {
				t.Errorf("EBNF File recognition = %t, want %t", got, wantRecognition)
			}
			tree, err := d2parser.Parse(c.Name+".d2", strings.NewReader(*c.Source), nil)
			if c.Reject {
				if err == nil {
					t.Error("expected a syntax error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(tree.Nodes)
			if err != nil {
				t.Fatal(err)
			}
			var got []any
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			checkGrammarFields(t, "nodes", c.Nodes, got)
		})
	}
}

type conformanceCase struct {
	Name              string  `json:"name"`
	Source            *string `json:"source"`
	Nodes             []any   `json:"nodes"`
	Reject            bool    `json:"reject"`
	LexicalConstraint string  `json:"lexicalConstraint"`
}

func readConformanceCases(t *testing.T) []conformanceCase {
	t.Helper()
	files, err := filepath.Glob("../docs/grammar/testdata/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("could not find grammar conformance files: %v", err)
	}
	var cases []conformanceCase
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var group []conformanceCase
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&group); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if _, err := dec.Token(); err != io.EOF || len(group) == 0 {
			t.Fatalf("%s: expected one nonempty array of conformance cases", file)
		}
		cases = append(cases, group...)
	}
	return cases
}

// Fixtures select the object fields that discriminate a particular syntax form.
// An array expectation always checks all elements, in order.
func checkGrammarFields(t *testing.T, path string, want, got any) {
	t.Helper()
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("%s: expected an object, got %v", path, got)
		}
		for key, value := range w {
			field, ok := g[key]
			if !ok {
				t.Fatalf("%s: missing field %q", path, key)
			}
			checkGrammarFields(t, path+"."+key, value, field)
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(w) != len(g) {
			t.Fatalf("%s: expected an array of length %d, got %v", path, len(w), got)
		}
		for i, value := range w {
			checkGrammarFields(t, fmt.Sprintf("%s[%d]", path, i), value, g[i])
		}
	default:
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("%s: expected %v, got %v", path, want, got)
		}
	}
}
