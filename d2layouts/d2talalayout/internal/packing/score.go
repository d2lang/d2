package packing

import (
	"cmp"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// Readability and an attainable authored frame take precedence over compactness.
// Keep these as independent criteria so coordinate magnitude cannot change
// their priority or hide differences between footprint scores.
type packingScore struct {
	captionCollisions int
	excessArea        float64
	footprint         float64
}

func (s packingScore) compare(t packingScore) int {
	if c := cmp.Compare(s.captionCollisions, t.captionCollisions); c != 0 {
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

var worstPackingScore = packingScore{math.MaxInt, math.Inf(1), math.Inf(1)}

type packingFrame struct {
	horizontal            bool
	desired, reserved     float64
	gridWidth, gridHeight float64
	captions              captionComparisons
}

func newPackingFrame(root *layoutgraph.Node, children layoutgraph.Nodes, guard *limits.WorkGuard) (packingFrame, error) {
	frame := packingFrame{}
	for _, child := range children {
		if err := guard.Step(); err != nil {
			return packingFrame{}, err
		}
		if child.Label != nil && child.Label.PositionFixed() && child.Label.Position.IsOutside() && plainTile(child) {
			group, err := newCaptionGroup(children, root, guard)
			if err != nil {
				return packingFrame{}, err
			}
			frame.captions = captionComparisons{first: group, second: group}
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

func plainTile(n *layoutgraph.Node) bool {
	return !n.IsContainer() && len(n.Edges) == 0 && len(n.Nears) == 0 &&
		n.Hierarchy == nil && n.Cluster == nil && n.Sequence == nil && !n.IsInvisible &&
		!n.Is3D && !n.IsMultiple && n.Icon == nil && n.IsRectangular()
}

func (frame packingFrame) score(nodes layoutgraph.Nodes, guard *limits.WorkGuard) (packingScore, error) {
	topLeft, bottomRight, err := packFixedBoundingBox(nodes, guard)
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
	score.captionCollisions, err = frame.captions.count(guard)
	if err != nil {
		return packingScore{}, err
	}
	return score, guard.Finish()
}

type captionGroup struct {
	bodies, owners layoutgraph.Nodes
}

func newCaptionGroup(nodes layoutgraph.Nodes, root *layoutgraph.Node, guard *limits.WorkGuard) (captionGroup, error) {
	var group captionGroup
	for _, n := range nodes {
		if err := guard.Step(); err != nil {
			return captionGroup{}, err
		}
		if n.Container != root || !plainTile(n) {
			continue
		}
		group.bodies = append(group.bodies, n)
		if n.Label != nil && n.Label.PositionFixed() && n.Label.Position.IsOutside() {
			group.owners = append(group.owners, n)
		}
	}
	return group, nil
}

type captionComparisons struct {
	first, second captionGroup
	split         bool
}

// Already packed bodies keep their relative positions. When comparing
// translations, only intersections between the moving and stationary sets can
// change. The incumbent/final comparison counts all intersections once.
func (frame packingFrame) forCandidates(packed, moving layoutgraph.Nodes, root *layoutgraph.Node, guard *limits.WorkGuard) (packingFrame, error) {
	if len(frame.captions.first.owners) == 0 {
		return frame, nil
	}
	stationary, err := newCaptionGroup(packed, root, guard)
	if err != nil {
		return packingFrame{}, err
	}
	translated, err := newCaptionGroup(moving, root, guard)
	if err != nil {
		return packingFrame{}, err
	}
	frame.captions = captionComparisons{first: stationary, second: translated, split: true}
	return frame, nil
}

func (pairs captionComparisons) count(guard *limits.WorkGuard) (int, error) {
	count, err := countCaptionBodyOverlaps(pairs.first.owners, pairs.second.bodies, guard)
	if err != nil || !pairs.split {
		return count, err
	}
	other, err := countCaptionBodyOverlaps(pairs.second.owners, pairs.first.bodies, guard)
	return count + other, err
}

func countCaptionBodyOverlaps(owners, bodies layoutgraph.Nodes, guard *limits.WorkGuard) (int, error) {
	count := 0
	for _, owner := range owners {
		if err := guard.Step(); err != nil {
			return 0, err
		}
		l := owner.Label
		tl := owner.LabelTopLeft(l.Position, l.Width, l.Height)
		for _, body := range bodies {
			if err := guard.Step(); err != nil {
				return 0, err
			}
			if body == owner {
				continue
			}
			if tl.X < body.TopLeft.X+body.Width && tl.X+l.Width > body.TopLeft.X &&
				tl.Y < body.TopLeft.Y+body.Height && tl.Y+l.Height > body.TopLeft.Y {
				count++
			}
		}
	}
	return count, nil
}
