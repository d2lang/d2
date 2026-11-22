package packing

import (
	"cmp"
	"math"
	"slices"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// candidateOrderingDisabled makes prepare decline, so tests can compare the
// plain loop with the ordered one. Production code never sets it.
var candidateOrderingDisabled bool

// packTrial is a candidate position (its index in the candidate list) and
// the score of the packing with the subgraph there.
type packTrial struct {
	index int
	score float64
}

// candidateOrdering is BinPack's score-ordered candidate validation, with
// scratch reused across the subgraphs of one packGuarded call.
//
// BinPack keeps the valid candidate with the lowest score (the first in
// candidate order among equal scores), where a candidate is valid when its
// transaction commits and the checks after the commit pass, and its score is
// binPackScoreGuarded after the commit. Committing every candidate is the
// expensive part. Instead, prepare drops the candidates whose commit is
// certain to be rejected (Transaction.PlacementRejected) and scores the
// others on the translated geometry without a transaction; the caller then
// validates them in ascending order of score, stable in candidate order, and
// stops at the first valid one: no candidate with a lower score is valid, and
// none earlier in candidate order with the same score. Candidates whose score
// is NaN or +Inf can never be chosen (the choice needs score < best, which
// starts at +Inf) and are dropped. This changes which commits run, so only
// the work charged to the budgets changes.
type candidateOrdering struct {
	trials   []packTrial // the candidates kept, in candidate order
	order    []int32     // indices into trials, by ascending score
	topLefts []geo.Point
	saved    []geo.Point
}

// prepare fills o.trials and o.order for currPacking's candidate positions
// and reports whether the ordering applies. It does not apply when the graph
// has clusters or sequences: a commit then also synchronizes them, which can
// move other nodes, so the score after the commit need not be the score of
// the translated geometry. Without them a commit's only change is its
// operation (BinPack's transactions do not affect containers), and nothing
// else moves before the checks and the score.
func (o *candidateOrdering) prepare(
	txn *layoutgraph.Transaction,
	g *layoutgraph.Graph,
	candidates []*geo.Point,
	currPacking layoutgraph.Nodes,
	currTL geo.Point,
	packedWithCurr layoutgraph.Nodes,
	root *layoutgraph.Node,
	guard *limits.WorkGuard,
) (bool, error) {
	o.trials = o.trials[:0]
	o.order = o.order[:0]
	if candidateOrderingDisabled || len(g.Clusters) != 0 || len(g.Sequences) != 0 {
		return false, nil
	}
	for _, n := range currPacking {
		if n == nil || n.TopLeft == nil {
			// The translation fails inside the commit, as it did.
			return false, nil
		}
	}
	// All positions are saved before any moves: nodes that share a top-left
	// point are translated once each, as the commit's operation does.
	o.saved = o.saved[:0]
	for _, n := range currPacking {
		o.saved = append(o.saved, *n.TopLeft)
	}
	for index, p := range candidates {
		if err := guard.Step(); err != nil {
			return false, err
		}
		offsetX := p.X - currTL.X
		offsetY := p.Y - currTL.Y
		o.topLefts = o.topLefts[:0]
		for _, n := range currPacking {
			o.topLefts = append(o.topLefts, geo.Point{X: n.TopLeft.X + offsetX, Y: n.TopLeft.Y + offsetY})
		}
		if txn.PlacementRejected(currPacking, o.topLefts) {
			continue
		}
		for _, n := range currPacking {
			n.Translate(offsetX, offsetY)
		}
		score, err := binPackScoreGuarded(packedWithCurr, root, guard)
		for k, n := range currPacking {
			*n.TopLeft = o.saved[k]
		}
		if err != nil {
			return false, err
		}
		if !(score < math.Inf(1)) {
			continue
		}
		o.trials = append(o.trials, packTrial{index: index, score: score})
	}
	// A stable sort's result depends only on the keys, so sorting indices
	// gives the order sorting the trials would, with the sort that []int32
	// already instantiates.
	for k := range o.trials {
		o.order = append(o.order, int32(k))
	}
	slices.SortStableFunc(o.order, func(a, b int32) int {
		return cmp.Compare(o.trials[a].score, o.trials[b].score)
	})
	return true, nil
}
