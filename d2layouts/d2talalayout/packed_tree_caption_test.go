package d2talalayout_test

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestPackedTileClearsTreeEndpointCaptions(t *testing.T) {
	for index, source := range []string{
		`direction: left
H: Main {width: 130; height: 75}
N0: Finish {width: 84; height: 85}
H -> N0: {source-arrowhead: Accepted for processing and release}
N1: Finish {width: 84; height: 85}
H -> N1: {source-arrowhead: Q}
Other`,
		`direction: left
H: Main {width: 80; height:48}
N0: Finish {shape: rectangle; width: 117; height: 50}
N0 -> H: {target-arrowhead: Next; source-arrowhead: Q}
N1: Job {shape: oval; width: 117; height: 64}
N1 -> H: {target-arrowhead: Next; source-arrowhead: Next}
Other`,
	} {
		diagram := compileRoutingDiagram(t, t.Context(), source, 1, nil)
		reference := compileRoutingDiagram(t, t.Context(), source+"\nOther.style.fill: white", 1, nil)
		if len(diagram.Connections) != 2 || !reflect.DeepEqual(reference.Connections, diagram.Connections) {
			t.Fatal("clearing the detached tile changed a connector")
		}
		if len(reference.Shapes) != len(diagram.Shapes) {
			t.Fatal("clearing the detached tile changed the node inventory")
		}
		for i, s := range diagram.Shapes {
			old := reference.Shapes[i]
			if s.ID != old.ID || s.Width != old.Width || s.Height != old.Height || s.ID != "Other" && s.Pos != old.Pos {
				t.Fatalf("clearing the detached tile changed %s", s.ID)
			}
		}
		tl, br := diagram.BoundingBox()
		oldTL, oldBR := reference.BoundingBox()
		if tl.X < oldTL.X || tl.Y < oldTL.Y || br.X > oldBR.X || br.Y > oldBR.Y {
			t.Fatalf("clearing the tile expanded the drawing from %v..%v to %v..%v", oldTL, oldBR, tl, br)
		}
		var tile *geo.Box
		for _, shape := range diagram.Shapes {
			if shape.ID == "Other" {
				tile = geo.NewBox(geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)), float64(shape.Width), float64(shape.Height))
				break
			}
		}
		if tile == nil {
			t.Fatal("missing detached tile")
		}
		var captions []*geo.Box
		for _, edge := range diagram.Connections {
			for _, target := range [...]bool{false, true} {
				caption := edge.SrcLabel
				if target {
					caption = edge.DstLabel
				}
				if caption == nil {
					continue
				}
				b := geo.NewBox(edge.GetArrowheadLabelPosition(target), float64(caption.LabelWidth), float64(caption.LabelHeight))
				for _, other := range captions {
					if b.Overlaps(*other) {
						t.Fatalf("caption overlaps another caption on %s", edge.ID)
					}
				}
				captions = append(captions, b)
				if b.Overlaps(*tile) {
					t.Fatalf("detached tile overlaps %q on %s", caption.Label, edge.ID)
				}
			}
		}
		if want := []int{2, 4}[index]; len(captions) != want {
			t.Fatalf("got %d endpoint captions, want %d", len(captions), want)
		}
	}
}
