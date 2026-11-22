package placement

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// placementRow is one offset row (currentY) of iterPlacementsAroundPoint.
type placementRow struct {
	offset float64 // currentY
	y      float64 // rounded point Y
	key    uint32  // packed cell key of y
	past   bool    // y is past the fixed origin
	inside bool    // key is inside the dedupe window
	dy     uint32  // key - window.baseY when inside
}

// countPlacementPoints is generatePlacementPoints with a counting charge
// (charge.guard == nil): the same points in the same order, the same scratch
// state and the same units. Around every diamond point the stepped generator
// visits a box of offsets (columns currentX times rows currentY) and rounds,
// keys and fixed-origin-checks each offset; the rows do not depend on the
// column, so they are computed once per diamond point here, with the same
// float loops and expressions, and only the duplicate test runs per offset.
func (optim *sizedOptimizer) countPlacementPoints(
	node *layoutgraph.Node,
	median *geo.Point,
	dist float64,
	minimizingSelf bool,
	scratch *placementPointsScratch,
	charge *placementCharge,
) error {
	if charge.guard != nil {
		return optim.generatePlacementPoints(node, median, dist, minimizingSelf, scratch, charge)
	}
	var units uint64
	for x := dist; x >= -dist; x-- {
		units++
		maxY := dist - math.Abs(x)
		for y := 0.0; y <= maxY; y++ {
			units++
			if y != 0 {
				if err := optim.countPlacementsAroundPoint(node, median, x, -y, minimizingSelf, scratch, &units); err != nil {
					return err
				}
			}
			if err := optim.countPlacementsAroundPoint(node, median, x, y, minimizingSelf, scratch, &units); err != nil {
				return err
			}
		}
	}
	// The unit total is far below overflow: at most 203^2 diamond points of at
	// most 101^2 offsets each (larger nodes are never searched).
	if err := charge.add(units); err != nil {
		return err
	}
	return optim.generatePlacementTail(node, median, scratch, charge)
}

// countPlacementsAroundPoint is iterPlacementsAroundPoint(node, x, y,
// minimizingSelf, add) with a counting add.
func (optim *sizedOptimizer) countPlacementsAroundPoint(
	node *layoutgraph.Node,
	median *geo.Point,
	x, y float64,
	minimizingSelf bool,
	scratch *placementPointsScratch,
	units *uint64,
) error {
	width, height := node.Width, node.Height
	x *= node.Graph.CellSize
	y *= node.Graph.CellSize
	increment := node.Graph.CellSize
	cellSize := optim.cellSize
	fixedOrigin := optim.fixedOrigin
	window := &scratch.window

	*units++
	if err := optim.countPlacement(
		roundToNearestCellSize(median.X+x, cellSize),
		roundToNearestCellSize(median.Y+y, cellSize),
		scratch,
	); err != nil || !minimizingSelf {
		return err
	}

	rows := scratch.rows[:0]
	for currentY := y - height; currentY <= y; currentY += increment {
		pY := roundToNearestCellSize(median.Y+currentY, cellSize)
		key := uint32(int64(pY / cellSize))
		dy := key - window.baseY
		rows = append(rows, placementRow{
			offset: currentY,
			y:      pY,
			key:    key,
			past:   fixedOrigin != nil && pY < fixedOrigin.Y,
			inside: dy < window.height,
			dy:     dy,
		})
	}
	scratch.rows = rows

	for currentX := x - width; currentX <= x; currentX += increment {
		pX := roundToNearestCellSize(median.X+currentX, cellSize)
		pastX := fixedOrigin != nil && pX < fixedOrigin.X
		keyX := uint32(int64(pX / cellSize))
		dx := keyX - window.baseX
		insideX := dx < window.width
		for i := range rows {
			row := &rows[i]
			if currentX == x && row.offset == y {
				continue
			}
			*units++
			if pastX || row.past {
				continue
			}
			if insideX && row.inside {
				bit := row.dy*window.width + dx
				word, mask := bit>>6, uint64(1)<<(bit&63)
				if window.words[word]&mask != 0 {
					continue
				}
				if len(scratch.points) >= maxOptimizerPlacementCandidates {
					return placementLimitError()
				}
				window.words[word] |= mask
			} else {
				key := uint64(keyX)<<32 | uint64(row.key)
				if _, dup := scratch.seen[key]; dup {
					continue
				}
				if len(scratch.points) >= maxOptimizerPlacementCandidates {
					return placementLimitError()
				}
				scratch.seen[key] = struct{}{}
			}
			scratch.seenCount++
			scratch.points = append(scratch.points, geo.Point{X: pX, Y: row.y})
		}
	}
	return nil
}

// countPlacement is the generator's add for a rounded point, without the
// charge.
func (optim *sizedOptimizer) countPlacement(pX, pY float64, scratch *placementPointsScratch) error {
	if fo := optim.fixedOrigin; fo != nil && (pX < fo.X || pY < fo.Y) {
		return nil
	}
	window := &scratch.window
	kx := uint32(int64(pX / optim.cellSize))
	ky := uint32(int64(pY / optim.cellSize))
	if dx, dy := kx-window.baseX, ky-window.baseY; dx < window.width && dy < window.height {
		bit := dy*window.width + dx
		word, mask := bit>>6, uint64(1)<<(bit&63)
		if window.words[word]&mask != 0 {
			return nil
		}
		if len(scratch.points) >= maxOptimizerPlacementCandidates {
			return placementLimitError()
		}
		window.words[word] |= mask
	} else {
		key := uint64(kx)<<32 | uint64(ky)
		if _, dup := scratch.seen[key]; dup {
			return nil
		}
		if len(scratch.points) >= maxOptimizerPlacementCandidates {
			return placementLimitError()
		}
		scratch.seen[key] = struct{}{}
	}
	scratch.seenCount++
	scratch.points = append(scratch.points, geo.Point{X: pX, Y: pY})
	return nil
}

func placementLimitError() error {
	return fmt.Errorf("%w: TALA LocalOptimize placement candidates exceed limit %d", limits.ErrOptimizationResourceLimit, maxOptimizerPlacementCandidates)
}
