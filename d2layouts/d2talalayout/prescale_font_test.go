package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
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
