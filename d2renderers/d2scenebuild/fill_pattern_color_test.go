package d2scenebuild_test

import (
	"context"
	"image/color"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2target"
)

func TestBuildFillPatternColor(t *testing.T) {
	t.Parallel()

	diagram := d2target.NewDiagram()
	diagram.Root.Fill = "#000000"
	diagram.Root.FillPattern = "dots"
	diagram.Root.FillPatternColor = "#ffffff"
	shape := *d2target.BaseShape()
	shape.ID = "box"
	shape.Pos = d2target.Point{X: 10, Y: 10}
	shape.Width = 100
	shape.Height = 60
	shape.Fill = "#000000"
	shape.Stroke = "#ffffff"
	shape.FillPattern = "lines"
	shape.FillPatternColor = "#ff0000"
	diagram.Shapes = []d2target.Shape{shape}

	scene, err := d2scenebuild.Build(context.Background(), diagram, d2scenebuild.Options{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	var foundRootOverlay, foundShapeOverlay bool
	var visit func(n *d2scene.Node)
	visit = func(n *d2scene.Node) {
		if n == nil {
			return
		}
		if n.ID == "root:background:fill-pattern" {
			foundRootOverlay = true
			if n.Blend != d2scene.BlendNormal {
				t.Fatalf("root custom pattern overlay Blend = %v, want BlendNormal", n.Blend)
			}
			rect, ok := n.Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("root pattern primitive = %T, want d2scene.Rect", n.Primitive)
			}
			pat, ok := rect.Fill.(d2scene.PatternPaint)
			if !ok || pat.Root == nil || len(pat.Root.Children) == 0 {
				t.Fatalf("unexpected root PatternPaint: %+v", rect.Fill)
			}
			if pat.Root.Opacity != 1 {
				t.Fatalf("root custom pattern tile Opacity = %v, want 1", pat.Root.Opacity)
			}
			dot, ok := pat.Root.Children[0].Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("dot primitive = %T, want d2scene.Rect", pat.Root.Children[0].Primitive)
			}
			solid, ok := dot.Fill.(d2scene.SolidPaint)
			if !ok || solid.Color != (color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}) {
				t.Fatalf("root dot color = %+v, want white", dot.Fill)
			}
		}
		if n.ID == "box:fill-pattern:0" {
			foundShapeOverlay = true
			if n.Blend != d2scene.BlendNormal {
				t.Fatalf("shape custom pattern overlay Blend = %v, want BlendNormal", n.Blend)
			}
			rect, ok := n.Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("shape pattern primitive = %T, want d2scene.Rect", n.Primitive)
			}
			pat, ok := rect.Fill.(d2scene.PatternPaint)
			if !ok || pat.Root == nil || len(pat.Root.Children) == 0 {
				t.Fatalf("unexpected shape PatternPaint: %+v", rect.Fill)
			}
			line, ok := pat.Root.Children[0].Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("line primitive = %T, want d2scene.Rect", pat.Root.Children[0].Primitive)
			}
			solid, ok := line.Fill.(d2scene.SolidPaint)
			if !ok || solid.Color != (color.NRGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}) {
				t.Fatalf("shape line color = %+v, want red", line.Fill)
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	visit(scene.Root)
	if !foundRootOverlay || !foundShapeOverlay {
		t.Fatalf("missing overlays: root=%v shape=%v", foundRootOverlay, foundShapeOverlay)
	}
}
