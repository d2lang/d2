package d2dagrelayout

// The rank-spacing and cross-rank passes as they were before the
// index-based rewrite (layout.go at d2 e22f4d566), kept as the reference of the property tests: the
// original code, with its functions renamed, and with one *legacyOrders
// parameter whose rec.* calls record the orders the code takes from ranging
// over its maps (they do nothing when rec is nil). Those orders are random
// (see map_order.go); the tests replay them in the rewrite.

import (
	"math"
	"sort"

	"github.com/d2lang/d2/d2graph"
)

func legacyGetRanks(g *d2graph.Graph, isHorizontal bool, rec *legacyOrders) (ranks [][]*d2graph.Object, objectRanks, startingParentRanks, endingParentRanks map[*d2graph.Object]int) {
	alignedObjects := make(map[float64][]*d2graph.Object)
	for _, obj := range g.Objects {
		if !obj.IsContainer() {
			if !isHorizontal {
				y := math.Ceil(obj.TopLeft.Y + obj.Height/2)
				alignedObjects[y] = append(alignedObjects[y], obj)
			} else {
				x := math.Ceil(obj.TopLeft.X + obj.Width/2)
				alignedObjects[x] = append(alignedObjects[x], obj)
			}
		}
	}

	levels := make([]float64, 0, len(alignedObjects))
	for l := range alignedObjects {
		levels = append(levels, l)
	}
	rec.levelOrder(levels)
	sort.Slice(levels, func(i, j int) bool {
		return levels[i] < levels[j]
	})

	ranks = make([][]*d2graph.Object, 0, len(levels))
	objectRanks = make(map[*d2graph.Object]int)
	for i, l := range levels {
		for _, obj := range alignedObjects[l] {
			objectRanks[obj] = i
		}
		ranks = append(ranks, alignedObjects[l])
	}

	startingParentRanks = make(map[*d2graph.Object]int)
	endingParentRanks = make(map[*d2graph.Object]int)
	for _, obj := range g.Objects {
		if obj.IsContainer() {
			continue
		}
		r := objectRanks[obj]
		// update all ancestor's min/max ranks
		for parent := obj.Parent; parent != nil && parent != g.Root; parent = parent.Parent {
			if start, has := startingParentRanks[parent]; !has || r < start {
				startingParentRanks[parent] = r
			}
			if end, has := endingParentRanks[parent]; !has || r > end {
				endingParentRanks[parent] = r
			}
		}
	}

	return ranks, objectRanks, startingParentRanks, endingParentRanks
}

type legacyCrossRankIndex struct {
	incident map[*d2graph.Object][]*d2graph.Edge
}

func legacyNewCrossRankIndex(g *d2graph.Graph) *legacyCrossRankIndex {
	index := &legacyCrossRankIndex{
		incident: make(map[*d2graph.Object][]*d2graph.Edge, len(g.Objects)),
	}
	for _, edge := range g.Edges {
		index.incident[edge.Src] = append(index.incident[edge.Src], edge)
		if edge.Dst != edge.Src {
			index.incident[edge.Dst] = append(index.incident[edge.Dst], edge)
		}
	}
	return index
}

func legacyShiftReachableDownIndexed(g *d2graph.Graph, index *legacyCrossRankIndex, obj *d2graph.Object, start, distance float64, isHorizontal, isMargin bool, rec *legacyOrders) map[*d2graph.Object]struct{} {
	rec.beginShift()
	q := []*d2graph.Object{obj}

	needsMove := make(map[*d2graph.Object]struct{})
	seen := make(map[*d2graph.Object]struct{})
	shifted := make(map[*d2graph.Object]struct{})
	shiftedEdges := make(map[*d2graph.Edge]struct{})
	queue := func(o *d2graph.Object) {
		if _, in := seen[o]; in {
			return
		}
		q = append(q, o)
	}

	// if object below is within this distance after shifting, also shift it
	threshold := 100.
	checkBelow := func(curr *d2graph.Object) {
		currBottom := curr.TopLeft.Y + curr.Height
		currRight := curr.TopLeft.X + curr.Width
		if isHorizontal {
			originalRight := currRight
			if _, in := shifted[curr]; in {
				originalRight -= distance
			}
			for _, other := range g.Objects {
				if other == curr || curr.IsDescendantOf(other) {
					continue
				}
				if originalRight < other.TopLeft.X &&
					other.TopLeft.X < originalRight+distance+threshold &&
					curr.TopLeft.Y < other.TopLeft.Y+other.Height &&
					other.TopLeft.Y < currBottom {
					queue(other)
				}
			}
		} else {
			originalBottom := currBottom
			if _, in := shifted[curr]; in {
				originalBottom -= distance
			}
			for _, other := range g.Objects {
				if other == curr || curr.IsDescendantOf(other) {
					continue
				}
				if originalBottom < other.TopLeft.Y &&
					other.TopLeft.Y < originalBottom+distance+threshold &&
					curr.TopLeft.X < other.TopLeft.X+other.Width &&
					other.TopLeft.X < currRight {
					queue(other)
				}
			}
		}
	}

	processQueue := func() {
		for len(q) > 0 {
			curr := q[0]
			q = q[1:]
			if _, was := seen[curr]; was {
				continue
			}
			// skip other objects behind start
			if curr != obj {
				if _, in := needsMove[curr]; !in {
					if isHorizontal {
						if curr.TopLeft.X < start {
							continue
						}
					} else {
						if curr.TopLeft.Y < start {
							continue
						}
					}
				}
			}

			if isHorizontal {
				_, shift := needsMove[curr]
				if !shift {
					if !isMargin {
						shift = start < curr.TopLeft.X
					} else {
						shift = start <= curr.TopLeft.X
					}
				}

				if shift {
					curr.TopLeft.X += distance
					shifted[curr] = struct{}{}
				}
			} else {
				_, shift := needsMove[curr]
				if !shift {
					if !isMargin {
						shift = start < curr.TopLeft.Y
					} else {
						shift = start <= curr.TopLeft.Y
					}
				}
				if shift {
					curr.TopLeft.Y += distance
					shifted[curr] = struct{}{}
				}
			}
			seen[curr] = struct{}{}
			rec.insert(curr)

			if curr.Parent != g.Root && !curr.IsDescendantOf(obj) {
				queue(curr.Parent)
			}

			for _, child := range curr.ChildrenArray {
				queue(child)
			}

			for _, e := range index.incident[curr] {
				if _, in := shiftedEdges[e]; in {
					continue
				}
				if e.Src == curr && e.Dst == curr {
					if isHorizontal {
						for _, p := range e.Route {
							p.X += distance
						}
					} else {
						for _, p := range e.Route {
							p.Y += distance
						}
					}
					shiftedEdges[e] = struct{}{}
					continue
				} else if e.Src == curr {
					last := e.Route[len(e.Route)-1]
					if isHorizontal {
						if start <= last.X &&
							e.Dst.TopLeft.X+e.Dst.Width < last.X+distance {
							needsMove[e.Dst] = struct{}{}
						}
					} else {
						if start <= last.Y &&
							e.Dst.TopLeft.Y+e.Dst.Height < last.Y+distance {
							needsMove[e.Dst] = struct{}{}
						}
					}
					queue(e.Dst)
					first := e.Route[0]
					startIndex := 0
					_, wasShifted := shifted[curr]
					if isHorizontal {
						if wasShifted && first.X < curr.TopLeft.X && first.X < start {
							first.X += distance
							startIndex++
						}
						for i := startIndex; i < len(e.Route); i++ {
							p := e.Route[i]
							if start <= p.X {
								p.X += distance
							}
						}
					} else {
						if wasShifted && first.Y < curr.TopLeft.Y && first.Y < start {
							first.Y += distance
							startIndex++
						}
						for i := startIndex; i < len(e.Route); i++ {
							p := e.Route[i]
							if start <= p.Y {
								p.Y += distance
							}
						}
					}
					shiftedEdges[e] = struct{}{}
				} else if e.Dst == curr {
					first := e.Route[0]
					if isHorizontal {
						if start <= first.X &&
							e.Src.TopLeft.X+e.Src.Width < first.X+distance {
							needsMove[e.Src] = struct{}{}
						}
					} else {
						if start <= first.Y &&
							e.Src.TopLeft.Y+e.Src.Height < first.Y+distance {
							needsMove[e.Src] = struct{}{}
						}
					}
					queue(e.Src)
					last := e.Route[len(e.Route)-1]
					endIndex := len(e.Route)
					_, wasShifted := shifted[curr]
					if isHorizontal {
						if wasShifted && last.X < curr.TopLeft.X && last.X < start {
							last.X += distance
							endIndex--
						}
						for i := 0; i < endIndex; i++ {
							p := e.Route[i]
							if start <= p.X {
								p.X += distance
							}
						}
					} else {
						if wasShifted && last.Y < curr.TopLeft.Y && last.Y < start {
							last.Y += distance
							endIndex--
						}
						for i := 0; i < endIndex; i++ {
							p := e.Route[i]
							if start <= p.Y {
								p.Y += distance
							}
						}
					}
					shiftedEdges[e] = struct{}{}
				}
			}

			// check for nodes below that need to move from the shift
			checkBelow(curr)
		}
	}

	processQueue()

	grown := make(map[*d2graph.Object]struct{})
	for o := range seen {
		rec.visit(o)
		if o.Parent == g.Root {
			continue
		}
		if _, in := shifted[o.Parent]; in {
			continue
		}
		if _, in := grown[o.Parent]; in {
			continue
		}

		for parent := o.Parent; parent != g.Root; parent = parent.Parent {
			if _, in := shifted[parent]; in {
				break
			}
			if _, in := grown[parent]; in {
				break
			}

			if isHorizontal {
				if parent.TopLeft.X < start {
					parent.Width += distance
					grown[parent] = struct{}{}

					checkBelow(parent)
					processQueue()
				}
			} else {
				if parent.TopLeft.Y < start {
					parent.Height += distance
					grown[parent] = struct{}{}

					checkBelow(parent)
					processQueue()
				}
			}
		}
	}

	increasedMargins := make(map[*d2graph.Object]struct{})
	movedObjects := make([]*d2graph.Object, 0, len(shifted))
	for obj := range shifted {
		movedObjects = append(movedObjects, obj)
	}
	for obj := range grown {
		movedObjects = append(movedObjects, obj)
	}
	for _, moved := range movedObjects {
		counts := true
		// check if any other shifted is directly above
		for _, other := range movedObjects {
			if other == moved {
				continue
			}
			if isHorizontal {
				if other.TopLeft.Y+other.Height < moved.TopLeft.Y ||
					moved.TopLeft.Y+moved.Height < other.TopLeft.Y {
					// doesn't line up vertically
					continue
				}

				// above and within threshold
				if other.TopLeft.X < moved.TopLeft.X &&
					moved.TopLeft.X < other.TopLeft.X+other.Width+threshold {
					counts = false
					break
				}
			} else {
				if other.TopLeft.X+other.Width < moved.TopLeft.X ||
					moved.TopLeft.X+moved.Width < other.TopLeft.X {
					// doesn't line up horizontally
					continue
				}

				// above and within threshold
				if other.TopLeft.Y < moved.TopLeft.Y &&
					moved.TopLeft.Y < other.TopLeft.Y+other.Height+threshold {
					counts = false
					break
				}
			}
		}
		if counts {
			increasedMargins[moved] = struct{}{}
		}
	}

	return increasedMargins
}

func legacyAdjustRankSpacing(g *d2graph.Graph, rankSep float64, isHorizontal bool, rec *legacyOrders) {
	ranks, objectRanks, startingParentRanks, endingParentRanks := legacyGetRanks(g, isHorizontal, rec)

	// shifting bottom rank down first, then moving up to next rank
	for rank := len(ranks) - 1; rank >= 0; rank-- {
		var startingParents []*d2graph.Object
		var endingParents []*d2graph.Object
		for _, obj := range ranks[rank] {
			if obj.Parent == g.Root {
				continue
			}
			if r, has := endingParentRanks[obj.Parent]; has && r == rank {
				endingParents = append(endingParents, obj.Parent)
			}
			if r, has := startingParentRanks[obj.Parent]; has && r == rank {
				startingParents = append(startingParents, obj.Parent)
			}
		}

		startingAncestorPositions := make(map[*d2graph.Object]float64)
		for len(startingParents) > 0 {
			var ancestors []*d2graph.Object
			for _, parent := range startingParents {
				_, padding := parent.Spacing()
				if _, has := startingAncestorPositions[parent]; !has {
					startingAncestorPositions[parent] = math.Inf(1)
				}
				var startPosition float64
				if isHorizontal {
					paddingIncrease := math.Max(0, padding.Left-rankSep/2)
					startPosition = parent.TopLeft.X - paddingIncrease
				} else {
					paddingIncrease := math.Max(0, padding.Top-rankSep/2)
					startPosition = parent.TopLeft.Y - paddingIncrease
				}
				startingAncestorPositions[parent] = math.Min(startingAncestorPositions[parent], startPosition)
				for _, child := range parent.ChildrenArray {
					if r, has := objectRanks[child]; has {
						if r != rank {
							continue
						}
					} else {
						if startingParentRanks[child] != rank {
							continue
						}
					}
					margin, _ := child.Spacing()
					if isHorizontal {
						startPosition = child.TopLeft.X - margin.Left - padding.Left
					} else {
						startPosition = child.TopLeft.Y - margin.Top - padding.Top
					}
					startingAncestorPositions[parent] = math.Min(startingAncestorPositions[parent], startPosition)
				}
				if parent.Parent != g.Root {
					ancestors = append(ancestors, parent.Parent)
				}
			}
			startingParents = ancestors
		}

		endingAncestorPositions := make(map[*d2graph.Object]float64)
		for len(endingParents) > 0 {
			var ancestors []*d2graph.Object
			for _, parent := range endingParents {
				_, padding := parent.Spacing()
				if _, has := endingAncestorPositions[parent]; !has {
					endingAncestorPositions[parent] = math.Inf(-1)
				}
				var endPosition float64
				if isHorizontal {
					endPosition = parent.TopLeft.X + parent.Width + padding.Right - rankSep/2.
				} else {
					endPosition = parent.TopLeft.Y + parent.Height + padding.Bottom - rankSep/2.
				}

				endingAncestorPositions[parent] = math.Max(endingAncestorPositions[parent], endPosition)
				for _, child := range parent.ChildrenArray {
					if r, has := objectRanks[child]; has {
						if r != rank {
							continue
						}
					} else {
						if endingParentRanks[child] != rank {
							continue
						}
					}
					margin, _ := child.Spacing()

					if isHorizontal {
						endPosition = child.TopLeft.X + child.Width + margin.Right + padding.Right
					} else {
						endPosition = child.TopLeft.Y + child.Height + margin.Bottom + padding.Bottom
					}
					endingAncestorPositions[parent] = math.Max(endingAncestorPositions[parent], endPosition)
				}
				if parent.Parent != g.Root {
					ancestors = append(ancestors, parent.Parent)
				}
			}
			endingParents = ancestors
		}

		startingAdjustmentOrder := make([]*d2graph.Object, 0, len(startingAncestorPositions))
		for ancestor := range startingAncestorPositions {
			startingAdjustmentOrder = append(startingAdjustmentOrder, ancestor)
		}
		rec.ancestorOrder(startingAdjustmentOrder, startingAncestorPositions)
		// adjust starting ancestors top-down
		sort.Slice(startingAdjustmentOrder, func(i, j int) bool {
			iPos := startingAncestorPositions[startingAdjustmentOrder[i]]
			jPos := startingAncestorPositions[startingAdjustmentOrder[j]]
			return iPos < jPos
		})

		endingAdjustmentOrder := make([]*d2graph.Object, 0, len(endingAncestorPositions))
		for ancestor := range endingAncestorPositions {
			endingAdjustmentOrder = append(endingAdjustmentOrder, ancestor)
		}
		rec.ancestorOrder(endingAdjustmentOrder, endingAncestorPositions)

		// adjust ending ancestors bottom-up
		sort.Slice(endingAdjustmentOrder, func(i, j int) bool {
			iPos := endingAncestorPositions[endingAdjustmentOrder[i]]
			jPos := endingAncestorPositions[endingAdjustmentOrder[j]]
			return jPos < iPos
		})

		for _, ancestor := range endingAdjustmentOrder {
			var position float64
			if isHorizontal {
				position = ancestor.TopLeft.X + ancestor.Width
			} else {
				position = ancestor.TopLeft.Y + ancestor.Height
			}
			endDelta := endingAncestorPositions[ancestor] - position
			if endDelta > 0 {
				for _, obj := range g.Objects {
					if !obj.IsContainer() {
						continue
					}
					start := startingParentRanks[obj]
					end := endingParentRanks[obj]
					if start <= rank && rank <= end {
						if isHorizontal && position <= obj.TopLeft.X+obj.Width {
							obj.Width += endDelta
						} else if !isHorizontal &&
							position <= obj.TopLeft.Y+obj.Height {
							obj.Height += endDelta
						}
					}
				}
				shiftDown(g, position, endDelta, isHorizontal)
			}
		}

		for _, ancestor := range startingAdjustmentOrder {
			var position float64
			if isHorizontal {
				position = ancestor.TopLeft.X
			} else {
				position = ancestor.TopLeft.Y
			}
			startDelta := position - startingAncestorPositions[ancestor]
			if startDelta > 0 {
				for _, obj := range g.Objects {
					if !obj.IsContainer() {
						continue
					}
					start := startingParentRanks[obj]
					end := endingParentRanks[obj]
					if start <= rank && rank <= end {
						if isHorizontal && obj.TopLeft.X <= position {
							obj.Width += startDelta
						} else if !isHorizontal && obj.TopLeft.Y <= position {
							obj.Height += startDelta
						}
					}
				}
				shiftUp(g, position, startDelta, isHorizontal)
			}
		}
	}
}

func legacyAdjustCrossRankSpacing(g *d2graph.Graph, rankSep float64, isHorizontal bool, rec *legacyOrders) {
	index := legacyNewCrossRankIndex(g)
	var prevMarginTop, prevMarginBottom, prevMarginLeft, prevMarginRight map[*d2graph.Object]float64
	if isHorizontal {
		prevMarginLeft = make(map[*d2graph.Object]float64)
		prevMarginRight = make(map[*d2graph.Object]float64)
	} else {
		prevMarginTop = make(map[*d2graph.Object]float64)
		prevMarginBottom = make(map[*d2graph.Object]float64)
	}
	for _, obj := range g.Objects {
		if obj.IsGridDiagram() {
			continue
		}
		margin, padding := obj.Spacing()
		if !isHorizontal {
			if prevShift, has := prevMarginBottom[obj]; has {
				margin.Bottom -= prevShift
			}
			if margin.Bottom > 0 {
				increased := legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.Y+obj.Height, margin.Bottom, isHorizontal, true, rec)
				for o := range increased {
					prevMarginBottom[o] = math.Max(prevMarginBottom[o], margin.Bottom)
				}
			}
			if padding.Bottom > 0 {
				legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.Y+obj.Height, padding.Bottom, isHorizontal, false, rec)
				obj.Height += padding.Bottom
			}
			if prevShift, has := prevMarginTop[obj]; has {
				margin.Top -= prevShift
			}
			if margin.Top > 0 {
				increased := legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.Y, margin.Top, isHorizontal, true, rec)
				for o := range increased {
					prevMarginTop[o] = math.Max(prevMarginTop[o], margin.Top)
				}
			}
			if padding.Top > 0 {
				legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.Y, padding.Top, isHorizontal, false, rec)
				obj.Height += padding.Top
			}
		} else {
			if prevShift, has := prevMarginRight[obj]; has {
				margin.Right -= prevShift
			}
			if margin.Right > 0 {
				increased := legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.X+obj.Width, margin.Right, isHorizontal, true, rec)
				for o := range increased {
					prevMarginRight[o] = math.Max(prevMarginRight[o], margin.Right)
				}
			}
			if padding.Right > 0 {
				legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.X+obj.Width, padding.Right, isHorizontal, false, rec)
				obj.Width += padding.Right
			}
			if prevShift, has := prevMarginLeft[obj]; has {
				margin.Left -= prevShift
			}
			if margin.Left > 0 {
				increased := legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.X, margin.Left, isHorizontal, true, rec)
				for o := range increased {
					prevMarginLeft[o] = math.Max(prevMarginLeft[o], margin.Left)
				}
			}
			if padding.Left > 0 {
				legacyShiftReachableDownIndexed(g, index, obj, obj.TopLeft.X, padding.Left, isHorizontal, false, rec)
				obj.Width += padding.Left
			}
		}
	}
}
