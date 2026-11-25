package d2talalayout_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestPrescaleRetainsAuthoredFont(t *testing.T) {
	for _, size := range []int{11, 48} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			source := fmt.Sprintf("hub: Gateway {style.font-size: %d}\npeer: API\n", size) + strings.Repeat("hub -> peer\n", 4)
			d := compileRoutingDiagram(t, t.Context(), source, 1, &d2svg.RenderOpts{})
			if len(d.Shapes) != 2 {
				t.Fatalf("got %d shapes, want hub and peer", len(d.Shapes))
			}
			for _, shape := range d.Shapes {
				switch shape.ID {
				case "hub":
					if shape.FontSize != size || shape.Height < 200 {
						t.Errorf("hub has font %d, height %d; want font %d with room for four edges", shape.FontSize, shape.Height, size)
					}
				case "peer":
					if shape.FontSize <= 16 {
						t.Errorf("automatic peer font stayed at %d on a dense edge bundle", shape.FontSize)
					}
				default:
					t.Fatalf("unexpected shape %q", shape.ID)
				}
			}
		})
	}
}

func TestIsolatedTextLoopKeepsMeasuredSize(t *testing.T) {
	const source = "a: Q {shape: text}\n"
	plain := compileRoutingDiagram(t, t.Context(), source, 1, nil).Shapes[0]
	d := compileRoutingDiagram(t, t.Context(), source+"a -> a\n", 1, nil)
	n := d.Shapes[0]
	if n.Width != plain.Width || n.Height != plain.Height || n.FontSize != plain.FontSize {
		t.Errorf("text with a loop is %dx%d at %d px, want %dx%d at %d px", n.Width, n.Height, n.FontSize, plain.Width, plain.Height, plain.FontSize)
	}
	if len(d.Connections) != 1 || len(d.Connections[0].Route) != 5 {
		t.Fatalf("expected one routed loop with three bends")
	}
	// On a mixed board the existing loop clearance participates in packing.
	mixed := compileRoutingDiagram(t, t.Context(), source+"a -> a\nOther\n", 1, nil)
	for _, n := range mixed.Shapes {
		if n.ID == "a" && (n.Width != 42 || n.Height != 42) {
			t.Errorf("mixed-board loop box changed to %dx%d", n.Width, n.Height)
		}
	}
}

func TestRepeatedLayoutKeepsAutomaticFontsAdjustable(t *testing.T) {
	for _, serialized := range []bool{false, true} {
		for _, edit := range []string{"automatic", "authored", "replace", "edit value", "shared scalar"} {
			t.Run(fmt.Sprintf("%s/serialized=%t", edit, serialized), func(t *testing.T) {
				src := "hub: Gateway\npeer: API\nhub -> peer"
				if edit == "authored" {
					src += "\nhub.style.font-size: 19"
				}
				ruler, err := textmeasure.NewRuler()
				if err != nil {
					t.Fatal(err)
				}
				ctx := t.Context()
				layout := func(ctx context.Context, g *d2graph.Graph) error {
					return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{1}})
				}
				_, g, err := d2lib.Compile(ctx, src, &d2lib.CompileOptions{
					Ruler: ruler, Layout: new("tala"),
					LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return layout, nil },
				}, &d2svg.RenderOpts{})
				if err != nil {
					t.Fatal(err)
				}
				hub := g.Objects[0]
				wantHub, wantPeer := 32, 32
				switch edit {
				case "authored":
					wantHub = 19
				case "replace":
					hub.Style.FontSize = &d2graph.Scalar{Value: "16"}
					wantHub = 16
				case "shared scalar":
					g.Objects[1].Style.FontSize = hub.Style.FontSize
					wantPeer = 16
				case "edit value":
					hub.Style.FontSize.Value = "19"
					wantHub = 19
				}
				if serialized {
					data, err := d2graph.SerializeGraph(g)
					if err != nil {
						t.Fatal(err)
					}
					g = d2graph.NewGraph()
					if err := d2graph.DeserializeGraph(data, g); err != nil {
						t.Fatal(err)
					}
				}
				for len(g.Edges) < 6 {
					if _, err := g.Root.Connect([]d2ast.String{d2ast.FlatUnquotedString("hub")}, []d2ast.String{d2ast.FlatUnquotedString("peer")}, false, true, ""); err != nil {
						t.Fatal(err)
					}
				}
				if err := layout(ctx, g); err != nil {
					t.Fatal(err)
				}
				if h, p := g.Objects[0].Text().FontSize, g.Objects[1].Text().FontSize; h != wantHub || p != wantPeer {
					t.Errorf("six connectors give fonts %d and %d, want %d and %d", h, p, wantHub, wantPeer)
				}
			})
		}
	}
}
