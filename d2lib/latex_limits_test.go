package d2lib

import (
	"context"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2latex"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestCompileRejectsExcessiveLatexGroupNesting(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	formula := strings.Repeat("{", d2latex.MaxGroupNestingDepth+1) + "x" + strings.Repeat("}", d2latex.MaxGroupNestingDepth+1)
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "object label",
			input: "x: |latex\n  " + formula + "\n|",
		},
		{
			name:  "edge label",
			input: "x -> y: |latex\n  " + formula + "\n|",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := Compile(context.Background(), test.input, &CompileOptions{Ruler: ruler}, nil)
			want := "latex group nesting depth 129 exceeds limit 128"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Compile() error = %v, want %q", err, want)
			}
		})
	}
}
