package d2sketch_test

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2sketch"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestSketchPencilFillSampling(t *testing.T) {
	s := *d2target.BaseShape()
	s.Width, s.Height = 160, 80
	transition := &d2target.SketchFillTransition{From: "transparent", To: "#fecd86"}
	s.SketchFillTransition = transition
	sample := func(p float64) *d2sketch.PencilFill {
		transition.Progress = p
		return d2sketch.SamplePencilFill(s)
	}
	for _, endpoint := range []struct {
		progress float64
		paint    string
	}{{0, "transparent"}, {1, "#fecd86"}} {
		got := sample(endpoint.progress)
		if len(got.Bands) != 0 || got.Paper != endpoint.paint {
			t.Fatalf("endpoint %v: %+v", endpoint.progress, got)
		}
	}
	early, middle, late := sample(.25), sample(.5), sample(.9)
	if len(early.Bands) == 0 || len(early.Bands) >= len(middle.Bands) || len(middle.Bands) >= len(late.Bands) {
		t.Fatalf("staggered pencil passes: %d, %d, %d", len(early.Bands), len(middle.Bands), len(late.Bands))
	}
	if early.Bands[0] != middle.Bands[0] || late.GraphiteOpacity >= middle.GraphiteOpacity {
		t.Fatal("completed pencil strokes must hold still until they settle into the final fill")
	}
	transition.From, transition.To = transition.To, transition.From
	if erase := sample(.75); !reflect.DeepEqual(early, erase) {
		t.Fatal("erasing must undo the forward strokes over clear paper")
	}
	s.Width, s.Height = math.MaxInt, math.MaxInt
	if bands := len(sample(.1).Bands); bands == 0 || bands > 64 {
		t.Fatalf("large shape has %d pencil bands", bands)
	}
}

func TestSketchPencilFillSVG(t *testing.T) {
	s := *d2target.BaseShape()
	s.ID, s.Fill, s.Stroke = "cache", "B1", "N1"
	s.Width, s.Height = 160, 80
	static, err := d2sketch.Oval(s, "frame")
	if err != nil {
		t.Fatal(err)
	}
	s.SketchFillTransition = &d2target.SketchFillTransition{From: "N7", To: s.Fill, Progress: 1}
	end, err := d2sketch.Oval(s, "frame")
	if err != nil || end != static {
		t.Fatalf("final pencil frame differs from the authored oval: %v", err)
	}
	s.SketchFillTransition.Progress = .5
	mid, err := d2sketch.Oval(s, "frame")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`patternUnits="userSpaceOnUse"`, `fill="url(#pencil-frame-`, `fill-B1`, `fill-N7`, `-strokes"`} {
		if !strings.Contains(mid, fragment) {
			t.Errorf("missing %s in %s", fragment, mid)
		}
	}
}

func TestSketchPencilFillBehindMarkdown(t *testing.T) {
	for _, tc := range []struct {
		name, position string
		progress       float64
		wantBacking    bool
	}{
		{"default", "", .5, false},
		{"inside", "INSIDE_TOP_LEFT", .5, false},
		{"outside", "OUTSIDE_TOP_CENTER", .5, true},
		{"border", "BORDER_TOP_CENTER", .5, true},
		{"start", "", 0, true},
		{"end", "", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := d2target.NewDiagram()
			s := d2target.BaseShape()
			s.ID, s.Type, s.Width, s.Height = "cache", d2target.ShapeRectangle, 160, 80
			s.Fill, s.Stroke, s.LabelPosition = "#ffaa77", "#112233", tc.position
			s.FontSize, s.Language, s.Label = 16, "markdown", "**partition**"
			s.LabelWidth, s.LabelHeight = 100, 30
			s.SketchFillTransition = &d2target.SketchFillTransition{From: "#ffffff", To: s.Fill, Progress: tc.progress}
			d.Shapes = []d2target.Shape{*s}

			sketch := true
			out, err := d2svg.Render(d, &d2svg.RenderOpts{Sketch: &sketch})
			if err != nil {
				t.Fatal(err)
			}
			// The Markdown viewport starts with its backdrop or its text group.
			_, viewport, found := strings.Cut(string(out), `viewBox="0 0 100 30" overflow="hidden">`)
			if !found || strings.HasPrefix(viewport, "<rect ") != tc.wantBacking {
				t.Fatalf("SVG Markdown backing differs from expected %t", tc.wantBacking)
			}

			doc, err := d2scenebuild.Build(context.Background(), d, d2scenebuild.Options{
				Sketch: true, SketchBudget: d2scenebuild.SketchBudget{MaxOperationSets: 100, MaxOperations: 10000, MaxPathCommands: 10000},
			})
			if err != nil {
				t.Fatal(err)
			}
			var hasBacking func(*d2scene.Node) bool
			hasBacking = func(n *d2scene.Node) bool {
				if n.ID == "cache:markdown:background" {
					return true
				}
				for _, child := range n.Children {
					if hasBacking(child) {
						return true
					}
				}
				return false
			}
			if got := hasBacking(doc.Root); got != tc.wantBacking {
				t.Errorf("scene Markdown backing=%t, want %t", got, tc.wantBacking)
			}
		})
	}
}
