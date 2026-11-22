package placement

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/placementcost"
	"github.com/d2lang/d2/lib/geo"
)

// This file lets the sized optimizer reuse the scores its counted
// moveNodeToBest computed for a leaf at a position in an earlier pass.
//
// A score (Score, ColumnCrossingCost, NodeSymmetry of a node placed at a
// point) is a deterministic function of the graph: the node's position and
// size, the geometry of the other nodes, cluster arrangements, herd fences,
// the routing costs and the topology. An entry for node X at position P is
// stored with the memo's epoch and its move clock, and stays valid while
// nothing it depends on can have changed except X's own position (which is
// part of the key):
//
//   - a move of one node (moveNodeToBest of X that moved X, or left X or a
//     descendant on other bits) advances the move clock; X's own entries stay
//     valid, every other node's entries stored before it become invalid;
//   - a hub re-search scores with the hub's spoke edges suppressed, so the
//     memo is not used during it, and a move of the hub is recorded after it,
//     once the edges are restored (reSearchHub);
//   - everything else that may change the graph starts a new epoch, which
//     invalidates every entry: a swap, a transpose that succeeded or erred, a
//     failed transpose that opened a transaction (it charged the transaction
//     guard) unless an image of every node's geometry, every cluster's
//     arrangement and every herd fence is unchanged, herd fences synced to
//     other values, an optimize call that fails and restores its snapshot, and
//     every stage the owner runs between optimize calls unless the state
//     beginExternalChanges recorded before it is bit-identical after it
//     (endExternalChanges).
//
// So a valid entry was computed in a graph that differs from the current one
// at most in X's position, which the key fixes: it is the score the scorer
// would compute again.
//
// Skipping a scoring call skips only its cancellation polls, which a context
// with a Done channel cannot observe, and the lazy initialization of the
// routing costs it might perform, so the memo is used only with such a context
// and when the costs the scorer can read are initialized. Work charges are
// made by the caller for every candidate as before.

// Memo entry levels: a lower bound of Score (ScoreUnlessAbove's value for a
// discarded candidate: the full score is at least this), Score itself, or
// Score with ColumnCrossingCost and NodeSymmetry.
const (
	scoreMemoEmpty uint8 = iota
	scoreMemoLowerBound
	scoreMemoEdgeLength
	scoreMemoAll
)

type optimizerScoreSlot struct {
	node           *layoutgraph.Node
	x, y           uint64 // math.Float64bits of the node's TopLeft when scored
	version        uint64 // the slot is empty unless this is the memo's epoch
	clock          uint64 // the move clock when stored
	turn, crossing uint64 // math.Float64bits of the routing costs when scored
	level          uint8
	score          float64
	columnCrossing float64
	symmetry       float64
}

// optimizerScoreMemo holds the scores of the current epoch in an open
// addressing table whose slots of other epochs count as empty.
type optimizerScoreMemo struct {
	stats     *scoreMemoStats
	enabled   bool
	suspended bool   // set while the optimizer scores with other edges (hub re-search)
	version   uint64 // the epoch
	next      uint64 // the last epoch handed out
	// The move clock and the latest moves: lastMover moved at lastMove, and
	// otherMove is the latest move of a node other than lastMover (0: none
	// in this epoch).
	clock     uint64
	lastMover *layoutgraph.Node
	lastMove  uint64
	otherMove uint64
	// table holds the slots (pooled; slots is table.slots); spare is the
	// previous table, reused by a rebuild of the same size.
	table, spare *optimizerScoreTable
	slots        []optimizerScoreSlot
	used         int // slots of the current epoch
	image        optimizerStateImage
}

// optimizerScoreTable is a score memo table. Released tables are kept for
// later optimizers in scoreTableCache; a cached table is cleared (every slot
// empty for any epoch, since epochs start at 1).
type optimizerScoreTable struct {
	slots []optimizerScoreSlot
}

const minScoreTableSlots = 64

// Released tables wait in a small cache for the next optimizers (one per
// placement level, one after another; one per seed running concurrently):
// at most maxCachedScoreTablesPerSize tables of each size from
// minScoreTableSlots to maxCachedScoreTableSlots slots, so at most
// 2 * (64 + 128 + ... + 2048) slots of 88 bytes (709 KiB) in all. Larger
// tables, and tables of a size whose places are taken, are left to the
// garbage collector. A sync.Pool would not be bounded where it never frees
// anything (TinyGo's, in d2's WebAssembly build), and the standard runtime
// empties it at every other garbage collection; the cache is never emptied.
const (
	maxCachedScoreTableSlots    = 2048
	maxCachedScoreTablesPerSize = 2
	scoreTableSizes             = 6 // minScoreTableSlots << 0 ... << 5
)

var scoreTableCache struct {
	mutex  sync.Mutex
	tables [scoreTableSizes][maxCachedScoreTablesPerSize]*optimizerScoreTable
}

// scoreTableSize returns the cache index of a table of size slots, or -1 if
// tables of that size are not cached.
func scoreTableSize(size int) int {
	for k := 0; k < scoreTableSizes; k++ {
		if size == minScoreTableSlots<<k {
			return k
		}
	}
	return -1
}

// getScoreTable returns a cleared table of size slots (a power of two at
// least minScoreTableSlots).
func getScoreTable(size int) *optimizerScoreTable {
	if k := scoreTableSize(size); k >= 0 {
		cache := &scoreTableCache
		cache.mutex.Lock()
		for i, table := range cache.tables[k] {
			if table != nil {
				cache.tables[k][i] = nil
				cache.mutex.Unlock()
				return table
			}
		}
		cache.mutex.Unlock()
	}
	return &optimizerScoreTable{slots: make([]optimizerScoreSlot, size)}
}

// putScoreTable clears table and keeps it for a later optimizer if the cache
// has a free place for its size.
func putScoreTable(table *optimizerScoreTable) {
	if table == nil {
		return
	}
	k := scoreTableSize(len(table.slots))
	if k < 0 {
		return
	}
	clear(table.slots)
	cache := &scoreTableCache
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	for i, cached := range cache.tables[k] {
		if cached == nil {
			cache.tables[k][i] = table
			return
		}
	}
}

// scoreMemoContextKey marks a context (tests only) whose optimizers keep the
// memo off, or count its hits: the value is a *scoreMemoStats.
type scoreMemoContextKey struct{}

// scoreMemoStats configures and observes the memo of the optimizers placed
// under a context carrying it (tests only). With verify set, every hit is
// checked against a fresh evaluation, and every swap search, transpose or
// compaction skipped as known to find or change nothing (optimizer_quiet.go,
// compaction_schedule.go) is run and checked.
type scoreMemoStats struct {
	off                         bool
	verify                      bool
	lookups, hits, lowerBounded uint64
	quietSwaps, quietTransposes uint64
	compactionSkips             uint64
	mismatches                  uint64
	firstMismatch               string
}

// verifyHit compares a memo hit for node at its current position with a
// fresh evaluation (tests only): stored scores must be bit-identical, and a
// lower bound must not exceed the score nor discard where the score does not.
func (memo *optimizerScoreMemo) verifyHit(node *layoutgraph.Node, edgeAbductions []*layoutgraph.EdgeAbduction, known optimizerScoreSlot, bound, offset float64) {
	stats := memo.stats
	if stats == nil || !stats.verify || known.level == scoreMemoEmpty {
		return
	}
	ctx := context.Background()
	full, err := placementcost.NodeEdgeLength(ctx, node, placementcost.EdgeLengthOptions{EdgeAbductions: edgeAbductions, IncludeNodeSizes: true, PenalizeDirection: true})
	mismatch := ""
	switch {
	case err != nil:
		mismatch = err.Error()
	case known.level == scoreMemoLowerBound:
		if known.score > full {
			mismatch = fmt.Sprintf("lower bound %v above score %v", known.score, full)
		} else if !math.IsInf(bound, 1) && geo.PrecisionCompare(known.score-offset, bound, geo.PRECISION) == 1 &&
			geo.PrecisionCompare(full-offset, bound, geo.PRECISION) != 1 {
			mismatch = fmt.Sprintf("lower bound %v discards, score %v does not", known.score, full)
		}
	case math.Float64bits(known.score) != math.Float64bits(full):
		mismatch = fmt.Sprintf("stored score %v, fresh %v", known.score, full)
	case known.level == scoreMemoAll:
		columnCrossing, err1 := placementcost.ColumnCrossingCost(ctx, node, edgeAbductions)
		symmetry, err2 := placementcost.NodeSymmetry(ctx, node, edgeAbductions)
		if err1 != nil || err2 != nil || math.Float64bits(columnCrossing) != math.Float64bits(known.columnCrossing) ||
			math.Float64bits(symmetry) != math.Float64bits(known.symmetry) {
			mismatch = fmt.Sprintf("stored (%v, %v), fresh (%v, %v)", known.columnCrossing, known.symmetry, columnCrossing, symmetry)
		}
	}
	if mismatch != "" {
		if stats.mismatches == 0 {
			stats.firstMismatch = fmt.Sprintf("node %d at (%v, %v): %s", node.ID, node.TopLeft.X, node.TopLeft.Y, mismatch)
		}
		stats.mismatches++
	}
}

// enable turns the memo on with an empty first version, unless ctx asks for
// it to stay off.
func (memo *optimizerScoreMemo) enable(ctx context.Context) {
	if stats, ok := ctx.Value(scoreMemoContextKey{}).(*scoreMemoStats); ok {
		memo.stats = stats
		if stats.off {
			return
		}
	}
	memo.enabled = true
	memo.invalidate()
}

// invalidate starts a new epoch: every stored score becomes unusable.
func (memo *optimizerScoreMemo) invalidate() {
	memo.next++
	memo.version = memo.next
	memo.used = 0
	memo.lastMover, memo.lastMove, memo.otherMove = nil, 0, 0
}

// moved records that node (with its descendants) moved and nothing else
// changed: the entries of every other node stored before become unusable.
func (memo *optimizerScoreMemo) moved(node *layoutgraph.Node) {
	memo.clock++
	if node != memo.lastMover {
		memo.otherMove = memo.lastMove
		memo.lastMover = node
	}
	memo.lastMove = memo.clock
}

// live reports whether a slot of the current epoch is still valid: no node
// other than its own moved since it was stored.
func (memo *optimizerScoreMemo) live(slot *optimizerScoreSlot) bool {
	latestOther := memo.lastMove
	if slot.node == memo.lastMover {
		latestOther = memo.otherMove
	}
	return latestOther <= slot.clock
}

// positionsExact reports whether every snapshot node is at its recorded
// position, bit for bit.
func positionsExact(snapshots []nodePositionSnapshot) bool {
	for _, snapshot := range snapshots {
		current := snapshot.node.TopLeft
		if (current == nil) != (snapshot.topLeft.pointer == nil) {
			return false
		}
		if current != nil && (math.Float64bits(current.X) != math.Float64bits(snapshot.topLeft.value.X) ||
			math.Float64bits(current.Y) != math.Float64bits(snapshot.topLeft.value.Y)) {
			return false
		}
	}
	return true
}

// optimizerScoreHash mixes a node's ID with a position's bits.
func optimizerScoreHash(id layoutgraph.EntityID, x, y uint64) uint64 {
	h := uint64(id)*0x9e3779b97f4a7c15 ^ x
	h *= 0xbf58476d1ce4e5b9
	h ^= y + h>>29
	h *= 0x94d049bb133111eb
	return h ^ h>>32
}

// slot returns the entry for node at (x, y) of the current version, or the
// empty slot where it goes.
func (memo *optimizerScoreMemo) slot(node *layoutgraph.Node, x, y uint64) *optimizerScoreSlot {
	if memo.table == nil {
		memo.table = getScoreTable(minScoreTableSlots)
		memo.slots = memo.table.slots
	}
	mask := uint64(len(memo.slots) - 1)
	for index := optimizerScoreHash(node.IDValue(), x, y) & mask; ; index = (index + 1) & mask {
		slot := &memo.slots[index]
		if slot.version != memo.version {
			return slot
		}
		if slot.node == node && slot.x == x && slot.y == y {
			return slot
		}
	}
}

// lookup returns the entry for node at (x, y) of the current version stored
// with the given routing cost bits; its level is scoreMemoEmpty if there is
// none.
func (memo *optimizerScoreMemo) lookup(node *layoutgraph.Node, x, y, turn, crossing uint64) optimizerScoreSlot {
	if memo.table == nil {
		return optimizerScoreSlot{}
	}
	slot := memo.slot(node, x, y)
	if slot.version != memo.version || !memo.live(slot) || slot.turn != turn || slot.crossing != crossing {
		slot = nil
	}
	if memo.stats != nil {
		memo.stats.lookups++
		if slot != nil {
			memo.stats.hits++
			if slot.level == scoreMemoLowerBound {
				memo.stats.lowerBounded++
			}
		}
	}
	if slot == nil {
		return optimizerScoreSlot{}
	}
	return *slot
}

// store records scores for node at (x, y) unless the entry already holds at
// least as much. A stored lower bound is only replaced by a full score.
func (memo *optimizerScoreMemo) store(node *layoutgraph.Node, x, y, turn, crossing uint64, level uint8, score, columnCrossing, symmetry float64) {
	slot := memo.slot(node, x, y)
	if slot.version == memo.version {
		if memo.live(slot) && slot.level >= level && slot.turn == turn && slot.crossing == crossing {
			return
		}
	} else {
		memo.used++
	}
	*slot = optimizerScoreSlot{
		node: node, x: x, y: y, version: memo.version, clock: memo.clock, turn: turn, crossing: crossing,
		level: level, score: score, columnCrossing: columnCrossing, symmetry: symmetry,
	}
	if 2*memo.used > len(memo.slots) {
		memo.grow()
	}
}

// grow rebuilds the table without its dead slots, twice as large unless
// that leaves it at most a quarter full.
func (memo *optimizerScoreMemo) grow() {
	oldTable := memo.table
	old := oldTable.slots
	live := 0
	for i := range old {
		if old[i].version == memo.version && memo.live(&old[i]) {
			live++
		}
	}
	size := len(old)
	if 4*live > size {
		size *= 2
	}
	// The previous table is kept for the next rebuild of the same size.
	var table *optimizerScoreTable
	if memo.spare != nil && len(memo.spare.slots) == size {
		table = memo.spare
		clear(table.slots)
	} else {
		putScoreTable(memo.spare)
		table = getScoreTable(size)
	}
	memo.table, memo.slots, memo.spare = table, table.slots, oldTable
	memo.used = live
	mask := uint64(len(memo.slots) - 1)
	for i := range old {
		entry := &old[i]
		if entry.version != memo.version || !memo.live(entry) {
			continue
		}
		for index := optimizerScoreHash(entry.node.IDValue(), entry.x, entry.y) & mask; ; index = (index + 1) & mask {
			if memo.slots[index].version != memo.version {
				memo.slots[index] = *entry
				break
			}
		}
	}
}

// release returns the tables to their pools and drops node references; the
// memo keeps working (with new tables) if used again.
func (memo *optimizerScoreMemo) release() {
	putScoreTable(memo.table)
	putScoreTable(memo.spare)
	memo.table, memo.spare, memo.slots = nil, nil, nil
	memo.invalidate()
	memo.image.release()
}

// usable reports whether node's scores may come from the memo for this call:
// the memo is on, ctx cannot observe skipped polls, and every routing cost
// the scorer can read is initialized (a skipped call cannot be the one that
// would have initialized it). It returns the cost bits stored with entries.
func (memo *optimizerScoreMemo) usable(ctx context.Context, node *layoutgraph.Node) (turn, crossing uint64, ok bool) {
	if !memo.enabled || memo.suspended || ctx.Done() == nil || node == nil || node.Graph == nil {
		return 0, 0, false
	}
	costs := node.Graph.RoutingCosts()
	if !(costs.Turn > 0) {
		return 0, 0, false
	}
	if !(costs.Crossing > 0) {
		// Only table-column edges read the crossing cost.
		for _, e := range node.Edges {
			if e == nil || e.IsBetweenTableColumns() {
				return 0, 0, false
			}
		}
	}
	return math.Float64bits(costs.Turn), math.Float64bits(costs.Crossing), true
}

// optimizerStateImage is the state of the optimizer's mutation snapshot set
// (every graph node and its container, cluster and sequence descendants,
// every cluster, every herd fence of those nodes) and the routing costs, as
// bits: what a transpose that opened a transaction could have changed. It is
// compared after such a transpose; a difference starts a new version.
type optimizerStateImage struct {
	version  uint64 // the memo epoch and move clock the image describes; version 0: none
	clock    uint64
	nodes    []optimizerImageNode
	clusters []optimizerImageCluster
	herds    []optimizerImageHerd
	turn     uint64
	crossing uint64
	// captureGraph's set and the graph's node and cluster counts.
	seen          map[*layoutgraph.Node]struct{}
	graphNodes    int
	graphClusters int
}

type optimizerImageNode struct {
	node          *layoutgraph.Node
	x, y          uint64
	width, height uint64
	placed        bool
}

type optimizerImageCluster struct {
	cluster     *layoutgraph.Cluster
	arrangement layoutgraph.ClusterArrangement
	desired     layoutgraph.ClusterArrangement
	padding     uint64
}

type optimizerImageHerd struct {
	herd        *layoutgraph.HerdAssignment
	orientation geo.Orientation
	val         uint64
}

func (image *optimizerStateImage) release() {
	clear(image.nodes)
	clear(image.clusters)
	clear(image.herds)
	image.nodes = image.nodes[:0]
	image.clusters = image.clusters[:0]
	image.herds = image.herds[:0]
	image.version = 0
}

func imageNodeOf(node *layoutgraph.Node) optimizerImageNode {
	entry := optimizerImageNode{node: node, width: math.Float64bits(node.Width), height: math.Float64bits(node.Height)}
	if node.TopLeft != nil {
		entry.placed = true
		entry.x, entry.y = math.Float64bits(node.TopLeft.X), math.Float64bits(node.TopLeft.Y)
	}
	return entry
}

func imageClusterOf(cluster *layoutgraph.Cluster) optimizerImageCluster {
	return optimizerImageCluster{cluster: cluster, arrangement: cluster.Arrangement, desired: cluster.DesiredArrangement, padding: math.Float64bits(cluster.Padding)}
}

func imageHerdOf(herd *layoutgraph.HerdAssignment) optimizerImageHerd {
	return optimizerImageHerd{herd: herd, orientation: herd.Orientation, val: math.Float64bits(herd.Val)}
}

// capture records the current state of the snapshot's sets.
func (image *optimizerStateImage) capture(g *layoutgraph.Graph, snapshot *optimizerMutationSnapshot, version, clock uint64) {
	image.release()
	for i := range snapshot.nodes {
		image.nodes = append(image.nodes, imageNodeOf(snapshot.nodes[i].node))
	}
	for i := range snapshot.clusters {
		image.clusters = append(image.clusters, imageClusterOf(snapshot.clusters[i].cluster))
	}
	for i := range snapshot.herds {
		image.herds = append(image.herds, imageHerdOf(snapshot.herds[i].herd))
	}
	costs := g.RoutingCosts()
	image.turn, image.crossing = math.Float64bits(costs.Turn), math.Float64bits(costs.Crossing)
	image.version, image.clock = version, clock
}

// captureGraph records the state of g's snapshot set, found the way
// captureOptimizerMutationStateInto finds it (graph nodes, then their
// container, cluster and sequence members, breadth first), every cluster of
// g and every herd fence of those nodes. A nil node leaves no image.
func (image *optimizerStateImage) captureGraph(g *layoutgraph.Graph, version, clock uint64) {
	image.release()
	if image.seen == nil {
		image.seen = make(map[*layoutgraph.Node]struct{}, len(g.Nodes))
	} else {
		clear(image.seen)
	}
	ok := true
	add := func(node *layoutgraph.Node) {
		if node == nil {
			ok = false
			return
		}
		if _, seen := image.seen[node]; seen {
			return
		}
		image.seen[node] = struct{}{}
		image.nodes = append(image.nodes, imageNodeOf(node))
	}
	for _, node := range g.Nodes {
		add(node)
	}
	for i := 0; ok && i < len(image.nodes); i++ {
		node := image.nodes[i].node
		for _, child := range g.Containers[node] {
			add(child)
		}
		if cluster := g.Clusters[node]; cluster != nil {
			for _, child := range cluster.Nodes {
				add(child)
			}
		}
		if sequence := g.Sequences[node]; sequence != nil {
			for _, child := range sequence.Nodes {
				add(child)
			}
		}
	}
	for _, cluster := range g.Clusters {
		if cluster != nil {
			image.clusters = append(image.clusters, imageClusterOf(cluster))
		}
	}
	for i := range image.nodes {
		if herd := image.nodes[i].node.HerdAssignment; herd != nil {
			image.herds = append(image.herds, imageHerdOf(herd))
		}
	}
	clear(image.seen)
	if !ok {
		image.release()
		return
	}
	costs := g.RoutingCosts()
	image.turn, image.crossing = math.Float64bits(costs.Turn), math.Float64bits(costs.Crossing)
	image.version, image.clock = version, clock
	image.graphNodes = len(g.Nodes)
	image.graphClusters = len(g.Clusters)
}

// unchanged reports whether every recorded node, cluster, herd fence and the
// routing costs still have the recorded bits, and g still has as many nodes
// (the first recorded ones, in order) and clusters.
func (image *optimizerStateImage) unchanged(g *layoutgraph.Graph) bool {
	if image.version == 0 || len(g.Nodes) != image.graphNodes || len(g.Clusters) != image.graphClusters {
		return false
	}
	for i, node := range g.Nodes {
		if image.nodes[i].node != node {
			return false
		}
	}
	for i := range image.nodes {
		if imageNodeOf(image.nodes[i].node) != image.nodes[i] {
			return false
		}
	}
	for i := range image.clusters {
		if imageClusterOf(image.clusters[i].cluster) != image.clusters[i] {
			return false
		}
	}
	for i := range image.herds {
		if imageHerdOf(image.herds[i].herd) != image.herds[i] {
			return false
		}
	}
	costs := g.RoutingCosts()
	return math.Float64bits(costs.Turn) == image.turn && math.Float64bits(costs.Crossing) == image.crossing
}

// matches reports whether the snapshot's sets still have the recorded state.
func (image *optimizerStateImage) matches(g *layoutgraph.Graph, snapshot *optimizerMutationSnapshot) bool {
	if len(snapshot.nodes) != len(image.nodes) || len(snapshot.clusters) != len(image.clusters) || len(snapshot.herds) != len(image.herds) {
		return false
	}
	for i := range image.nodes {
		if snapshot.nodes[i].node != image.nodes[i].node || imageNodeOf(image.nodes[i].node) != image.nodes[i] {
			return false
		}
	}
	for i := range image.clusters {
		if imageClusterOf(image.clusters[i].cluster) != image.clusters[i] {
			return false
		}
	}
	for i := range image.herds {
		if imageHerdOf(image.herds[i].herd) != image.herds[i] {
			return false
		}
	}
	costs := g.RoutingCosts()
	return math.Float64bits(costs.Turn) == image.turn && math.Float64bits(costs.Crossing) == image.crossing
}
