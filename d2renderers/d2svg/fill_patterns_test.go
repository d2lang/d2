package d2svg

import (
	"bytes"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestRenderFillPatternUsage(t *testing.T) {
	for _, frame := range []string{"", "d2-frame"} {
		for _, tc := range []struct {
			name, class, shape, root string
			want                     bool
		}{
			{name: "literal label"},
			{name: "partial class", class: "not-paper-overlay"},
			{name: "exact class", class: "another paper-overlay", want: true},
			{name: "shape pattern", shape: "paper", want: true},
			{name: "root pattern", root: "PAPER", want: true},
		} {
			t.Run(frame+"/"+tc.name, func(t *testing.T) {
				d := d2target.NewDiagram()
				s := d2target.Shape{ID: "a", Type: "rectangle", Width: 100, Height: 60, Opacity: 1, Fill: "B6", Text: d2target.Text{Label: "How to style paper-overlay"}}
				s.Classes, s.FillPattern = []string{tc.class}, tc.shape
				d.Shapes = append(d.Shapes, s)
				d.Root.FillPattern = tc.root
				out, err := Render(d, &RenderOpts{MasterID: frame})
				if err != nil {
					t.Fatal(err)
				}
				for _, fragment := range []string{`<pattern id="paper-`, `.paper-overlay{fill:url(#paper-`} {
					if got := bytes.Contains(out, []byte(fragment)); got != tc.want {
						t.Errorf("output contains %q = %v, want %v", fragment, got, tc.want)
					}
				}
			})
		}
	}
}

func TestFillPatternReferences(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`<text>url(#paper-d2-test) paper-overlay</text>`, false},
		{`<title>paper-overlay</title><!-- class="paper-overlay" -->`, false},
		{`<rect class="not-paper-overlay paper-overlay-alt"/>`, false},
		{`<rect class="other paper&#45;overlay"/>`, true},
		{`<rect fill="url(#paper-d2-test)"/>`, true},
		{`<rect fill="url(#paper-d2-test-other)"/>`, false},
		{`<pattern href="#paper-d2-test-other"/>`, false},
		{`<rect class="other&#160;paper-overlay"/>`, false},
		{`<foreignObject><g class="paper&#45;overlay"/></foreignObject>`, true},
		{`<rect style="fill: url( '#paper-d2-test' );"/>`, true},
		{`<pattern href="#paper-d2-test"/>`, true},
		{`<style>.highlight { fill: url(#paper-d2-test) }</style>`, true},
		{`<style>.highlight { fill: url(#pap&#101;r-d2-test) }</style>`, true},
		{`<g xmlns:custom="urn:custom" custom:class="paper-overlay"/>`, false},
		{`<script>shape.classList.add("paper-overlay")</script>`, true},
		{`<g onload="use('paper-overlay')"/>`, true},
	} {
		if got := usedFillPatterns(tc.body, "d2-test", nil)["paper"]; got != tc.want {
			t.Errorf("pattern usage in %s = %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestFillPatternCustomStylesheet(t *testing.T) {
	original := BaseStylesheet
	t.Cleanup(func() { BaseStylesheet = original })
	BaseStylesheet += `.custom { fill: url(#lines-d2-test) }`
	got := usedFillPatterns(`<text>paper-overlay</text>`, "d2-test", map[string]bool{"text": true})
	if !got["paper"] || !got["lines"] || got["grain"] {
		t.Fatalf("patterns for custom stylesheet: %v", got)
	}
}
