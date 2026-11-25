package placement

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// This file answers the sized optimizer's closest-unoccupied-distance search
// (findClosestUnoccupiedDistanceStepped, the reference) without stepping the
// guard for every ring, ring point, offset and scanned node.
//
// Nothing moves while one search runs, so whether a rounded point is
// occupied, and the units the stepped check of that point charges, are a pure
// function of the point. The walk below visits the same rings, ring points and
// offsets with the same float sequences and the same checked-set semantics as
// the stepped walk, answers each rounded point once from a scan table of the
// other nodes, adds up exactly the units the stepped walk charges, and the
// caller charges that total with one Add. OptimizationWorkGuard rejects a
// charge without changing its state, so on a rejection (or whenever the walk
// meets anything it does not model) the checked set is rolled back and the
// stepped walk runs from the identical state.

// optimizerCheckedSet is the per-node set of raw ring offsets the
// closest-unoccupied search has examined. It has exactly the semantics of a
// map[geo.Point]struct{} keyed by the offsets: keys compare with == per
// component (so -0 equals +0), a key with a NaN component is never found and
// every insertion of one adds an entry. Keys are kept in insertion order so a
// search can be rolled back.
type optimizerCheckedSet struct {
	keys  []geo.Point
	slots []int32 // 1 + index into keys; 0 is empty
}

func (set *optimizerCheckedSet) len() int {
	return len(set.keys)
}

func (set *optimizerCheckedSet) reset() {
	set.keys = set.keys[:0]
	clear(set.slots)
}

func (set *optimizerCheckedSet) has(x, y float64) bool {
	if len(set.slots) == 0 || x != x || y != y {
		return false
	}
	mask := uint64(len(set.slots) - 1)
	for slot := optimizerOccupancyHash(x, y) & mask; ; slot = (slot + 1) & mask {
		index := set.slots[slot]
		if index == 0 {
			return false
		}
		key := &set.keys[index-1]
		if key.X == x && key.Y == y {
			return true
		}
	}
}

// add inserts a key that has() reported absent.
func (set *optimizerCheckedSet) add(x, y float64) {
	set.keys = append(set.keys, geo.Point{X: x, Y: y})
	if x != x || y != y {
		// Never found again, but counted, like a NaN map key.
		return
	}
	if 2*len(set.keys) > len(set.slots) {
		set.rehash()
		return
	}
	set.index(len(set.keys) - 1)
}

func (set *optimizerCheckedSet) index(keyIndex int) {
	mask := uint64(len(set.slots) - 1)
	key := set.keys[keyIndex]
	for slot := optimizerOccupancyHash(key.X, key.Y) & mask; ; slot = (slot + 1) & mask {
		if set.slots[slot] == 0 {
			set.slots[slot] = int32(keyIndex + 1)
			return
		}
	}
}

func (set *optimizerCheckedSet) rehash() {
	size := 64
	for size < 4*len(set.keys) {
		size *= 2
	}
	if cap(set.slots) >= size {
		set.slots = set.slots[:size]
		clear(set.slots)
	} else {
		set.slots = make([]int32, size)
	}
	for i, key := range set.keys {
		if key.X == key.X && key.Y == key.Y {
			set.index(i)
		}
	}
}

// truncate drops every key inserted after the first n.
func (set *optimizerCheckedSet) truncate(n int) {
	if n >= len(set.keys) {
		return
	}
	set.keys = set.keys[:n]
	clear(set.slots)
	for i, key := range set.keys {
		if key.X == key.X && key.Y == key.Y {
			set.index(i)
		}
	}
}

// optimizerScanEntry is one graph node as optimizerScanOverlap sees it while
// the scanned node is tried at other points.
type optimizerScanEntry struct {
	left, top     float64 // TopLeft
	right, bottom float64 // TopLeft.X+Width, TopLeft.Y+Height
	reachRight    float64 // right + maxSafeDelta
	reachBottom   float64 // bottom + maxSafeDelta
	kind          uint8
	// deltaKnown has bit o set when delta[o] holds DeltaTo for orientation o.
	deltaKnown uint16
	delta      [9]int
	node       *layoutgraph.Node
}

const (
	optimizerScanPlaced uint8 = iota
	optimizerScanSelf
	optimizerScanUnplaced
)

// optimizerScanTable answers optimizerScanOverlap(node, point, exceptions)
// for one node with exceptions nil or []*Node{node}, for any point, while no
// graph node other than node moves and node's edges do not change.
type optimizerScanTable struct {
	node          *layoutgraph.Node
	width, height float64
	edgeUnits     uint64
	entries       []optimizerScanEntry
}

// build snapshots node.Graph.Nodes. It reports false when a scan could stop
// with an error (a nil graph node).
func (table *optimizerScanTable) build(node *layoutgraph.Node) bool {
	table.node = node
	table.width = node.Width
	table.height = node.Height
	table.edgeUnits = uint64(len(node.Edges))
	nodes := node.Graph.Nodes
	if cap(table.entries) < len(nodes) {
		table.entries = make([]optimizerScanEntry, len(nodes))
	} else {
		table.entries = table.entries[:len(nodes)]
	}
	const maxSafeDelta = 500.0
	for i, other := range nodes {
		entry := &table.entries[i]
		switch {
		case other == nil:
			table.entries = table.entries[:0]
			return false
		case other == node:
			*entry = optimizerScanEntry{kind: optimizerScanSelf, node: other}
		case other.TopLeft == nil:
			*entry = optimizerScanEntry{kind: optimizerScanUnplaced, node: other}
		default:
			right := other.TopLeft.X + other.Width
			bottom := other.TopLeft.Y + other.Height
			*entry = optimizerScanEntry{
				left:        other.TopLeft.X,
				top:         other.TopLeft.Y,
				right:       right,
				bottom:      bottom,
				reachRight:  right + maxSafeDelta,
				reachBottom: bottom + maxSafeDelta,
				kind:        optimizerScanPlaced,
				node:        other,
			}
		}
	}
	return true
}

// orientation is layoutgraph's (*Node).orientationAtPoint(entry's node, point)
// for the table's node placed at (x, y), from the same expressions.
func (entry *optimizerScanEntry) orientation(x, y, right, bottom float64) geo.Orientation {
	if bottom < entry.top {
		if right < entry.left {
			return geo.TopLeft
		}
		if entry.right < x {
			return geo.TopRight
		}
		return geo.Top
	}
	if entry.bottom < y {
		if right < entry.left {
			return geo.BottomLeft
		}
		if entry.right < x {
			return geo.BottomRight
		}
		return geo.Bottom
	}
	if entry.right < x {
		return geo.Right
	}
	if right < entry.left {
		return geo.Left
	}
	return geo.NONE
}

// deltaAt returns node.DeltaTo(entry's node, (x, y)). DeltaTo depends on its
// point only through orientationAtPoint (its edge scan, gaps, loop offsets and
// margins are fixed while the table is in use), so one value is computed per
// orientation. ok is false when DeltaTo reports an error.
func (table *optimizerScanTable) deltaAt(entry *optimizerScanEntry, x, y, right, bottom float64) (float64, bool) {
	o := entry.orientation(x, y, right, bottom)
	bit := uint16(1) << uint(o)
	if entry.deltaKnown&bit == 0 {
		value, _, err := table.node.DeltaToCounted(entry.node, &geo.Point{X: x, Y: y})
		if err != nil {
			return 0, false
		}
		entry.delta[o] = value
		entry.deltaKnown |= bit
	}
	return float64(entry.delta[o]), true
}

// scan is optimizerScanOverlap(node, (x, y), nil): the answer, the units the
// stepped scan charges, and the number of visited entries other than node
// (with exceptions []*Node{node} the stepped scan charges one more unit for
// each of them). ok is false where the scan would stop with an error.
func (table *optimizerScanTable) scan(x, y float64) (overlaps bool, units, others uint64, ok bool) {
	const maxSafeDelta = 500.0
	right := x + table.width
	bottom := y + table.height
	reachRight := right + maxSafeDelta
	reachBottom := bottom + maxSafeDelta
	for i := range table.entries {
		entry := &table.entries[i]
		units++
		if entry.kind == optimizerScanSelf {
			continue
		}
		others++
		if entry.kind == optimizerScanUnplaced {
			continue
		}
		if x > entry.reachRight || reachRight < entry.left || y > entry.reachBottom || reachBottom < entry.top {
			continue
		}
		units += table.edgeUnits
		delta, ok := table.deltaAt(entry, x, y, right, bottom)
		if !ok {
			return false, units, others, false
		}
		if x < entry.right+delta && right+delta > entry.left && y < entry.bottom+delta && bottom+delta > entry.top {
			return true, units, others, true
		}
	}
	return false, units, others, true
}

// optimizerPointMemo maps a rounded point to whether it is occupied for the
// node being placed (another node's top-left is there, or the node placed
// there overlaps another node with spacing) and the units the stepped check
// charges: unitsExcepted with exceptions []*Node{node} (the closest-unoccupied
// search), units with nil exceptions (moveNodeToBest's canMove). Entries of
// earlier nodes are invalidated by a generation counter.
type optimizerPointMemo struct {
	slots      []optimizerPointMemoSlot
	used       int
	generation uint32
}

type optimizerPointMemoSlot struct {
	x, y          float64
	units         uint64
	unitsExcepted uint64
	generation    uint32
	occupant      int32 // graph index of the first node at the point, or -1
	occupied      bool
}

func (memo *optimizerPointMemo) reset() {
	memo.used = 0
	memo.generation++
	if memo.generation == 0 || len(memo.slots) == 0 {
		if len(memo.slots) == 0 {
			memo.slots = make([]optimizerPointMemoSlot, 256)
		} else {
			clear(memo.slots)
		}
		memo.generation = 1
	}
}

// slot returns the entry for (x, y) and whether it holds a value; an empty
// returned slot is where the value goes. x and y are finite.
func (memo *optimizerPointMemo) slot(x, y float64) (*optimizerPointMemoSlot, bool) {
	mask := uint64(len(memo.slots) - 1)
	for index := optimizerOccupancyHash(x, y) & mask; ; index = (index + 1) & mask {
		slot := &memo.slots[index]
		if slot.generation != memo.generation {
			return slot, false
		}
		if slot.x == x && slot.y == y {
			return slot, true
		}
	}
}

// store fills the empty slot returned by slot and returns the entry's
// (possibly new) address.
func (memo *optimizerPointMemo) store(slot *optimizerPointMemoSlot, x, y float64, answer optimizerPointAnswer) *optimizerPointMemoSlot {
	*slot = optimizerPointMemoSlot{x: x, y: y, units: answer.units, unitsExcepted: answer.unitsExcepted, generation: memo.generation, occupant: answer.occupant, occupied: answer.occupied}
	memo.used++
	if 2*memo.used <= len(memo.slots) {
		return slot
	}
	old := memo.slots
	generation := memo.generation
	memo.slots = make([]optimizerPointMemoSlot, 2*len(old))
	mask := uint64(len(memo.slots) - 1)
	var stored *optimizerPointMemoSlot
	for i := range old {
		entry := &old[i]
		if entry.generation != generation {
			continue
		}
		for index := optimizerOccupancyHash(entry.x, entry.y) & mask; ; index = (index + 1) & mask {
			if memo.slots[index].generation != generation {
				memo.slots[index] = *entry
				if entry == slot {
					stored = &memo.slots[index]
				}
				break
			}
		}
	}
	return stored
}

// optimizerUnoccupiedSearch holds the scan table and point memo of the node
// being placed, shared by its closest-unoccupied search and the
// moveNodeToBest that follows it, and the scratch of one counted search.
//
// The table and memo describe the graph as it was when prepare ran: they stay
// valid while no other node moves and node's edges do not change, which holds
// from the start of a closest-unoccupied search through the candidate
// generation and shuffle to the end of the following moveNodeToBest (that
// search moves only node itself, and only leaves use the memo). release ends
// that span.
type optimizerUnoccupiedSearch struct {
	optim    *sizedOptimizer
	node     *layoutgraph.Node
	prepared bool
	table    optimizerScanTable
	memo     optimizerPointMemo
	grid     optimizerPointGrid

	// One closest-unoccupied search.
	p              geo.Point
	minimizingSelf bool
	checked        *optimizerCheckedSet
	units          uint64
	rows           []optimizerLatticeValue // rounded Y of each offset row of one ring point
}

// optimizerLatticeValue is a rounded coordinate value = lattice*cellSize with
// lattice = math.Round(v/cellSize).
type optimizerLatticeValue struct {
	value, lattice float64
}

// optimizerPointGrid is a dense memo over a window of lattice cells: the
// rounded point (rx*cellSize, ry*cellSize) lives in cell (rx, ry) when that
// cell is inside the window, every other point in the hash memo. A point's
// place depends only on the point, so the two are one memo.
type optimizerPointGrid struct {
	baseX, baseY  int64
	width, height int64
	cells         []optimizerPointCell
	generation    uint32
}

type optimizerPointCell struct {
	units, unitsExcepted uint32
	generation           uint32
	occupant             int32
	occupied             bool
}

// optimizerPointAnswer is what a stepped check of a rounded point finds for
// the node being placed: whether it is occupied (a node's top-left is there,
// or the node placed there overlaps another with spacing), the units of
// indexedCanMove for a leaf (units) and of the search's check
// (unitsExcepted), and the graph index of the first node whose top-left is
// at the point (-1 for none), from which the full occupancy scan's units
// follow.
type optimizerPointAnswer struct {
	units, unitsExcepted uint64
	occupant             int32
	occupied             bool
}

const (
	optimizerGridRings    = 8
	optimizerGridMaxCells = 1 << 16
	// Lattice coordinates and rounded values below these bounds are exact
	// integers and exact products.
	optimizerLatticeLimit = 1 << 40
	optimizerValueLimit   = 1 << 52
)

// reset empties the memo and sets its window (width or height 0: no window).
func (grid *optimizerPointGrid) reset(baseX, baseY, width, height int64) {
	grid.generation++
	if grid.generation == 0 {
		clear(grid.cells)
		grid.generation = 1
	}
	if width <= 0 || height <= 0 || width > optimizerGridMaxCells || height > optimizerGridMaxCells || width*height > optimizerGridMaxCells {
		grid.width, grid.height = 0, 0
		return
	}
	grid.baseX, grid.baseY, grid.width, grid.height = baseX, baseY, width, height
	if need := int(width * height); cap(grid.cells) < need {
		grid.cells = make([]optimizerPointCell, need)
	} else {
		grid.cells = grid.cells[:need]
	}
}

// index returns the cell of lattice coordinates (rx, ry), or -1.
func (grid *optimizerPointGrid) index(rx, ry float64) int {
	if grid.width == 0 || !(rx >= -optimizerLatticeLimit && rx <= optimizerLatticeLimit) || !(ry >= -optimizerLatticeLimit && ry <= optimizerLatticeLimit) {
		return -1
	}
	dx := int64(rx) - grid.baseX
	dy := int64(ry) - grid.baseY
	if uint64(dx) >= uint64(grid.width) || uint64(dy) >= uint64(grid.height) {
		return -1
	}
	return int(dy*grid.width + dx)
}

// centerGrid sets the dense window around the median p: rings up to
// optimizerGridRings with the node's offsets.
func (search *optimizerUnoccupiedSearch) centerGrid(p geo.Point) {
	cellSize := search.optim.cellSize
	centerX := math.Round(p.X / cellSize)
	centerY := math.Round(p.Y / cellSize)
	spanX := math.Ceil(search.node.Width / cellSize)
	spanY := math.Ceil(search.node.Height / cellSize)
	if !(math.Abs(centerX) < optimizerLatticeLimit && math.Abs(centerY) < optimizerLatticeLimit && spanX >= 0 && spanY >= 0 &&
		spanX < optimizerGridMaxCells && spanY < optimizerGridMaxCells) {
		search.grid.reset(0, 0, 0, 0)
		return
	}
	search.grid.reset(
		int64(centerX)-optimizerGridRings-int64(spanX)-2,
		int64(centerY)-optimizerGridRings-int64(spanY)-2,
		2*optimizerGridRings+int64(spanX)+5,
		2*optimizerGridRings+int64(spanY)+5,
	)
}

// countedUnoccupiedUsable reports whether the counted checks model every
// branch the stepped checks can take for this optimizer state: the occupancy
// table is current and overlap checks use the full graph scan (graphs below
// the interval-tree threshold).
func (optim *sizedOptimizer) countedUnoccupiedUsable(node *layoutgraph.Node) bool {
	index := &optim.spatialIndex
	return index.graph == optim.g && node.Graph == optim.g && index.nodeCount == len(optim.g.Nodes) &&
		len(optim.g.Nodes) <= limits.MaxEngineNodes && index.occupancyUsable && !index.spatialUsable
}

// prepare snapshots the graph for node. It reports false when the counted
// checks cannot be used.
func (search *optimizerUnoccupiedSearch) prepare(optim *sizedOptimizer, node *layoutgraph.Node) bool {
	search.release()
	if !optim.countedUnoccupiedUsable(node) || !search.table.build(node) {
		return false
	}
	search.optim = optim
	search.node = node
	search.memo.reset()
	search.grid.reset(0, 0, 0, 0)
	search.prepared = true
	return true
}

// preparedFor reports whether the table and memo are those of node.
func (search *optimizerUnoccupiedSearch) preparedFor(optim *sizedOptimizer, node *layoutgraph.Node) bool {
	return search.prepared && search.optim == optim && search.node == node
}

func (search *optimizerUnoccupiedSearch) release() {
	search.prepared = false
	search.optim = nil
	search.node = nil
	search.checked = nil
	search.table.node = nil
	clear(search.table.entries)
}

// evaluate computes a finite rounded point's memo entry from the occupancy
// table and the scan table. ok is false where a stepped check would stop
// with an error.
func (search *optimizerUnoccupiedSearch) evaluate(pointX, pointY float64) (optimizerPointAnswer, bool) {
	// indexedIsOccupied: one unit, then the occupancy table.
	answer := optimizerPointAnswer{units: 1, unitsExcepted: 1, occupant: -1}
	occupant, occupied := search.optim.spatialIndex.occupancyLookup(pointX, pointY)
	if occupied {
		if occupant < 0 || occupant > math.MaxInt32 {
			return optimizerPointAnswer{}, false
		}
		answer.occupant = int32(occupant)
	} else {
		overlaps, scanUnits, others, ok := search.table.scan(pointX, pointY)
		if !ok {
			return optimizerPointAnswer{}, false
		}
		occupied = overlaps
		answer.units += scanUnits
		answer.unitsExcepted += scanUnits + others
	}
	answer.occupied = occupied
	return answer, true
}

// point returns the memo entry of the finite rounded point (pointX, pointY)
// with lattice coordinates (rx, ry) (NaN when unknown), computing it when
// absent.
func (search *optimizerUnoccupiedSearch) point(pointX, pointY, rx, ry float64) (optimizerPointAnswer, bool) {
	if index := search.grid.index(rx, ry); index >= 0 {
		cell := &search.grid.cells[index]
		if cell.generation == search.grid.generation {
			return optimizerPointAnswer{units: uint64(cell.units), unitsExcepted: uint64(cell.unitsExcepted), occupant: cell.occupant, occupied: cell.occupied}, true
		}
		answer, ok := search.evaluate(pointX, pointY)
		if ok && answer.units <= math.MaxUint32 && answer.unitsExcepted <= math.MaxUint32 {
			*cell = optimizerPointCell{units: uint32(answer.units), unitsExcepted: uint32(answer.unitsExcepted), generation: search.grid.generation, occupant: answer.occupant, occupied: answer.occupied}
		}
		return answer, ok
	}
	slot, found := search.memo.slot(pointX, pointY)
	if found {
		return optimizerPointAnswer{units: slot.units, unitsExcepted: slot.unitsExcepted, occupant: slot.occupant, occupied: slot.occupied}, true
	}
	answer, ok := search.evaluate(pointX, pointY)
	if ok {
		search.memo.store(slot, pointX, pointY, answer)
	}
	return answer, ok
}

// canMove is indexedCanMove(node, (x, y)) for the node away from (x, y) and
// from its position at prepare time: whether it can move there and the units
// the stepped check charges. For a node that moves with descendants
// (scanOccupancy) indexedCanMove scans the graph's nodes for an occupant one
// step each (up to the first one at the point, all of them when there is
// none) instead of asking the occupancy table for one unit; the node itself
// cannot be the occupant away from both positions. ok is false where the
// stepped check would take a path the counted check does not model.
func (search *optimizerUnoccupiedSearch) canMove(x, y float64, scanOccupancy bool) (canMove bool, units uint64, ok bool) {
	if !optimizerIndexFinite(x) || !optimizerIndexFinite(y) {
		return false, 0, false
	}
	// A candidate is a rounded point lattice*cellSize; its lattice coordinate
	// is then exactly x/cellSize. Anything else uses the hash memo.
	cellSize := search.optim.cellSize
	rx, ry := x/cellSize, y/cellSize
	if rx != math.Trunc(rx) || ry != math.Trunc(ry) || math.Abs(x) >= optimizerValueLimit || math.Abs(y) >= optimizerValueLimit ||
		rx*cellSize != x || ry*cellSize != y {
		rx = math.NaN()
	}
	answer, ok := search.point(x, y, rx, ry)
	if !ok {
		return false, 0, false
	}
	units = answer.units
	if scanOccupancy {
		if answer.occupant >= 0 {
			units = uint64(answer.occupant) + 1
		} else {
			units = uint64(len(search.table.entries)) + answer.units - 1
		}
	}
	return !answer.occupied, units, true
}

// round is roundToNearestCellSize(v, cellSize) with its lattice coordinate.
func (search *optimizerUnoccupiedSearch) round(v float64) optimizerLatticeValue {
	lattice := math.Round(v / search.optim.cellSize)
	value := lattice * search.optim.cellSize
	if !(math.Abs(value) < optimizerValueLimit) {
		// The product may be inexact: keep the value, use the hash memo.
		lattice = math.NaN()
	}
	return optimizerLatticeValue{value: value, lattice: lattice}
}

// pointOccupied is isPointOccupiedGuarded(x, y) without the guard; ok is
// false where the stepped check would take a path this search does not model.
func (search *optimizerUnoccupiedSearch) pointOccupied(x, y float64) (occupied bool, ok bool) {
	return search.roundedPointOccupied(search.round(search.p.X+x), search.round(search.p.Y+y))
}

func (search *optimizerUnoccupiedSearch) roundedPointOccupied(pointX, pointY optimizerLatticeValue) (occupied bool, ok bool) {
	optim := search.optim
	if optim.fixedOrigin != nil && (pointX.value < optim.fixedOrigin.X || pointY.value < optim.fixedOrigin.Y) {
		return true, true
	}
	if !optimizerIndexFinite(pointX.value) || !optimizerIndexFinite(pointY.value) {
		return false, false
	}
	answer, ok := search.point(pointX.value, pointY.value, pointX.lattice, pointY.lattice)
	if !ok {
		return false, false
	}
	search.units += answer.unitsExcepted
	return answer.occupied, true
}

// findUnoccupied is findUnoccupiedGuarded without the guard.
func (search *optimizerUnoccupiedSearch) findUnoccupied(x, y float64) (unoccupied bool, ok bool) {
	optim := search.optim
	search.units++
	skip := false
	if search.checked != nil {
		if search.checked.has(x, y) {
			skip = true
		} else {
			if search.checked.len() >= maxOptimizerPlacementCandidates {
				return false, false
			}
			search.checked.add(x, y)
		}
	}
	if !skip {
		occupied, ok := search.pointOccupied(x, y)
		if !ok {
			return false, false
		}
		if !occupied {
			return true, true
		}
	}
	if !search.minimizingSelf {
		return false, true
	}
	cellSize := optim.cellSize
	// The offset rows (j) are the same for every column (i): round them once.
	rows := search.rows[:0]
	for j := y - search.node.Height; j <= y; j += cellSize {
		rows = append(rows, search.round(search.p.Y+j))
	}
	search.rows = rows
	for i := x - search.node.Width; i <= x; i += cellSize {
		pointX := search.round(search.p.X + i)
		row := 0
		for j := y - search.node.Height; j <= y; j += cellSize {
			pointY := rows[row]
			row++
			search.units++
			if i == x && j == y {
				continue
			}
			if search.checked != nil {
				if search.checked.has(i, j) {
					continue
				}
				if search.checked.len() >= maxOptimizerPlacementCandidates {
					return false, false
				}
				search.checked.add(i, j)
			}
			occupied, ok := search.roundedPointOccupied(pointX, pointY)
			if !ok {
				return false, false
			}
			if !occupied {
				return true, true
			}
		}
	}
	return false, true
}

// run is findClosestUnoccupiedDistanceStepped's walk without the guard. found
// is false when no ring up to 100 has an unoccupied placement (the stepped
// search then fails after charging the same units).
func (search *optimizerUnoccupiedSearch) run() (distance float64, found bool, ok bool) {
	optim := search.optim
	p := search.p
	cellSize := optim.cellSize
	for curr := 0.0; curr <= 100.0; curr++ {
		search.units++
		currCell := curr * cellSize
		x := currCell
		y := 0.0
		for x >= -currCell {
			search.units++
			if y != 0 {
				candidateX := roundToNearestCellSize(p.X+x, cellSize)
				candidateY := roundToNearestCellSize(p.Y-y, cellSize)
				if optim.fixedOrigin == nil || (candidateX >= optim.fixedOrigin.X && candidateY >= optim.fixedOrigin.Y) {
					unoccupied, ok := search.findUnoccupied(x, -y)
					if !ok {
						return 0, false, false
					}
					if unoccupied {
						return curr, true, true
					}
				}
			}
			candidateX := roundToNearestCellSize(p.X+x, cellSize)
			candidateY := roundToNearestCellSize(p.Y+y, cellSize)
			if optim.fixedOrigin == nil || (candidateX >= optim.fixedOrigin.X && candidateY >= optim.fixedOrigin.Y) {
				unoccupied, ok := search.findUnoccupied(x, y)
				if !ok {
					return 0, false, false
				}
				if unoccupied {
					return curr, true, true
				}
			}
			if x > 0 {
				y += cellSize
			} else {
				y -= cellSize
			}
			x -= cellSize
		}
	}
	return 0, false, true
}

// findClosestUnoccupiedDistanceCounted runs the counted search and charges
// its units, or runs the stepped search when the counted one cannot be used or
// the guard rejects the total. Its result, error and the guard's state are
// those of findClosestUnoccupiedDistanceStepped.
//
// It leaves the scan table and point memo of node prepared for the
// moveNodeToBest that follows (see optimizerUnoccupiedSearch).
func (optim *sizedOptimizer) findClosestUnoccupiedDistanceCounted(node *layoutgraph.Node, p *geo.Point, minimizingSelf bool, checked *optimizerCheckedSet, guard *limits.OptimizationWorkGuard) (float64, error) {
	search := &optim.unoccupied
	if !search.prepare(optim, node) {
		return optim.findClosestUnoccupiedDistanceStepped(node, p, minimizingSelf, checked, guard)
	}
	search.p = *p
	search.minimizingSelf = minimizingSelf
	search.checked = checked
	search.units = 0
	search.centerGrid(*p)
	defer func() {
		search.checked = nil
	}()
	mark := 0
	if checked != nil {
		mark = checked.len()
	}
	stepped := func() (float64, error) {
		if checked != nil {
			checked.truncate(mark)
		}
		return optim.findClosestUnoccupiedDistanceStepped(node, p, minimizingSelf, checked, guard)
	}
	distance, found, ok := search.run()
	if !ok {
		return stepped()
	}
	var steppedDistance float64
	replayed, err := chargeOrReplay(guard, search.units, func() error {
		var replayErr error
		steppedDistance, replayErr = stepped()
		return replayErr
	})
	if replayed {
		return steppedDistance, err
	}
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("could not find closest unoccupied distance for p=%v, node=%s", p, node.DebugID())
	}
	return distance, nil
}
