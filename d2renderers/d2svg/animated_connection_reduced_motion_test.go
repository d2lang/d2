package d2svg_test

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2animate"
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
		salt     *string
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
			name:     "salted_animated",
			script:   `a -> b: { style.animated: true }`,
			salt:     go2.Pointer("custom-salt"),
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

			renderOpts := &d2svg.RenderOpts{
				Salt: tc.salt,
			}
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

			diagramHash, err := diagram.HashID(renderOpts.Salt)
			assert.Success(t, err)

			out, err := d2svg.Render(diagram, renderOpts)
			assert.Success(t, err)

			var parsed any
			assert.Success(t, xml.Unmarshal(out, &parsed))

			svg := string(out)
			rootClass := fmt.Sprintf(`class="%s d2-svg"`, diagramHash)
			if !strings.Contains(svg, rootClass) {
				t.Fatalf("rendered SVG missing expected diagram hash class %q", rootClass)
			}

			reducedMotionRule := fmt.Sprintf(
				"@media (prefers-reduced-motion:reduce){.%s .animated-connection{animation:none !important}}",
				diagramHash,
			)
			const unscopedReducedMotionSelector = "{.animated-connection{"
			if tc.animated {
				if !strings.Contains(svg, "@keyframes dashdraw") {
					t.Fatal("animated connection missing @keyframes dashdraw")
				}
				if !strings.Contains(svg, "animation: dashdraw") {
					t.Fatal("animated connection missing inline dashdraw animation")
				}
				if !strings.Contains(svg, reducedMotionRule) {
					t.Fatalf("animated connection missing scoped reduced-motion override %q", reducedMotionRule)
				}
				if strings.Contains(svg, unscopedReducedMotionSelector) {
					t.Fatalf("animated connection unexpectedly emitted unscoped selector %q", unscopedReducedMotionSelector)
				}
				if got := strings.Count(svg, "prefers-reduced-motion"); got != 1 {
					t.Fatalf("prefers-reduced-motion count = %d, want 1", got)
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

func TestAnimatedConnectionReducedMotionMultiboard(t *testing.T) {
	t.Parallel()

	ruler, err := textmeasure.NewRuler()
	assert.Success(t, err)

	renderOpts := &d2svg.RenderOpts{}
	layoutResolver := func(string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	script := `a.style.animated: true
a -> b
steps: {
	1: {
		a -> b: { style.animated: true }
	}
}
`
	diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
	}, renderOpts)
	assert.Success(t, err)

	masterID, err := diagram.HashID(renderOpts.Salt)
	assert.Success(t, err)
	renderOpts.MasterID = masterID

	boards, err := d2svg.RenderMultiboard(diagram, renderOpts)
	assert.Success(t, err)

	out, err := d2animate.Wrap(diagram, boards, *renderOpts, 1000)
	assert.Success(t, err)

	var parsed any
	assert.Success(t, xml.Unmarshal(out, &parsed))

	svg := string(out)
	boardClass := fmt.Sprintf(`class="%s "`, masterID)
	if !strings.Contains(svg, boardClass) {
		t.Fatalf("multiboard SVG missing expected master hash class %q", boardClass)
	}
	reducedMotionRule := fmt.Sprintf(
		"@media (prefers-reduced-motion:reduce){.%s .animated-connection{animation:none !important}}",
		masterID,
	)
	if got := strings.Count(svg, reducedMotionRule); got != 1 {
		t.Fatalf("multiboard SVG emitted the scoped animated-connection reduced-motion override %d times, want 1", got)
	}
	for _, selector := range []string{
		"svg[data-d2-animation=" + masterID + "]>g{animation-play-state:paused!important}",
		"." + masterID + " .animated-shape{animation:none}",
	} {
		if !strings.Contains(svg, selector) {
			t.Errorf("missing reduced-motion rule %q", selector)
		}
	}
	const unscopedReducedMotionSelector = "{.animated-connection{"
	if strings.Contains(svg, unscopedReducedMotionSelector) {
		t.Fatalf("multiboard animated connection unexpectedly emitted unscoped selector %q", unscopedReducedMotionSelector)
	}
}
