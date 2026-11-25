package d2talalayout

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestBoardLeafUsesExportedFaces(t *testing.T) {
	host := geo.NewBox(geo.NewPoint(-10.75, -5.25), 81.75, 81.75)
	leaf := geo.NewBox(nil, 80, 80)
	for _, tc := range []struct {
		direction string
		dx, dy    float64
	}{
		{"left", -1, 0}, {"right", 1, 0}, {"up", 0, -1}, {"down", 0, 1},
	} {
		for _, incoming := range []bool{false, true} {
			box, route := placeBoardLeaf(t.Context(), host, leaf, []*geo.Box{host}, nil, parseDirection(tc.direction), incoming)
			if box == nil || len(route) != 2 {
				t.Fatalf("%s incoming=%t: missing placement", tc.direction, incoming)
			}
			a, b := route[0], route[1]
			if b.X-a.X != tc.dx*48 || b.Y-a.Y != tc.dy*48 {
				t.Errorf("%s incoming=%t: unexpected route %v", tc.direction, incoming, route)
			}
			h, l := a, b
			sign := 1.
			if incoming {
				h, l, sign = b, a, -1
			}
			if h.X != 30.5+sign*tc.dx*40.5 || h.Y != 35.5+sign*tc.dy*40.5 {
				t.Errorf("%s incoming=%t: host port %v misses the exported face center", tc.direction, incoming, h)
			}
			lx := box.Center().X - sign*tc.dx*40
			ly := box.Center().Y - sign*tc.dy*40
			if math.Abs(lx-l.X)+math.Abs(ly-l.Y) > 0.5 {
				t.Errorf("%s incoming=%t: leaf port %v misses facing side at (%g,%g)", tc.direction, incoming, l, lx, ly)
			}
		}
	}
	if host.TopLeft.X != -10.75 || host.TopLeft.Y != -5.25 || leaf.TopLeft != nil {
		t.Fatal("placing a leaf mutated the input boxes")
	}
}

func TestBoardLeafDeclinesBlockedPorts(t *testing.T) {
	host := geo.NewBox(geo.NewPoint(0, 0), 80, 80)
	leaf := geo.NewBox(nil, 50, 50)
	lines := [][]*geo.Point{
		{geo.NewPoint(40, 0), geo.NewPoint(40, -100)},
		{geo.NewPoint(40, 80), geo.NewPoint(40, 180)},
		{geo.NewPoint(80, 40), geo.NewPoint(180, 40)},
	}
	if box, _ := placeBoardLeaf(t.Context(), host, leaf, []*geo.Box{host}, lines, geo.Right, false); box != nil {
		t.Fatal("used an occupied port or routed against the explicit flow")
	}
	if box, _ := placeBoardLeaf(t.Context(), host, leaf, []*geo.Box{host}, lines, geo.NONE, false); box == nil || box.TopLeft.X >= 0 {
		t.Fatal("missed the free left port of an undirected board")
	}
	lines = append(lines, []*geo.Point{geo.NewPoint(0, 40), geo.NewPoint(-100, 40)})
	if box, _ := placeBoardLeaf(t.Context(), host, leaf, []*geo.Box{host}, lines, geo.NONE, false); box != nil {
		t.Fatal("shared an occupied center port")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if box, _ := placeBoardLeaf(ctx, host, leaf, []*geo.Box{host}, nil, geo.NONE, false); box != nil {
		t.Fatal("accepted a canceled placement")
	}
}

func TestBoardLeafClearsPaintedNodesAndWires(t *testing.T) {
	body := image.Rect(140, 20, 190, 70)
	wire := geo.Segment{Start: geo.NewPoint(80, 45), End: geo.NewPoint(140, 45)}
	for _, tc := range []struct {
		name    string
		segment geo.Segment
		clear   bool
	}{
		{"diagonal crossing", geo.Segment{Start: geo.NewPoint(10, -50), End: geo.NewPoint(210, 150)}, false},
		{"inside new leaf", geo.Segment{Start: geo.NewPoint(155, 30), End: geo.NewPoint(170, 60)}, false},
		{"parallel arrowheads", geo.Segment{Start: geo.NewPoint(75, 57), End: geo.NewPoint(110, 57)}, false},
		{"diagonal passes outside", geo.Segment{Start: geo.NewPoint(0, 80), End: geo.NewPoint(200, 350)}, true},
	} {
		if got := boardLeafFits(body, wire, nil, []geo.Segment{tc.segment}); got != tc.clear {
			t.Errorf("%s: clear=%t", tc.name, got)
		}
	}
	if boardLeafFits(body, wire, []image.Rectangle{image.Rect(100, 30, 110, 40)}, nil) {
		t.Error("ran the new arrow through a neighboring shape's clearance")
	}
}
