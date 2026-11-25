package d2parser_test

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/internal/grammartest"
)

func TestGrammarConformance(t *testing.T) {
	grammar := grammartest.Load(t)
	t.Run("edge-chain-production", func(t *testing.T) {
		// Start at EdgeChain so a match through File's keyPath alternative cannot mask an invalid arrow.
		edgeGrammar := maps.Clone(grammar)
		edgeGrammar["File"] = grammar["EdgeChain"]
		for _, tc := range []struct {
			source string
			want   bool
		}{
			{"build-server", false},
			{`"build" - server`, false},
			{"build--server", true},
			{"build->server", true},
		} {
			if got := grammartest.Recognizes(t, edgeGrammar, tc.source); got != tc.want {
				t.Errorf("EdgeChain recognition of %q = %t, want %t", tc.source, got, tc.want)
			}
		}
	})
	f, err := os.Open("../docs/grammar/testdata/conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var cases []struct {
		Name              string  `json:"name"`
		Source            *string `json:"source"`
		Nodes             []any   `json:"nodes"`
		Reject            bool    `json:"reject"`
		LexicalConstraint string  `json:"lexicalConstraint"`
	}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatal("unexpected data after the conformance cases")
	}
	if len(cases) == 0 {
		t.Fatal("empty conformance corpus")
	}
	names := map[string]bool{}
	for _, c := range cases {
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
