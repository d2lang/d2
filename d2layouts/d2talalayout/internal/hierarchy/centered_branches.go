package hierarchy

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/d2/lib/xsort"
)

// A branch shares a flow axis with its fork and join. Dimensions and positions
// below are in (across flow, along flow) coordinates, for all four directions.
type branchNode struct {
	crossSize, depthSize float64
	oldCross, oldDepth   float64 // center across flow, leading face along flow
	cross, depth         float64 // new center port and leading face
	incoming             int
	next                 []*branchNode
	visited              bool
}

type branchBlock struct {
	nodes               []*branchNode
	left, right, height float64
}

func (b *branchBlock) add(child branchBlock, cross, depth float64) {
	for _, node := range child.nodes {
		node.cross += cross
		node.depth += depth
	}
	b.nodes = append(b.nodes, child.nodes...)
	b.left = min(b.left, cross+child.left)
	b.right = max(b.right, cross+child.right)
	b.height = max(b.height, depth+child.height)
}

const branchGap = 42.0

type branchLayout struct {
	// At least one shorter lane must actually be off center in the incumbent.
	unbalanced bool
}

// series consumes consecutive nodes until the next join. Recursively walking
// each arm of a fork must stop at the *same* join, solely fed by those arms.
// An early merge, cross-link, shortcut, competing entry, or cycle fails this
// test. Nodes are discovered once; the size cap bounds nested block assembly.
func (l *branchLayout) series(node *branchNode) (block branchBlock, join *branchNode, ok bool) {
	for {
		if node.visited {
			return block, nil, false
		}
		node.visited = true
		half := math.Floor(node.crossSize / 2)
		depth := block.height
		if len(block.nodes) > 0 {
			depth += branchGap
		}
		block.add(branchBlock{[]*branchNode{node}, -half, node.crossSize - half, node.depthSize}, 0, depth)
		switch len(node.next) {
		case 0:
			return block, nil, true
		case 1:
			node = node.next[0]
			if node.incoming > 1 {
				return block, node, true
			}
		default:
			var arms []branchBlock
			var end *branchNode
			// Preserve the selected drawing's lane order. Equal coordinates
			// retain the stable edge order.
			xsort.StableFunc(node.next, func(a, b *branchNode) int { return cmp.Compare(a.oldCross, b.oldCross) })
			for _, next := range node.next {
				if next.incoming != 1 {
					return block, nil, false
				}
				arm, stop, valid := l.series(next)
				if !valid || stop == nil || end != nil && stop != end {
					return block, nil, false
				}
				arms = append(arms, arm)
				end = stop
			}
			if end.incoming != len(arms) {
				return block, nil, false
			}
			l.placeArms(&block, arms, node, end)
			node = end
		}
	}
}

func (l *branchLayout) placeArms(block *branchBlock, arms []branchBlock, start, end *branchNode) {
	var height float64
	for _, arm := range arms {
		height = max(height, arm.height)
	}
	// Leave room between the outer lanes and the sides of the fork/join
	// for the horizontal port approach. Keep a middle lane on the main axis.
	clearance := math.Ceil(max(start.crossSize, end.crossSize)/2) + layoutgraph.MinPortClearance
	offsets := make([]float64, len(arms))
	if len(arms) == 2 {
		offset := max(clearance, math.Ceil((arms[0].right-arms[1].left+branchGap)/2))
		offsets[0], offsets[1] = -offset, offset
	} else {
		offsets[0] = -max(clearance, math.Ceil(arms[0].right-arms[1].left+branchGap))
		offsets[2] = max(clearance, math.Ceil(arms[1].right-arms[2].left+branchGap))
	}
	top := block.height + branchGap
	for i, arm := range arms {
		first, last := arm.nodes[0], arm.nodes[len(arm.nodes)-1]
		if arm.height < height {
			above := first.oldDepth - (start.oldDepth + start.depthSize)
			below := end.oldDepth - (last.oldDepth + last.depthSize)
			l.unbalanced = l.unbalanced || math.Abs(above-below) > layoutgraph.NodeGap
		}
		block.add(arm, offsets[i], top+math.Floor((height-arm.height)/2))
	}
}

// CenteredBranchCandidate proposes one composition of closed, disjoint
// parallel regions on a small, flat workflow. The incumbent stays intact.
func CenteredBranchCandidate(ctx context.Context, g *layoutgraph.Graph) (*layoutgraph.Graph, error) {
	// A fork and join give a connected DAG at least as many edges as nodes.
	if g.IsRootHierarchy || len(g.Nodes) < 5 || len(g.Nodes) > 64 || len(g.Edges) < len(g.Nodes) || len(g.Edges) > 2*len(g.Nodes) {
		return g, nil
	}
	direction := g.Direction(nil)
	horizontal := direction.IsHorizontal()
	reverse := direction == geo.Top || direction == geo.Left
	nodes := make(map[layoutgraph.EntityID]*branchNode, len(g.Nodes))
	for _, n := range g.Nodes {
		if n.Container != nil || n.IsContainer() || n.FixedTopLeft != nil || len(n.Nears) != 0 || n.Icon != nil ||
			n.ShapeType() != shape.SQUARE_TYPE && n.ShapeType() != shape.REAL_SQUARE_TYPE || n.Is3D || n.IsMultiple || n.IsInvisible ||
			n.Label != nil && (n.Label.Position != label.InsideMiddleCenter || n.Label.Width > n.Width || n.Label.Height > n.Height) {
			return g, nil
		}
		across, along, width, height := n.TopLeft.X, n.TopLeft.Y, n.Width, n.Height
		if horizontal {
			across, along, width, height = along, across, height, width
		}
		// Each lane uses an integer port inside the leading and trailing
		// faces. A one-pixel face has only its two corners on that grid.
		if width < 2 {
			return g, nil
		}
		if reverse {
			along = -along - height
		}
		nodes[n.ID] = &branchNode{crossSize: width, depthSize: height, oldCross: across + width/2, oldDepth: along}
	}
	for _, edge := range g.Edges {
		if edge.HasSourceArrow() || edge.TargetArrowhead != layoutgraph.TriangleArrowhead || edge.IsInvisible || edge.IsCurve ||
			edge.Label != nil || edge.SourceArrowheadLabel != nil || edge.TargetArrowheadLabel != nil {
			return g, nil
		}
		from, to := nodes[edge.From.ID], nodes[edge.To.ID]
		// An unhinted mesh may have chosen an entirely different orientation.
		// Refine a downward reading order only when it already exists.
		if direction == geo.NONE && to.oldDepth < from.oldDepth+from.depthSize {
			return g, nil
		}
		if len(from.next) == 3 || to.incoming == 3 || slices.Contains(from.next, to) {
			return g, nil
		}
		from.next = append(from.next, to)
		to.incoming++
	}
	var root *branchNode
	for _, n := range g.Nodes {
		if node := nodes[n.ID]; node.incoming == 0 {
			if root != nil {
				return g, nil
			}
			root = node
		}
	}
	if root == nil {
		return g, nil
	}
	var layout branchLayout
	block, end, ok := layout.series(root)
	if !ok || end != nil || !layout.unbalanced || len(block.nodes) != len(nodes) ||
		block.height > limits.MaxGraphSize || block.right-block.left > limits.MaxGraphSize {
		return g, nil
	}
	candidate, err := layoutgraph.Clone(ctx, g)
	if err != nil {
		return nil, err
	}
	point := func(cross, depth float64) *geo.Point {
		cross -= block.left
		if reverse {
			depth = block.height - depth
		}
		if horizontal {
			return geo.NewPoint(depth, cross)
		}
		return geo.NewPoint(cross, depth)
	}
	for _, n := range candidate.Nodes {
		v := nodes[n.ID]
		depth := v.depth
		if reverse {
			depth += v.depthSize
		}
		n.TopLeft = point(v.cross-math.Floor(v.crossSize/2), depth)
	}
	for _, edge := range candidate.Edges {
		a, b := nodes[edge.From.ID], nodes[edge.To.ID]
		// At an ordinary chain, centered top/bottom ports stay straight. At
		// a fork or join the outer arms use distinct faces and one bend.
		// A shared join/fork separates incoming and outgoing side ports.
		switch {
		case a.cross == b.cross:
			edge.Points = []*geo.Point{point(a.cross, a.depth+a.depthSize), point(b.cross, b.depth)}
		case len(a.next) > 1 && b.incoming == 1:
			depth := a.depth + a.depthSize/2
			if a.incoming > 1 {
				if a.depthSize < 48 {
					return g, nil
				}
				depth += 14
			}
			side := a.cross - math.Floor(a.crossSize/2)
			if b.cross > a.cross {
				side += a.crossSize
			}
			edge.Points = []*geo.Point{point(side, depth), point(b.cross, depth), point(b.cross, b.depth)}
		case b.incoming > 1 && len(a.next) == 1:
			depth := b.depth + b.depthSize/2
			if len(b.next) > 1 {
				if b.depthSize < 48 {
					return g, nil
				}
				depth -= 14
			}
			side := b.cross - math.Floor(b.crossSize/2)
			if a.cross > b.cross {
				side += b.crossSize
			}
			edge.Points = []*geo.Point{point(a.cross, a.depth+a.depthSize), point(a.cross, depth), point(side, depth)}
		default:
			return g, nil
		}
	}
	return candidate, ctx.Err()
}
