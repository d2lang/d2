package d2lib

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestRankFlatEdgeLabelClearance(t *testing.T) {
	const source = `
a: Ingress {rank: stage}
b: Worker {rank: stage}
c: Storage {rank: stage}
d: Cache {rank: stage}
a -> b: dispatch requests
a -> b: replicate jobs
b -> a: request acknowledgement
c -> d: invalidate cached data
a -> d: remote cache refresh
d -> a: cache read
a -> a: retry ingress
`
	for _, engine := range []string{"dagre", "elk", "tala"} {
		for _, direction := range []string{"down", "up", "right", "left"} {
			t.Run(engine+"/"+direction, func(t *testing.T) {
				diagram, _, err := Compile(d2log.WithDefault(t.Context()), "direction: "+direction+"\n"+source, rankCompileOptions(engine), nil)
				require.NoError(t, err)
				require.Len(t, diagram.Connections, 7)
				require.Len(t, diagram.Shapes, 4)
				checkRankEdgeLabels(t, diagram)
			})
		}
	}
}

func TestTALADenseRankEdgeLabels(t *testing.T) {
	var source strings.Builder
	for n := 'a'; n <= 'f'; n++ {
		fmt.Fprintf(&source, "%c.rank: stage\n", n)
	}
	for group := 0; group < 2; group++ {
		for from := 'a'; from <= 'f'; from++ {
			for to := from + 1; to <= 'f'; to++ {
				fmt.Fprintf(&source, "%c -> %c: validation for %c/%c stage %d\n", from, to, from, to, group)
			}
		}
	}
	for _, direction := range []string{"down", "up", "right", "left"} {
		t.Run(direction, func(t *testing.T) {
			diagram, _, err := Compile(d2log.WithDefault(t.Context()), "direction: "+direction+"\n"+source.String(), rankCompileOptions("tala"), nil)
			require.NoError(t, err)
			require.Len(t, diagram.Connections, 30)
			checkRankEdgeLabels(t, diagram)
		})
	}
}

func checkRankEdgeLabels(t *testing.T, diagram *d2target.Diagram) {
	t.Helper()
	labels := make([]*geo.Box, 0, len(diagram.Connections))
	for i, edge := range diagram.Connections {
		require.Positive(t, edge.LabelWidth)
		require.Positive(t, edge.LabelHeight)
		box := geo.NewBox(edge.GetLabelTopLeft(), float64(edge.LabelWidth), float64(edge.LabelHeight))
		for j, previous := range labels {
			require.False(t, box.Overlaps(*previous), "labels %q and %q overlap", edge.Label, diagram.Connections[j].Label)
		}
		for _, shape := range diagram.Shapes {
			node := geo.NewBox(geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)), float64(shape.Width), float64(shape.Height))
			require.False(t, node.Overlaps(*box), "label %d (%q) overlaps %s", i, edge.Label, shape.ID)
		}
		labels = append(labels, box)
	}
}
