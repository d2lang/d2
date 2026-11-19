package d2svg_test

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/go2"
)

func TestAnimatedConnectionReducedMotion(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		script   string
		sketch   bool
		animated bool
	}{
		{
			name:     "standard_unidirectional",
			script:   `a -> b: { style.animated: true }`,
			animated: true,
		},
		{
			name:     "standard_bidirectional",
			script:   `a <-> b: { style.animated: true }`,
			animated: true,
		},
		{
			name:     "sketch_animated",
			script:   `a -> b: { style.animated: true }`,
			sketch:   true,
			animated: true,
		},
		{
			name:     "static_connection",
			script:   `a -> b`,
			animated: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ruler, err := textmeasure.NewRuler()
			assert.Success(t, err)

			renderOpts := &d2svg.RenderOpts{}
			if tc.sketch {
				renderOpts.Sketch = go2.Pointer(true)
			}
			layoutResolver := func(string) (d2graph.LayoutGraph, error) {
				return d2dagrelayout.DefaultLayout, nil
			}
			diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), tc.script, &d2lib.CompileOptions{
				Ruler:          ruler,
				LayoutResolver: layoutResolver,
			}, renderOpts)
			assert.Success(t, err)

			out, err := d2svg.Render(diagram, renderOpts)
			assert.Success(t, err)

			var parsed any
			assert.Success(t, xml.Unmarshal(out, &parsed))

			svg := string(out)
			const reducedMotionRule = "@media (prefers-reduced-motion: reduce) {\n\t.animated-connection {\n\t\tanimation: none !important;\n\t}\n}"
			if tc.animated {
				if !strings.Contains(svg, "@keyframes dashdraw") {
					t.Fatal("animated connection missing @keyframes dashdraw")
				}
				if !strings.Contains(svg, "animation: dashdraw") {
					t.Fatal("animated connection missing inline dashdraw animation")
				}
				if !strings.Contains(svg, reducedMotionRule) {
					t.Fatalf("animated connection missing reduced-motion override %q", reducedMotionRule)
				}
			} else {
				if strings.Contains(svg, "@keyframes dashdraw") {
					t.Fatal("static connection unexpectedly emitted @keyframes dashdraw")
				}
				if strings.Contains(svg, "prefers-reduced-motion") {
					t.Fatal("static connection unexpectedly emitted prefers-reduced-motion rule")
				}
			}
		})
	}
}
