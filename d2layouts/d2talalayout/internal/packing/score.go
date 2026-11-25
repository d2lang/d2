package packing

import (
	"cmp"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// Readability and an attainable authored frame take precedence over compactness.
// Keep these as independent criteria so coordinate magnitude cannot change
// their priority or hide differences between footprint scores.
type packingScore struct {
	decorationBodyOverlaps int
	decorationPairs        int
	excessArea             float64
	footprint              float64
}

func (s packingScore) compare(t packingScore) int {
	if c := cmp.Compare(s.decorationBodyOverlaps, t.decorationBodyOverlaps); c != 0 {
		return c
	}
	if c := cmp.Compare(s.decorationPairs, t.decorationPairs); c != 0 {
		return c
	}
	if c := cmp.Compare(s.excessArea, t.excessArea); c != 0 {
		return c
	}
	return cmp.Compare(s.footprint, t.footprint)
}

func (s packingScore) usable() bool {
	return s.footprint < math.Inf(1) && s.excessArea < math.Inf(1)
}

var worstPackingScore = packingScore{math.MaxInt, math.MaxInt, math.Inf(1), math.Inf(1)}

type packingFrame struct {
	useCanvasBounds       bool
	decoratedCanvas       bool
	horizontal            bool
	desired, reserved     float64
	gridWidth, gridHeight float64
	decorations           decorationComparisons
}

func newPackingFrame(root *layoutgraph.Node, children layoutgraph.Nodes, edgeless bool, guard *limits.WorkGuard) (packingFrame, error) {
	frame := packingFrame{useCanvasBounds: root == nil, decoratedCanvas: root == nil && edgeless}
	// Fixed decorations are sufficient for comparing these positions only
	// while unplaced captions cannot compete for space around multiple tiles.
	if frame.decoratedCanvas {
		unplaced := 0
		for _, n := range children {
			if err := guard.Step(); err != nil {
				return packingFrame{}, err
			}
			if l := n.Label; l != nil && !l.PositionFixed() && (l.Width > n.Width || l.Height > n.Height) {
				unplaced++
				if unplaced > 1 || n.Icon != nil && n.Icon.PositionFixed() && n.Icon.Position.IsOutside() {
					frame.decoratedCanvas = false
					break
				}
			}
		}
	}
	for _, child := range children {
		if err := guard.Step(); err != nil {
			return packingFrame{}, err
		}
		// The canvas is cropped to the visible tiles. Other components can
		// carry decorations or links that the footprint score cannot protect.
		if frame.useCanvasBounds && (!plainTile(child) || child.Label != nil &&
			(child.Label.Position.IsOutside() || child.Label.Width > child.Width || child.Label.Height > child.Height)) {
			frame.useCanvasBounds = false
		}
		if child.Label != nil && child.Label.PositionFixed() && child.Label.Position.IsOutside() && frame.canCompareDecorations(child) {
			group, err := frame.newDecorationGroup(children, root, guard)
			if err != nil {
				return packingFrame{}, err
			}
			frame.decorations = decorationComparisons{first: group, second: group}
			break
		}
	}
	if root == nil || root.DesiredWidth == nil && root.DesiredHeight == nil {
		return frame, nil
	}
	both := root.DesiredWidth != nil && root.DesiredHeight != nil
	if !both {
		frame.horizontal = root.DesiredWidth != nil
		if frame.horizontal {
			frame.desired = *root.DesiredWidth
		} else {
			frame.desired = *root.DesiredHeight
		}
	}
	if root.Graph == nil || !root.IsRectangular() || len(children) < 2 {
		return frame, nil
	}
	needed := float64(len(children)-1) * layoutgraph.NodeGap
	first := children[0]
	for _, child := range children {
		if err := guard.Step(); err != nil {
			return packingFrame{}, err
		}
		if !plainTile(child) || child.FixedTopLeft != nil ||
			child.Label != nil && child.Label.Position.IsOutside() {
			return frame, nil
		}
		if both && (child.Width != first.Width || child.Height != first.Height) {
			return frame, nil
		}
		if frame.horizontal {
			needed += child.Width
		} else {
			needed += child.Height
		}
	}
	padding := root.Graph.ContainerPadding(root, false)
	if both {
		width := *root.DesiredWidth - padding.Left() - padding.Right()
		height := *root.DesiredHeight - padding.Top() - padding.Bottom()
		columns := math.Floor((width + layoutgraph.NodeGap) / (first.Width + layoutgraph.NodeGap))
		rows := math.Floor((height + layoutgraph.NodeGap) / (first.Height + layoutgraph.NodeGap))
		if columns >= 1 && rows >= 1 && columns*rows >= float64(len(children)) {
			frame.gridWidth, frame.gridHeight = width, height
		}
		return frame, nil
	}
	available := frame.desired - padding.Top() - padding.Bottom()
	if frame.horizontal {
		available = frame.desired - padding.Left() - padding.Right()
	}
	if needed <= available {
		frame.reserved = available
	}
	return frame, nil
}

// A frame's walls constrain label positions that are still automatic. Score
// its plain tiles here; the canvas can also compare icons and near groups.
func (frame packingFrame) canCompareDecorations(n *layoutgraph.Node) bool {
	return captionTile(n) && (frame.decoratedCanvas || n.Icon == nil && len(n.Nears) == 0)
}

func plainTile(n *layoutgraph.Node) bool {
	return n.Icon == nil && len(n.Nears) == 0 && captionTile(n)
}

// A fixed caption moves with its tile, including when the tile carries an
// icon or moves alongside a near peer in the same rigid component.
func captionTile(n *layoutgraph.Node) bool {
	return !n.IsContainer() && len(n.Edges) == 0 &&
		n.Hierarchy == nil && n.Cluster == nil && n.Sequence == nil && !n.IsInvisible &&
		!n.Is3D && !n.IsMultiple && n.IsRectangular()
}

func (frame packingFrame) score(nodes layoutgraph.Nodes, guard *limits.WorkGuard) (packingScore, error) {
	bounds := packFixedBoundingBox
	if frame.useCanvasBounds {
		bounds = packBoundingBox
	}
	topLeft, bottomRight, err := bounds(nodes, guard)
	if err != nil {
		return packingScore{}, err
	}
	width := bottomRight.X - topLeft.X
	height := bottomRight.Y - topLeft.Y
	score := packingScore{}
	if frame.gridWidth > 0 {
		score.excessArea = math.Max(width, frame.gridWidth)*math.Max(height, frame.gridHeight) - frame.gridWidth*frame.gridHeight
	}
	extent := &height
	if frame.horizontal {
		extent = &width
	}
	shortfall := 0.
	if frame.reserved > 0 {
		*extent = math.Max(*extent, frame.reserved)
	} else if frame.desired > 0 {
		shortfall = math.Max(0, frame.desired-*extent)
	}
	score.footprint = width*height + math.Pow(width-height, 2)*subgraphSquareDampener +
		math.Pow(shortfall, 2)*subgraphSquareDampener
	score.decorationBodyOverlaps, err = frame.decorations.count(guard)
	if err != nil {
		return packingScore{}, err
	}
	score.decorationPairs, err = frame.decorations.decorationOverlapCount(guard)
	if err != nil {
		return packingScore{}, err
	}
	return score, guard.Finish()
}

type decorationGroup struct {
	bodies      layoutgraph.Nodes
	decorations []packedDecoration
}

// Measured captions and icons keep their offsets during rigid packing trials.
type packedDecoration struct {
	owner         *layoutgraph.Node
	offset        geo.Point
	width, height float64
}

func (c packedDecoration) box() geo.Box {
	return geo.Box{TopLeft: &geo.Point{X: c.owner.TopLeft.X + c.offset.X, Y: c.owner.TopLeft.Y + c.offset.Y}, Width: c.width, Height: c.height}
}

func (frame packingFrame) newDecorationGroup(nodes layoutgraph.Nodes, root *layoutgraph.Node, guard *limits.WorkGuard) (decorationGroup, error) {
	var group decorationGroup
	for _, n := range nodes {
		if err := guard.Step(); err != nil {
			return decorationGroup{}, err
		}
		if n.Container != root || !frame.canCompareDecorations(n) {
			continue
		}
		group.bodies = append(group.bodies, n)
		add := func(position label.Position, width, height float64) {
			p := n.LabelTopLeft(position, width, height)
			group.decorations = append(group.decorations, packedDecoration{n, geo.Point{X: p.X - n.TopLeft.X, Y: p.Y - n.TopLeft.Y}, width, height})
		}
		if n.Label != nil && n.Label.PositionFixed() && n.Label.Position.IsOutside() {
			add(n.Label.Position, n.Label.Width, n.Label.Height)
		}
		if n.Icon != nil && n.Icon.PositionFixed() && n.Icon.Position.IsOutside() {
			size := math.Ceil(n.IconSize(n.Icon.Position))
			add(n.Icon.Position, size, size)
		}
	}
	return group, nil
}

type decorationComparisons struct {
	first, second decorationGroup
	split         bool
}

// Already packed bodies keep their relative positions. When comparing
// translations, only intersections between the moving and stationary sets can
// change. The incumbent/final comparison counts all intersections once.
func (frame packingFrame) forCandidates(packed, moving layoutgraph.Nodes, root *layoutgraph.Node, guard *limits.WorkGuard) (packingFrame, error) {
	if len(frame.decorations.first.decorations) == 0 {
		return frame, nil
	}
	stationary, err := frame.newDecorationGroup(packed, root, guard)
	if err != nil {
		return packingFrame{}, err
	}
	translated, err := frame.newDecorationGroup(moving, root, guard)
	if err != nil {
		return packingFrame{}, err
	}
	frame.decorations = decorationComparisons{first: stationary, second: translated, split: true}
	return frame, nil
}

func (pairs decorationComparisons) count(guard *limits.WorkGuard) (int, error) {
	count, err := countDecorationBodyOverlaps(pairs.first.decorations, pairs.second.bodies, guard)
	if err != nil || !pairs.split {
		return count, err
	}
	other, err := countDecorationBodyOverlaps(pairs.second.decorations, pairs.first.bodies, guard)
	return count + other, err
}

func countDecorationBodyOverlaps(decorations []packedDecoration, bodies layoutgraph.Nodes, guard *limits.WorkGuard) (int, error) {
	count := 0
	for _, owner := range decorations {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		caption := owner.box()
		for _, body := range bodies {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			if body == owner.owner {
				continue
			}
			if caption.Overlaps(body.Box) {
				count++
			}
		}
	}
	return count, nil
}

// Compare the two measured exterior rectangles once per pair. During a trial
// pairs on the same stationary or moving component cannot change.
func (pairs decorationComparisons) decorationOverlapCount(guard *limits.WorkGuard) (int, error) {
	hits := 0
	for i, a := range pairs.first.decorations {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		caption := a.box()
		others := pairs.second.decorations
		if !pairs.split {
			others = others[i+1:]
		}
		for _, b := range others {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			if a.owner != b.owner && caption.Overlaps(b.box()) {
				hits++
			}
		}
	}
	return hits, nil
}
