package layoutgraph

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// randomContainerGraph builds nested containers with overlapping and
// separate children, some nodes outside g.Nodes and some edges.
func randomContainerGraph(rng *rand.Rand) *Graph {
	g := NewGraph()
	var all []*Node
	var containers []*Node
	id := EntityID(1)
	count := 4 + rng.Intn(40)
	for i := 0; i < count; i++ {
		node := NewNode(id, float64(20+rng.Intn(200)), float64(20+rng.Intn(200)))
		id++
		node.TopLeft = geo.NewPoint(float64(rng.Intn(1500)), float64(rng.Intn(1500)))
		var parent *Node
		if len(containers) > 0 && rng.Intn(3) != 0 {
			parent = containers[rng.Intn(len(containers))]
		}
		if rng.Intn(6) != 0 {
			g.AddNewNodeToContainer(parent, node)
		} else {
			// Reachable only through the container map.
			g.AddNodeToContainer(parent, node)
			node.Graph = g
		}
		all = append(all, node)
		if rng.Intn(3) == 0 {
			containers = append(containers, node)
			node.isContainer = true
		}
	}
	for i := 0; i < count; i++ {
		from, to := all[rng.Intn(len(all))], all[rng.Intn(len(all))]
		if from == to {
			continue
		}
		edge := g.Connect(from, to)
		edge.MinWidth = rng.Intn(120)
		edge.MinHeight = rng.Intn(120)
	}
	for _, node := range all {
		if rng.Intn(5) == 0 {
			node.margin = Spacing{top: float64(rng.Intn(40)), left: float64(rng.Intn(40))}
		}
	}
	return g
}

// TestContainerOverlapCheckMatchesGraphCheck compares the transaction's
// mark-based container check with hasBadOverlapStateContext: verdict, error
// and units, for every container of random nested layouts, also after the
// snapshot hints were overwritten by another snapshot.
func TestContainerOverlapCheckMatchesGraphCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	checks, bad := 0, 0
	for seed := int64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			g := randomContainerGraph(rng)
			guard, err := limits.NewWorkGuard(ctx, "ContainerCheckTest", maxTransactionWorkUnits)
			require.NoError(t, err)
			txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{AffectContainers: true}, guard)
			require.NoError(t, err)
			if rng.Intn(2) == 0 {
				// Another snapshot rewrites every hint.
				other := &GraphState{}
				require.NoError(t, other.updateContext(g, guard))
			}
			for _, node := range g.Nodes {
				if rng.Intn(4) == 0 {
					node.TopLeft.X += float64(rng.Intn(200) - 100)
				}
			}
			txn.beginContainerPass()
			for container := range g.Containers {
				if container == nil || container.TopLeft == nil {
					continue
				}
				runGuard, err := limits.NewWorkGuard(ctx, "ContainerCheckRun", maxTransactionWorkUnits)
				require.NoError(t, err)
				txn.guard = runGuard
				gotBad, gotErr := txn.containerOverlapBad(container)
				legacyGuard, err := limits.NewWorkGuard(ctx, "ContainerCheckRun", maxTransactionWorkUnits)
				require.NoError(t, err)
				wantBad, wantErr := g.hasBadOverlapStateContext(container, txn.PriorGraphState, legacyGuard)
				require.Equal(t, wantErr, gotErr)
				require.Equal(t, wantBad, gotBad)
				require.Equal(t, legacyGuard.Used(), runGuard.Used())
				checks++
				if wantBad {
					bad++
				}
			}
		})
	}
	require.Positive(t, checks)
	require.Positive(t, bad, "fixtures never produced an overlapping container")
}
