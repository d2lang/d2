package d2animate

import (
	"bytes"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestAnimationFontCoverage(t *testing.T) {
	for _, tc := range []struct {
		name, extraMarkup, extraCorpus string
		whole                          bool
	}{
		{name: "entire playback"},
		{name: "referenced text", extraMarkup: `<use href="#label"/>`, whole: true},
		{name: "foreign object", extraMarkup: `<foreignObject/>`, whole: true},
		{name: "shaping and browser fallback", extraCorpus: "À café e\u0301 · Ελληνικά · עברית (42) · 漢字 🧑‍💻", whole: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diagram := d2target.NewDiagram()
			diagram.Shapes = []d2target.Shape{{Text: d2target.Text{Label: "cache"}}}
			next := d2target.NewDiagram()
			connection := d2target.BaseConnection()
			connection.Route = []*geo.Point{{}, {X: 1, Y: 1}}
			connection.Label = "hit"
			connection.LabelPosition = "UNLOCKED_MIDDLE"
			connection.DstLabel = &d2target.Text{Label: "9" + tc.extraCorpus}
			next.Connections = []d2target.Connection{*connection}
			diagram.Steps = []*d2target.Diagram{next}
			frames := [][]byte{
				[]byte(`<g><text id="label" class="text">cache</text></g>`),
				[]byte(`<g><text class="text">hit</text><text class="text-mono-bold">9` + tc.extraCorpus + `</text>` + tc.extraMarkup + `</g>`),
			}
			out, err := WrapTimed(diagram, frames, d2svg.RenderOpts{}, []int{100, 25})
			if err != nil {
				t.Fatal(err)
			}
			regular, bold := "cachehit", "9"
			faces := 2
			if tc.whole {
				regular = diagram.GetNestedCorpus()
				bold = regular
				// Whole-corpus fallback preserves the original substring triggers,
				// including the ordinary mono face selected by text-mono-bold.
				faces = 3
			}
			for _, uri := range []string{
				d2fonts.SourceSansPro.Font(0, d2fonts.FONT_STYLE_REGULAR).GetSVGEncodedSubset(regular),
				d2fonts.SourceCodePro.Font(0, d2fonts.FONT_STYLE_BOLD).GetSVGEncodedSubset(bold),
			} {
				if !bytes.Contains(out, []byte(uri)) {
					t.Fatal("missing expected font subset")
				}
			}
			if got := bytes.Count(out, []byte("@font-face")); got != faces {
				t.Fatalf("embedded %d faces, want %d", got, faces)
			}
		})
	}
}
