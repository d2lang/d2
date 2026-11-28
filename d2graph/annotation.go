package d2graph

import (
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
)

// MText returns the note as measurable Markdown at the given size.
func (a *Annotation) MText(fontSize int) *d2target.MText {
	return &d2target.MText{
		Text:     a.Markdown(),
		FontSize: fontSize,
		Language: "markdown",
		Shape:    d2target.ShapeText,
	}
}

// setAnnotationDimensions measures every note and caption on the board.
// Annotations take no part in layout, so a note that cannot be measured is
// estimated rather than failing the diagram.
func (g *Graph) setAnnotationDimensions(mtexts []*d2target.MText, ruler *textmeasure.Ruler, fontFamily, monoFontFamily *d2fonts.FontFamily) {
	measure := func(a *Annotation, fontSize int) {
		if a == nil || a.Markdown() == "" {
			return
		}
		dims, err := getMarkdownDimensions(mtexts, ruler, a.MText(fontSize), fontFamily, monoFontFamily)
		if err != nil || dims == nil {
			lines, longest := 1, 0
			run := 0
			for _, r := range a.Markdown() {
				if r == '\n' {
					lines++
					run = 0
					continue
				}
				run++
				longest = max(longest, run)
			}
			dims = d2target.NewTextDimensions(longest*fontSize/2, lines*fontSize*3/2)
		}
		a.LabelDimensions = *dims
	}
	measure(g.Root.Annotation, d2target.CaptionFontSize)
	for _, obj := range g.Objects {
		measure(obj.Annotation, d2target.AnnotationFontSize)
	}
	for _, edge := range g.Edges {
		measure(edge.Annotation, d2target.AnnotationFontSize)
	}
}
