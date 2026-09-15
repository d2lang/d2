package textmeasure

import (
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/lib/geo"
)

func TestMeasureMarkdownMatchesPaintedDimensions(t *testing.T) {
	// LayoutMarkdown remains the painting path. In particular, its graph-sized
	// viewport uses the original HTML tree, before CSS whitespace normalization.
	cases := []struct {
		name     string
		markdown string
		wantErr  string
	}{
		{name: "empty", markdown: " \n\t\n"},
		{name: "styled whitespace", markdown: "left  **bold**\n*italic*  right&nbsp;&nbsp;end"},
		{name: "break before unicode newline", markdown: "first<br>\n日本語 👩🏼‍❤️‍👨🏼\n\nlast"},
		{name: "heading and inherited code styles", markdown: "# *heading* `日本語`\n\n**`bold code`** and *`italic code`*"},
		{name: "fenced tabs and blank lines", markdown: "```\na\tb\n\n日本語\n```\n\nafter"},
		{name: "nested and empty list items", markdown: "3. outer\n   - nested\n   -\n   - last\n\n> quote\n\n---"},
		{name: "table with empty cells", markdown: "| Left | Center |\n|---|:---:|\n| **bold** | `code` |\n| | |\n\nafter"},
		{name: "links and omitted image", markdown: "[a & b](https://example.com/?a=1&b=2) ![image](https://example.com/image.png)"},
		{name: "repaired raw html", markdown: "<p>first<br><strong>second"},
		{name: "unsupported block", markdown: "<video></video>", wantErr: "native Markdown SVG does not support HTML element <video>"},
		{name: "unsupported nested inline", markdown: "<p>first <span>second</span></p>", wantErr: "native Markdown SVG does not support HTML element <span>"},
	}
	proportional := d2fonts.HandDrawn
	mono := d2fonts.SourceCodePro
	fonts := []struct {
		name       string
		family     *d2fonts.FontFamily
		monoFamily *d2fonts.FontFamily
		size       int
	}{
		{name: "defaults", size: MarkdownFontSize},
		{name: "nondefault proportional", family: &proportional, monoFamily: &mono, size: 24},
		{name: "monospace base", family: &mono, monoFamily: &mono, size: 16},
	}
	for _, font := range fonts {
		t.Run(font.name, func(t *testing.T) {
			measureRuler, err := NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			paintRuler, err := NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			// Reuse both rulers across cases to cover populated font caches too.
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					width, height, measureErr := MeasureMarkdown(tc.markdown, measureRuler, font.family, font.monoFamily, font.size)
					layout, layoutErr := LayoutMarkdown(tc.markdown, paintRuler, font.family, font.monoFamily, font.size)
					if tc.wantErr != "" {
						if measureErr == nil || measureErr.Error() != tc.wantErr {
							t.Fatalf("MeasureMarkdown error = %v, want %q", measureErr, tc.wantErr)
						}
						if layoutErr == nil || layoutErr.Error() != tc.wantErr {
							t.Fatalf("LayoutMarkdown error = %v, want %q", layoutErr, tc.wantErr)
						}
						if width != 0 || height != 0 {
							t.Fatalf("failed measurement returned dimensions (%d, %d)", width, height)
						}
						return
					}
					if measureErr != nil || layoutErr != nil {
						t.Fatalf("unexpected errors: measurement=%v, layout=%v", measureErr, layoutErr)
					}
					if width != layout.Width || height != layout.Height {
						t.Fatalf("measurement=(%d, %d), painted viewport=(%d, %d)", width, height, layout.Width, layout.Height)
					}
				})
			}
		})
	}
}

func TestMeasureMarkdownPreservesRulerSettings(t *testing.T) {
	for _, boundsWithDot := range []bool{false, true} {
		for _, markdown := range []string{"# heading\n\n```\nfirst\n\nlast\n```", "", "<video></video>"} {
			ruler, err := NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			control, err := NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range []*Ruler{ruler, control} {
				r.Orig = geo.NewPoint(7.25, -3.5)
				r.LineHeightFactor = 2.25
				r.boundsWithDot = boundsWithDot
			}
			origin := ruler.Orig
			_, _, err = MeasureMarkdown(markdown, ruler, nil, nil, 16)
			if (err != nil) != (markdown == "<video></video>") {
				t.Fatalf("markdown %q: unexpected error %v", markdown, err)
			}
			if ruler.LineHeightFactor != 2.25 || ruler.boundsWithDot != boundsWithDot || ruler.Orig != origin || *ruler.Orig != *control.Orig {
				t.Fatalf("markdown %q changed ruler settings (boundsWithDot=%v)", markdown, boundsWithDot)
			}
			// A later ordinary measurement must behave as on an untouched ruler;
			// transient drawing state and lazy font caches need not be identical.
			spec := d2fonts.SourceSansPro.Font(16, d2fonts.FONT_STYLE_REGULAR)
			width, height := ruler.MeasurePrecise(spec, "after\nmarkdown ")
			wantWidth, wantHeight := control.MeasurePrecise(spec, "after\nmarkdown ")
			if width != wantWidth || height != wantHeight || *ruler.Dot != *control.Dot {
				t.Fatalf("markdown %q changed subsequent measurement: got (%v, %v, %v), want (%v, %v, %v)", markdown, width, height, ruler.Dot, wantWidth, wantHeight, control.Dot)
			}
		}
	}
}

func BenchmarkMeasureMarkdown(b *testing.B) {
	const markdown = "# Service dependencies\n\n" +
		"The **API** accepts requests and sends `job.created` events to the worker.\n\n" +
		"| Component | Responsibility | Status |\n" +
		"|---|---|---|\n" +
		"| API | Validate requests | **Ready** |\n" +
		"| Queue | Buffer `job.created` events | Durable |\n" +
		"| Worker | Process jobs and retry failures | *Running* |\n\n" +
		"- Retry transient failures\n- Record processing time\n\n" +
		"```\nPOST /jobs\n  -> queue\n  -> worker\n```\n\n" +
		"See the [service guide](https://example.com/guide) for details."
	ruler, err := NewRuler()
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _, err := MeasureMarkdown(markdown, ruler, nil, nil, MarkdownFontSize)
		if err != nil {
			b.Fatal(err)
		}
	}
}
