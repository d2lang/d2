package placementcost

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

type edgeLengthStatic struct {
	direction       geo.Orientation
	directionFactor float64
}

// NodeEdgeLengthScorer reuses incident-edge topology during one candidate sweep.
// Geometry is read from live nodes on every Score. The caller must keep edges,
// abductions, container/cluster membership, labels and directions unchanged until
// Close. In particular, a scorer must not survive a topology-changing swap.
// It is local mutable scratch and must not be shared between goroutines.
type NodeEdgeLengthScorer struct {
	node       *layoutgraph.Node
	options    EdgeLengthOptions
	scratch    *edgeScratch
	setup      edgeLengthStatic
	prepChecks int
	sweep      bool
	// Set by ScoreUnlessAbove for the Score call it makes.
	exitArmed             bool
	exitOffset, exitBound float64
}

// NewNodeEdgeLengthScorer starts an unprepared sweep. Preparation is lazy so
// cancellation and the caller's work charge occur at the original score point.
// The returned value must not be copied after its first Score; call Close when
// the sweep ends to release references retained by its pooled scratch.
func NewNodeEdgeLengthScorer(node *layoutgraph.Node, options EdgeLengthOptions) NodeEdgeLengthScorer {
	return NodeEdgeLengthScorer{node: node, options: options}
}

// NewNodeEdgeLengthSweepScorer is NewNodeEdgeLengthScorer for a sweep that
// moves only node (with its descendants) between Score calls, the way the
// optimizer tries candidate positions; the caller guarantees that no other
// node's geometry changes until Close. Each edge's obstacle scan is then
// filtered once and the boxes of the obstacles that cannot move are kept.
// Score returns the same values; contexts without a Done channel observe the
// same polling points as NodeEdgeLength.
func NewNodeEdgeLengthSweepScorer(node *layoutgraph.Node, options EdgeLengthOptions) NodeEdgeLengthScorer {
	return NodeEdgeLengthScorer{node: node, options: options, sweep: true}
}

// Score has the same value and cancellation checkpoints as NodeEdgeLength while
// the scorer's topology remains fixed. The caller still charges the original
// scoring work budget for each candidate; reusing setup does not bypass limits.
func (scorer *NodeEdgeLengthScorer) Score(ctx context.Context) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	if scorer.node == nil {
		return 0, fmt.Errorf("EdgeLength: scorer is closed")
	}
	if scorer.scratch == nil {
		s := scratchPool.Get()
		// Publish ownership before preparation so a caller's deferred Close also
		// releases scratch if a malformed graph or context panics during setup.
		scorer.scratch = s
		before := p.checks
		setup, err := prepareNodeEdgeLength(&p, scorer.node, scorer.options, s)
		if err != nil {
			putEdgeScratch(s)
			scorer.scratch = nil
			return 0, err
		}
		scorer.scratch, scorer.setup, scorer.prepChecks = s, setup, p.checks-before
	} else if p.done == nil {
		// Preparation only inspects topology. Replay its exact polls without
		// repeating endpoint matching and label-count construction. This keeps
		// cancellation observability equal even for a context counting Err calls.
		// (A context with a Done channel observes no individual polls; see
		// scoringPoll.)
		for range scorer.prepChecks {
			if err := p.check(); err != nil {
				return 0, err
			}
		}
	}
	// The sweep path polls differently, so contexts that observe every
	// polling point (no Done channel) use the plain scan.
	scorer.scratch.sweepOn = scorer.sweep && p.done != nil
	scorer.scratch.sweepMover = scorer.node
	s := scorer.scratch
	s.exitOn = scorer.exitArmed && p.done != nil
	s.exitOffset, s.exitBound = scorer.exitOffset, scorer.exitBound
	value, err := evaluateNodeEdgeLength(&p, scorer.node, scorer.options, s, scorer.setup)
	s.exitOn = false
	return value, err
}

// ScoreUnlessAbove is Score for a caller that discards the candidate when
// geo.PrecisionCompare(score-offset, bound, geo.PRECISION) == 1 and bound is
// not +Inf. In that case it may stop as soon as the running sum compares
// above, either after an edge or already when an edge's base distance (before
// its obstacle scan and penalties, which only add to it) is added, and return
// that partial sum: the caller then decides the same, and otherwise the value
// is Score's.
//
// Stopping is exact because every contribution still to come is non-negative
// (or NaN, which compares above): each edge's cost is clamped at the minimum
// gap (0 or the cell size) and then only gains direction and label penalties
// made of non-negative factors; nears add distances, herd fences add d or
// cell size - d for d < 0, flow continuity adds the turn cost times a
// non-negative factor. That needs a non-negative cell size, a turn cost that
// is already initialized (it is derived from the geometry on first use, and a
// skipped first use would move that initialization), a crossing cost that is
// initialized unless the node has no table-column edge (only those read it),
// and no common-uncle term (it can be negative); else the full score is
// computed. Both costs are non-negative once initialized (products of
// positive weights, edge counts and the longest edge). A partial sum only grows when non-negative values
// are added (rounding is monotone), so the full score minus offset is at
// least the partial one and compares above bound too.
func (scorer *NodeEdgeLengthScorer) ScoreUnlessAbove(ctx context.Context, offset, bound float64) (float64, error) {
	if scorer.node == nil || !scorer.options.IncludeNodeSizes || math.IsInf(bound, 1) || ctx.Done() == nil || !scorer.nonNegativeRest() {
		return scorer.Score(ctx)
	}
	scorer.exitOffset, scorer.exitBound, scorer.exitArmed = offset, bound, true
	defer func() { scorer.exitArmed = false }()
	return scorer.Score(ctx)
}

// nonNegativeRest reports whether every contribution of the node's score
// after an edge is non-negative (see ScoreUnlessAbove).
func (scorer *NodeEdgeLengthScorer) nonNegativeRest() bool {
	graph := scorer.node.Graph
	if graph == nil || !(graph.CellSize >= 0) {
		return false
	}
	if _, uncle := graph.CommonUncleSiblings[scorer.node]; uncle {
		return false
	}
	costs := graph.RoutingCosts()
	if !(costs.Turn > 0) || !(costs.Crossing >= 0) {
		return false
	}
	if costs.Crossing == 0 {
		for _, e := range scorer.node.Edges {
			if e.IsBetweenTableColumns() {
				return false
			}
		}
	}
	return true
}

// Close releases scratch and makes the scorer unusable. It is safe to call more
// than once, including after an unsuccessful Score.
func (scorer *NodeEdgeLengthScorer) Close() {
	if scorer.scratch != nil {
		putEdgeScratch(scorer.scratch)
	}
	*scorer = NodeEdgeLengthScorer{}
}
