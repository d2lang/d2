package d2scenebuild

import (
	"context"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func annotatedSceneDiagram() *d2target.Diagram {
	diagram := d2target.NewDiagram()
	diagram.Shapes = []d2target.Shape{metadataShape("a", 10, "", ""), metadataShape("b", 300, "", "")}
	diagram.Shapes[0].Annotation = &d2target.Annotation{Shape: d2target.AnnotationCircle, Label: "late", LabelWidth: 30, LabelHeight: 21}
	diagram.Shapes[1].Annotation = &d2target.Annotation{Shape: d2target.AnnotationCross}
	connection := *d2target.BaseConnection()
	connection.ID, connection.Stroke = "edge", "#000"
	connection.Route = []*geo.Point{{X: 110, Y: 50}, {X: 300, Y: 50}}
	connection.Annotation = &d2target.Annotation{Shape: d2target.AnnotationTrace}
	diagram.Connections = []d2target.Connection{connection}
	diagram.Root.Annotation = &d2target.Annotation{Label: "A caption.", LabelWidth: 110, LabelHeight: 31}
	return diagram
}

func lookupSceneNode(node *d2scene.Node, id string) *d2scene.Node {
	if node.ID == id {
		return node
	}
	for _, child := range node.Children {
		if found := lookupSceneNode(child, id); found != nil {
			return found
		}
	}
	return nil
}

func TestBuildAnnotations(t *testing.T) {
	pad := int64(0)
	document, err := Build(context.Background(), annotatedSceneDiagram(), Options{Pad: &pad})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	// Annotations are the last things painted, in declaration order, then
	// the caption.
	children := document.Root.Children
	var tail []string
	for _, child := range children[len(children)-4:] {
		tail = append(tail, child.ID)
	}
	if got := strings.Join(tail, " "); got != "a:annotation b:annotation edge:annotation root:caption" {
		t.Fatalf("scene ends with %q", got)
	}

	ring := lookupSceneNode(document.Root, "a:annotation:ink:0")
	path, ok := ring.Primitive.(d2scene.Path)
	if !ok || path.Fill != nil || path.Stroke == nil || path.Stroke.Width != d2target.AnnotationStrokeWidth ||
		path.Stroke.Cap != d2scene.CapRound || path.Stroke.Join != d2scene.JoinRound || len(path.Commands) < 8 {
		t.Fatalf("ring is not a round pen stroke: %+v", ring.Primitive)
	}
	if lookupSceneNode(document.Root, "a:annotation:note") == nil {
		t.Fatal("ring has no note")
	}
	if lookupSceneNode(document.Root, "b:annotation:ink:1") == nil {
		t.Fatal("cross has no second stroke")
	}
	if ink := lookupSceneNode(document.Root, "edge:annotation:ink"); ink == nil || ink.Opacity != d2target.AnnotationTraceOpacity {
		t.Fatalf("trace is not a translucent highlighter: %+v", ink)
	}

	// The scene uses the same extent as the SVG: marks and caption included.
	bare := annotatedSceneDiagram()
	bare.Shapes[0].Annotation, bare.Shapes[1].Annotation = nil, nil
	bare.Connections[0].Annotation, bare.Root.Annotation = nil, nil
	baseline, err := Build(context.Background(), bare, Options{Pad: &pad})
	if err != nil {
		t.Fatalf("baseline Build() error = %v", err)
	}
	if document.ViewBox.Height <= baseline.ViewBox.Height || document.ViewBox.Y >= baseline.ViewBox.Y {
		t.Fatalf("view box %+v does not hold the annotations of %+v", document.ViewBox, baseline.ViewBox)
	}
}

func TestBuildPartlyDrawnAnnotations(t *testing.T) {
	pad := int64(0)
	build := func(drawn *[2]float64) *d2scene.Document {
		diagram := annotatedSceneDiagram()
		diagram.Shapes[0].Annotation.Drawn = drawn
		document, err := Build(context.Background(), diagram, Options{Pad: &pad})
		if err != nil {
			t.Fatalf("Build() error = %v", err)
		}
		return document
	}
	commands := func(document *d2scene.Document) int {
		node := lookupSceneNode(document.Root, "a:annotation:ink:0")
		if node == nil {
			return 0
		}
		return len(node.Primitive.(d2scene.Path).Commands)
	}
	whole, half, none := build(nil), build(&[2]float64{0, 0.5}), build(&[2]float64{0, 0})
	if h, w := commands(half), commands(whole); h == 0 || h >= w {
		t.Fatalf("half a ring has %d path commands against %d", h, w)
	}
	// The note waits for the pen, and an undrawn mark paints nothing.
	if lookupSceneNode(half.Root, "a:annotation:note") != nil {
		t.Fatal("note appeared before the mark was drawn")
	}
	if lookupSceneNode(none.Root, "a:annotation") != nil {
		t.Fatal("an undrawn mark still built a node")
	}
	// Every frame of a transition has the same view box.
	if whole.ViewBox != none.ViewBox {
		t.Fatalf("frames have view boxes %+v and %+v", whole.ViewBox, none.ViewBox)
	}
}
