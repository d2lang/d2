package d2lib

import (
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2target"
)

const gridHandoffSource = `
grid-columns: 2
classes: {cell: {width: 160; height: 48; style: {font: mono; fill: "#DEEEEF"}}}
anchor: writer {class: cell}
pg03: pg-03 {class: cell}
steps: {promote: {
 pg03: null
 pg09: pg-09 {class: cell}
}}`

func TestGridHandoffRollsChangedSuffix(t *testing.T) {
	prev, graph := compileGridMotionTest(t, gridHandoffSource)
	next := prev.Steps[0]
	oldNodes, nextNodes := gridNodes(prev, graph), gridNodes(next, graph.Steps[0])
	h := detectGridHandoffs(oldNodes, nextNodes)
	if h == nil || len(h.pairs) != 1 {
		t.Fatalf("missing slot handoff: old=%+v new=%+v", oldNodes["pg03"].shape, nextNodes["pg09"].shape)
	}
	p := h.pairs[0]
	if p.prefix != 4 {
		t.Fatalf("shared prefix length %d; old/new text %+v/%+v", p.prefix, p.from.Text, p.to.Text)
	}
	for _, tc := range []struct {
		ms   int
		want *d2target.Shape
	}{{0, p.from}, {420, p.to}, {900, p.to}} {
		frame := h.apply(nil, tc.ms, 900)
		if len(frame) != 1 || !reflect.DeepEqual(frame[0], *tc.want) {
			t.Fatalf("wrong authored endpoint at %d ms: %+v", tc.ms, frame)
		}
	}
	mid := h.apply([]d2target.Shape{*p.from, *p.to}, 210, 900)
	if len(mid) != 4 || !sameSlot(&mid[0], p.from) || mid[0].Opacity != p.from.Opacity || mid[0].Label != "" {
		t.Fatalf("expected one fixed outline and three runs, got %+v", mid)
	}
	prefix, out, in := mid[1], mid[2], mid[3]
	if prefix.Label != "pg-0" || prefix.Opacity != p.from.Opacity || out.Label != "3" || in.Label != "9" || out.Pos.Y >= in.Pos.Y || in.Pos.X != out.Pos.X || in.Opacity <= 0 || out.Opacity <= 0 {
		t.Fatalf("incorrect moving suffix: %+v / %+v / %+v", prefix, out, in)
	}
	other := h.apply(nil, 140, 900)
	if other[1].Pos != prefix.Pos {
		t.Fatal("prefix moved during handoff")
	}
	edges := gridEdgeShapeMap(h.edgeShapes(mid))
	if !sameSlot(edges["pg03"], edges["pg09"]) {
		t.Fatal("old and new connections must stay on the same outline")
	}
	if p.from.Label != "pg-03" || p.to.Label != "pg-09" {
		t.Fatal("sampling mutated authored endpoints")
	}
}

func TestGridHandoffFallbacks(t *testing.T) {
	prev, graph := compileGridMotionTest(t, gridHandoffSource)
	next := prev.Steps[0]
	oldNodes := gridNodes(prev, graph)
	for _, tc := range []struct {
		name   string
		change func(map[string]*gridNode)
	}{
		{"no surviving neighbor", func(n map[string]*gridNode) { delete(n, "anchor") }},
		{"neighbor moved", func(n map[string]*gridNode) { n["anchor"].shape.Pos.X++ }},
		{"new slot moved", func(n map[string]*gridNode) { n["pg09"].shape.Pos.Y++ }},
		{"new style", func(n map[string]*gridNode) { n["pg09"].shape.Stroke = "red" }},
		{"nested contents", func(n map[string]*gridNode) { n["pg09"].children = []*gridNode{n["anchor"]} }},
		{"non-grid item", func(n map[string]*gridNode) { n["pg09"].gridCell = false }},
		{"multiline", func(n map[string]*gridNode) { n["pg09"].shape.Label = "primary\nnode" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := gridNodes(next, graph.Steps[0])
			for _, node := range n {
				clone := *node.shape
				node.shape = &clone
			}
			tc.change(n)
			if h := detectGridHandoffs(oldNodes, n); h != nil {
				t.Fatal("ordinary grid transition expected")
			}
		})
	}
	for _, labels := range [][2]string{{"ﬃ01", "ﬃ02"}, {"alpha", "bravo"}, {"short", "longer"}} {
		a, b := *oldNodes["pg03"].shape, *oldNodes["pg03"].shape
		a.Label, b.Label = labels[0], labels[1]
		if p := handoffPrefix(&a, &b); p != 0 {
			t.Fatalf("invalid split at %d in %q -> %q", p, a.Label, b.Label)
		}
	}
}

func TestGridHandoffEdgeEndpoints(t *testing.T) {
	const src = `grid-columns: 2
classes: {cell: {width: 100; height: 45; style: {stroke-width: 7; font: mono}}}
service: Writer {class: cell}
a: pg-03 {class: cell}
service -> a
steps: {promote: {a: null; b: pg-09 {class: cell}; service -> b}}`
	prev, g := compileGridMotionTest(t, src)
	next := prev.Steps[0]
	m := newGridMotion(prev, next, g, g.Steps[0])
	for _, ms := range []int{0, 105, 210, 315, 420, 900} {
		frame := m.sample(next, ms, 900)
		if err := d2target.ValidateRenderTarget(frame); err != nil {
			t.Fatal(err)
		}
		shapes := gridEdgeShapeMap(frame.Shapes)
		want := 2
		if ms == 0 || ms >= gridHandoffMS {
			want = 1
		}
		if n := len(frame.Connections); n != want {
			t.Fatalf("time %d ms: wanted %d connections, got %d", ms, want, n)
		}
		for _, e := range frame.Connections {
			if shapes[e.Dst] == nil || shapes[e.Src] == nil || e.Opacity <= 0 {
				t.Fatalf("time %d ms: missing painted endpoint on %s -> %s", ms, e.Src, e.Dst)
			}
		}
	}
}
