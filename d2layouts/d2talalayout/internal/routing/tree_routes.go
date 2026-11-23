package routing

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

type TreeEdgePath struct {
	SourcePortNode            *OVGNode
	TargetPortNode            *OVGNode
	SourceMidpoint            *geo.Point
	TargetMidpoint            *geo.Point
	SourceOrientationToTarget geo.Orientation
}

func isSentinelEdgeSource(tree *layoutgraph.Tree) bool {
	return tree != nil && tree.SentinelEdge != nil && tree.SentinelEdge.From == tree.Node
}

// get the S-shaped path information for the tree node's sentinelEdge
func (ovg *OVG) treeEdgePath(t *layoutgraph.Tree, portArrowheads map[*OVGNode]map[layoutgraph.Arrowhead]struct{}) TreeEdgePath {
	path, _ := ovg.treeEdgePathForBuild(t, portArrowheads, nil)
	return path
}

func (ovg *OVG) treeEdgePathForBuild(t *layoutgraph.Tree, portArrowheads map[*OVGNode]map[layoutgraph.Arrowhead]struct{}, guard *ovgBuildGuard) (TreeEdgePath, error) {
	step := func() error {
		if guard == nil {
			return nil
		}
		return guard.step()
	}
	if err := step(); err != nil {
		return TreeEdgePath{}, err
	}
	parent := t.SentinelNode()
	child := t.Node

	parentPortIndex := parent.CenterPortIndex(t.Orientation)
	childPortIndex := child.CenterPortIndex(t.Orientation.GetOpposite())
	parentPortNode := ovg.Ports[parent][parentPortIndex]
	childPortNode := ovg.Ports[child][childPortIndex]

	if arrowheads, has := portArrowheads[parentPortNode]; has {
		// check the arrowhead on the parent
		arrowhead := t.SentinelEdge.TargetArrowhead
		if !isSentinelEdgeSource(t) {
			arrowhead = t.SentinelEdge.SourceArrowhead
		}
		if len(arrowheads) > 0 {
			if _, in := arrowheads[arrowhead]; !in {
				var closestPort *OVGNode
				closestDistance := math.Inf(1)
				// try to find a different port since we have a mismatched arrowhead
				for _, portIndex := range parent.PortIndices(t.Orientation) {
					if err := step(); err != nil {
						return TreeEdgePath{}, err
					}
					if portIndex == parentPortIndex {
						continue
					}
					usePort := false
					port := ovg.Ports[parent][portIndex]
					_, hasArrowheads := portArrowheads[port]
					if !hasArrowheads {
						usePort = true
					} else if _, matches := portArrowheads[port][arrowhead]; matches {
						usePort = true
					}
					if usePort {
						distance := geo.EuclideanDistance(
							port.Point.X,
							port.Point.Y,
							childPortNode.X,
							childPortNode.Y,
						)
						if distance < closestDistance {
							closestPort = port
							closestDistance = distance
						}
					}
				}
				if closestPort != nil {
					parentPortNode = closestPort
				}
			}
		}
	}

	childPortNode, err := alignedPortNodeForBuild(t, parentPortNode, childPortNode, guard)
	if err != nil {
		return TreeEdgePath{}, err
	}
	// alignedPortNodeForBuild may synthesize a port so that a nearly aligned tree
	// edge stays straight. addTreeNodes canonicalizes that coordinate into the
	// OVG and records it in the child's port list. Resolve it here on subsequent
	// route lookups; otherwise child-as-source routing starts from a fresh node
	// with no OVG edges, and shared points lose their canonical owner metadata.
	for _, portNode := range ovg.Ports[child] {
		if err := step(); err != nil {
			return TreeEdgePath{}, err
		}
		if nonNilEquals(portNode.Point, childPortNode.Point) {
			childPortNode = portNode
			break
		}
	}

	parentMidpoint, childMidpoint := ovg.treeEdgeMidpoints(parent, t.Orientation, parentPortNode.Point, childPortNode.Point)

	// if we are the target, swap these around
	if isSentinelEdgeSource(t) {
		return TreeEdgePath{
			SourcePortNode:            childPortNode,
			TargetPortNode:            parentPortNode,
			SourceMidpoint:            childMidpoint,
			TargetMidpoint:            parentMidpoint,
			SourceOrientationToTarget: t.Orientation,
		}, nil
	} else {
		return TreeEdgePath{
			SourcePortNode:            parentPortNode,
			TargetPortNode:            childPortNode,
			SourceMidpoint:            parentMidpoint,
			TargetMidpoint:            childMidpoint,
			SourceOrientationToTarget: t.Orientation.GetOpposite(),
		}, nil
	}
}

func alignedPortNodeForBuild(tree *layoutgraph.Tree, parentPort, childPort *OVGNode, guard *ovgBuildGuard) (*OVGNode, error) {
	isWithinThreshold := func(a, b, t float64) bool {
		return math.Abs(a-b) <= t
	}
	parent := tree.SentinelNode()
	child := tree.Node

	endOfNodeBuffer := 5.

	// in some cases, parent.center = (100.5, 100) and child.center = (100, 150)
	// however, this is just a rounding error and then we compare equality by upto 1 in difference
	nodesAreVerticallyAligned := isWithinThreshold(parent.Center().X, child.Center().X, treeChildAlignmentTolerance)
	portsAreVerticallyMisaligned := parentPort.X != childPort.X
	if tree.Orientation.IsVertical() && nodesAreVerticallyAligned && portsAreVerticallyMisaligned {
		if child.TopLeft.X+endOfNodeBuffer < parentPort.X && parentPort.X < child.TopLeft.X+child.Width-endOfNodeBuffer {
			if guard != nil {
				return guard.newDerivedNode(geo.NewPoint(parentPort.X, childPort.Y))
			}
			return NewOVGNode(geo.NewPoint(parentPort.X, childPort.Y)), nil
		}
	}

	nodesAreHorizontallyAligned := isWithinThreshold(parent.Center().Y, child.Center().Y, treeChildAlignmentTolerance)
	portsAreHorizontallyMisaligned := parentPort.Y != childPort.Y
	if tree.Orientation.IsHorizontal() && nodesAreHorizontallyAligned && portsAreHorizontallyMisaligned {
		if child.TopLeft.Y+endOfNodeBuffer < parentPort.Y && parentPort.Y < child.TopLeft.Y+child.Height-endOfNodeBuffer {
			if guard != nil {
				return guard.newDerivedNode(geo.NewPoint(childPort.X, parentPort.Y))
			}
			return NewOVGNode(geo.NewPoint(childPort.X, parentPort.Y)), nil
		}
	}

	return childPort, nil
}

type treeFace struct {
	parent *layoutgraph.Node
	side   geo.Orientation
}

// All tree edges leaving the same face use one track, including siblings
// with different widths or heights. Where placement shrinks the gap, bring
// the track halfway to the nearest facing box. Keep the usual parent-relative
// bend when placement adds space for an edge label.
func measureShortTreeGaps(g *layoutgraph.Graph, guard *ovgBuildGuard) (map[treeFace]float64, error) {
	var gaps map[treeFace]float64
	if len(g.NodeToTree) == 0 {
		return nil, nil
	}
	// Split subgraphs can share NodeToTree. Only local children participate.
	for _, node := range g.Nodes {
		if err := guard.step(); err != nil {
			return nil, err
		}
		t := g.NodeToTree[node]
		if t == nil {
			continue
		}
		parent := t.SentinelNode()
		var gap float64
		switch t.Orientation {
		case geo.Top:
			gap = parent.TopLeft.Y - (node.TopLeft.Y + node.Height)
		case geo.Bottom:
			gap = node.TopLeft.Y - (parent.TopLeft.Y + parent.Height)
		case geo.Left:
			gap = parent.TopLeft.X - (node.TopLeft.X + node.Width)
		case geo.Right:
			gap = node.TopLeft.X - (parent.TopLeft.X + parent.Width)
		}
		// A child overlapping the parent on this axis has no inter-layer
		// channel. It must not collapse the whole fork's track.
		if gap <= 0 || gap >= layoutgraph.TreeParentSpacing {
			continue
		}
		if gaps == nil {
			gaps = make(map[treeFace]float64)
		}
		face := treeFace{parent, t.Orientation}
		if old, ok := gaps[face]; !ok || gap < old {
			gaps[face] = gap
		}
	}
	return gaps, nil
}

func (ovg *OVG) treeEdgeMidpoints(parent *layoutgraph.Node, side geo.Orientation, parentPort, childPort *geo.Point) (*geo.Point, *geo.Point) {
	gap, ok := ovg.shortTreeGaps[treeFace{parent, side}]
	if !ok {
		gap = layoutgraph.TreeParentSpacing
	}
	switch side {
	case geo.Left:
		x := parent.TopLeft.X - gap/2
		return geo.NewPoint(x, parentPort.Y), geo.NewPoint(x, childPort.Y)
	case geo.Right:
		x := parent.TopLeft.X + parent.Width + gap/2
		return geo.NewPoint(x, parentPort.Y), geo.NewPoint(x, childPort.Y)
	case geo.Top:
		y := parent.TopLeft.Y - gap/2
		return geo.NewPoint(parentPort.X, y), geo.NewPoint(childPort.X, y)
	case geo.Bottom:
		y := parent.TopLeft.Y + parent.Height + gap/2
		return geo.NewPoint(parentPort.X, y), geo.NewPoint(childPort.X, y)
	}
	return new(geo.Point), new(geo.Point)
}
