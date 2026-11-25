package packing

import (
	"context"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/graphbounds"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

func packBoundsGuard(t *testing.T, limit int64) *limits.WorkGuard {
	t.Helper()
	guard, err := limits.NewWorkGuard(context.Background(), "BinPack", limit)
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

func samePackBoundsFloat(a, b float64) bool {
	return math.Float64bits(a) == math.Float64bits(b) || (math.IsNaN(a) && math.IsNaN(b))
}

func samePackBoundsPoint(value geo.Point, pointer *geo.Point) bool {
	if pointer == nil {
		return false
	}
	return samePackBoundsFloat(value.X, pointer.X) && samePackBoundsFloat(value.Y, pointer.Y)
}

func samePackBoundsError(a, b error) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Error() == b.Error()
}

// checkPackBoundsParity runs the value bounds and graphbounds on the same
// nodes under the same limit and compares corners, errors and units.
func checkPackBoundsParity(t *testing.T, nodes layoutgraph.Nodes, limit int64) {
	t.Helper()
	for _, node := range nodes {
		for _, all := range []layoutgraph.Nodes{nodes, nil} {
			want := packBoundsGuard(t, limit)
			got := packBoundsGuard(t, limit)
			wantTL, wantBR, wantErr := graphbounds.NodeBoundingBox(node, all, want)
			gotTL, gotBR, gotErr := packNodeBoundingBox(node, all, got)
			if !samePackBoundsError(gotErr, wantErr) || got.Used() != want.Used() {
				t.Fatalf("node bounds: err %v / %v, units %d / %d", gotErr, wantErr, got.Used(), want.Used())
			}
			if wantErr == nil && (!samePackBoundsPoint(gotTL, wantTL) || !samePackBoundsPoint(gotBR, wantBR)) {
				t.Fatalf("node bounds = %v %v, want %v %v", gotTL, gotBR, *wantTL, *wantBR)
			}
		}
	}
	want := packBoundsGuard(t, limit)
	got := packBoundsGuard(t, limit)
	wantTL, wantBR, wantErr := graphbounds.BoundingBox(nodes, want)
	gotTL, gotBR, gotErr := packBoundingBox(nodes, got)
	if !samePackBoundsError(gotErr, wantErr) || got.Used() != want.Used() {
		t.Fatalf("bounds: err %v / %v, units %d / %d", gotErr, wantErr, got.Used(), want.Used())
	}
	if wantErr == nil && (!samePackBoundsPoint(gotTL, wantTL) || !samePackBoundsPoint(gotBR, wantBR)) {
		t.Fatalf("bounds = %v %v, want %v %v", gotTL, gotBR, *wantTL, *wantBR)
	}
	want = packBoundsGuard(t, limit)
	got = packBoundsGuard(t, limit)
	wantTL, wantBR, wantErr = graphbounds.FixedBoundingBox(nodes, want)
	gotTL, gotBR, gotErr = packFixedBoundingBox(nodes, got)
	if !samePackBoundsError(gotErr, wantErr) || got.Used() != want.Used() {
		t.Fatalf("fixed bounds: err %v / %v, units %d / %d", gotErr, wantErr, got.Used(), want.Used())
	}
	if wantErr == nil && (!samePackBoundsPoint(gotTL, wantTL) || !samePackBoundsPoint(gotBR, wantBR)) {
		t.Fatalf("fixed bounds = %v %v, want %v %v", gotTL, gotBR, *wantTL, *wantBR)
	}
}

func TestPackValueBoundsMatchGraphbounds(t *testing.T) {
	rng := rand.New(rand.NewSource(0x7A1A3))
	outside := []label.Position{
		label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideTopRight,
		label.OutsideLeftTop, label.OutsideLeftMiddle, label.OutsideLeftBottom,
		label.OutsideRightTop, label.OutsideRightMiddle, label.OutsideRightBottom,
		label.OutsideBottomLeft, label.OutsideBottomCenter, label.OutsideBottomRight,
		label.InsideMiddleCenter,
	}
	coordinate := func() float64 {
		switch rng.Intn(40) {
		case 0:
			return math.NaN()
		case 1:
			return math.Copysign(0, -1)
		case 2:
			return math.Inf(1)
		}
		return math.Round(-300+rng.Float64()*600) + float64(rng.Intn(4))/4
	}
	for iteration := 0; iteration < 600; iteration++ {
		count := rng.Intn(9)
		// A chain of containers (sometimes a cycle) that nodes may sit in.
		depth := rng.Intn(4)
		chain := make([]*layoutgraph.Node, depth)
		for i := range chain {
			chain[i] = layoutgraph.NewNode(layoutgraph.EntityID(1000+i), 900, 900)
			chain[i].TopLeft = geo.NewPoint(-400, -400)
			if i > 0 {
				chain[i].Container = chain[i-1]
			}
		}
		if depth > 1 && rng.Intn(10) == 0 {
			chain[0].Container = chain[depth-1]
		}
		nodes := make(layoutgraph.Nodes, 0, count)
		for i := 0; i < count; i++ {
			node := layoutgraph.NewNode(layoutgraph.EntityID(i+1), 5+math.Round(rng.Float64()*150), 5+math.Round(rng.Float64()*150))
			if rng.Intn(15) != 0 {
				node.TopLeft = geo.NewPoint(coordinate(), coordinate())
			}
			if depth > 0 && rng.Intn(3) != 0 {
				node.Container = chain[rng.Intn(depth)]
			}
			if rng.Intn(2) == 0 {
				node.LoopOffsets = map[geo.Orientation]float64{
					geo.Top: rng.Float64() * 30, geo.Bottom: rng.Float64() * 30,
					geo.Left: rng.Float64() * 30, geo.Right: rng.Float64() * 30,
				}
			}
			switch rng.Intn(5) {
			case 0:
				node.Is3D = true
			case 1:
				node.SetShape(shape.HEXAGON_TYPE)
				node.Is3D = true
			case 2:
				node.IsMultiple = true
			}
			if rng.Intn(2) == 0 {
				node.Label = &layoutgraph.Label{
					Position: outside[rng.Intn(len(outside))],
					Width:    1 + rng.Float64()*180,
					Height:   1 + rng.Float64()*80,
				}
			}
			if rng.Intn(4) == 0 {
				node.Icon = &layoutgraph.Icon{Position: outside[rng.Intn(len(outside))]}
			}
			if node.TopLeft != nil && rng.Intn(5) == 0 {
				node.FixedTopLeft = geo.NewPoint(node.TopLeft.X-50+rng.Float64()*100, node.TopLeft.Y-50+rng.Float64()*100)
			}
			nodes = append(nodes, node)
			if rng.Intn(12) == 0 {
				nodes = append(nodes, node)
			}
		}
		for _, limit := range []int64{1 << 40, 0, 1, 2, 3, 5, 8, 13, int64(rng.Intn(60))} {
			checkPackBoundsParity(t, nodes, limit)
		}
	}
}
