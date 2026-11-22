package layoutgraph

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// prefilterGraph builds a random nested graph with edges (some with minimum
// sizes), margins, loop offsets, labels, fixed nodes, overlaps, and sometimes
// shared top-left points, a NaN coordinate, a cluster or a sequence.
func prefilterGraph(r *rand.Rand) (*Graph, []*Node) {
	g := NewGraph()
	count := 4 + r.Intn(22)
	nodes := make([]*Node, 0, count)
	depth := map[*Node]int{}
	parentOf := map[*Node]*Node{}
	for i := 0; i < count; i++ {
		node := NewNode(EntityID(i+1), float64(20+10*r.Intn(12)), float64(20+10*r.Intn(12)))
		var parent *Node
		if len(nodes) > 0 && r.Intn(3) == 0 {
			candidate := nodes[r.Intn(len(nodes))]
			if depth[candidate] < 3 {
				parent = candidate
			}
		}
		parentOf[node] = parent
		if parent != nil {
			depth[node] = depth[parent] + 1
		}
		g.AddNewNodeToContainer(parent, node)
		nodes = append(nodes, node)
		if r.Intn(5) == 0 {
			node.margin = Spacing{top: float64(r.Intn(40)), bottom: float64(r.Intn(40)), left: float64(r.Intn(40)), right: float64(r.Intn(40))}
		}
		if r.Intn(8) == 0 {
			node.LoopOffsets = map[geo.Orientation]float64{geo.Top: float64(r.Intn(30)), geo.Left: float64(r.Intn(30)), geo.Right: float64(r.Intn(30)), geo.Bottom: float64(r.Intn(30))}
		}
		if r.Intn(4) == 0 {
			positions := []label.Position{label.InsideTopCenter, label.OutsideTopCenter, label.InsideMiddleCenter, label.OutsideBottomCenter, label.OutsideLeftMiddle}
			node.Label = &Label{Text: "l", Position: positions[r.Intn(len(positions))], Width: float64(10 + r.Intn(100)), Height: float64(10 + r.Intn(30))}
		}
		if r.Intn(12) == 0 {
			node.FixedTopLeft = geo.NewPoint(float64(10*r.Intn(5)), float64(10*r.Intn(5)))
		}
	}
	// Leaves on a coarse grid (frequent overlaps); containers wrap their
	// children, sometimes too tightly.
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		children := g.Containers[node]
		if len(children) == 0 {
			node.TopLeft = geo.NewPoint(float64(40*r.Intn(30)), float64(40*r.Intn(20)))
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, child := range children {
			minX = math.Min(minX, child.TopLeft.X)
			minY = math.Min(minY, child.TopLeft.Y)
			maxX = math.Max(maxX, child.TopLeft.X+child.Width)
			maxY = math.Max(maxY, child.TopLeft.Y+child.Height)
		}
		pad := float64(10 + 10*r.Intn(3))
		if r.Intn(6) == 0 {
			pad = -5
		}
		node.TopLeft = geo.NewPoint(minX-pad, minY-pad)
		node.Width = maxX - minX + 2*pad
		node.Height = maxY - minY + 2*pad
		if r.Intn(8) == 0 {
			// A container away from its children (a bad state to start from).
			node.TopLeft = geo.NewPoint(float64(40*r.Intn(30)), float64(40*r.Intn(20)))
		}
	}
	for e := r.Intn(2 * count); e > 0; e-- {
		a, b := nodes[r.Intn(len(nodes))], nodes[r.Intn(len(nodes))]
		if a == b {
			continue
		}
		edge := g.Connect(a, b)
		if r.Intn(3) == 0 {
			edge.MinWidth = 20 + r.Intn(100)
			edge.MinHeight = 20 + r.Intn(100)
		}
	}
	if r.Intn(10) == 0 {
		// Two leaves share one top-left point.
		a, b := nodes[r.Intn(len(nodes))], nodes[r.Intn(len(nodes))]
		if a != b && len(g.Containers[a]) == 0 && len(g.Containers[b]) == 0 {
			b.TopLeft = a.TopLeft
		}
	}
	if r.Intn(20) == 0 {
		victim := nodes[r.Intn(len(nodes))]
		if len(g.Containers[victim]) == 0 {
			victim.TopLeft = geo.NewPoint(math.NaN(), victim.TopLeft.Y)
		}
	}
	if r.Intn(10) == 0 {
		vessel := NewNode(EntityID(1000), 10, 10)
		g.AddNewNodeToContainer(nil, vessel)
		vessel.TopLeft = geo.NewPoint(float64(40*r.Intn(30)), float64(40*r.Intn(20)))
		members := []*Node{NewNode(1001, 30, 30), NewNode(1002, 40, 20)}
		for _, member := range members {
			member.TopLeft = geo.NewPoint(vessel.TopLeft.X, vessel.TopLeft.Y)
			member.Graph = g
		}
		if r.Intn(2) == 0 {
			vessel.isClusterVessel = true
			cluster := &Cluster{Vessel: vessel, Nodes: members, Graph: g, Padding: 10, Arrangement: Row, DesiredArrangement: Row}
			for _, member := range members {
				member.Cluster = cluster
			}
			g.Clusters[vessel] = cluster
			cluster.SyncGeometry()
		} else {
			sequence := &Sequence{Vessel: vessel, Nodes: members, Graph: g}
			for _, member := range members {
				member.Sequence = sequence
			}
			g.Sequences[vessel] = sequence
			sequence.SyncGeometry()
		}
		nodes = append(nodes, vessel)
	}
	return g, nodes
}

// Whenever PlacementRejected answers true, the trial's Commit rejects it.
// Trials translate random node sets (BinPack's operation) or move random
// leaves to absolute positions (transpose's), from rollback points, after
// accepted commits, after UpdateState and after g.Nodes reorders.
func TestPlacementRejectedImpliesCommitRejects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	predictions, rejections, trials := 0, 0, 0
	for seed := int64(1); seed <= 3000; seed++ {
		r := rand.New(rand.NewSource(seed))
		g, _ := prefilterGraph(r)
		options := TransactionOptions{}
		switch r.Intn(4) {
		case 0:
			options.IgnoreContainerEscape = true
		case 1:
			options.IgnoreContainerEscape = true
			options.AffectEdgeRoutes = true
		case 2:
			options.AffectContainers = true
		case 3:
			options.AffectContainers = true
			options.StableStructure = true
		}
		txn, err := g.newTransactionWithOptionsContext(ctx, options, nil)
		if err != nil {
			continue
		}
		for step := 0; step < 12; step++ {
			switch r.Intn(10) {
			case 0:
				_ = txn.UpdateState()
			case 1:
				if !options.StableStructure && len(g.Nodes) > 1 {
					i, j := r.Intn(len(g.Nodes)), r.Intn(len(g.Nodes))
					g.Nodes[i], g.Nodes[j] = g.Nodes[j], g.Nodes[i]
					if r.Intn(2) == 0 {
						_ = txn.UpdateState()
					}
				}
			case 2:
				txn.Rollback()
			}
			var moving []*Node
			var topLefts []geo.Point
			var op func() error
			if r.Intn(2) == 0 {
				// BinPack: translate a random set of nodes.
				for _, node := range g.Nodes {
					if node.TopLeft != nil && r.Intn(3) == 0 {
						moving = append(moving, node)
					}
				}
				dx, dy := float64(40*(r.Intn(21)-10)), float64(40*(r.Intn(21)-10))
				if r.Intn(5) == 0 {
					dx += 0.25
				}
				for _, node := range moving {
					topLefts = append(topLefts, geo.Point{X: node.TopLeft.X + dx, Y: node.TopLeft.Y + dy})
				}
				op = func() error {
					for _, node := range moving {
						node.Translate(dx, dy)
					}
					return nil
				}
			} else {
				// transpose: leaves to absolute positions.
				type target struct {
					node *Node
					x, y float64
				}
				var targets []target
				for _, node := range g.Nodes {
					if node.TopLeft != nil && len(g.Containers[node]) == 0 && !node.isClusterVessel && r.Intn(4) == 0 {
						x := float64(40 * r.Intn(30))
						y := float64(40 * r.Intn(20))
						targets = append(targets, target{node, x, y})
						moving = append(moving, node)
						// moveNodeAbsWithChildren's arithmetic.
						nx, ny := node.TopLeft.X, node.TopLeft.Y
						if !(nx == x && ny == y) {
							dx, dy := x-nx, y-ny
							if !(dx == 0 && dy == 0) {
								nx += dx
								ny += dy
							}
						}
						topLefts = append(topLefts, geo.Point{X: nx, Y: ny})
					}
				}
				op = func() error {
					for _, target := range targets {
						target.node.MoveAbsWithChildren(target.x, target.y)
					}
					return nil
				}
			}
			if len(moving) == 0 {
				continue
			}
			trials++
			predicted := txn.PlacementRejected(moving, topLefts)
			txn.Clear()
			txn.AddOp(op)
			commitErr := txn.Commit(ctx)
			if predicted {
				predictions++
				if !errors.Is(commitErr, ErrInvalidCandidate) {
					t.Fatalf("seed %d step %d: predicted a rejection, Commit = %v (options %+v)", seed, step, commitErr, options)
				}
			}
			if errors.Is(commitErr, ErrInvalidCandidate) {
				rejections++
			}
			if commitErr == nil {
				switch r.Intn(3) {
				case 0:
					txn.Rollback()
				case 1:
					_ = txn.UpdateState()
				}
			}
		}
	}
	t.Logf("%d trials, %d rejected, %d predicted", trials, rejections, predictions)
	if predictions < trials/10 {
		t.Fatalf("only %d predictions in %d trials (%d rejections)", predictions, trials, rejections)
	}
}

// A child moved onto its own container (which did not contain or come near it
// at the rollback point) is not an overlap: the container is its ancestor.
func TestPlacementRejectedSkipsAncestors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := NewGraph()
	P := NewNode(1, 100, 100)
	p1 := NewNode(2, 50, 50)
	g.AddNewNodeToContainer(nil, P)
	g.AddNewNodeToContainer(P, p1)
	P.TopLeft = geo.NewPoint(500, 500)
	p1.TopLeft = geo.NewPoint(0, 0)
	txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{IgnoreContainerEscape: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if txn.PlacementRejected([]*Node{p1}, []geo.Point{{X: 520, Y: 520}}) {
		t.Fatal("predicted a rejection for a child moved onto its container")
	}
	txn.AddOp(func() error { p1.Translate(520, 520); return nil })
	if err := txn.Commit(ctx); err != nil {
		t.Fatalf("Commit = %v, want the move accepted", err)
	}
}

// After g.Nodes is reordered and UpdateState is called, a dirty-path Commit
// reuses the exception index built from the old order. Here the stale entries
// at both nodes' new positions name the other node's ancestors, so Commit
// accepts p1 overlapping q1; PlacementRejected must not predict a rejection.
func TestPlacementRejectedFollowsStaleExceptionIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := NewGraph()
	Q := NewNode(1, 50, 50)
	P := NewNode(2, 50, 50)
	p1 := NewNode(3, 50, 50)
	q1 := NewNode(4, 50, 50)
	g.AddNewNodeToContainer(nil, Q)
	g.AddNewNodeToContainer(nil, P)
	g.AddNewNodeToContainer(P, p1)
	g.AddNewNodeToContainer(Q, q1)
	Q.TopLeft = geo.NewPoint(5000, 5000)
	P.TopLeft = geo.NewPoint(3000, 3000)
	p1.TopLeft = geo.NewPoint(0, 0)
	q1.TopLeft = geo.NewPoint(1000, 0)
	txn, err := g.newTransactionWithOptionsContext(ctx, TransactionOptions{IgnoreContainerEscape: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	txn.AddOp(func() error { return nil })
	if err := txn.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if g.Nodes[0] != Q || g.Nodes[1] != P || g.Nodes[2] != p1 || g.Nodes[3] != q1 {
		t.Fatalf("unexpected node order")
	}
	g.Nodes[0], g.Nodes[1], g.Nodes[2], g.Nodes[3] = p1, q1, Q, P
	if err := txn.UpdateState(); err != nil {
		t.Fatal(err)
	}
	predicted := txn.PlacementRejected([]*Node{p1}, []geo.Point{{X: 1000, Y: 0}})
	txn.Clear()
	txn.AddOp(func() error { p1.Translate(1000, 0); return nil })
	commitErr := txn.Commit(ctx)
	if commitErr != nil {
		t.Fatalf("Commit = %v; the stale index excepts both directions, so it accepts", commitErr)
	}
	if predicted {
		t.Fatal("predicted a rejection that the stale exception index prevents")
	}
}
