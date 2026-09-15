package d2svg_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestRenderReusesSuppliedRuler(t *testing.T) {
	regular := d2fonts.FontFaces.Get(d2fonts.HandDrawn.Font(0, d2fonts.FONT_STYLE_REGULAR))
	italic := d2fonts.FontFaces.Get(d2fonts.HandDrawn.Font(0, d2fonts.FONT_STYLE_ITALIC))
	bold := d2fonts.FontFaces.Get(d2fonts.HandDrawn.Font(0, d2fonts.FONT_STYLE_BOLD))
	semibold := d2fonts.FontFaces.Get(d2fonts.HandDrawn.Font(0, d2fonts.FONT_STYLE_SEMIBOLD))
	family, err := d2fonts.AddFontFamily("svg-ruler-reuse", regular, italic, bold, semibold)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"markdown", "shapes", "connections", "mixed", "all blank", "tooltip"} {
		d := measurementDiagram(kind)
		d.FontFamily = family
		want, err := d2svg.Render(d, nil)
		if err != nil {
			t.Fatal(err)
		}
		for repeat := 0; repeat < 2; repeat++ {
			// Warm the caller-owned font cache and observe the public drawing state:
			// the renderer must actually use this ruler, not merely accept it.
			ruler.Measure(family.Font(16, d2fonts.FONT_STYLE_REGULAR), "before rendering")
			dot := ruler.Dot
			opts := &d2svg.RenderOpts{Ruler: ruler}
			got, err := d2svg.Render(d, opts)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("%s repeat %d: supplied ruler changed SVG: %v", kind, repeat, err)
			}
			if ruler.Dot == dot || opts.Ruler != ruler || ruler.LineHeightFactor != 1 {
				t.Fatalf("%s: caller ruler was not reused or its configuration changed", kind)
			}
		}
	}
}

func TestRenderRulerIsExcludedFromJSON(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	pad := int64(12)
	opts := d2svg.RenderOpts{Pad: &pad}
	want, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Ruler = ruler
	got, err := json.Marshal(opts)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("ruler changed serialized options: %s (%v)", got, err)
	}
	var decoded d2svg.RenderOpts
	if err := json.Unmarshal([]byte(`{"Ruler":{},"Pad":12}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Ruler != nil || decoded.Pad == nil || *decoded.Pad != pad {
		t.Fatalf("JSON supplied mutable measurement state: %+v", decoded)
	}
}

func TestRenderMeasurementIsolation(t *testing.T) {
	d := measurementDiagram("mixed")
	shared := &d2svg.RenderOpts{}
	for _, mode := range []string{"nil options", "shared options", "separate rulers"} {
		t.Run(mode, func(t *testing.T) {
			want, err := d2svg.Render(d, nil)
			if err != nil {
				t.Fatal(err)
			}
			const workers = 4
			results := make(chan error, workers)
			for i := 0; i < workers; i++ {
				var opts *d2svg.RenderOpts
				if mode == "shared options" {
					opts = shared
				} else if mode == "separate rulers" {
					ruler, err := textmeasure.NewRuler()
					if err != nil {
						t.Fatal(err)
					}
					opts = &d2svg.RenderOpts{Ruler: ruler}
				}
				go func() {
					for repeat := 0; repeat < 2; repeat++ {
						got, err := d2svg.Render(d, opts)
						if err != nil || !bytes.Equal(got, want) {
							results <- fmt.Errorf("render changed: %v", err)
							return
						}
					}
					results <- nil
				}()
			}
			for i := 0; i < workers; i++ {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			if shared.Ruler != nil {
				t.Fatal("render installed mutable state in shared options")
			}
		})
	}
}

func TestRenderMultiboardMeasurementIsolation(t *testing.T) {
	root := measurementDiagram("mixed")
	first, second := measurementDiagram("markdown"), measurementDiagram("tooltip")
	first.Shapes[0].Label = "**first Ω**"
	second.Shapes[0].Label = "second Ж `code`"
	mono := d2fonts.SourceCodePro
	first.FontFamily = &mono
	root.Layers = []*d2target.Diagram{first}
	root.Steps = []*d2target.Diagram{second}
	for _, masterID := range []string{"", "animation-root"} {
		base := &d2svg.RenderOpts{MasterID: masterID}
		var want [][]byte
		for _, d := range []*d2target.Diagram{root, first, second} {
			out, err := d2svg.Render(d, base)
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, out)
		}
		for _, supplied := range []bool{false, true} {
			opts := *base
			if supplied {
				var err error
				opts.Ruler, err = textmeasure.NewRuler()
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := d2svg.RenderMultiboard(root, &opts)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("master=%q supplied=%v: board order or SVG changed: %v", masterID, supplied, err)
			}
			root.Shapes[0].Link = "javascript:alert(1)"
			got, err = d2svg.RenderMultiboard(root, &opts)
			root.Shapes[0].Link = ""
			if err == nil || !strings.Contains(err.Error(), "unsafe link URL scheme") || !reflect.DeepEqual(got, want[1:]) {
				t.Fatalf("master=%q supplied=%v: partial child outputs/error changed: %v", masterID, supplied, err)
			}
		}
	}
}
