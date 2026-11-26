package d2scenebuild_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestBoardOutlineRenderersAgree(t *testing.T) {
	d := d2target.NewDiagram()
	d.Shapes = []d2target.Shape{{ID: "frame", Type: d2target.ShapeRectangle, Pos: d2target.Point{X: 10, Y: 20}, Width: 100, Height: 60, Fill: "B4", Stroke: "B1", StrokeWidth: 2, Opacity: 1, OutlineProgress: new(.5)}}
	svg, err := d2svg.Render(d, nil)
	if err != nil || !bytes.Contains(svg, []byte(`d="M 10 50 L 10 20 L 60 20 L 110 20 L 110 50"`)) {
		t.Fatalf("expected open SVG contour, render error %v", err)
	}
	doc, err := d2scenebuild.Build(t.Context(), d, d2scenebuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	frame := doc.Root.Children[1]
	if frame.ID != "frame" {
		t.Fatalf("unexpected scene node %q", frame.ID)
	}
	nodes := frame.Children
	if len(nodes) != 2 {
		t.Fatalf("got %d contour primitives", len(nodes))
	}
	fill, ok := nodes[0].Primitive.(d2scene.Rect)
	if !ok || fill.Stroke != nil {
		t.Fatal("partially drawn rectangle should retain only its body fill")
	}
	line, ok := nodes[1].Primitive.(d2scene.Path)
	want := []d2scene.PathCommand{d2scene.MoveTo(10, 50), d2scene.LineTo(10, 20), d2scene.LineTo(60, 20), d2scene.LineTo(110, 20), d2scene.LineTo(110, 50)}
	if !ok || line.Stroke == nil || !reflect.DeepEqual(line.Commands, want) {
		t.Fatalf("unexpected intermediate raster contour: %#v", line.Commands)
	}
	dark := int64(200)
	darkSVG, err := d2svg.Render(d, &d2svg.RenderOpts{DarkThemeID: &dark})
	if err != nil || !bytes.Contains(darkSVG, []byte(`d="M 10 50 L 10 20 L 60 20 L 110 20 L 110 50"`)) {
		t.Fatalf("responsive SVG omitted the trace: %v", err)
	}
	darkScene, err := d2scenebuild.Build(t.Context(), d, d2scenebuild.Options{ThemeID: &dark})
	if err != nil {
		t.Fatal(err)
	}
	darkLine := darkScene.Root.Children[1].Children[1].Primitive.(d2scene.Path)
	if reflect.DeepEqual(darkLine.Stroke.Paint, line.Stroke.Paint) || !reflect.DeepEqual(darkLine.Commands, line.Commands) {
		t.Fatal("outline stroke should follow the theme and retain its geometry")
	}
	d.Shapes[0].OutlineProgress = new(1.)
	full, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	complete, err := d2scenebuild.Build(t.Context(), d, d2scenebuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	completedNodes := complete.Root.Children[1].Children
	if len(completedNodes) != 1 {
		t.Fatal("completed scene added geometry")
	}
	closed, ok := completedNodes[0].Primitive.(d2scene.Rect)
	if !ok || closed.Stroke == nil || complete.ViewBox != doc.ViewBox {
		t.Fatal("completed scene lost the closed rectangle or changed its viewport")
	}
	d.Shapes[0].OutlineProgress = nil
	static, err := d2svg.Render(d, nil)
	// Independent diagram hashes can differ because the intermediate field
	// participates in target serialization. The completed shape is a rect.
	if err != nil || bytes.Count(full, []byte("<rect ")) != bytes.Count(static, []byte("<rect ")) || bytes.Count(full, []byte("<path ")) != bytes.Count(static, []byte("<path ")) {
		t.Fatal("completed SVG did not return to the static shape geometry")
	}
	for _, bad := range []float64{math.NaN(), -0.1, 1.1} {
		d.Shapes[0].OutlineProgress = &bad
		if err := d2target.ValidateRenderTarget(d); err == nil {
			t.Fatal("invalid outline progress accepted by target")
		}
		if _, err := d2scenebuild.Build(t.Context(), d, d2scenebuild.Options{}); err == nil {
			t.Fatal("invalid outline progress accepted by scene builder")
		}
	}
}
