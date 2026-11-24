package appendix_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svg/appendix"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestAppendixFallbackFonts(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	opts := &d2svg.RenderOpts{}
	script := `api: API {tooltip: "Verify office traffic: Ж é 漢"}
api -> db`
	diagram, _, err := d2lib.Compile(context.Background(), script, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		},
	}, opts)
	if err != nil {
		t.Fatal(err)
	}
	svg, err := d2svg.Render(diagram, opts)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := appendix.AppendRendered(diagram, opts, ruler, svg)
	if err != nil {
		t.Fatal(err)
	}
	regular := []byte(`font-family: "font-regular"`)
	bold := []byte(`font-family: "font-bold"`)
	if !bytes.Contains(rendered, regular) || bytes.Contains(rendered, bold) {
		t.Fatal("expected only the full regular font for the tooltip")
	}

	// Consumers can pass SVG with their own styles, even ones referring to
	// the fallback family by name. Both global aliases remain available.
	custom := bytes.Replace(svg, []byte("</svg></svg>"), []byte(`<style>.x {font-family:font-bold}</style><text class="x">Custom</text></svg></svg>`), 1)
	preserved, err := appendix.AppendWithError(diagram, opts, ruler, custom)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(preserved, regular) || !bytes.Contains(preserved, bold) {
		t.Fatal("custom SVG lost a full font")
	}
}
