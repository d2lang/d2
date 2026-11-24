package d2talalayout_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2renderers/d2svg"
)

func TestHorizontalSourceOrientationConsidersWholeDrawing(t *testing.T) {
	for _, tt := range []struct {
		name, external, local string
		horizontal            bool
	}{
		{"smaller", "pipe -> x\npipe -> y\npipe -> z\nx -> y -> h\nz -> x", "", true},
		{"larger", "pipe -> review\npipe -> audit\npipe -> archive\nreview -> archive", "", false},
		{"explicit", "pipe -> x\npipe -> y\npipe -> z\nx -> y -> h\nz -> x", "direction: down", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			shapes := hierarchyGeometry(t, fmt.Sprintf("direction: right\npipe: {\n%s\na -> b -> c\n}\n%s", tt.local, tt.external))
			a, b, c := shapes["pipe.a"], shapes["pipe.b"], shapes["pipe.c"]
			if tt.horizontal {
				require.Less(t, a.Pos.X, b.Pos.X)
				require.Less(t, b.Pos.X, c.Pos.X)
				require.Equal(t, a.Pos.Y, b.Pos.Y)
				require.Equal(t, b.Pos.Y, c.Pos.Y)
			} else {
				require.Less(t, a.Pos.Y, b.Pos.Y)
				require.Less(t, b.Pos.Y, c.Pos.Y)
				require.Equal(t, a.Pos.X, b.Pos.X)
				require.Equal(t, b.Pos.X, c.Pos.X)
			}
		})
	}
}

func TestAuthoredDimensionRelievesOnlyCrowdedParallelPorts(t *testing.T) {
	for _, tt := range []struct {
		dir       string
		freeWidth bool
	}{
		{"down", true}, {"right", false},
	} {
		t.Run(tt.dir, func(t *testing.T) {
			source := "direction: " + tt.dir + "\nqueue: Queue {height: 200; style.font-size: 16}\nworker: Worker {height: 200; style.font-size: 16}\n" + strings.Repeat("queue -> worker\n", 4)
			d := compileRoutingDiagram(t, t.Context(), source, 1, &d2svg.RenderOpts{})
			for _, n := range d.Shapes {
				require.Equal(t, 200, n.Height)
				require.Equal(t, 16, n.FontSize)
				if tt.freeWidth {
					require.Equal(t, 200, n.Width)
				} else {
					require.Less(t, n.Width, 100)
				}
			}
			require.Len(t, d.Connections, 4)
			for i, e := range d.Connections {
				for _, other := range d.Connections[:i] {
					for _, end := range []int{0, 1} {
						a, b := e.Route[end*(len(e.Route)-1)], other.Route[end*(len(other.Route)-1)]
						require.GreaterOrEqual(t, math.Hypot(a.X-b.X, a.Y-b.Y), 40.0)
					}
				}
			}
		})
	}
}

// The queue also talks to a third node, so it is not itself eligible to grow.
func TestPortSizingKeepsDistinctArrivalsAtUnchangedPeer(t *testing.T) {
	source := "direction: right\nP: Pub {height: 180}\nQ: Queue {height: 50}\n" + strings.Repeat("P -> Q\n", 9) + "Q -> A\nD -> D\nC -> A\nE -> B"
	for _, seed := range []int64{1, 2, 3} {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			d := compileRoutingDiagram(t, t.Context(), source, seed, &d2svg.RenderOpts{})
			ports := map[[2]float64]bool{}
			for _, e := range d.Connections {
				if e.Dst != "Q" {
					continue
				}
				port := e.Route[len(e.Route)-1]
				key := [2]float64{port.X, port.Y}
				require.False(t, ports[key], "two arrows arrive at the same point on Q")
				ports[key] = true
			}
			require.Len(t, ports, 9)
		})
	}
}
