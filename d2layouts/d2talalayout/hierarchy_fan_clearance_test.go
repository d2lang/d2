package d2talalayout_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func hiddenFanArrivals(edges []d2target.Connection) int {
	type endpoint struct {
		id     string
		tip    geo.Point
		dx, dy int
	}
	seen := make(map[endpoint]bool)
	count := 0
	for _, e := range edges {
		if e.DstArrow == d2target.NoArrowhead {
			continue
		}
		tip := *e.Route[len(e.Route)-1]
		prev := *e.Route[len(e.Route)-2]
		key := endpoint{id: e.Dst, tip: tip}
		if prev.X > tip.X {
			key.dx = 1
		} else if prev.X < tip.X {
			key.dx = -1
		}
		if prev.Y > tip.Y {
			key.dy = 1
		} else if prev.Y < tip.Y {
			key.dy = -1
		}
		if seen[key] {
			count++
		}
		seen[key] = true
	}
	return count
}

func TestHierarchyFanDistinctArrivalsAndCapacity(t *testing.T) {
	for _, tc := range []struct {
		direction string
		count     int
		join      bool
	}{
		{"down", 9, true}, {"up", 9, true}, {"left", 9, true}, {"right", 9, true},
		{"down", 9, false}, {"down", 68, true},
	} {
		t.Run(fmt.Sprintf("%s-%d-join-%t", tc.direction, tc.count, tc.join), func(t *testing.T) {
			var src strings.Builder
			fmt.Fprintf(&src, "shape: hierarchy\ndirection: %s\nvars: {d2-config: {data: {tala-seeds: [1;2;3]}}}\n", tc.direction)
			if tc.count == 68 {
				src.WriteString("start: Stage {width: 96; height: 52}\n")
				for i := 0; i < tc.count; i++ {
					fmt.Fprintf(&src, "n%d: Stage {width: 96; height: 52}\n", i)
				}
				src.WriteString("end: Stage {width: 96; height: 52}\n")
			} else {
				src.WriteString("start: Hub\n")
				if tc.join {
					src.WriteString("end: Sink\n")
				}
				for i := 0; i < tc.count; i++ {
					fmt.Fprintf(&src, "n%d: peer {width: 100; height: 76}\n", i)
				}
			}
			for i := 0; i < tc.count; i++ {
				fmt.Fprintf(&src, "start -> n%d\n", i)
				if tc.join {
					fmt.Fprintf(&src, "n%d -> end\n", i)
				}
			}
			if !tc.join {
				src.WriteString("key: Legend\n")
			}
			d := compileRoutingDiagram(t, t.Context(), src.String(), 1, nil)
			if got := fanRouteCrossings(d.Connections); got != 0 {
				t.Errorf("proper fan crossings = %d", got)
			}
			hidden := hiddenFanArrivals(d.Connections)
			if tc.count == 9 && hidden != 0 {
				t.Errorf("%d hidden arrivals on a hub with enough room", hidden)
			}
			if tc.count == 68 && hidden == 0 {
				t.Error("expected the 52px-high faces to retain their shared ports")
			}
			for _, e := range d.Connections {
				if len(e.Route) > 3 {
					t.Fatalf("added bend to %s", e.ID)
				}
			}
		})
	}
}

func TestMoveStraightLaneToClearDuplicateReceiverPort(t *testing.T) {
	const source = `shape: hierarchy
direction: down
vars: {d2-config: {data: {tala-seeds: [1;2;3]}}}
a: {width: 104; height: 64}
b: {width: 104; height: 64}
c: {width: 104; height: 64}
x: {width: 104; height: 64}
y: {width: 104; height: 64}
z: {width: 104; height: 64}
a -> x
a -> y
a -> z
b -> x
b -> y
b -> z
c -> x
c -> y
c -> z`
	d := compileRoutingDiagram(t, t.Context(), source, 1, nil)
	if got := fanRouteCrossings(d.Connections); got > 5 {
		t.Errorf("still has %d crossings, want at most 5", got)
	}
	if got := hiddenFanArrivals(d.Connections); got != 0 {
		t.Errorf("%d coincident receiver arrows", got)
	}
	for _, e := range d.Connections {
		if e.Src == "a" && e.Dst == "x" && len(e.Route) != 2 {
			t.Fatal("the a -> x lane acquired bends")
		}
	}
}
