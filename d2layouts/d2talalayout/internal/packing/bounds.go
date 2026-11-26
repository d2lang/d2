package packing

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/graphbounds"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// The bounds below are graphbounds.NodeBoundingBox, BoundingBox and
// FixedBoundingBox with the corners kept as values: the same guard steps in
// the same order, the same float operations and the same errors.
// graphbounds allocates two points per node and a seen map per container
// level walk, and BinPack evaluates bounds for every candidate position.

// packNodeBoundingBox is graphbounds.NodeBoundingBox with value corners.
func packNodeBoundingBox(node *layoutgraph.Node, allNodes layoutgraph.Nodes, guard *limits.WorkGuard) (geo.Point, geo.Point, error) {
	return packNodeBounds(node, allNodes, &graphbounds.BodyExtrema{}, guard)
}

func packNodeBounds(node *layoutgraph.Node, allNodes layoutgraph.Nodes, extrema *graphbounds.BodyExtrema, guard *limits.WorkGuard) (geo.Point, geo.Point, error) {
	if err := guard.Step(); err != nil {
		return geo.Point{}, geo.Point{}, err
	}
	if node == nil || node.TopLeft == nil {
		return geo.Point{}, geo.Point{}, invariant.New("BinPack bounding box contains an unplaced node")
	}
	topLeft := geo.Point{X: node.TopLeft.X, Y: node.TopLeft.Y}
	bottomRight := geo.Point{X: math.Round(topLeft.X + node.Width), Y: math.Round(topLeft.Y + node.Height)}
	if dx, dy := node.ModifierElementAdjustments(); dx != 0 || dy != 0 {
		topLeft.Y -= dy
		bottomRight.X += dx
	}
	topLeft.X -= node.LoopOffsets[geo.Left]
	topLeft.Y -= node.LoopOffsets[geo.Top]
	bottomRight.X += node.LoopOffsets[geo.Right]
	bottomRight.Y += node.LoopOffsets[geo.Bottom]

	if node.Label != nil && node.Label.Position.IsOutside() && allNodes != nil {
		if err := extrema.Measure(allNodes, guard); err != nil {
			return geo.Point{}, geo.Point{}, err
		}
		labelTopLeft := packOutsideLabelTopLeft(node, node.Label.Position, node.Label.Width, node.Label.Height)
		boundaryPadding := float64(label.PADDING)
		outsidePadding := 2. * label.PADDING
		if labelTopLeft.X < topLeft.X {
			padding := boundaryPadding
			if extrema.Left < node.TopLeft.X {
				padding = outsidePadding
			}
			topLeft.X = math.Floor(labelTopLeft.X - padding)
		}
		if labelTopLeft.Y < topLeft.Y {
			padding := boundaryPadding
			if extrema.Top < node.TopLeft.Y {
				padding = outsidePadding
			}
			topLeft.Y = math.Floor(labelTopLeft.Y - padding)
		}
		if labelTopLeft.X+node.Label.Width > bottomRight.X {
			padding := boundaryPadding
			if extrema.Right > node.TopLeft.X+node.Width {
				padding = outsidePadding
			}
			bottomRight.X = math.Ceil(labelTopLeft.X + node.Label.Width + padding)
		}
		if labelTopLeft.Y+node.Label.Height > bottomRight.Y {
			padding := boundaryPadding
			if extrema.Bottom > node.TopLeft.Y+node.Height {
				padding = outsidePadding
			}
			bottomRight.Y = math.Ceil(labelTopLeft.Y + node.Label.Height + padding)
		}
	}
	if node.Icon != nil && !node.IsImage() && node.Icon.Position.IsOutside() && allNodes != nil {
		iconSize := node.OutsideIconReserve()
		iconTopLeft := packOutsideLabelTopLeft(node, node.Icon.Position, iconSize, iconSize)
		outsidePadding := 2. * label.PADDING
		topLeft.X = math.Min(topLeft.X, math.Floor(iconTopLeft.X-outsidePadding))
		topLeft.Y = math.Min(topLeft.Y, math.Floor(iconTopLeft.Y-outsidePadding))
		bottomRight.X = math.Max(bottomRight.X, math.Ceil(iconTopLeft.X+iconSize+outsidePadding))
		bottomRight.Y = math.Max(bottomRight.Y, math.Ceil(iconTopLeft.Y+iconSize+outsidePadding))
	}
	return topLeft, bottomRight, nil
}

// packOutsideLabelTopLeft is *node.LabelTopLeft(position, width, height)
// for an outside position: label.Position.GetPointOnBox(node.GetBox(),
// label.PADDING, width, height) with the same operations on values (the
// library returns its point and the box center as new points).
func packOutsideLabelTopLeft(node *layoutgraph.Node, position label.Position, width, height float64) geo.Point {
	box := node.Shape.GetBox()
	padding := float64(label.PADDING)
	p := geo.Point{X: box.TopLeft.X, Y: box.TopLeft.Y}
	centerX := box.TopLeft.X + box.Width/2
	centerY := box.TopLeft.Y + box.Height/2
	switch position {
	case label.OutsideTopLeft:
		p.X -= padding
		p.Y -= padding + height
	case label.OutsideTopCenter:
		p.X = centerX - width/2
		p.Y -= padding + height
	case label.OutsideTopRight:
		p.X += box.Width - width - padding
		p.Y -= padding + height
	case label.OutsideLeftTop:
		p.X -= padding + width
		p.Y += padding
	case label.OutsideLeftMiddle:
		p.X -= padding + width
		p.Y = centerY - height/2
	case label.OutsideLeftBottom:
		p.X -= padding + width
		p.Y += box.Height - height - padding
	case label.OutsideRightTop:
		p.X += box.Width + padding
		p.Y += padding
	case label.OutsideRightMiddle:
		p.X += box.Width + padding
		p.Y = centerY - height/2
	case label.OutsideRightBottom:
		p.X += box.Width + padding
		p.Y += box.Height - height - padding
	case label.OutsideBottomLeft:
		p.X += padding
		p.Y += box.Height + padding
	case label.OutsideBottomCenter:
		p.X = centerX - width/2
		p.Y += box.Height + padding
	case label.OutsideBottomRight:
		p.X += box.Width - width - padding
		p.Y += box.Height + padding
	default:
		return *node.LabelTopLeft(position, width, height)
	}
	return p
}

// packBoundingBox is graphbounds.BoundingBox with value corners.
func packBoundingBox(nodes layoutgraph.Nodes, guard *limits.WorkGuard) (geo.Point, geo.Point, error) {
	if len(nodes) == 0 {
		return geo.Point{X: math.Inf(-1), Y: math.Inf(-1)}, geo.Point{X: math.Inf(1), Y: math.Inf(1)}, guard.Finish()
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	var extrema graphbounds.BodyExtrema
	for _, node := range nodes {
		topLeft, bottomRight, err := packNodeBounds(node, nodes, &extrema, guard)
		if err != nil {
			return geo.Point{}, geo.Point{}, err
		}
		minX = math.Min(minX, topLeft.X)
		minY = math.Min(minY, topLeft.Y)
		maxX = math.Max(maxX, bottomRight.X)
		maxY = math.Max(maxY, bottomRight.Y)
	}
	if err := guard.Finish(); err != nil {
		return geo.Point{}, geo.Point{}, err
	}
	return geo.Point{X: minX, Y: minY}, geo.Point{X: maxX, Y: maxY}, nil
}

// packContainerLevel is graphbounds' containerLevel; the ancestry seen set
// is a short list (chains are shallow) instead of a map.
func packContainerLevel(node *layoutgraph.Node, guard *limits.WorkGuard) (int, error) {
	var storage [16]*layoutgraph.Node
	seen := storage[:0]
	level := 0
	for current := node; current != nil; current = current.OwningContainer() {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		for _, previous := range seen {
			if previous == current {
				return 0, invariant.New("BinPack found a cycle in container ancestry")
			}
		}
		seen = append(seen, current)
		level++
	}
	return level, nil
}

// packFixedOrigin is graphbounds' fixedOrigin returning the origin's value.
func packFixedOrigin(nodes layoutgraph.Nodes, guard *limits.WorkGuard) (geo.Point, bool, error) {
	var container *layoutgraph.Node
	minimumLevel := math.MaxInt
	for _, node := range nodes {
		if err := guard.Step(); err != nil {
			return geo.Point{}, false, err
		}
		current := node.OwningContainer()
		if current == nil {
			container = nil
			break
		}
		level, err := packContainerLevel(current, guard)
		if err != nil {
			return geo.Point{}, false, err
		}
		if level < minimumLevel {
			minimumLevel = level
			container = current
		}
	}
	for _, child := range nodes {
		if err := guard.Step(); err != nil {
			return geo.Point{}, false, err
		}
		if child.OwningContainer() != container {
			continue
		}
		if origin := child.FixedOrigin(); origin != nil {
			return *origin, true, nil
		}
	}
	return geo.Point{}, false, guard.Finish()
}

// packFixedBoundingBox is graphbounds.FixedBoundingBox with value corners.
func packFixedBoundingBox(nodes layoutgraph.Nodes, guard *limits.WorkGuard) (geo.Point, geo.Point, error) {
	topLeft, bottomRight, err := packBoundingBox(nodes, guard)
	if err != nil {
		return geo.Point{}, geo.Point{}, err
	}
	origin, hasOrigin, err := packFixedOrigin(nodes, guard)
	if err != nil {
		return geo.Point{}, geo.Point{}, err
	}
	if hasOrigin {
		topLeft = origin
	}
	return topLeft, bottomRight, guard.Finish()
}
