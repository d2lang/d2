package d2talalayout_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/geo"
)

func TestHierarchyLoopsClearNeighboringRanks(t *testing.T) {
	for _, tc := range []struct {
		name, source, neighbor string
		loops                  int
	}{
		{"left", "shape: hierarchy\ndirection: left\napi -> db\ndb -> db\ndb -> db", "api", 2},
		{"down", "shape: hierarchy\napi -> db\ndb -> db: retry\ndb -> db: refresh", "api", 2},
		{"automatic tree", "Gateway -> Worker\nGateway -> Audit\nWorker -> Store\nWorker -> Notify\nAudit -> Archive\nWorker -> Worker: retry\nWorker -> Worker: refresh", "Gateway", 2},
		{"more loops than corners", "shape: hierarchy\ndirection: up\napi -> db: sync\n" + strings.Repeat("db -> db\n", 5), "api", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), tc.source, 1, &d2svg.RenderOpts{})
			shapes := map[string]geo.Box{}
			for _, n := range d.Shapes {
				shapes[n.ID] = geo.Box{TopLeft: geo.NewPoint(float64(n.Pos.X), float64(n.Pos.Y)), Width: float64(n.Width), Height: float64(n.Height)}
			}
			neighbor := shapes[tc.neighbor]
			loops := 0
			for _, e := range d.Connections {
				if e.Src != e.Dst {
					continue
				}
				loops++
				host := shapes[e.Src]
				if host.Overlaps(neighbor) {
					t.Fatalf("loop host %s overlaps %s", e.Src, tc.neighbor)
				}
				if tc.name == "left" && host.TopLeft.X+host.Width >= neighbor.TopLeft.X {
					t.Fatal("leftward edge has no open space between the two node boundaries")
				}
				for i := 1; i < len(e.Route); i++ {
					seg := geo.Segment{Start: e.Route[i-1], End: e.Route[i]}
					if neighbor.Intersects(seg, -0.01) || neighbor.Contains(seg.Start) {
						t.Fatalf("%s traverses %s at %v..%v", e.ID, tc.neighbor, seg.Start, seg.End)
					}
				}
				if e.Label != "" {
					box := geo.NewBox(e.GetLabelTopLeft(), float64(e.LabelWidth), float64(e.LabelHeight))
					if box.Overlaps(neighbor) {
						t.Fatalf("%s label %q overlaps %s", e.ID, e.Label, tc.neighbor)
					}
				}
			}
			if loops != tc.loops {
				t.Fatalf("got %d loops", loops)
			}
		})
	}
}

func TestSelfLoopArrowheadLabelsUseSeparatePorts(t *testing.T) {
	d := compileRoutingDiagram(t, t.Context(), "api -> api: {target-arrowhead.label: request}\napi -> api: {target-arrowhead.label: reply}", 1, &d2svg.RenderOpts{})
	if len(d.Connections) != 2 {
		t.Fatalf("got %d loops", len(d.Connections))
	}
	var labels [2]geo.Box
	var targets [2]geo.Point
	for i, e := range d.Connections {
		labels[i] = geo.Box{TopLeft: e.GetArrowheadLabelPosition(true), Width: float64(e.DstLabel.LabelWidth), Height: float64(e.DstLabel.LabelHeight)}
		targets[i] = *e.Route[len(e.Route)-1]
	}
	if labels[0].Overlaps(labels[1]) || targets[0] == targets[1] {
		t.Fatalf("loop arrowheads at %v have overlapping labels or a shared port", targets)
	}
}
