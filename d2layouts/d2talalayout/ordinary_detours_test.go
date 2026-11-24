package d2talalayout_test

import (
	"math"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestOrdinaryRoutesAvoidUnnecessaryContainerWallCrossings(t *testing.T) {
	for _, tc := range []struct {
		name, source, container, from, to string
		wantCrossings                     int
	}{
		{"siblings", "team: {app -- db; app -- cache; db -- cache}", "team", "team.app", "team.db", 0},
		{"arrival", `g0: {n0; n1}
g1: {n0; n1; n2}
g2: {n0; n1; n2}
g1.n0 -- g2.n2
g1 -- g2
g1.n2 -- g2.n0
g0 -- g2
g0.n0 -- g1.n0
g1.n2 -- g2.n1
g1.n0 -- g0
g0.n1 -- g2.n1`, "g2", "g1.n2", "g2.n1", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), tc.source, 3, nil)
			var box *geo.Box
			for _, s := range d.Shapes {
				if s.ID == tc.container {
					box = geo.NewBox(geo.NewPoint(float64(s.Pos.X), float64(s.Pos.Y)), float64(s.Width), float64(s.Height))
					break
				}
			}
			if box == nil {
				t.Fatal("missing container")
			}
			for _, e := range d.Connections {
				if e.Src != tc.from || e.Dst != tc.to {
					continue
				}
				crossings := 0
				for i := 1; i < len(e.Route); i++ {
					crossings += len(box.Intersections(*geo.NewSegment(e.Route[i-1], e.Route[i])))
				}
				if crossings != tc.wantCrossings {
					t.Fatalf("%s crosses %s %d times, want %d", e.ID, tc.container, crossings, tc.wantCrossings)
				}
				return
			}
			t.Fatal("missing test edge")
		})
	}
}

func TestOrdinaryMeshDetourRemovesTwoCrossings(t *testing.T) {
	const mesh = `B -- C
D -- E
A -- B
C -- G
C -- D
G -- A
F -- G
B -- F
D -- F
E -- F
A -- E`
	d := compileRoutingDiagram(t, t.Context(), mesh, 2, nil)
	if n := fanRouteCrossings(d.Connections); n > 1 {
		t.Fatalf("mesh has %d proper crossings, want at most one", n)
	}
	for _, e := range d.Connections {
		if e.Src != "A" || e.Dst != "B" {
			continue
		}
		var length float64
		for i := 1; i < len(e.Route); i++ {
			a, b := e.Route[i-1], e.Route[i]
			length += math.Abs(a.X-b.X) + math.Abs(a.Y-b.Y)
		}
		// The original two-bend route was 273 px with two crossings.
		if length > 353 || len(e.Route) > 4 {
			t.Fatalf("expensive detour: %g px, %d bends", length, len(e.Route)-2)
		}
		return
	}
	t.Fatal("missing test edge")
}
