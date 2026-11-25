package d2talalayout_test

import (
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func TestDisconnectedTilesFollowTranslatedCanvasPin(t *testing.T) {
	const source = `gateway: Gateway {left: %d; top: %d; width: 90; height: 66}
queue: Queue {width: 90; height: 66}
worker: Worker {width: 90; height: 66}
cache: Cache {width: 90; height: 66}`
	original := make(map[string]d2target.Point)
	for i, pin := range []d2target.Point{{}, {X: 70, Y: 100}, {X: 500, Y: 500}} {
		d := compileRoutingDiagram(t, t.Context(), fmt.Sprintf(source, pin.X, pin.Y), 1, nil)
		for _, n := range d.Shapes {
			if n.ID == "gateway" && n.Pos != pin {
				t.Fatalf("gateway at %v, want %v", n.Pos, pin)
			}
			relative := d2target.Point{X: n.Pos.X - pin.X, Y: n.Pos.Y - pin.Y}
			if i == 0 {
				original[n.ID] = relative
			} else if relative != original[n.ID] {
				t.Errorf("pin %v: %s moved from %v to %v relative to gateway", pin, n.ID, original[n.ID], relative)
			}
		}
	}
}

func TestCanvasPackingPreservesDecoratedSiblings(t *testing.T) {
	const source = `node0: Staging transfer {width: 55; height: 90; left: 50; top: 400; label.near: outside-bottom-center}
node1: Incoming authorization {width: 80; height: 50; label.near: outside-right-center; icon: "status.svg"; icon.near: outside-top-right}
node6: Routing preference {width: 55; height: 50; label.near: outside-top-left; icon: "status.svg"; icon.near: outside-top-right}
node7: Staging transfer {width: 80; height: 50; label.near: outside-top-left}`
	shapes := hierarchyGeometry(t, source)
	caption := func(id string) geo.Box {
		n := shapes[id]
		body := geo.NewBox(geo.NewPoint(float64(n.Pos.X), float64(n.Pos.Y)), float64(n.Width), float64(n.Height))
		w, h := float64(n.LabelWidth), float64(n.LabelHeight)
		p := label.FromString(n.LabelPosition).GetPointOnBox(body, label.PADDING, w, h)
		return geo.Box{TopLeft: p, Width: w, Height: h}
	}
	a, b := caption("node6"), caption("node7")
	if a.Overlaps(b) {
		t.Fatal("two sibling titles overlap after packing around the pin")
	}
}
