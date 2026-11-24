package d2scenebuild

import (
	"context"
	"image/color"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
)

func TestSketchPencilFillClipsToRoughOutline(t *testing.T) {
	for _, shapeType := range []string{d2target.ShapeRectangle, d2target.ShapeOval, d2target.ShapeCylinder} {
		t.Run(shapeType, func(t *testing.T) {
			b := newSketchShapeTestBuilder(context.Background(), d2target.NewDiagram(), largeSketchTestBudget())
			s := sketchTestShape("cache", shapeType, 31, 47, 120, 90)
			s.SketchFillTransition = &d2target.SketchFillTransition{From: "#ffffff", To: "#ffaa77", Progress: .5}
			nodes, err := b.buildSketchOrdinaryShape(s, "")
			if err != nil {
				t.Fatal(err)
			}
			node := nodes[0]
			path := node.Primitive.(d2scene.Path)
			paper := d2scene.SolidPaint{Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255}}
			ink := d2scene.SolidPaint{Color: color.NRGBA{R: 255, G: 170, B: 119, A: 255}}
			if path.Fill != paper || path.Stroke != nil || len(node.Children) != 2 {
				t.Fatalf("expected paper under a clipped pencil layer and a separate outline")
			}
			layer := node.Children[0]
			if layer.Clip == nil || !reflect.DeepEqual(layer.Clip.Path.Commands, path.Commands) {
				t.Fatal("pencil strokes must follow the original rough silhouette")
			}
			pencil := layer.Children[0].Primitive.(d2scene.Path)
			outline := node.Children[1].Primitive.(d2scene.Path)
			if pencil.Fill != ink || len(pencil.Commands) == 0 || outline.Stroke == nil || outline.Fill != nil || !reflect.DeepEqual(outline.Commands, path.Commands) {
				t.Fatal("expected target ink inside the original rough outline")
			}
		})
	}
}
