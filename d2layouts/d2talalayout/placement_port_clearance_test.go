package d2talalayout

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

func TestPortClearanceChecksEachPairAtMixedPeer(t *testing.T) {
	// Two incoming routes already coincide. A third arrival must stay clear
	// of the peer's outgoing route as well as the other arrivals.
	peer := func(thirdX float64) *layoutgraph.Node {
		n := layoutgraph.NewNode(1, 100, 100)
		source := layoutgraph.NewNode(2, 100, 100)
		other := layoutgraph.NewNode(3, 100, 100)
		for i, x := range []float64{0, 0, thirdX} {
			edge := layoutgraph.NewEdge(source, n)
			edge.ID = int64(i + 1)
			edge.Points = []*geo.Point{geo.NewPoint(x, -100), geo.NewPoint(x, 0)}
			n.Edges = append(n.Edges, edge)
		}
		outgoing := layoutgraph.NewEdge(n, other)
		outgoing.ID = 4
		outgoing.Points = []*geo.Point{geo.NewPoint(80, 0), geo.NewPoint(80, -100)}
		n.Edges = append(n.Edges, outgoing)
		return n
	}
	before := peer(40)
	for _, tt := range []struct {
		x    float64
		want bool
	}{{40, true}, {79, false}, {80, false}} {
		guard, err := limits.NewWorkGuard(t.Context(), "test", 100)
		require.NoError(t, err)
		ok, err := preservesPortClearance(guard, before, peer(tt.x))
		require.NoError(t, err)
		require.Equal(t, tt.want, ok)
	}
	guard, err := limits.NewWorkGuard(t.Context(), "test", 1)
	require.NoError(t, err)
	_, err = preservesPortClearance(guard, before, before)
	require.Error(t, err)
}
