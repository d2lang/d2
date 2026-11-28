package graphbounds

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/invariant"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// WorkStepper is the aggregate accounting contract shared by callers. Bounds
// never create or reset a budget: every scan is charged to the caller's guard.
type WorkStepper interface {
	// Step charges one unit of bounds work.
	Step() error
	// Finish observes cancellation after the final bounds operation.
	Finish() error
}

// BodyExtrema caches the outermost body coordinates for one unchanged set
// of peers. Every exterior caption in a bounding-box calculation compares
// against those same coordinates.
type BodyExtrema struct {
	Left, Top, Right, Bottom float64
	measured                 bool
}

func (b *BodyExtrema) Measure(nodes layoutgraph.Nodes, guard WorkStepper) error {
	if b.measured {
		return nil
	}
	b.Left, b.Top = math.Inf(1), math.Inf(1)
	b.Right, b.Bottom = math.Inf(-1), math.Inf(-1)
	for _, n := range nodes {
		if err := guard.Step(); err != nil {
			return err
		}
		if n == nil || n.TopLeft == nil {
			continue
		}
		// Use the same strict comparisons as a scan of an individual side.
		// Equal edges share the boundary and NaN cannot disqualify a peer.
		if x := n.TopLeft.X; x < b.Left {
			b.Left = x
		}
		if y := n.TopLeft.Y; y < b.Top {
			b.Top = y
		}
		if x := n.TopLeft.X + n.Width; x > b.Right {
			b.Right = x
		}
		if y := n.TopLeft.Y + n.Height; y > b.Bottom {
			b.Bottom = y
		}
	}
	b.measured = true
	return nil
}

// NodeBoundingBox computes the packing-compatible bounds for one placed node. When
// allNodes is non-nil, outside labels and icons reserve their exact boundary
// space and outside-label extremity scans consume the shared work budget.
func NodeBoundingBox(
	node *layoutgraph.Node,
	allNodes layoutgraph.Nodes,
	guard WorkStepper,
) (*geo.Point, *geo.Point, error) {
	bounds, err := nodeBounds(node, allNodes, &BodyExtrema{}, guard)
	if err != nil {
		return nil, nil, err
	}
	return geo.NewPoint(bounds.minX, bounds.minY), geo.NewPoint(bounds.maxX, bounds.maxY), nil
}

// box holds a node's bounds as values: the corners NodeBoundingBox returns,
// without allocating them.
type box struct {
	minX, minY, maxX, maxY float64
}

// nodeBounds is NodeBoundingBox with the corners kept in locals: the same
// steps, the same floating-point operations in the same order on the same
// values (topLeft.X/Y are minX/minY, bottomRight.X/Y are maxX/maxY).
func nodeBounds(
	node *layoutgraph.Node,
	allNodes layoutgraph.Nodes,
	extrema *BodyExtrema,
	guard WorkStepper,
) (box, error) {
	if err := guard.Step(); err != nil {
		return box{}, err
	}
	if node == nil || node.TopLeft == nil {
		return box{}, invariant.New("BinPack bounding box contains an unplaced node")
	}
	minX, minY := node.TopLeft.X, node.TopLeft.Y
	maxX, maxY := math.Round(minX+node.Width), math.Round(minY+node.Height)
	if dx, dy := node.ModifierElementAdjustments(); dx != 0 || dy != 0 {
		minY -= dy
		maxX += dx
	}
	minX -= node.LoopOffsets[geo.Left]
	minY -= node.LoopOffsets[geo.Top]
	maxX += node.LoopOffsets[geo.Right]
	maxY += node.LoopOffsets[geo.Bottom]

	if node.Label != nil && node.Label.Position.IsOutside() && allNodes != nil {
		if err := extrema.Measure(allNodes, guard); err != nil {
			return box{}, err
		}
		labelTopLeft := node.LabelTopLeft(node.Label.Position, node.Label.Width, node.Label.Height)
		boundaryPadding := float64(label.PADDING)
		outsidePadding := 2. * label.PADDING
		if labelTopLeft.X < minX {
			padding := boundaryPadding
			if extrema.Left < node.TopLeft.X {
				padding = outsidePadding
			}
			minX = math.Floor(labelTopLeft.X - padding)
		}
		if labelTopLeft.Y < minY {
			padding := boundaryPadding
			if extrema.Top < node.TopLeft.Y {
				padding = outsidePadding
			}
			minY = math.Floor(labelTopLeft.Y - padding)
		}
		if labelTopLeft.X+node.Label.Width > maxX {
			padding := boundaryPadding
			if extrema.Right > node.TopLeft.X+node.Width {
				padding = outsidePadding
			}
			maxX = math.Ceil(labelTopLeft.X + node.Label.Width + padding)
		}
		if labelTopLeft.Y+node.Label.Height > maxY {
			padding := boundaryPadding
			if extrema.Bottom > node.TopLeft.Y+node.Height {
				padding = outsidePadding
			}
			maxY = math.Ceil(labelTopLeft.Y + node.Label.Height + padding)
		}
	}
	if node.Icon != nil && !node.IsImage() && node.Icon.Position.IsOutside() && allNodes != nil {
		iconSize := node.OutsideIconReserve()
		iconTopLeft := node.IconTopLeft(node.Icon.Position, iconSize, iconSize)
		outsidePadding := 2. * label.PADDING
		minX = math.Min(minX, math.Floor(iconTopLeft.X-outsidePadding))
		minY = math.Min(minY, math.Floor(iconTopLeft.Y-outsidePadding))
		maxX = math.Max(maxX, math.Ceil(iconTopLeft.X+iconSize+outsidePadding))
		maxY = math.Max(maxY, math.Ceil(iconTopLeft.Y+iconSize+outsidePadding))
	}
	return box{minX: minX, minY: minY, maxX: maxX, maxY: maxY}, nil
}

// BoundingBox computes the packing-compatible bounds for a node set.
func BoundingBox(nodes layoutgraph.Nodes, guard WorkStepper) (*geo.Point, *geo.Point, error) {
	if len(nodes) == 0 {
		return geo.NewPoint(math.Inf(-1), math.Inf(-1)), geo.NewPoint(math.Inf(1), math.Inf(1)), guard.Finish()
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	var extrema BodyExtrema
	for _, node := range nodes {
		bounds, err := nodeBounds(node, nodes, &extrema, guard)
		if err != nil {
			return nil, nil, err
		}
		minX = math.Min(minX, bounds.minX)
		minY = math.Min(minY, bounds.minY)
		maxX = math.Max(maxX, bounds.maxX)
		maxY = math.Max(maxY, bounds.maxY)
	}
	if err := guard.Finish(); err != nil {
		return nil, nil, err
	}
	return geo.NewPoint(minX, minY), geo.NewPoint(maxX, maxY), nil
}

func containerLevel(node *layoutgraph.Node, guard WorkStepper) (int, error) {
	level := 0
	seen := make(map[*layoutgraph.Node]struct{})
	for current := node; current != nil; current = current.OwningContainer() {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		if _, exists := seen[current]; exists {
			return 0, invariant.New("BinPack found a cycle in container ancestry")
		}
		seen[current] = struct{}{}
		level++
	}
	return level, nil
}

func fixedOrigin(nodes layoutgraph.Nodes, guard WorkStepper) (*geo.Point, error) {
	var container *layoutgraph.Node
	minimumLevel := math.MaxInt
	for _, node := range nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		current := node.OwningContainer()
		if current == nil {
			container = nil
			break
		}
		level, err := containerLevel(current, guard)
		if err != nil {
			return nil, err
		}
		if level < minimumLevel {
			minimumLevel = level
			container = current
		}
	}
	for _, child := range nodes {
		if err := guard.Step(); err != nil {
			return nil, err
		}
		if child.OwningContainer() != container {
			continue
		}
		if origin := child.FixedOrigin(); origin != nil {
			return origin, nil
		}
	}
	return nil, guard.Finish()
}

// FixedBoundingBox computes node-set bounds and substitutes the inherited fixed origin
// used by packing when one of the relevant children is pinned.
func FixedBoundingBox(nodes layoutgraph.Nodes, guard WorkStepper) (*geo.Point, *geo.Point, error) {
	topLeft, bottomRight, err := BoundingBox(nodes, guard)
	if err != nil {
		return nil, nil, err
	}
	origin, err := fixedOrigin(nodes, guard)
	if err != nil {
		return nil, nil, err
	}
	if origin != nil {
		topLeft = origin
	}
	return topLeft, bottomRight, guard.Finish()
}
