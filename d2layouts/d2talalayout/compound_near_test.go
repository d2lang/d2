package d2talalayout_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
)

func TestNearTargetsOrdinaryCompoundMember(t *testing.T) {
	for _, tc := range []struct {
		name, flow, note, target string
	}{
		{"chain", "Cut -- Drill -- Sand -- Seal", "Memo", "Cut"},
		{"between containers", "Cut -- Drill -- Sand -- Seal", "Office.Memo", "Sand"},
		{"grouped target", "Cut -> Drill -> Sand -> Seal -> Cut", "Memo", "Cut"},
		{"grouped target between containers", "Cut -> Drill -> Sand -> Seal -> Cut", "Office.Memo", "Sand"},
	} {
		for _, noteFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/noteFirst=%t", tc.name, noteFirst), func(t *testing.T) {
				note := fmt.Sprintf("%s: {shape: text; near: Workshop.%s}\n", tc.note, tc.target)
				workshop := fmt.Sprintf("Workshop: {%s}\n", tc.flow)
				source := workshop + note
				if noteFirst {
					source = note + workshop
				}
				d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
				shapes := make(map[string]d2target.Shape, len(d.Shapes))
				for _, n := range d.Shapes {
					shapes[n.ID] = n
				}
				centerDistance := func(id string) float64 {
					a, aOK := shapes[tc.note]
					b, bOK := shapes["Workshop."+id]
					if !aOK || !bOK {
						t.Fatalf("missing %s or Workshop.%s", tc.note, id)
					}
					return math.Hypot(float64(a.Pos.X-b.Pos.X)+float64(a.Width-b.Width)/2,
						float64(a.Pos.Y-b.Pos.Y)+float64(a.Height-b.Height)/2)
				}
				targetDistance := centerDistance(tc.target)
				for _, other := range []string{"Cut", "Drill", "Sand", "Seal"} {
					if other != tc.target && centerDistance(other) <= targetDistance {
						t.Errorf("%s is as close or closer to %s than its named target %s", tc.note, other, tc.target)
					}
				}
			})
		}
	}
}

func TestPinnedNearClosesOnlyTheRequestedGap(t *testing.T) {
	const source = `Anchor: Here {left: 300; top: 350; width: 90; height: 62}
Job: Main {near: Anchor}
t0: Tile 0
t1: Tile 1`
	d := compileRoutingDiagram(t, t.Context(), source, 3, nil)
	want := map[string]d2target.Point{
		"Anchor": {X: 300, Y: 350},
		"Job":    {X: 270, Y: 432}, // 20 px below the bottom of the pin.
		"t0":     {X: 270, Y: 264},
		"t1":     {X: 270, Y: 178},
	}
	if len(d.Shapes) != len(want) {
		t.Fatalf("got %d shapes, want %d", len(d.Shapes), len(want))
	}
	for _, n := range d.Shapes {
		if n.Pos != want[n.ID] {
			t.Errorf("%s moved to %v, want %v", n.ID, n.Pos, want[n.ID])
		}
	}
}
