package d2svg

import (
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2latex"
	"github.com/d2lang/d2/d2renderers/d2svgimport"
	"github.com/d2lang/d2/lib/geo"
)

func TestCompactGlyphPath(t *testing.T) {
	for _, path := range []string{
		"M500 500L501 499L490 480L479 470Z",
		"M400 400Q403 405 411 420T410 411T400 405",
		"M-10 -3H-11H-12V-4V-5M510 500L511 503ZM512 501H513V502Z",
		"M100 100M101 101M102 102Z",
		"M1048576 -1048576L-1048576 1048576Z",
	} {
		got := compactGlyphPath(path)
		if len(got) >= len(path) {
			t.Fatalf("did not shorten %q: %s", path, got)
		}
		limits := d2svgimport.PathLimits{MaxBytes: 1000, MaxCommands: 100}
		want, err := d2svgimport.ParsePath(t.Context(), "glyph", path, limits)
		if err != nil {
			t.Fatal(err)
		}
		cmds, err := d2svgimport.ParsePath(t.Context(), "glyph", got, limits)
		if err != nil || !reflect.DeepEqual(cmds, want) {
			t.Fatalf("geometry changed: %q → %q (%v)", path, got, err)
		}
	}
	for _, path := range []string{
		"", "M0", "M1 2L3", "M4 5L6 7 8 9", "M1 2q3 4 5 6", "M3.5 4",
		"M0 0C1 2 3 4 5 6", "M1 2L+10 -10", "M1e2 20", "M0 0L1048577 4",
	} {
		if got := compactGlyphPath(path); got != path {
			t.Errorf("unrecognized path %q changed to %q", path, got)
		}
	}
}

func TestCompactMathJaxGlyphs(t *testing.T) {
	d := "M100 200L101 201Z"
	variant := "M100 200H101Z"
	other := `<path d="` + d + `"/><path data-c="1" fill="red" d="` + d + `"/><text>` + d + `</text>`
	large := "M" + strings.Repeat("0", 16<<10) + " 0"
	input := func(a, b string) string {
		return other + `<path data-c="1" d="` + a + `"/><path data-c="1" d="` + b + `" transform="scale(2)"/> <path data-c="2" d="` + a + `"/><path data-c="3" d="` + large + `"/>`
	}
	if got, want := compactMathJaxGlyphs(input(d, variant)), input("M100 200l1 1Z", "M100 200h1Z"); got != want {
		t.Fatal("expected only d attributes on matching glyphs to change")
	}
}

func TestLatexLabelCustomStylesheet(t *testing.T) {
	const formula = "a+a"
	original := BaseStylesheet
	t.Cleanup(func() { BaseStylesheet = original })
	raw, err := d2latex.Render(formula)
	if err != nil {
		t.Fatal(err)
	}
	_, glyphs, ok := strings.Cut(raw, `<path data-c=`)
	if !ok {
		t.Fatal("expected MathJax glyph")
	}
	firstGlyph, _, _ := strings.Cut(glyphs, ">")
	BaseStylesheet += `path[d^='M'] { fill: red }`
	g, err := latexLabel(formula, &geo.Point{}, "", nil)
	if err != nil || !strings.Contains(g.Render(), firstGlyph) {
		t.Fatalf("custom stylesheet requires original glyph attributes: %v", err)
	}
}
