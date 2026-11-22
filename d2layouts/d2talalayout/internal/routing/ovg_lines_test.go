package routing

import (
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// The counted grouping must give connectNodes' map grouping: the same lines
// in the same order, the same nodes per line in the same order (nil for NaN
// keys), and line positions with the bits the map keeps (-0/+0 merged at the
// last assigned value).
func TestLineGroupingMatchesMaps(t *testing.T) {
	random := rand.New(rand.NewSource(23))
	coordinate := func() float64 {
		switch random.Intn(12) {
		case 0:
			return math.NaN()
		case 1:
			return math.Copysign(0, -1)
		case 2:
			return 0
		default:
			return float64(random.Intn(7) * 10)
		}
	}
	for trial := range 500 {
		var eligible []*OVGNode
		for range random.Intn(60) {
			eligible = append(eligible, NewOVGNode(geo.NewPoint(coordinate(), coordinate())))
		}
		mapped, counted := &ovgLineScratch{lineIndex: map[float64]int32{}}, &ovgLineScratch{lineIndex: map[float64]int32{}}
		wantH, wantV := mapped.groupLines(eligible, false)
		gotH, gotV := counted.groupLines(eligible, true)
		for axis, pair := range [][2]*ovgLineGroups{{wantH, gotH}, {wantV, gotV}} {
			want, got := pair[0], pair[1]
			if len(want.positions) != len(got.positions) {
				t.Fatalf("trial %d axis %d: %d lines, want %d", trial, axis, len(got.positions), len(want.positions))
			}
			for i := range want.positions {
				w, g := want.positions[i], got.positions[i]
				if math.Float64bits(w) != math.Float64bits(g) && !(math.IsNaN(w) && math.IsNaN(g)) {
					t.Fatalf("trial %d axis %d line %d: position %v, want %v", trial, axis, i, g, w)
				}
				wl, gl := want.line(i), got.line(i)
				if (wl == nil) != (gl == nil) || len(wl) != len(gl) {
					t.Fatalf("trial %d axis %d line %d: %d nodes, want %d", trial, axis, i, len(gl), len(wl))
				}
				for k := range wl {
					if wl[k] != gl[k] {
						t.Fatalf("trial %d axis %d line %d: node %d differs", trial, axis, i, k)
					}
				}
			}
		}
	}
}

// reserveNodes only changes capacity: every lookup answers as before
// (signed zeros share an entry, NaN never matches) and the node list is the
// same.
func TestReserveNodesKeepsLookups(t *testing.T) {
	random := rand.New(rand.NewSource(31))
	coordinate := func() float64 {
		switch random.Intn(10) {
		case 0:
			return math.NaN()
		case 1:
			return math.Copysign(0, -1)
		case 2:
			return 0
		default:
			return float64(random.Intn(9))
		}
	}
	for trial := range 200 {
		ovg := NewOVG(nil)
		for range random.Intn(40) {
			ovg.AddNode(NewOVGNode(geo.NewPoint(coordinate(), coordinate())))
		}
		nodes := append([]*OVGNode(nil), ovg.Nodes...)
		probes := make([]geo.Point, 200)
		want := make([]*OVGNode, len(probes))
		for i := range probes {
			probes[i] = geo.Point{X: coordinate(), Y: coordinate()}
			want[i] = ovg.OccupiedPoints[probes[i]]
		}
		ovg.reserveNodes(1 + random.Intn(50))
		if len(ovg.Nodes) != len(nodes) {
			t.Fatalf("trial %d: %d nodes, want %d", trial, len(ovg.Nodes), len(nodes))
		}
		for i := range nodes {
			if ovg.Nodes[i] != nodes[i] {
				t.Fatalf("trial %d: node %d changed", trial, i)
			}
		}
		for i, probe := range probes {
			if got := ovg.OccupiedPoints[probe]; got != want[i] {
				t.Fatalf("trial %d: lookup %v changed", trial, probe)
			}
		}
	}
}
