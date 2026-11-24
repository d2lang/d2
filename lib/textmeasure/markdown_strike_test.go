package textmeasure

import (
	"math"
	"testing"
)

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
