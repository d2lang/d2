package d2animate

import (
	"image"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestLayerZoomUsesEnclosingBoardLink(t *testing.T) {
	link := `root.layers."west.coast".layers.api`
	portal := d2target.Shape{Width: 50, Height: 20, Link: link}
	inside := &d2target.Diagram{Name: "api", Shapes: []d2target.Shape{{Width: 40, Height: 10, Link: `root.layers."west.coast"`}}}
	step := &d2target.Diagram{Name: "api"}
	other := &d2target.Diagram{Name: "peer"}
	region := &d2target.Diagram{Name: "west.coast", Shapes: []d2target.Shape{
		portal, portal, {Link: link, Width: 10}, {Link: `root.layers."west.coast".steps.api`, Width: 25, Height: 10},
	}, Layers: []*d2target.Diagram{inside, other}, Steps: []*d2target.Diagram{step}}
	doc := &d2target.Diagram{Layers: []*d2target.Diagram{region}}
	selected, path := doc.ResolveBoard([]string{"west.coast"})
	nav := NewLayerNavigation(selected, path)

	if got := nav.destination(link); got != inside {
		t.Fatalf("quoted link inside shorthand target resolved to %p, want %p", got, inside)
	}
	for _, from := range []*d2target.Diagram{region, inside} {
		to := region
		if from == region {
			to = inside
		}
		zoom, ok := nav.Zoom(from, to)
		if !ok || zoom.Focus != &region.Shapes[1] || zoom.Inward != (from == region) {
			t.Fatalf("%s to %s: wrong direction or portal: %+v", from.Name, to.Name, zoom)
		}
	}
	for _, boards := range [][2]*d2target.Diagram{{region, step}, {region, other}, {other, inside}, {inside, inside}, {inside, doc}} {
		if _, ok := nav.Zoom(boards[0], boards[1]); ok {
			t.Fatalf("unexpected zoom from %s to %s", boards[0].Name, boards[1].Name)
		}
	}
}

func TestLayerZoomMatchesStudioPhases(t *testing.T) {
	view := image.Rect(-30, -10, 570, 290)
	zoom := LayerZoom{Focus: &d2target.Shape{Pos: d2target.Point{X: 30, Y: 50}, Width: 80, Height: 40}, Inward: true}
	identity := LayerPose{Scale: 1, Opacity: 1}
	start, _ := zoom.Poses(view, 0)
	panned, _ := zoom.Poses(view, layerPanPhase)
	inside, child := zoom.Poses(view, 1)
	if start != identity || panned != (LayerPose{Scale: 1, DX: 200, DY: 70, Opacity: 1}) || child != identity {
		t.Fatalf("unexpected endpoints or pan: start %+v, panned %+v, child %+v", start, panned, child)
	}
	if inside.Scale != 7.5 || inside.Scale*70+inside.DX != 270 || inside.Scale*70+inside.DY != 140 {
		t.Fatalf("enlarged portal does not fill and center in viewport: %+v", inside)
	}
	zOut := zoom
	zOut.Inward = false
	for _, tIn := range []float64{0, .2, .4, .7, 1} {
		in, _ := zoom.Poses(view, tIn)
		out, _ := zOut.Poses(view, 1-tIn)
		if math.Abs(in.DX-out.DX) > 1e-8 || math.Abs(in.DY-out.DY) > 1e-8 || math.Abs(in.Scale-out.Scale) > 1e-8 {
			t.Fatalf("outward differs from reverse of parent pose at %g: %+v vs %+v", tIn, in, out)
		}
	}
	parent, shrinking := zOut.Poses(view, layerShrinkPhase)
	if shrinking.Opacity != 0 || !(parent.Opacity > 0 && parent.Opacity < 1) {
		t.Fatalf("outgoing child should finish before the enclosing board: %+v, %+v", parent, shrinking)
	}
}

func TestLayerRoutesRequireEveryEnclosingPortal(t *testing.T) {
	root := &d2target.Diagram{}
	a, b, detail := &d2target.Diagram{Name: "a"}, &d2target.Diagram{Name: "b"}, &d2target.Diagram{Name: "detail"}
	step := &d2target.Diagram{Name: "review"}
	portal := func(link string) d2target.Shape {
		return d2target.Shape{Link: link, Width: 20, Height: 15}
	}
	root.Layers = []*d2target.Diagram{a, b}
	root.Shapes = []d2target.Shape{portal("root.layers.a"), portal("root.layers.b")}
	a.Layers, a.Steps = []*d2target.Diagram{detail}, []*d2target.Diagram{step}
	a.Shapes = []d2target.Shape{portal("root.layers.a.layers.detail")}
	nav := NewLayerNavigation(root, nil)

	for _, want := range [][]*d2target.Diagram{
		{a, root, b}, {detail, a, root, b}, {b, root, a, detail},
		{root, a, detail}, {detail, a, root}, {step, root, b},
	} {
		route := nav.Route(want[0], want[len(want)-1])
		if len(route) != len(want)-1 {
			t.Fatalf("expected %d legs between %q and %q, got %+v", len(want)-1, want[0].Name, want[len(want)-1].Name, route)
		}
		for i, leg := range route {
			zoom, ok := nav.Zoom(want[i], want[i+1])
			if !ok || leg.From != want[i] || leg.To != want[i+1] || leg.Zoom != zoom {
				t.Fatalf("leg %d was not the expected enclosing portal: %+v", i, leg)
			}
		}
	}
	if route := nav.Route(a, step); len(route) != 0 {
		t.Fatal("unexpected camera between steps in one layer")
	}
	a.Shapes = nil
	if route := NewLayerNavigation(root, nil).Route(detail, b); len(route) != 0 {
		t.Fatal("partial branch routes must fall back to the ordinary board cut")
	}
	root.Shapes = append(root.Shapes, portal("root.layers.a.layers.detail"))
	if route := NewLayerNavigation(root, nil).Route(root, detail); len(route) != 1 || route[0].Zoom.Focus != &root.Shapes[2] {
		t.Fatal("explicit direct portals take priority")
	}
}
