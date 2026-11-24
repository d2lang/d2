package textmeasure

import (
	"math"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
)

func TestPlainMarkdownStrikes(t *testing.T) {
	for _, tc := range []struct {
		plain, md string
		want      [][2]int
	}{
		{"Retired API", "~~Retired API~~", [][2]int{{0, 11}}},
		{"Retired API", "Retired API", [][2]int{}},
		{"A & B < C", "A ~~&amp; B~~ &lt; ~~C~~", [][2]int{{2, 5}, {8, 9}}},
		{"idle API", "~~idle~~ **API**", nil},
		{"idle API", "~~idle~~ [API](https://example.com)", nil},
		{"idle API", "# ~~idle API~~", nil},
		{"idle API", "~~idle API v2~~", nil},
		{"idle API", "~~idle~~\n\nAPI", nil},
		{"**API**", "~~\\*\\*API\\*\\*~~", [][2]int{{0, 7}}},
	} {
		if got := PlainMarkdownStrikes(tc.plain, tc.md); !slices.Equal(got, tc.want) || (got == nil) != (tc.want == nil) {
			t.Errorf("%q -> %q: got ranges %v, want %v", tc.plain, tc.md, got, tc.want)
		}
	}
}

func TestStrikeAdvances(t *testing.T) {
	r, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	font := d2fonts.SourceSansPro.Font(16, d2fonts.FONT_STYLE_BOLD)
	ranges := r.StrikeAdvances("iiiWWW", [][2]int{{3, 6}}, font)
	if len(ranges) != 1 || ranges[0][0] >= .5 || ranges[0][0] <= 0 || ranges[0][1] != 1 {
		t.Fatalf("expected wide Ws to occupy more than half the shaped line, got %v", ranges)
	}
	for _, invalid := range [][][2]int{{{-1, 1}}, {{5, 8}}, {{2, 3}, {1, 2}}, {{2, 2}}} {
		if got := r.StrikeAdvances("iiiWWW", invalid, font); len(got) != 0 {
			t.Errorf("invalid spans %v measured as %v", invalid, got)
		}
	}
	if got := r.StrikeAdvances("iiiWWW", [][2]int{{1, 2}}, d2fonts.Font{}); len(got) != 0 {
		t.Errorf("absent font produced advances %v", got)
	}
	if got := r.StrikeAdvances("é", [][2]int{{0, 1}}, font); len(got) != 0 {
		t.Errorf("partial UTF-8 character produced advances %v", got)
	}
}

func TestMarkdownStrikeChanged(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		changed  bool
	}{
		{"Keep old route", "Keep ~~old~~ route", true},
		{"~~old~~ route", "old route", true},
		{"**one** two", "~~**one**~~ two", true},
		{"<s>old</s>", "~~old~~", false},
		{"[old](https://a.example)", "[~~old~~](https://a.example)", true},
		{"[old](https://a.example)", "[~~old~~](https://b.example)", false},
		{"old route", "~~new~~ route", false},
		{"old route", "**~~old~~** route", false},
		{"`old`", "`~~old~~`", false},
		{"\\~old\\~", "~~old~~", false},
	} {
		if got := MarkdownStrikeChanged(tc.from, tc.to); got != tc.changed {
			t.Errorf("MarkdownStrikeChanged(%q, %q) = %t, want %t", tc.from, tc.to, got, tc.changed)
		}
	}
}

func TestMarkdownMixedStrikeFrame(t *testing.T) {
	ruler, err := NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	old := "~~Keep~~\n\n~~Remove~~\n\nAdd"
	next := "~~Keep~~\n\nRemove\n\n~~Add~~"
	widthByLine := func(progress float64) map[float64]float64 {
		t.Helper()
		frame, err := LayoutMarkdownStrikeFrame(old, next, progress, ruler, nil, nil, 16)
		if err != nil {
			t.Fatal(err)
		}
		widths := make(map[float64]float64)
		for _, p := range frame.Primitives {
			if p.Kind == MarkdownRectPrimitive && p.Decoration == MarkdownTextDecorationLineThrough {
				widths[p.Y] += p.Width
			}
		}
		return widths
	}
	before, half, after := widthByLine(0), widthByLine(.5), widthByLine(1)
	if len(before) != 2 || len(half) != 3 || len(after) != 2 {
		t.Fatalf("unexpected strike spans %v, %v, %v", before, half, after)
	}
	for y, w := range half {
		if want := (before[y] + after[y]) / 2; math.Abs(w-want) > 1e-7 {
			t.Errorf("strike width on baseline %g = %g, want %g", y, w, want)
		}
	}
}
