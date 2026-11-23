package ranklabel

import (
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestTitleDetourHonorsObstaclesAndPortDirection(t *testing.T) {
	start, end := geo.NewPoint(0.25, 40.25), geo.NewPoint(200.25, 40.25)
	e := &d2graph.Edge{Src: &d2graph.Object{}, Dst: &d2graph.Object{}, Route: geo.Route{start, end}}
	title := geo.NewBox(geo.NewPoint(80.5, 35.5), 40, 20)
	above := &d2graph.Object{Box: geo.NewBox(geo.NewPoint(70, 10), 60, 24)}
	g := &d2graph.Graph{Objects: []*d2graph.Object{above}, Edges: []*d2graph.Edge{e}}
	r := avoidTitle(g, e, 1, title, []*geo.Box{title}, 8)
	if len(r) < 4 || r[0] != start || r[len(r)-1] != end || r[1].Y != start.Y || r[len(r)-2].Y != end.Y {
		t.Fatalf("detour lost a terminal or port direction: %v", r)
	}
	for i := 1; i < len(r); i++ {
		a, b := r[i-1], r[i]
		if a.X != b.X && a.Y != b.Y || entersBox(a, b, title) || entersBox(a, b, above.Box) {
			t.Fatalf("invalid detour segment %v -> %v", a, b)
		}
	}
	below := &d2graph.Object{Box: geo.NewBox(geo.NewPoint(70, 60), 60, 24)}
	g.Objects = append(g.Objects, below)
	if got := avoidTitle(g, e, 1, title, []*geo.Box{title}, 8); got != nil {
		t.Fatalf("found a detour despite occupied tracks on both sides: %v", got)
	}
}
