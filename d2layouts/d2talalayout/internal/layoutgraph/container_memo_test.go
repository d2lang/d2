package layoutgraph

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// memoGraphGeometry lists every node's geometry bits in a deterministic
// order (g.Nodes, then container keys' children in g.Nodes order).
func memoGraphGeometry(g *Graph) []uint64 {
	var out []uint64
	add := func(node *Node) {
		if node == nil || node.TopLeft == nil {
			out = append(out, 1<<63)
			return
		}
		out = append(out, math.Float64bits(node.TopLeft.X), math.Float64bits(node.TopLeft.Y),
			math.Float64bits(node.Width), math.Float64bits(node.Height))
	}
	for _, node := range g.Nodes {
		add(node)
		for _, child := range g.Containers[node] {
			add(child)
		}
	}
	return out
}

// spreadContainerGraph builds well-separated nested containers (wrapped
// around their children) and free nodes, so that small moves are mostly
// accepted, with some neighbours inside the 500-unit broad phase.
func spreadContainerGraph(rng *rand.Rand) *Graph {
	g := NewGraph()
	id := EntityID(1)
	newNode := func(parent *Node, x, y float64) *Node {
		node := NewNode(id, float64(30+rng.Intn(40)), float64(30+rng.Intn(40)))
		id++
		node.TopLeft = geo.NewPoint(x, y)
		g.AddNewNodeToContainer(parent, node)
		return node
	}
	var all, containers []*Node
	groups := 2 + rng.Intn(4)
	for group := 0; group < groups; group++ {
		baseX := float64(group*(400+rng.Intn(500))) + 0.5*float64(rng.Intn(2))
		baseY := float64(rng.Intn(300))
		container := newNode(nil, baseX, baseY)
		container.isContainer = true
		containers = append(containers, container)
		all = append(all, container)
		var inner *Node
		if rng.Intn(2) == 0 {
			inner = newNode(container, baseX+40, baseY+200)
			inner.isContainer = true
			containers = append(containers, inner)
			all = append(all, inner)
			for i := 0; i < 1+rng.Intn(2); i++ {
				all = append(all, newNode(inner, baseX+60+float64(i*120), baseY+220))
			}
		}
		for i := 0; i < 2+rng.Intn(3); i++ {
			all = append(all, newNode(container, baseX+40+float64(i*120), baseY+40))
		}
	}
	for i := 0; i < 1+rng.Intn(3); i++ {
		all = append(all, newNode(nil, float64(rng.Intn(groups*700)), 900+float64(rng.Intn(300))))
	}
	// Wrap innermost first.
	for i := len(containers) - 1; i >= 0; i-- {
		containers[i].wrapChildren()
	}
	for i := 0; i < len(all); i++ {
		from, to := all[rng.Intn(len(all))], all[rng.Intn(len(all))]
		if from != to && rng.Intn(2) == 0 {
			g.Connect(from, to)
		}
	}
	return g
}

// TestStableStructureCommitsMatchLegacy runs the same random sequence of
// moves, commits, rollbacks and state updates on two copies of a random
// nested layout, one transaction with StableStructure (container memo) and
// one without, and compares every commit's error and the resulting geometry,
// and the units of every accepted commit. (A rejected commit's units depend
// on the map order in which the legacy loop meets the first bad container,
// which differs between the two copies' maps.)
func TestStableStructureCommitsMatchLegacy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	commits, rejected := 0, 0
	var clean, rescan, full, wraps int
	for seed := int64(0); seed < 250; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			type side struct {
				g     *Graph
				guard *limits.WorkGuard
				txn   *Transaction
			}
			sides := make([]side, 2)
			for i := range sides {
				var g *Graph
				if seed%2 == 0 {
					g = randomContainerGraph(rand.New(rand.NewSource(seed)))
				} else {
					g = spreadContainerGraph(rand.New(rand.NewSource(seed)))
				}
				guard, err := limits.NewWorkGuard(ctx, "MemoTest", maxTransactionWorkUnits)
				require.NoError(t, err)
				options := TransactionOptions{AffectContainers: true, StableStructure: i == 1}
				txn, err := g.newTransactionWithOptionsContext(ctx, options, guard)
				require.NoError(t, err)
				sides[i] = side{g: g, guard: guard, txn: txn}
			}
			require.NotNil(t, sides[1].txn.memo)
			rng := rand.New(rand.NewSource(seed * 7919))
			for step := 0; step < 40; step++ {
				index := rng.Intn(len(sides[0].g.Nodes))
				dx := float64(rng.Intn(300) - 150)
				dy := float64(rng.Intn(300) - 150)
				if seed%2 == 1 {
					dx, dy = float64(rng.Intn(41)-20), float64(rng.Intn(41)-20)
				}
				if rng.Intn(4) == 0 {
					dx, dy = 0, 0
				}
				action := rng.Intn(3)
				var errs [2]error
				var used [2]int64
				for i := range sides {
					s := sides[i]
					guard, err := limits.NewWorkGuard(ctx, "MemoTest", maxTransactionWorkUnits)
					require.NoError(t, err)
					s.txn.guard = guard
					node := s.g.Nodes[index]
					s.txn.AddOp(func() error {
						node.MoveWithChildren(dx, dy)
						return nil
					})
					errs[i] = s.txn.Commit(ctx)
					used[i] = guard.Used()
					s.txn.Clear()
					if errs[i] == nil {
						switch action {
						case 0:
							require.NoError(t, s.txn.UpdateState())
						default:
							s.txn.Rollback()
						}
					}
				}
				if errs[0] == nil || errs[1] == nil {
					require.Equal(t, errs[0], errs[1])
				} else {
					require.Equal(t, errs[0].Error(), errs[1].Error())
				}
				if errs[0] == nil {
					require.Equal(t, used[0], used[1], "units of step %d", step)
				}
				require.Equal(t, memoGraphGeometry(sides[0].g), memoGraphGeometry(sides[1].g), "geometry after step %d", step)
				commits++
				if errs[0] != nil {
					rejected++
				}
			}
			hits := sides[1].txn.memo.hits
			clean, rescan, full, wraps = clean+hits.clean, rescan+hits.rescan, full+hits.full, wraps+hits.wrap
			sides[1].txn.Release()
		})
	}
	require.Positive(t, rejected)
	require.Positive(t, clean, "memo never reused a check")
	require.Positive(t, rescan, "memo never rescanned a check")
	require.Positive(t, full)
	require.Positive(t, wraps, "memo never reused a wrap")
	t.Logf("commits %d rejected %d; checks clean %d rescan %d full %d; wraps reused %d", commits, rejected, clean, rescan, full, wraps)
	require.Greater(t, commits-rejected, commits/5, "too few accepted commits to exercise the memo")
}

// TestCommitFastPathsMatchDoneLessContext runs the same random transaction
// sequences under a context with a Done channel (fused dirty/box pass,
// container memo) and under context.Background (whose nil Done channel keeps
// every legacy loop), comparing errors, geometry and the units of accepted
// commits, for BinPack-like (IgnoreContainerEscape) and container-affecting
// transactions.
func TestCommitFastPathsMatchDoneLessContext(t *testing.T) {
	doneCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	accepted := 0
	for seed := int64(0); seed < 300; seed++ {
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			options := TransactionOptions{IgnoreContainerEscape: true}
			switch seed % 3 {
			case 1:
				options = TransactionOptions{AffectContainers: true, StableStructure: true}
			case 2:
				options = TransactionOptions{AffectContainers: true}
			}
			contexts := []context.Context{doneCtx, context.Background()}
			type side struct {
				g   *Graph
				txn *Transaction
			}
			sides := make([]side, 2)
			for i := range sides {
				var g *Graph
				if seed%2 == 0 {
					g = randomContainerGraph(rand.New(rand.NewSource(seed)))
				} else {
					g = spreadContainerGraph(rand.New(rand.NewSource(seed)))
				}
				guard, err := limits.NewWorkGuard(contexts[i], "FastPathTest", maxTransactionWorkUnits)
				require.NoError(t, err)
				txn, err := g.newTransactionWithOptionsContext(contexts[i], options, guard)
				require.NoError(t, err)
				sides[i] = side{g: g, txn: txn}
			}
			rng := rand.New(rand.NewSource(seed * 104729))
			for step := 0; step < 30; step++ {
				index := rng.Intn(len(sides[0].g.Nodes))
				dx, dy := float64(rng.Intn(41)-20), float64(rng.Intn(41)-20)
				if seed%2 == 0 {
					dx, dy = float64(rng.Intn(300)-150), float64(rng.Intn(300)-150)
				}
				action := rng.Intn(3)
				var errs [2]error
				var used [2]int64
				for i := range sides {
					s := sides[i]
					guard, err := limits.NewWorkGuard(contexts[i], "FastPathTest", maxTransactionWorkUnits)
					require.NoError(t, err)
					s.txn.guard = guard
					node := s.g.Nodes[index]
					s.txn.AddOp(func() error {
						node.MoveWithChildren(dx, dy)
						return nil
					})
					errs[i] = s.txn.Commit(contexts[i])
					used[i] = guard.Used()
					s.txn.Clear()
					if errs[i] == nil {
						if action == 0 {
							require.NoError(t, s.txn.UpdateState())
						} else {
							s.txn.Rollback()
						}
					}
				}
				if errs[0] == nil || errs[1] == nil {
					require.Equal(t, errs[0], errs[1], "step %d", step)
				} else {
					require.Equal(t, errs[0].Error(), errs[1].Error(), "step %d", step)
				}
				if errs[0] == nil {
					accepted++
					require.Equal(t, used[0], used[1], "units of step %d", step)
				}
				require.Equal(t, memoGraphGeometry(sides[0].g), memoGraphGeometry(sides[1].g), "geometry after step %d", step)
			}
		})
	}
	require.Greater(t, accepted, 1000)
}

// TestReleasedCloneKeepsSourceOverlapSet checks that releasing a geometry
// clone that detached from its source (UpdateState) recycles only its own
// states: the existing-overlap set stays with the source.
func TestReleasedCloneKeepsSourceOverlapSet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := spreadContainerGraph(rand.New(rand.NewSource(3)))
	txn, err := g.newRequestTransaction(ctx, TransactionOptions{AffectContainers: true})
	require.NoError(t, err)
	set := txn.PriorGraphState.existingOverlaps
	require.NotNil(t, set)
	clone, err := txn.CloneGeometryContext()
	require.NoError(t, err)
	require.NoError(t, clone.UpdateState())
	require.NoError(t, clone.UpdateState())
	require.Same(t, set, clone.PriorGraphState.existingOverlaps)
	clone.Release()
	for _, spare := range g.spareOverlapSets {
		require.NotSame(t, set, spare)
	}
	require.NotEmpty(t, g.spareGraphStates)
	other, err := g.newRequestTransaction(ctx, TransactionOptions{})
	require.NoError(t, err)
	require.NotSame(t, set, other.PriorGraphState.existingOverlaps)
	require.Same(t, set, txn.PriorGraphState.existingOverlaps)
}
