package appendix_test

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svg/appendix"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

// mixedLinkTooltipScript mixes link icons and tooltip icons, and a scenario
// replaces one link with a link to its parent board (`_`). That link has no
// board label to pretty-print, which used to leave a numbered marker without
// an appendix entry and shift every later tooltip number.
const mixedLinkTooltipScript = `views: {
  label: Views
  shape: page
  style.multiple: true
  one: One {link: scenarios.one; shape: step}
  two: Two {link: scenarios.two; shape: step}
  three: Three {link: scenarios.three; shape: step}
}
c: {label: C; tooltip: "tooltip for C"}
d: {label: D; tooltip: "tooltip for D"}
c -- d
scenarios: {
  one: {
    views.one.link: _
  }
}
`

var (
	appendixIconNumberPattern = regexp.MustCompile(`class="appendix-icon">(?:<title>[^<]*</title>)?<circle[^>]*/><text[^>]*>(\d+)</text>`)
	appendixRowTextPattern    = regexp.MustCompile(`<text class="text" [^>]*>(.*?)</text>`)
)

func TestAppendixMixedLinkTooltipNumbering(t *testing.T) {
	t.Parallel()

	ctx := log.Leveled(log.With(context.Background(), testlog.New(t)), slog.LevelDebug)
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	diagram, _, err := d2lib.Compile(ctx, mixedLinkTooltipScript, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Scenarios) != 1 {
		t.Fatalf("scenarios = %d, want 1", len(diagram.Scenarios))
	}

	// The CLI rewrites board links to relative output paths before rendering,
	// so also cover a parent-board link that has a Link but no PrettyLink.
	relinked := *diagram.Scenarios[0]
	relinked.Shapes = append([]d2target.Shape(nil), relinked.Shapes...)
	for i := range relinked.Shapes {
		if relinked.Shapes[i].Link != "" {
			relinked.Shapes[i].Link = "index.svg"
		}
	}

	for _, tc := range []struct {
		name     string
		diagram  *d2target.Diagram
		wantRows []string
	}{
		{"root", diagram, []string{"one", "tooltip for C", "tooltip for D"}},
		{"scenarios.one", diagram.Scenarios[0], []string{"root", "tooltip for C", "tooltip for D"}},
		{"scenarios.one relinked", &relinked, []string{"index.svg", "tooltip for C", "tooltip for D"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svg, err := d2svg.Render(tc.diagram, &d2svg.RenderOpts{})
			if err != nil {
				t.Fatal(err)
			}
			out := string(appendix.Append(tc.diagram, nil, ruler, svg))
			markers, rows, rowTexts := splitAppendixNumbers(t, out)
			assertOneSequence(t, markers, rows)
			if strings.Join(rowTexts, "|") != strings.Join(tc.wantRows, "|") {
				t.Fatalf("appendix rows = %q, want %q", rowTexts, tc.wantRows)
			}
		})
	}
}

// splitAppendixNumbers returns the numbers on the diagram's markers, the
// numbers of the appendix rows, and the text of each appendix row.
func splitAppendixNumbers(t *testing.T, svg string) (markers, rows []int, rowTexts []string) {
	t.Helper()
	appendixStart := strings.Index(svg, `<g class="appendix"`)
	if appendixStart < 0 {
		t.Fatalf("no appendix in output")
	}
	parse := func(s string) []int {
		var numbers []int
		for _, match := range appendixIconNumberPattern.FindAllStringSubmatch(s, -1) {
			n, err := strconv.Atoi(match[1])
			if err != nil {
				t.Fatal(err)
			}
			numbers = append(numbers, n)
		}
		return numbers
	}
	for _, match := range appendixRowTextPattern.FindAllStringSubmatch(svg[appendixStart:], -1) {
		rowTexts = append(rowTexts, match[1])
	}
	return parse(svg[:appendixStart]), parse(svg[appendixStart:]), rowTexts
}

// assertOneSequence asserts that markers and appendix rows use a single
// sequence 1..n with no gaps or duplicates, and that every marker has exactly
// one appendix row.
func assertOneSequence(t *testing.T, markers, rows []int) {
	t.Helper()
	for i, n := range rows {
		if n != i+1 {
			t.Fatalf("appendix row numbers = %v, want 1..%d in order", rows, len(rows))
		}
	}
	seen := map[int]bool{}
	for _, n := range markers {
		if seen[n] {
			t.Fatalf("diagram marker numbers = %v, number %d is duplicated", markers, n)
		}
		seen[n] = true
		if n < 1 || n > len(rows) {
			t.Fatalf("diagram marker %d has no appendix row (markers %v, rows %v)", n, markers, rows)
		}
	}
	if len(markers) != len(rows) {
		t.Fatalf("diagram markers %v and appendix rows %v differ in count", markers, rows)
	}
}
