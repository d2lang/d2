package placement

import (
	"cmp"
	"context"
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// newGapReplayGraph builds a sparse graph: many unconnected boxes (whose gap
// reduction is a no-op), a few far-apart connected chains (which move), and
// optionally a container with connected children.
func newGapReplayGraph(seed int64, withContainer bool) *layoutgraph.Graph {
	random := rand.New(rand.NewSource(seed))
	graph := layoutgraph.NewGraph()
	id := layoutgraph.EntityID(1)
	var nodes []*layoutgraph.Node
	for row := 0; row < 6; row++ {
		for col := 0; col < 8; col++ {
			node := layoutgraph.NewNode(id, float64(20+random.Intn(40)), float64(20+random.Intn(40)))
			id++
			node.TopLeft = geo.NewPoint(float64(col*400+random.Intn(3)*10), float64(row*400+random.Intn(3)*10))
			graph.AddNodeUnchecked(node)
			nodes = append(nodes, node)
		}
	}
	for i := 0; i < 8; i++ {
		a := nodes[random.Intn(len(nodes))]
		b := nodes[random.Intn(len(nodes))]
		if a != b {
			graph.Connect(a, b)
		}
	}
	if withContainer {
		container := layoutgraph.NewNode(id, 10, 10)
		id++
		container.TopLeft = geo.NewPoint(4000, 0)
		graph.AddNodeUnchecked(container)
		var children []*layoutgraph.Node
		for i := 0; i < 3; i++ {
			child := layoutgraph.NewNode(id, 30, 30)
			id++
			child.TopLeft = geo.NewPoint(4100+float64(i)*600, 100)
			graph.AddNewNodeToContainer(container, child)
			children = append(children, child)
		}
		graph.Connect(children[0], children[1])
		container.TopLeft = geo.NewPoint(4050, 50)
		container.Width = 1300
		container.Height = 200
	}
	for _, node := range graph.Nodes {
		node.Graph = graph
	}
	graph.ComputeCellSize()
	return graph
}

func TestGapNormalizationNoopReplayIsUsed(t *testing.T) {
	graph := newGapReplayGraph(1, true)
	ctx, guard, err := layoutgraph.EnsureTransactionWorkGuard(context.Background(), "GapReplayTest")
	if err != nil {
		t.Fatal(err)
	}
	txn, err := graph.NewRequestTransaction(ctx, layoutgraph.TransactionOptions{AffectContainers: true})
	if err != nil {
		t.Fatal(err)
	}
	noops := &gapNoopReplay{guard: guard}
	for _, axis := range []layoutAxis{horizontalAxis, verticalAxis} {
		for _, direction := range []traversalDirection{forwardDirection, backwardDirection} {
			if _, err := gapNormalization(ctx, layoutgraph.Nodes(graph.Nodes), txn, graph, gapNormalizationOptions{
				axis: axis, direction: direction, costTxn: txn, noops: noops,
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if noops.replayed == 0 {
		t.Fatal("no no-op iteration was replayed")
	}
}

// gapNormalizationBaseline is gapNormalization exactly as it was before the
// no-op replay: the operation runs inside the outer Commit.
func gapNormalizationBaseline(ctx context.Context, nodes layoutgraph.Nodes, txn *layoutgraph.Transaction, g *layoutgraph.Graph, options gapNormalizationOptions) (bool, error) {
	ctx, _, err := layoutgraph.EnsureTransactionWorkGuard(ctx, "GapNormalizationTransactions")
	if err != nil {
		return false, err
	}
	sortedByConnections := make([]*layoutgraph.Node, len(nodes))
	copy(sortedByConnections, nodes)
	slices.SortFunc(sortedByConnections, func(a, b *layoutgraph.Node) int {
		if order := cmp.Compare(len(b.Edges), len(a.Edges)); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	changed := false
	for _, node := range sortedByConnections {
		if _, in := g.NodeToTree[node]; in {
			continue
		}
		if node.Hierarchy != nil {
			continue
		}
		candidateTxn, err := txn.CloneGeometryContext()
		if err != nil {
			return changed, err
		}
		txn.AddOp(func() error {
			_, _, err := reduceGapToNeighbors(ctx, node, candidateTxn, gapReductionOptions{
				axis:                   options.axis,
				direction:              options.direction,
				attemptRecoverSymmetry: true,
				costTxn:                options.costTxn,
			})
			return err
		})
		if err := txn.Commit(ctx); err != nil {
			if !layoutgraph.IsCandidateRejection(err) {
				return changed, err
			}
		} else {
			if err := txn.UpdateState(); err != nil {
				return changed, err
			}
			changed = true
		}
		txn.Clear()
	}
	return changed, nil
}

// normalizeGapsBaseline is NormalizeGaps exactly as it was before the no-op
// replay.
func normalizeGapsBaseline(ctx context.Context, graph *layoutgraph.Graph) (bool, error) {
	ctx, guard, err := layoutgraph.EnsureTransactionWorkGuard(ctx, "GapNormalizationTransactions")
	if err != nil {
		return false, err
	}
	txn, err := graph.NewRequestTransaction(ctx, layoutgraph.TransactionOptions{AffectContainers: true})
	if err != nil {
		return false, err
	}
	rollback := normalizeGapsRollback{
		graph:        graph,
		txn:          txn,
		graphState:   txn.PreservePriorGraphState(),
		cellSize:     graph.CellSize,
		routingCosts: graph.RoutingCosts(),
	}
	complete := false
	defer func() {
		if !complete {
			rollback.restore()
		}
	}()
	bidirectional := func(nodes layoutgraph.Nodes, axis layoutAxis) (bool, error) {
		forward, err := gapNormalizationBaseline(ctx, nodes, txn, graph, gapNormalizationOptions{
			axis: axis, direction: forwardDirection, costTxn: txn,
		})
		if err != nil {
			return false, err
		}
		backward, err := gapNormalizationBaseline(ctx, nodes, txn, graph, gapNormalizationOptions{
			axis: axis, direction: backwardDirection, costTxn: txn,
		})
		return forward || backward, err
	}
	changed := false
	containers, err := graph.ContainerRDFSOrder(nil, guard)
	if err != nil {
		return false, err
	}
	for _, container := range containers {
		nodes := layoutgraph.Nodes(graph.AllDescendantNodes(container, false))
		for _, axis := range []layoutAxis{horizontalAxis, verticalAxis} {
			current, err := bidirectional(nodes, axis)
			if err != nil {
				return false, err
			}
			changed = changed || current
		}
	}
	for _, axis := range []layoutAxis{horizontalAxis, verticalAxis} {
		current, err := bidirectional(layoutgraph.Nodes(graph.Nodes), axis)
		if err != nil {
			return false, err
		}
		changed = changed || current
	}
	graph.ResetTurnCost()
	graph.SyncSequences()
	graph.SyncClusters()
	if err := guard.Finish(); err != nil {
		return false, err
	}
	complete = true
	return changed, nil
}

func TestGapNormalizationNoopReplayMatchesBaseline(t *testing.T) {
	type outcome struct {
		graph   *layoutgraph.Graph
		changed bool
		units   int64
	}
	for _, withContainer := range []bool{false, true} {
		for seed := int64(1); seed <= 8; seed++ {
			run := func(normalize func(context.Context, *layoutgraph.Graph) (bool, error)) outcome {
				graph := newGapReplayGraph(seed, withContainer)
				ctx, guard, err := layoutgraph.EnsureTransactionWorkGuard(context.Background(), "GapReplayTest")
				if err != nil {
					t.Fatal(err)
				}
				changed, err := normalize(ctx, graph)
				if err != nil {
					t.Fatal(err)
				}
				return outcome{graph: graph, changed: changed, units: guard.Used()}
			}
			baseline := run(normalizeGapsBaseline)
			for _, replay := range []bool{false, true} {
				got := run(func(ctx context.Context, graph *layoutgraph.Graph) (bool, error) {
					return normalizeGaps(ctx, graph, replay)
				})
				if baseline.changed != got.changed || baseline.units != got.units {
					t.Fatalf("seed %d container %v replay %v: changed %v/%v units %d/%d",
						seed, withContainer, replay, baseline.changed, got.changed, baseline.units, got.units)
				}
				if len(baseline.graph.Nodes) != len(got.graph.Nodes) {
					t.Fatalf("seed %d: node count differs", seed)
				}
				for i, node := range baseline.graph.Nodes {
					other := got.graph.Nodes[i]
					for _, pair := range [][2]float64{
						{node.TopLeft.X, other.TopLeft.X}, {node.TopLeft.Y, other.TopLeft.Y},
						{node.Width, other.Width}, {node.Height, other.Height},
					} {
						if math.Float64bits(pair[0]) != math.Float64bits(pair[1]) {
							t.Fatalf("seed %d container %v replay %v node %d: geometry differs",
								seed, withContainer, replay, node.ID)
						}
					}
				}
			}
		}
	}
}

// nearestBetweenConnected must pick what nearestBetween picks over the ordered
// ConnectedNodes list, including when keys tie (it then defers to that list).
func TestNearestBetweenConnectedMatchesOrderedList(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		random := rand.New(rand.NewSource(seed))
		graph := layoutgraph.NewGraph()
		id := layoutgraph.EntityID(1)
		var containers []*layoutgraph.Node
		for range 3 {
			container := layoutgraph.NewNode(id, 400, 400)
			id++
			container.TopLeft = geo.NewPoint(float64(random.Intn(4))*500, 0)
			graph.AddNodeUnchecked(container)
			containers = append(containers, container)
		}
		var nodes []*layoutgraph.Node
		for range 45 {
			node := layoutgraph.NewNode(id, 20, 20)
			id++
			// A coarse grid makes equal coordinates, i.e. ties, common.
			node.TopLeft = geo.NewPoint(float64(random.Intn(8))*50, float64(random.Intn(8))*50)
			if parent := random.Intn(4); parent < 3 {
				graph.AddNewNodeToContainer(containers[parent], node)
			} else {
				graph.AddNodeUnchecked(node)
			}
			nodes = append(nodes, node)
		}
		for range 60 {
			a, b := nodes[random.Intn(len(nodes))], nodes[random.Intn(len(nodes))]
			if a != b {
				graph.Connect(a, b)
			}
		}
		for _, node := range graph.Nodes {
			node.Graph = graph
		}
		for range 30 {
			behind, ahead := nodes[random.Intn(len(nodes))], nodes[random.Intn(len(nodes))]
			if behind == ahead {
				continue
			}
			excluded := []*layoutgraph.Node{behind}
			if random.Intn(3) == 0 {
				excluded = append(excluded, nodes[random.Intn(len(nodes))])
			}
			inContainer := behind.NearestSharedAncestor(ahead)
			for _, isHorizontal := range []bool{true, false} {
				for _, forwards := range []bool{true, false} {
					want := nearestBetween(layoutgraph.Nodes(ahead.ConnectedNodes(excluded, graph)), behind, ahead, inContainer, isHorizontal, forwards)
					got := nearestBetweenConnected(ahead, excluded, graph, behind, inContainer, isHorizontal, forwards)
					if got != want {
						t.Fatalf("seed %d: nearest %v, want %v", seed, got, want)
					}
				}
			}
		}
	}
}
