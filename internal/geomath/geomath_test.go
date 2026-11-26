package geomath

import (
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestSegmentIntersectionMatchesIntersectionPoint compares SegmentIntersection
// and FirstBoxIntersection with geo.IntersectionPoint and the first point of
// geo.Box.Intersections, the original code, bit for bit, NaN sign and payload
// included, on random segments: many degenerate (axis-parallel, shared ends,
// collinear), some with NaN (of random sign and payload), infinite or
// signed-zero coordinates. Before NaN results were taken from
// geo.IntersectionPoint, about 0.6% of these cases differed in NaN bits.
func TestSegmentIntersectionMatchesIntersectionPoint(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewSource(7))
	coord := func() float64 {
		switch r.Intn(12) {
		case 0:
			return float64(r.Intn(21) - 10)
		case 1:
			return math.Round(r.Float64()*2000-1000) / 4
		case 2:
			return r.Float64()*2000 - 1000
		case 3:
			return float64(r.Intn(3)) * 100
		case 4:
			// A NaN of random sign and payload.
			return math.Float64frombits(0x7ff8000000000000 | uint64(r.Intn(2))<<63 | uint64(r.Int63())&0x0007ffffffffffff)
		case 5:
			return math.Inf(1 - 2*r.Intn(2))
		case 6:
			return math.Copysign(0, -1)
		default:
			return r.NormFloat64() * 300
		}
	}
	same := func(x, y float64, ok bool, p *geo.Point) bool {
		if p == nil {
			return !ok
		}
		return ok && math.Float64bits(x) == math.Float64bits(p.X) && math.Float64bits(y) == math.Float64bits(p.Y)
	}
	nans, found := 0, 0
	for i := 0; i < 400000; i++ {
		var p [8]geo.Point
		for k := range p {
			p[k] = geo.Point{X: coord(), Y: coord()}
			if k > 0 && r.Intn(6) == 0 {
				p[k].X = p[k-1].X
			}
			if k > 0 && r.Intn(6) == 0 {
				p[k].Y = p[k-1].Y
			}
			if k > 1 && r.Intn(10) == 0 {
				p[k] = p[r.Intn(k)]
			}
		}
		x, y, ok := SegmentIntersection(p[0].X, p[0].Y, p[1].X, p[1].Y, p[2].X, p[2].Y, p[3].X, p[3].Y)
		want := geo.IntersectionPoint(&p[0], &p[1], &p[2], &p[3])
		if !same(x, y, ok, want) {
			t.Fatalf("SegmentIntersection(%v %v %v %v) = %x, %x, %v; IntersectionPoint gives %v", p[0], p[1], p[2], p[3],
				math.Float64bits(x), math.Float64bits(y), ok, want)
		}
		if ok {
			found++
			if math.IsNaN(x) || math.IsNaN(y) {
				nans++
			}
		}

		// The box at p[4] with the size p[5], crossed by p[6]-p[7].
		b := geo.NewBox(&p[4], p[5].X, p[5].Y)
		x, y, ok = FirstBoxIntersection(b.TopLeft.X, b.TopLeft.Y, b.Width, b.Height, p[6].X, p[6].Y, p[7].X, p[7].Y)
		want = nil
		if is := b.Intersections(geo.Segment{Start: &p[6], End: &p[7]}); len(is) > 0 {
			want = is[0]
		}
		if !same(x, y, ok, want) {
			t.Fatalf("FirstBoxIntersection(%v %vx%v, %v-%v) = %x, %x, %v; Box.Intersections gives %v", p[4], p[5].X, p[5].Y, p[6], p[7],
				math.Float64bits(x), math.Float64bits(y), ok, want)
		}
	}
	if found < 50000 || nans < 1000 {
		t.Fatalf("only %d intersections, %d of them NaN", found, nans)
	}
}

// The Linux/amd64 race build used different NaN operands for the right-hand
// corners of this box. Compare against the reference on the current platform.
func TestFirstBoxIntersectionNaNCornerPayload(t *testing.T) {
	topLeft := geo.Point{
		X: math.Float64frombits(0xffff4f9fe5ef50b3),
		Y: math.Float64frombits(0xc07483a6b0f006bc),
	}
	box := geo.NewBox(&topLeft, math.Float64frombits(0x7ffbb3f890482d83), math.Float64frombits(0x7ff836bedcdfea45))
	start := geo.Point{
		X: math.Float64frombits(0x7ffbb3f890482d83),
		Y: math.Float64frombits(0x7ff8dfd964686250),
	}
	end := geo.Point{
		X: math.Float64frombits(0x4053d0b5e5698306),
		Y: math.Float64frombits(0x4059000000000000),
	}

	points := box.Intersections(geo.Segment{Start: &start, End: &end})
	if len(points) == 0 {
		t.Fatal("expected the reference to find an intersection")
	}
	x, y, hit := FirstBoxIntersection(topLeft.X, topLeft.Y, box.Width, box.Height, start.X, start.Y, end.X, end.Y)
	gotX, gotY := math.Float64bits(x), math.Float64bits(y)
	wantX, wantY := math.Float64bits(points[0].X), math.Float64bits(points[0].Y)
	if !hit || gotX != wantX || gotY != wantY {
		t.Fatalf("FirstBoxIntersection = (%016x, %016x, %t), want (%016x, %016x, true)", gotX, gotY, hit, wantX, wantY)
	}
}
