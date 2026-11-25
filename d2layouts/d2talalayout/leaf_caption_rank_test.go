package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestTreeNonadjacentLeafAnnotations(t *testing.T) {
	for _, direction := range []string{"down", "up"} {
		for _, nested := range []bool{false, true} {
			for _, middleHeight := range []int{30, 42} {
				t.Run(fmt.Sprintf("%s/nested=%t/middle-height=%d", direction, nested, middleHeight), func(t *testing.T) {
					source := fmt.Sprintf(`direction: %s
R: Root {width: 75; height: 56}
L0: X {width: 30; height: 42}
R -> L0: {target-arrowhead: Partially processed}
L1: X {width: 30; height: %d}
R -> L1
L2: X {width: 95; height: 42}
L2 <- R: {source-arrowhead: Partially processed}`, direction, middleHeight)
					if nested {
						source = "Box: {\n" + source + "\n}"
					}
					diagram := compileRoutingDiagram(t, t.Context(), source, 1, nil)
					if len(diagram.Connections) != 3 {
						t.Fatalf("got %d branches, want 3", len(diagram.Connections))
					}
					first, last := diagram.Connections[0], diagram.Connections[2]
					if first.DstLabel == nil || last.SrcLabel == nil {
						t.Fatal("lost a terminal caption")
					}
					a := geo.NewBox(first.GetArrowheadLabelPosition(true), float64(first.DstLabel.LabelWidth), float64(first.DstLabel.LabelHeight))
					b := geo.NewBox(last.GetArrowheadLabelPosition(false), float64(last.SrcLabel.LabelWidth), float64(last.SrcLabel.LabelHeight))
					if a.Overlaps(*b) {
						t.Fatalf("annotations on nonadjacent leaves overlap: %v and %v", a, b)
					}
				})
			}
		}
	}
}

func TestHorizontalPinnedTreeKeepsTerminalCaptionClearance(t *testing.T) {
	const source = `direction: right
Proposals -> Gateway
Scores -> Gateway: {source-arrowhead: Request received}
Payments -> Gateway: {source-arrowhead: Request received}
Inbox -> Payments
External -> Payments: {source-arrowhead: Request received}
Notify -> Inbox: {source-arrowhead: Request received}
Gateway: {left: 260; top: 190}`
	diagram := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	var captions []geo.Box
	for _, edge := range diagram.Connections {
		if edge.SrcLabel == nil {
			continue
		}
		b := geo.NewBox(edge.GetArrowheadLabelPosition(false), float64(edge.SrcLabel.LabelWidth), float64(edge.SrcLabel.LabelHeight))
		for _, other := range captions {
			if b.Overlaps(other) {
				t.Fatalf("source annotation on %s overlaps another annotation", edge.ID)
			}
		}
		for _, node := range diagram.Shapes {
			body := geo.NewBox(geo.NewPoint(float64(node.Pos.X), float64(node.Pos.Y)), float64(node.Width), float64(node.Height))
			if b.Overlaps(*body) {
				t.Fatalf("source annotation on %s overlaps %s", edge.ID, node.ID)
			}
		}
		captions = append(captions, *b)
	}
	if len(captions) != 4 {
		t.Fatalf("got %d source annotations, want 4", len(captions))
	}
}
