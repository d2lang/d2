package placementcost

import (
	"context"
	"runtime"
	"sync/atomic"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// EdgeLength's per-node evaluations are independent, and their sum is always
// formed in node order, so whether a node is evaluated by the caller or by a
// helper goroutine cannot change the result. Helpers are used only when the
// evaluation is long enough to pay for waking them.
//
// Evaluating a node costs about 0.3-0.7 µs per node or edge of the graph
// (measured over every EdgeLength call of the e2e corpus), so the work of a
// call is estimated as nodes + edges: a deterministic quantity of the input.
const (
	// edgeLengthParallelMinWork is the smallest nodes+edges count evaluated
	// with helpers (about 60 µs of work); smaller graphs are evaluated by the
	// caller alone.
	edgeLengthParallelMinWork = 128
	// edgeLengthWorkPerHelper is the nodes+edges count that justifies one
	// more helper goroutine.
	edgeLengthWorkPerHelper = 96
	// edgeLengthChunkNodes is how many consecutive nodes a participant claims
	// at a time.
	edgeLengthChunkNodes = 8
)

// edgeLengthHelpers is the number of helper goroutines for one evaluation.
// The caller always participates, and helpers are only used where the
// original implementation used workers (more than 10 nodes and GOMAXPROCS > 1).
func edgeLengthHelpers(nodeCount, edgeCount, gomaxprocs int) int {
	if edgeLengthWorkerCount(nodeCount, gomaxprocs) <= 1 {
		return 0
	}
	work := nodeCount + edgeCount
	if work < edgeLengthParallelMinWork {
		return 0
	}
	chunks := (nodeCount + edgeLengthChunkNodes - 1) / edgeLengthChunkNodes
	return max(0, min(work/edgeLengthWorkPerHelper, gomaxprocs-1, chunks-1))
}

const edgeLengthWorkerPanic = "TALA EdgeLength worker invariant failure"

// nodeEdgeLengthTerm is node n's contribution to EdgeLength. p polls the
// symmetry evaluation exactly where NodeSymmetry(ctx, ...) would (its first
// polling point is NodeSymmetry's entry check) and carries the evaluation's
// symmetry workspace and memo.
func nodeEdgeLengthTerm(ctx context.Context, p *scoringPoll, n *layoutgraph.Node, options EdgeLengthOptions, symmetryCost float64) (float64, error) {
	nl, err := NodeEdgeLength(ctx, n, options)
	if err != nil {
		return 0, err
	}
	if options.IncludeNodeSizes {
		columnCrossingCost, err := ColumnCrossingCost(ctx, n, options.EdgeAbductions)
		if err != nil {
			return 0, err
		}
		symmetry, err := memoNodeSymmetry(p, n, options.EdgeAbductions, true)
		if err != nil {
			return 0, err
		}
		nl = edgeLengthTermValue(nl, columnCrossingCost, symmetry, symmetryCost, len(n.Edges))
	}
	return nl, nil
}

// edgeLengthTermValue combines a node's parts into its EdgeLength term. Every
// evaluation path (including EdgeLengthSession) combines through this one
// non-inlined function, so the floating-point operations, and any fused
// multiply-subtract the compiler forms from them, are the same everywhere.
//
//go:noinline
func edgeLengthTermValue(nl, columnCrossingCost, symmetry, symmetryCost float64, edges int) float64 {
	nl += columnCrossingCost
	nl -= symmetry * symmetryCost * float64(edges)
	return nl
}

// sumNodeEdgeLengths sums every node's EdgeLength term in node order.
func sumNodeEdgeLengths(ctx context.Context, p *scoringPoll, graph *layoutgraph.Graph, options EdgeLengthOptions) (float64, error) {
	gomaxprocs := runtime.GOMAXPROCS(0)
	if helpers := edgeLengthHelpers(len(graph.Nodes), len(graph.Edges), gomaxprocs); helpers > 0 {
		return sumNodeEdgeLengthsShared(ctx, graph, options, helpers)
	}
	// Where the original implementation used workers, a panic while
	// evaluating nodes was reported only as an invariant failure; keep that.
	sanitize := edgeLengthWorkerCount(len(graph.Nodes), gomaxprocs) > 1
	return sumNodeEdgeLengthsInline(ctx, p, graph, options, sanitize)
}

func sumNodeEdgeLengthsInline(ctx context.Context, p *scoringPoll, graph *layoutgraph.Graph, options EdgeLengthOptions, sanitize bool) (totalSum float64, err error) {
	if sanitize {
		defer func() {
			if recovered := recover(); recovered != nil {
				// A recovered value can contain credentials or invoke an
				// attacker-controlled String method. Retain only the fact
				// that an evaluation violated an invariant.
				panic(edgeLengthWorkerPanic)
			}
		}()
	}
	symmetryCost := graph.CellSize
	p.symmetry = getSymmetryWorkspace()
	defer func() {
		putSymmetryWorkspace(p.symmetry)
		p.symmetry = nil
	}()
	p.symmetry.memoOn = p.done != nil
	for _, n := range graph.Nodes {
		nl, err := nodeEdgeLengthTerm(ctx, p, n, options, symmetryCost)
		if err != nil {
			return 0, err
		}
		if err := p.check(); err != nil {
			return 0, err
		}
		totalSum += nl
	}
	return totalSum, nil
}

// edgeLengthBatch shares one EdgeLength evaluation between the caller and
// helper goroutines. Participants claim chunks of consecutive nodes until none
// are left, so the caller never waits for a helper that has not started; it
// waits only for chunks that were claimed and are still being evaluated.
type edgeLengthBatch struct {
	ctx          context.Context
	nodes        []*layoutgraph.Node
	options      EdgeLengthOptions
	symmetryCost float64
	costs        []float64
	errs         []error // per chunk
	chunks       int64
	next         atomic.Int64
	pending      atomic.Int64
	spawn        atomic.Int64
	panicked     atomic.Bool
	done         chan struct{}
}

func sumNodeEdgeLengthsShared(ctx context.Context, graph *layoutgraph.Graph, options EdgeLengthOptions, helpers int) (float64, error) {
	chunks := (len(graph.Nodes) + edgeLengthChunkNodes - 1) / edgeLengthChunkNodes
	b := &edgeLengthBatch{
		ctx:          ctx,
		nodes:        graph.Nodes,
		options:      options,
		symmetryCost: graph.CellSize,
		costs:        make([]float64, len(graph.Nodes)),
		errs:         make([]error, chunks),
		chunks:       int64(chunks),
		done:         make(chan struct{}),
	}
	b.pending.Store(int64(chunks))
	b.spawn.Store(int64(helpers))
	// Each helper starts the next one, so the caller pays for waking only one.
	b.startHelper()
	b.work()
	<-b.done

	if b.panicked.Load() {
		panic(edgeLengthWorkerPanic)
	}
	for _, err := range b.errs {
		if err != nil {
			return 0, err
		}
	}
	var totalSum float64
	for _, c := range b.costs {
		totalSum += c
	}
	return totalSum, nil
}

func (b *edgeLengthBatch) startHelper() {
	if b.spawn.Add(-1) >= 0 {
		go func() {
			b.startHelper()
			b.work()
		}()
	}
}

func (b *edgeLengthBatch) work() {
	p := newScoringPoll(b.ctx)
	p.symmetry = getSymmetryWorkspace()
	defer putSymmetryWorkspace(p.symmetry)
	p.symmetry.memoOn = p.done != nil
	for {
		chunk := b.next.Add(1) - 1
		if chunk >= b.chunks {
			return
		}
		b.runChunk(&p, int(chunk))
		if b.pending.Add(-1) == 0 {
			close(b.done)
		}
	}
}

func (b *edgeLengthBatch) runChunk(p *scoringPoll, chunk int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// See sumNodeEdgeLengthsInline: keep only the fact of the failure.
			b.panicked.Store(true)
		}
	}()
	start := chunk * edgeLengthChunkNodes
	end := min(start+edgeLengthChunkNodes, len(b.nodes))
	for i := start; i < end; i++ {
		nl, err := nodeEdgeLengthTerm(b.ctx, p, b.nodes[i], b.options, b.symmetryCost)
		if err != nil {
			b.errs[chunk] = err
			return
		}
		if err := p.check(); err != nil {
			b.errs[chunk] = err
			return
		}
		b.costs[i] = nl
	}
}
