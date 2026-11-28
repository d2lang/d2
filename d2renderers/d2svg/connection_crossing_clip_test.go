package d2svg

import (
	"bytes"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestCrossingClipFallback(t *testing.T) {
	gap := func(x, y, r float64) d2target.ConnectionGap {
		return d2target.ConnectionGap{Center: geo.Point{X: x, Y: y}, Radius: r}
	}
	viewport := crossingViewport{width: 100, height: 100}
	for _, tc := range []struct {
		name string
		gaps []d2target.ConnectionGap
		mask bool
	}{
		{"separate", []d2target.ConnectionGap{gap(15, 20, 6), gap(15, 40, 6)}, false},
		{"tangent", []d2target.ConnectionGap{gap(20, 20, 5), gap(30, 20, 5)}, false},
		{"outside viewport", []d2target.ConnectionGap{gap(98, 20, 3)}, true},
		// The first and third disks overlap. The second sits between them in sweep order.
		{"nonadjacent overlap", []d2target.ConnectionGap{gap(20, 40, 10), gap(19, 60, 5), gap(24, 40, 4), gap(90, 40, 2)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var defs bytes.Buffer
			crossings := [][]d2target.ConnectionGap{tc.gaps}
			diagram := &d2target.Diagram{Connections: []d2target.Connection{{Animated: true}}}
			effects := viewport.planCrossings("board", crossings, diagram, false, false)
			viewport.writeCrossings(&defs, effects, crossings)
			effect := effects[0]
			if effect.mask != tc.mask {
				t.Fatalf("mask = %t, want %t", effect.mask, tc.mask)
			}
			if tc.mask {
				if !strings.HasPrefix(defs.String(), "<mask ") || strings.Count(defs.String(), "<circle ") != len(tc.gaps) {
					t.Fatalf("fallback lost a disk: %s", &defs)
				}
			} else if !strings.HasPrefix(defs.String(), "<clipPath ") {
				t.Fatalf("missing clip: %s", &defs)
			}
		})
	}
}

func TestCrossingClipDenseSweepFallback(t *testing.T) {
	// Every disk is disjoint, but each column would require comparing all row pairs.
	var gaps []d2target.ConnectionGap
	for col := range 64 {
		for row := range 64 {
			gaps = append(gaps, d2target.ConnectionGap{
				Center: geo.Point{X: float64(3*col + 1), Y: float64(3*row + 1)}, Radius: 1,
			})
		}
	}
	if (crossingViewport{width: 200, height: 200}).canClip(gaps) {
		t.Fatal("expected mask fallback after exhausting the overlap-check budget")
	}
}

func TestCustomStylesheetCrossingMaskOrder(t *testing.T) {
	original := BaseStylesheet
	t.Cleanup(func() { BaseStylesheet = original })
	BaseStylesheet += "mask:nth-of-type(2) circle { fill: black }"

	line := func(id string, z int, a, b geo.Point) d2target.Connection {
		c := d2target.BaseConnection()
		c.ID, c.ZIndex, c.Stroke, c.Route = id, z, "black", []*geo.Point{&a, &b}
		return *c
	}
	diagram := d2target.NewDiagram()
	diagram.Connections = []d2target.Connection{
		line("higher", 1, geo.Point{X: 0, Y: 20}, geo.Point{X: 100, Y: 20}),
		line("lower", 0, geo.Point{X: 0, Y: 60}, geo.Point{X: 100, Y: 60}),
		line("front", 2, geo.Point{X: 50, Y: 0}, geo.Point{X: 50, Y: 100}),
	}
	hash, err := diagram.HashID(nil)
	if err != nil {
		t.Fatal(err)
	}
	pad := int64(100)
	out, err := Render(diagram, &RenderOpts{Pad: &pad})
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out)
	higher, lower := "crossings-"+hash+"-0", "crossings-"+hash+"-1"
	first, second := strings.Index(svg, `<mask id="`+higher+`"`), strings.Index(svg, `<mask id="`+lower+`"`)
	if first < 0 || second <= first {
		t.Fatal("custom stylesheet no longer sees mask definitions in connection order")
	}
	first, second = strings.Index(svg, `<g mask="url(#`+lower+`)"`), strings.Index(svg, `<g mask="url(#`+higher+`)"`)
	if first < 0 || second <= first {
		t.Fatal("expected legacy mask groups around strokes in paint order")
	}
}
