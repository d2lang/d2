package placementcost

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/typedpool"
	"github.com/d2lang/d2/lib/geo"
)

// EdgeLengthOptions controls which geometric penalties contribute to edge length.
type EdgeLengthOptions struct {
	// EdgeAbductions supplies temporary endpoint ownership during placement.
	EdgeAbductions []*layoutgraph.EdgeAbduction
	// IncludeNodeSizes measures distances between node boundaries instead of origins.
	IncludeNodeSizes bool
	// EnforceMinimumGap penalizes connected nodes that are too close together.
	EnforceMinimumGap bool
	// PenalizeDirection adds cost when an edge departs from its preferred direction.
	PenalizeDirection bool
}

// NodesEdgeLength sums the edge-length cost rooted at each node in order.
func NodesEdgeLength(ctx context.Context, nodes layoutgraph.Nodes, options EdgeLengthOptions) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	result := 0.0
	for _, node := range nodes {
		length, err := nodeEdgeLength(&p, node, options)
		if err != nil {
			return 0, err
		}
		result += length
	}
	if err := p.check(); err != nil {
		return 0, err
	}
	return result, nil
}

type edgeScratch struct {
	used            []bool
	nRepl           []*layoutgraph.Node
	aRepl           []*layoutgraph.Node
	clusterOrder    []*layoutgraph.Node
	edgeAbductions  []*layoutgraph.EdgeAbduction
	obstructionSets [][]*layoutgraph.Node
	// obstructionKeys lists the containers whose children are in
	// obstructionSets; obstructionAdded mirrors it once it is long.
	obstructionKeys  []*layoutgraph.Node
	obstructionAdded map[*layoutgraph.Node]struct{}
	// labeledEdgeCount is only used for nodes with many labeled edges;
	// labelCounts[i] is the number of labeled incident edges whose endpoint
	// replacements have the same IDs as edge i's (0 for unlabeled edges).
	labeledEdgeCount map[labeledEdgePair]int
	labelCounts      []int
	// Ancestor chains of the endpoints of the edge being scanned.
	nodeAncestry, adjacentAncestry []*layoutgraph.Node
	// Sweep mode (see NewNodeEdgeLengthSweepScorer): per edge, its filtered
	// obstacles, built on first use.
	sweepOn    bool
	sweepMover *layoutgraph.Node
	sweepEdges []sweepEdgeObstacles
	// Early exit (see NodeEdgeLengthScorer.ScoreUnlessAbove).
	exitOn                bool
	exitOffset, exitBound float64
}

// sweepObstacle is an obstacle of a sweep scorer's edge that passed the
// topology filters of the obstacle scan; the box (excludes' values) is kept
// for obstacles that cannot move during the sweep.
type sweepObstacle struct {
	node                *layoutgraph.Node
	x, y, right, bottom float64
	finite              bool
	moving              bool
}

type sweepEdgeObstacles struct {
	built     bool
	obstacles []sweepObstacle
}

// sweepBuilt reports whether edge's sweep obstacles were collected.
func (s *edgeScratch) sweepBuilt(edge int) bool {
	return edge < len(s.sweepEdges) && s.sweepEdges[edge].built
}

// sweepObstacles returns edge's filtered obstacle sequence, building it from
// s.obstructionSets (this edge's scan in the current evaluation) on first use.
// The filters are the obstacle scan's own skips that do not depend on
// geometry: nil (an ancestor of everything), ancestors of either endpoint,
// other graphs, the endpoints, the endpoints' cluster vessels; plus a missing
// TopLeft for obstacles that cannot move.
func (s *edgeScratch) sweepObstacles(edge, edges int, nodeReplacement, adjacentNodeReplacement *layoutgraph.Node) []sweepObstacle {
	if len(s.sweepEdges) < edges {
		s.sweepEdges = slices.Grow(s.sweepEdges[:0], edges)[:edges]
	}
	entry := &s.sweepEdges[edge]
	if entry.built {
		return entry.obstacles
	}
	obstacles := entry.obstacles[:0]
	for _, obstructions := range s.obstructionSets {
		for _, obstruction := range obstructions {
			if obstruction == nil {
				continue
			}
			if nodeReplacement.IsDescendantOf(obstruction) || adjacentNodeReplacement.IsDescendantOf(obstruction) {
				continue
			}
			if obstruction.Graph != nodeReplacement.Graph {
				continue
			}
			if obstruction == nodeReplacement || obstruction == adjacentNodeReplacement {
				continue
			}
			if obstruction.IsClusterVessel() {
				if (nodeReplacement.Cluster != nil && nodeReplacement.Cluster.Vessel == obstruction) || (adjacentNodeReplacement.Cluster != nil && adjacentNodeReplacement.Cluster.Vessel == obstruction) {
					continue
				}
			}
			moving := obstruction.IsDescendantOf(s.sweepMover)
			if !moving && obstruction.TopLeft == nil {
				continue
			}
			obstacle := sweepObstacle{node: obstruction, moving: moving}
			if !moving {
				x, y := obstruction.TopLeft.X, obstruction.TopLeft.Y
				right, bottom := x+obstruction.Width, y+obstruction.Height
				obstacle.x, obstacle.y, obstacle.right, obstacle.bottom = x, y, right, bottom
				obstacle.finite = (x-x)+(y-y)+(right-right)+(bottom-bottom) == 0
			}
			obstacles = append(obstacles, obstacle)
		}
	}
	entry.built, entry.obstacles = true, obstacles
	return obstacles
}

// Above these sizes the scratch uses maps instead of linear scans; both give
// the same answers.
const (
	maxLinearObstructionKeys = 16
	maxLinearLabeledEdges    = 64
)

type labeledEdgePair struct {
	from layoutgraph.EntityID
	to   layoutgraph.EntityID
}

var scratchPool = typedpool.New(func() *edgeScratch { return new(edgeScratch) })

const maxPooledEdgeScratchEntries = 4096

// retainable reports whether the scratch is small enough to pool: every
// slice and map within maxPooledEdgeScratchEntries, and the sweep obstacles
// of all edges together too (they were capped per edge, so one scratch could
// keep 4096 x 4096 of them). TinyGo's sync.Pool (d2's wasm build) never
// frees what it holds. obstructionKeys and labelCounts bound what the maps
// that mirror them (obstructionAdded, labeledEdgeCount) have held. Entries
// of sweepEdges beyond its length keep their obstacle storage for reuse, so
// all of them count.
func (s *edgeScratch) retainable() bool {
	const limit = maxPooledEdgeScratchEntries
	if cap(s.used) > limit || cap(s.nRepl) > limit || cap(s.aRepl) > limit ||
		cap(s.clusterOrder) > limit || cap(s.edgeAbductions) > limit ||
		cap(s.obstructionSets) > limit || cap(s.obstructionKeys) > limit || len(s.obstructionAdded) > limit ||
		cap(s.labelCounts) > limit || len(s.labeledEdgeCount) > limit ||
		cap(s.nodeAncestry) > limit || cap(s.adjacentAncestry) > limit || cap(s.sweepEdges) > limit {
		return false
	}
	obstacles := 0
	for _, edge := range s.sweepEdges[:cap(s.sweepEdges)] {
		obstacles += cap(edge.obstacles)
	}
	return obstacles <= limit
}

// putEdgeScratch clears the scratch's references and pools it, or leaves a
// scratch that is too large to the collector. clusterOrder, edgeAbductions,
// obstructionSets and obstructionKeys are only ever filled by appending from
// length zero, and every shortening clears what it drops, so no entry beyond
// their length holds a reference.
func putEdgeScratch(s *edgeScratch) {
	if !s.retainable() {
		return
	}
	clear(s.used)
	s.used = s.used[:0]
	clear(s.nRepl)
	s.nRepl = s.nRepl[:0]
	clear(s.aRepl)
	s.aRepl = s.aRepl[:0]
	clear(s.clusterOrder)
	s.clusterOrder = s.clusterOrder[:0]
	clear(s.edgeAbductions)
	s.edgeAbductions = s.edgeAbductions[:0]
	clear(s.obstructionSets)
	s.obstructionSets = s.obstructionSets[:0]
	if len(s.obstructionAdded) != 0 {
		clear(s.obstructionAdded)
	}
	clear(s.obstructionKeys)
	s.obstructionKeys = s.obstructionKeys[:0]
	s.labelCounts = s.labelCounts[:0]
	if len(s.labeledEdgeCount) != 0 {
		clear(s.labeledEdgeCount)
	}
	s.nodeAncestry = releaseNodeChain(s.nodeAncestry)
	s.adjacentAncestry = releaseNodeChain(s.adjacentAncestry)
	s.sweepOn, s.sweepMover = false, nil
	s.exitOn = false
	for i := range s.sweepEdges {
		edge := &s.sweepEdges[i]
		clear(edge.obstacles)
		edge.obstacles = edge.obstacles[:0]
		edge.built = false
	}
	s.sweepEdges = s.sweepEdges[:0]
	scratchPool.Put(s)
}

// releaseNodeChain empties a pooled node slice without retaining references.
func releaseNodeChain(chain []*layoutgraph.Node) []*layoutgraph.Node {
	if cap(chain) > maxPooledEdgeScratchEntries {
		return nil
	}
	clear(chain[:cap(chain)])
	return chain[:0]
}

// Helper function to calculate midpoint and check semi-diagonal for horizontal directions
func calculateHorizontalMidpoint(nodeReplacement, adjacentNodeReplacement *layoutgraph.Node) (float64, bool) {
	ceil := math.Min(nodeReplacement.TopLeft.X+nodeReplacement.Width, adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)
	floor := math.Max(nodeReplacement.TopLeft.X, adjacentNodeReplacement.TopLeft.X)
	mid := (floor + ceil) / 2.
	isSemiDiagonal := math.Abs(nodeReplacement.TopLeft.X-adjacentNodeReplacement.TopLeft.X) > SideEdgeSpacing ||
		math.Abs(nodeReplacement.TopLeft.X+nodeReplacement.Width-(adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)) > SideEdgeSpacing
	return mid, isSemiDiagonal
}

// Helper function to calculate midpoint and check semi-diagonal for vertical directions
func calculateVerticalMidpoint(nodeReplacement, adjacentNodeReplacement *layoutgraph.Node) (float64, bool) {
	ceil := math.Min(nodeReplacement.TopLeft.Y+nodeReplacement.Height, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
	floor := math.Max(nodeReplacement.TopLeft.Y, adjacentNodeReplacement.TopLeft.Y)
	mid := (floor + ceil) / 2.
	isSemiDiagonal := math.Abs(nodeReplacement.TopLeft.Y-adjacentNodeReplacement.TopLeft.Y) > SideEdgeSpacing ||
		math.Abs(nodeReplacement.TopLeft.Y+nodeReplacement.Height-(adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)) > SideEdgeSpacing
	return mid, isSemiDiagonal
}

// NodeEdgeLength evaluates the incident-edge cost rooted at node.
func NodeEdgeLength(ctx context.Context, node *layoutgraph.Node, options EdgeLengthOptions) (float64, error) {
	p := newScoringPoll(ctx)
	if err := p.entry(); err != nil {
		return 0, err
	}
	return nodeEdgeLengthPolled(&p, node, options)
}

// nodeEdgeLength is NodeEdgeLength within an evaluation that already polls.
func nodeEdgeLength(p *scoringPoll, node *layoutgraph.Node, options EdgeLengthOptions) (float64, error) {
	if err := p.check(); err != nil {
		return 0, err
	}
	return nodeEdgeLengthPolled(p, node, options)
}

func nodeEdgeLengthPolled(p *scoringPoll, node *layoutgraph.Node, options EdgeLengthOptions) (float64, error) {
	s := scratchPool.Get()
	defer putEdgeScratch(s)
	setup, err := prepareNodeEdgeLength(p, node, options, s)
	if err != nil {
		return 0, err
	}
	return evaluateNodeEdgeLength(p, node, options, s, setup)
}

func prepareNodeEdgeLength(p *scoringPoll, node *layoutgraph.Node, options EdgeLengthOptions, s *edgeScratch) (edgeLengthStatic, error) {
	includeSizes := options.IncludeNodeSizes
	directionPenalty := options.PenalizeDirection

	edgeAbductions := options.EdgeAbductions
	if edgeAbductions == nil {
		s.clusterOrder = s.clusterOrder[:0]
		for vessel := range node.Graph.Clusters {
			s.clusterOrder = append(s.clusterOrder, vessel)
		}
		// Same order as layoutgraph.SortNodesByID (vessels have distinct IDs,
		// so the ascending order is unique) without sort.Slice's reflection
		// allocation on every preparation.
		slices.SortFunc(s.clusterOrder, func(a, b *layoutgraph.Node) int {
			return cmp.Compare(a.IDValue(), b.IDValue())
		})

		s.edgeAbductions = s.edgeAbductions[:0]
		for i, vessel := range s.clusterOrder {
			if err := p.at(i); err != nil {
				return edgeLengthStatic{}, err
			}
			cluster := node.Graph.Clusters[vessel]
			s.edgeAbductions = append(s.edgeAbductions, cluster.EdgeAbductions...)
		}
		edgeAbductions = s.edgeAbductions
	}

	if cap(s.used) < len(edgeAbductions) {
		s.used = make([]bool, len(edgeAbductions))
	} else {
		s.used = s.used[:len(edgeAbductions)]
	}
	usedEdgeAbductions := s.used
	// putEdgeScratch clears retained storage before pooling it; this scan keeps
	// cancellation bounded without clearing the same memory twice.
	if err := p.slice(len(usedEdgeAbductions)); err != nil {
		return edgeLengthStatic{}, err
	}

	if cap(s.nRepl) < len(node.Edges) {
		s.nRepl = make([]*layoutgraph.Node, len(node.Edges))
	} else {
		s.nRepl = s.nRepl[:len(node.Edges)]
	}
	nodeReplacements := s.nRepl
	if err := p.slice(len(nodeReplacements)); err != nil {
		return edgeLengthStatic{}, err
	}

	if cap(s.aRepl) < len(node.Edges) {
		s.aRepl = make([]*layoutgraph.Node, len(node.Edges))
	} else {
		s.aRepl = s.aRepl[:len(node.Edges)]
	}
	adjReplacements := s.aRepl
	if err := p.slice(len(adjReplacements)); err != nil {
		return edgeLengthStatic{}, err
	}

	for i, e := range node.Edges {
		if err := p.at(i); err != nil {
			return edgeLengthStatic{}, err
		}
		adjacentNode := node.Adjacent(e)
		// These are sometimes replaced by container children
		nodeReplacement := node
		adjacentNodeReplacement := adjacentNode

		if includeSizes {
			for j, edgeAbduction := range edgeAbductions {
				if err := p.at(j); err != nil {
					return edgeLengthStatic{}, err
				}
				if usedEdgeAbductions[j] {
					continue
				}
				if (edgeAbduction.CurrentFrom == node) && (edgeAbduction.CurrentTo == adjacentNode) {
					usedEdgeAbductions[j] = true
					if edgeAbduction.OriginallyTo != nil {
						adjacentNodeReplacement = edgeAbduction.OriginallyTo
					}
					if edgeAbduction.OriginallyFrom != nil {
						nodeReplacement = edgeAbduction.OriginallyFrom
					}
					break
				}
				if (edgeAbduction.CurrentFrom == adjacentNode) && (edgeAbduction.CurrentTo == node) {
					usedEdgeAbductions[j] = true
					if edgeAbduction.OriginallyFrom != nil {
						adjacentNodeReplacement = edgeAbduction.OriginallyFrom
					}
					if edgeAbduction.OriginallyTo != nil {
						nodeReplacement = edgeAbduction.OriginallyTo
					}
					break
				}
			}
		}

		nodeReplacements[i] = nodeReplacement
		adjReplacements[i] = adjacentNodeReplacement
	}

	direction := node.ContainerDirection()
	var directionFactor float64
	if directionPenalty {
		if direction != geo.NONE {
			// heavily weight specified direction with sizes
			if includeSizes {
				directionFactor = 6
			} else {
				directionFactor = 1.5
			}
		} else {
			if node.IsTable() {
				direction = geo.Right
				directionFactor = .5
			} else {
				// Check for nodes with more than 2 labeled edges to same node
				// Track both total edges and how many are "from" this node
				edgeCounts := make(map[*layoutgraph.Node]int)
				fromCounts := make(map[*layoutgraph.Node]int)
				hasMultipleLabels := false
				var multiLabelNode *layoutgraph.Node

				if includeSizes {
					for i, e := range node.Edges {
						if err := p.at(i); err != nil {
							return edgeLengthStatic{}, err
						}
						nodeReplacement := nodeReplacements[i]
						// If this node is a container and the replacement is a child, the edge isn't actually connected
						if nodeReplacement != node {
							continue
						}
						adjacentNodeReplacement := adjReplacements[i]
						if adjacentNodeReplacement != node.Adjacent(e) {
							continue
						}
						if e.Label != nil {
							if adjacentNodeReplacement == nodeReplacement {
								continue
							}
							edgeCounts[adjacentNodeReplacement]++
							from := e.From
							if semanticFrom, _, directed := e.DirectedEndpoints(); directed {
								from = semanticFrom
							}
							if from == node {
								fromCounts[adjacentNodeReplacement]++
							}
							if edgeCounts[adjacentNodeReplacement] > 2 {
								hasMultipleLabels = true
								multiLabelNode = adjacentNodeReplacement
							}
						}
					}
				}

				if hasMultipleLabels {
					// If more edges are from this node to the target, use Left orientation
					// Otherwise use Right orientation
					if fromCounts[multiLabelNode] > edgeCounts[multiLabelNode]/2 {
						direction = geo.Left
					} else {
						direction = geo.Right
					}
					directionFactor = 10
				} else {
					// Default to bottom right if no multiple labeled edges
					direction = geo.BottomRight
					directionFactor = .3
				}
			}

			if node.IsClusterVessel() {
				directionFactor = .2
				switch node.Graph.Clusters[node].Arrangement {
				case layoutgraph.Row:
					direction = geo.Bottom
				case layoutgraph.Column:
					direction = geo.Right
				}
			}
		}
	}

	// get the edges between each nodeReplacement and adjacentNodeReplacement
	labeled := 0
	for i, edge := range node.Edges {
		if err := p.at(i); err != nil {
			return edgeLengthStatic{}, err
		}
		if !includeSizes || edge.Label == nil {
			continue
		}
		labeled++
	}
	countLabeledEdgePairs(node, nodeReplacements, adjReplacements, labeled, s)

	return edgeLengthStatic{direction: direction, directionFactor: directionFactor}, nil
}

func evaluateNodeEdgeLength(p *scoringPoll, node *layoutgraph.Node, options EdgeLengthOptions, s *edgeScratch, setup edgeLengthStatic) (float64, error) {
	includeSizes := options.IncludeNodeSizes
	directionPenalty := options.PenalizeDirection
	minGapSize := 0.0
	if options.IncludeNodeSizes && options.EnforceMinimumGap {
		minGapSize = node.Graph.CellSize
	}
	nodeReplacements, adjReplacements := s.nRepl, s.aRepl
	direction, directionFactor := setup.direction, setup.directionFactor
	compass := directionCompass(direction)
	totalDistance := 0.0
	nodeContainer := node.EffectiveContainer()
	// Graph.TurnCost takes the graph's cost lock on every call. It derives the
	// cost from the geometry on its first use after a reset and returns that
	// value until the next reset, so within one evaluation (nothing moves) every
	// call returns the first call's value; the first call still happens where
	// the evaluation first needs the cost.
	var turnCost float64
	turnCostRead := false
	graphTurnCost := func() float64 {
		if !turnCostRead {
			turnCost = node.Graph.TurnCost()
			turnCostRead = true
		}
		return turnCost
	}
	for i, e := range node.Edges {
		if err := p.at(i); err != nil {
			return 0, err
		}
		adjacentNode := node.Adjacent(e)
		// Used during initialization and when adjacent is outer container
		if adjacentNode.TopLeft == nil {
			continue
		}
		nodeReplacement := nodeReplacements[i]
		adjacentNodeReplacement := adjReplacements[i]

		// If another node obstructs the direct path between two nodes, a more accurate heuristic penalizes this
		//      +---+
		//      |   |
		// +--->+   |
		// |    +---+
		// |
		// |
		// |
		// |    +---+
		// |    |   |
		// |    |   |
		// |    +-+-+
		// |      ^
		// |      |
		// |    +-+-+
		// |    |   |
		// +----+   |
		//      +---+

		var nodeVal *geo.Point
		var adjacentNodeVal *geo.Point

		// Lazy calculation of centers and edge direction - only when needed.
		// The centers are the points Center() returns, kept in this frame
		// instead of allocated.
		var nodeCenter, adjacentNodeCenter *geo.Point
		var nodeCenterPoint, adjacentNodeCenterPoint geo.Point
		var edgeDirection geo.Orientation
		var edgeDirectionCalculated bool
		var edgeDir geo.Orientation

		getCenters := func() {
			if nodeCenter == nil {
				nodeCenterPoint = geo.Point{X: nodeReplacement.TopLeft.X + nodeReplacement.Width/2, Y: nodeReplacement.TopLeft.Y + nodeReplacement.Height/2}
				adjacentNodeCenterPoint = geo.Point{X: adjacentNodeReplacement.TopLeft.X + adjacentNodeReplacement.Width/2, Y: adjacentNodeReplacement.TopLeft.Y + adjacentNodeReplacement.Height/2}
				nodeCenter, adjacentNodeCenter = &nodeCenterPoint, &adjacentNodeCenterPoint
			}
		}

		getEdgeDirection := func() geo.Orientation {
			if !edgeDirectionCalculated {
				if includeSizes {
					edgeDirection = nodeReplacement.Orientation(adjacentNodeReplacement)
				} else {
					edgeDirection = sizelessOrientation(nodeReplacement, adjacentNodeReplacement)
				}
				edgeDirectionCalculated = true
			}
			return edgeDirection
		}

		isDiagonal := false
		isSemiDiagonal := false
		if includeSizes {
			edgeDir = getEdgeDirection()
			if edgeDir == geo.NONE {
				// This is okay
				continue
			}
			// When the direction from A to C is Right, B obstructs the ideal straight line
			// But A can still make a perfectly good L shape
			// ┌─────────┐
			// │         │
			// │         │
			// │         │
			// │         │
			// │         ├──────────────────────────────────┐
			// │         │                                  │
			// │         │                                  │
			// │    A    │                                  │
			// │         │        ┌─────────┐           ┌───▼────┐
			// │         │        │   B     │           │        │
			// │         ├────────┼─────────┼──────────►│   C    │
			// │         │        │         │           │        │
			// │         │        │         │           │        │
			// └─────────┘        └─────────┘           └────────┘
			// It should count as a turnCost for this B, but if B was covering the other L-shape too, it should count as 2
			// We call this a "semi-diagonal"
			edgeDir = getEdgeDirection()
			if edgeDir.IsDiagonal() {
				isDiagonal = true
			}
			switch edgeDir {
			case geo.Top:
				mid, semiDiag := calculateHorizontalMidpoint(nodeReplacement, adjacentNodeReplacement)
				nodeVal = geo.NewPoint(mid, nodeReplacement.TopLeft.Y+nodeReplacement.Height)
				adjacentNodeVal = geo.NewPoint(mid, adjacentNodeReplacement.TopLeft.Y)
				isSemiDiagonal = semiDiag
			case geo.Bottom:
				mid, semiDiag := calculateHorizontalMidpoint(nodeReplacement, adjacentNodeReplacement)
				nodeVal = geo.NewPoint(mid, nodeReplacement.TopLeft.Y)
				adjacentNodeVal = geo.NewPoint(mid, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
				isSemiDiagonal = semiDiag
			case geo.Left:
				mid, semiDiag := calculateVerticalMidpoint(nodeReplacement, adjacentNodeReplacement)
				nodeVal = geo.NewPoint(nodeReplacement.TopLeft.X+nodeReplacement.Width, mid)
				adjacentNodeVal = geo.NewPoint(adjacentNodeReplacement.TopLeft.X, mid)
				isSemiDiagonal = semiDiag
			case geo.Right:
				mid, semiDiag := calculateVerticalMidpoint(nodeReplacement, adjacentNodeReplacement)
				nodeVal = geo.NewPoint(nodeReplacement.TopLeft.X, mid)
				adjacentNodeVal = geo.NewPoint(adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width, mid)
				isSemiDiagonal = semiDiag
			case geo.TopLeft, geo.TopRight, geo.BottomLeft, geo.BottomRight:
				getCenters()
				nodeVal = nodeCenter
				adjacentNodeVal = adjacentNodeCenter
			}
			if nodeReplacement.Cluster != nil || adjacentNodeReplacement.Cluster != nil {
				isSemiDiagonal = false
				isDiagonal = false
				getCenters()
				// if it's diagonal, you still have to use the boundaries, or else a line passes through innocent siblings
				switch edgeDir {
				case geo.Top, geo.TopLeft, geo.TopRight:
					nodeVal = geo.NewPoint(nodeCenter.X, nodeReplacement.TopLeft.Y+nodeReplacement.Height)
					adjacentNodeVal = geo.NewPoint(adjacentNodeCenter.X, adjacentNodeReplacement.TopLeft.Y)
				case geo.Bottom, geo.BottomLeft, geo.BottomRight:
					nodeVal = geo.NewPoint(nodeCenter.X, nodeReplacement.TopLeft.Y)
					adjacentNodeVal = geo.NewPoint(adjacentNodeCenter.X, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
				case geo.Left:
					nodeVal = geo.NewPoint(nodeReplacement.TopLeft.X+nodeReplacement.Width, nodeCenter.Y)
					adjacentNodeVal = geo.NewPoint(adjacentNodeReplacement.TopLeft.X, adjacentNodeCenter.Y)
				case geo.Right:
					nodeVal = geo.NewPoint(nodeReplacement.TopLeft.X, nodeCenter.Y)
					adjacentNodeVal = geo.NewPoint(adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width, adjacentNodeCenter.Y)
				}
			}
		} else {
			nodeVal = nodeReplacement.TopLeft
			adjacentNodeVal = adjacentNodeReplacement.TopLeft
		}

		// ... and the distance is a straight line
		var distance float64
		if includeSizes && e.HasTableColumn() {
			from := nodeReplacement
			to := adjacentNodeReplacement
			if node != e.From {
				from, to = to, from
			}
			distance = distanceBetweenTableColumns(node.Graph, e, from, to)
		} else {
			if nodeReplacement.Cluster != nil || adjacentNodeReplacement.Cluster != nil {
				//  I want to use the vessel of the cluster, otherwise during node placement, a connected node will jump back and forth between optimizing each one.
				if nodeReplacement.Cluster != nil {
					distance = placementDistance(nodeReplacement.Cluster.Vessel, adjacentNodeReplacement, includeSizes)
				} else {
					distance = placementDistance(adjacentNodeReplacement.Cluster.Vessel, nodeReplacement, includeSizes)
				}
			} else if isDiagonal {
				// the distance is the sum of that L shape
				distance = distanceToPoint(nodeReplacement, geo.NewPoint(nodeVal.X, adjacentNodeVal.Y), includeSizes) +
					distanceToPoint(adjacentNodeReplacement, geo.NewPoint(nodeVal.X, adjacentNodeVal.Y), includeSizes)
				if includeSizes {
					distance += graphTurnCost()
				}
			} else {
				distance = placementDistance(nodeReplacement, adjacentNodeReplacement, includeSizes)
			}
		}

		if s.exitOn && geo.PrecisionCompare(totalDistance+distance-s.exitOffset, s.exitBound, geo.PRECISION) == 1 {
			// See NodeEdgeLengthScorer.ScoreUnlessAbove: everything this edge
			// still adds to distance is non-negative, and so is every later
			// contribution, so the full sum compares above too; the obstacle
			// scan and penalties of this edge need not run.
			return totalDistance + distance, nil
		}

		if includeSizes && node.IsContainer() && adjacentNode.IsContainer() && nodeContainer == adjacentNode.EffectiveContainer() {
			containerAlignmentPenalty := 0.
			// If the widths are roughly equal in size, we want alignment on either one of the sides or the center
			if math.Min(node.Width, adjacentNode.Width) > math.Max(node.Width, adjacentNode.Width)*0.75 {
				if (node.TopLeft.X != adjacentNode.TopLeft.X) && (node.TopLeft.X+node.Width != adjacentNode.TopLeft.X+adjacentNode.Width) && ((node.TopLeft.X+node.Width)/2 != (adjacentNode.TopLeft.X+adjacentNode.Width)/2) {
					containerAlignmentPenalty = graphTurnCost()
				}
			}

			if math.Min(node.Height, adjacentNode.Height) > math.Max(node.Height, adjacentNode.Height)*0.75 {
				if (node.TopLeft.Y != adjacentNode.TopLeft.Y) && (node.TopLeft.Y+node.Height != adjacentNode.TopLeft.Y+adjacentNode.Height) && ((node.TopLeft.Y+node.Height)/2 != (adjacentNode.TopLeft.Y+adjacentNode.Height)/2) {
					containerAlignmentPenalty = graphTurnCost()
				}
			}

			distance += containerAlignmentPenalty
		}

		if includeSizes && !e.IsBetweenTableColumns() {
			bounds := scoringNodeBounds(nodeReplacement).including(scoringNodeBounds(adjacentNodeReplacement))
			// for diagonals and semi-diagonals we want to see if both corners are covered by any obstructions
			passesThroughCornerA := false
			passesThroughCornerB := false

			checkBestRouteBlocked := func(otherNode *layoutgraph.Node) bool {
				// Obstructions are initialized as siblings of endpoints, but we still need to eliminate cases
				// If the obstruction is one of the end nodes (already checked)
				// If the obstruction is a child of one of the end nodes (applicable when child connected to its container)
				if otherNode.IsDescendantOf(nodeReplacement) || otherNode.IsDescendantOf(adjacentNodeReplacement) {
					return false
				}
				if isDiagonal {
					if !passesThroughCornerA &&
						(otherNode.PassesThrough(nodeVal, geo.NewPoint(nodeVal.X, adjacentNodeVal.Y)) ||
							otherNode.PassesThrough(adjacentNodeVal, geo.NewPoint(nodeVal.X, adjacentNodeVal.Y))) {
						passesThroughCornerA = true
					}
					if !passesThroughCornerB &&
						(otherNode.PassesThrough(nodeVal, geo.NewPoint(adjacentNodeVal.X, nodeVal.Y)) ||
							otherNode.PassesThrough(adjacentNodeVal, geo.NewPoint(adjacentNodeVal.X, nodeVal.Y))) {
						passesThroughCornerB = true
					}
					if passesThroughCornerA && passesThroughCornerB {
						return true
					}
				} else {
					if otherNode.PassesThrough(nodeVal, adjacentNodeVal) {
						return true
					}
				}
				return false
			}

			// The L shape that goes clockwise
			isL1Blocked := false
			// The L shape that goes counterclockwise
			isL2Blocked := false

			if isSemiDiagonal {
				edgeDir = getEdgeDirection()
				switch edgeDir {
				case geo.Left, geo.Right:
					if math.Abs(nodeReplacement.TopLeft.Y-adjacentNodeReplacement.TopLeft.Y) <= SideEdgeSpacing {
						isL1Blocked = true
					} else if math.Abs(nodeReplacement.TopLeft.Y+nodeReplacement.Height-(adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)) <= SideEdgeSpacing {
						isL2Blocked = true
					}
				case geo.Top, geo.Bottom:
					if math.Abs(nodeReplacement.TopLeft.X-adjacentNodeReplacement.TopLeft.X) <= SideEdgeSpacing {
						isL2Blocked = true
					} else if math.Abs(nodeReplacement.TopLeft.X+nodeReplacement.Width-(adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)) <= SideEdgeSpacing {
						isL1Blocked = true
					}
				}
			}

			// Only used for semi-diagonals
			checkAlternateRouteBlocked := func(otherNode *layoutgraph.Node) bool {
				if otherNode.IsDescendantOf(nodeReplacement) || otherNode.IsDescendantOf(adjacentNodeReplacement) {
					return false
				}
				getCenters()
				edgeDir = getEdgeDirection()
				switch edgeDir {
				case geo.Top:
					floor := math.Max(nodeReplacement.TopLeft.X, adjacentNodeReplacement.TopLeft.X)
					ceil := math.Min(nodeReplacement.TopLeft.X+nodeReplacement.Width, adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)
					if !isL2Blocked {
						minX := math.Min(nodeReplacement.TopLeft.X, adjacentNodeReplacement.TopLeft.X)
						lowerMidX := minX + math.Abs(floor-minX)/2.
						// ┌────────────────┐
						// │                │
						// │                │
						// │                │
						// └────────────────┘
						//
						//
						//
						//          ┌──────┐
						//          │      │
						//          │      │
						//          └──────┘
						if nodeReplacement.TopLeft.X < adjacentNodeReplacement.TopLeft.X {
							if otherNode.PassesThrough(geo.NewPoint(lowerMidX, nodeCenter.Y), geo.NewPoint(lowerMidX, adjacentNodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(lowerMidX, adjacentNodeCenter.Y), adjacentNodeCenter) {
								isL2Blocked = true
							}
						}
						//        ┌──────┐
						//        │      │
						//        │      │
						//        └──────┘
						//
						//
						//
						// ┌────────────────┐
						// │                │
						// │                │
						// │                │
						// └────────────────┘
						//
						if nodeReplacement.TopLeft.X > adjacentNodeReplacement.TopLeft.X {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(lowerMidX, nodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(lowerMidX, adjacentNodeCenter.Y), geo.NewPoint(lowerMidX, nodeCenter.Y)) {
								isL2Blocked = true
							}
						}
					}
					if !isL1Blocked {
						maxX := math.Max(nodeReplacement.TopLeft.X+nodeReplacement.Width, adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)
						upperMidX := maxX - math.Abs(ceil-maxX)/2.
						// ┌────────────────┐
						// │                │
						// │                │
						// │                │
						// └────────────────┘
						//
						//
						//
						//   ┌──────┐
						//   │      │
						//   │      │
						//   └──────┘
						if nodeReplacement.TopLeft.X+nodeReplacement.Width > adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width {
							if otherNode.PassesThrough(geo.NewPoint(upperMidX, nodeCenter.Y), geo.NewPoint(upperMidX, adjacentNodeCenter.Y)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(upperMidX, adjacentNodeCenter.Y)) {
								isL1Blocked = true
							}
						}
						//   ┌──────┐
						//   │      │
						//   │      │
						//   └──────┘
						//
						//
						// ┌────────────────┐
						// │                │
						// │                │
						// │                │
						// └────────────────┘
						//
						if nodeReplacement.TopLeft.X+nodeReplacement.Width < adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(upperMidX, nodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(upperMidX, adjacentNodeCenter.Y), geo.NewPoint(upperMidX, nodeCenter.Y)) {
								isL1Blocked = true
							}
						}
					}
				case geo.Bottom:
					nodeReplacement, adjacentNodeReplacement = adjacentNodeReplacement, nodeReplacement
					nodeCenter, adjacentNodeCenter = adjacentNodeCenter, nodeCenter

					floor := math.Max(nodeReplacement.TopLeft.X, adjacentNodeReplacement.TopLeft.X)
					ceil := math.Min(nodeReplacement.TopLeft.X+nodeReplacement.Width, adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)
					if !isL2Blocked {
						minX := math.Min(nodeReplacement.TopLeft.X, adjacentNodeReplacement.TopLeft.X)
						lowerMidX := minX + math.Abs(floor-minX)/2.
						if nodeReplacement.TopLeft.X < adjacentNodeReplacement.TopLeft.X {
							if otherNode.PassesThrough(geo.NewPoint(lowerMidX, nodeCenter.Y), geo.NewPoint(lowerMidX, adjacentNodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(lowerMidX, adjacentNodeCenter.Y), adjacentNodeCenter) {
								isL2Blocked = true
							}
						}
						if nodeReplacement.TopLeft.X > adjacentNodeReplacement.TopLeft.X {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(lowerMidX, nodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(lowerMidX, adjacentNodeCenter.Y), geo.NewPoint(lowerMidX, nodeCenter.Y)) {
								isL2Blocked = true
							}
						}
					}
					if !isL1Blocked {
						maxX := math.Max(nodeReplacement.TopLeft.X+nodeReplacement.Width, adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width)
						upperMidX := maxX - math.Abs(ceil-maxX)/2.
						if nodeReplacement.TopLeft.X+nodeReplacement.Width > adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width {
							if otherNode.PassesThrough(geo.NewPoint(upperMidX, nodeCenter.Y), geo.NewPoint(upperMidX, adjacentNodeCenter.Y)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(upperMidX, adjacentNodeCenter.Y)) {
								isL1Blocked = true
							}
						}
						if nodeReplacement.TopLeft.X+nodeReplacement.Width < adjacentNodeReplacement.TopLeft.X+adjacentNodeReplacement.Width {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(upperMidX, nodeCenter.Y)) ||
								otherNode.PassesThrough(geo.NewPoint(upperMidX, adjacentNodeCenter.Y), geo.NewPoint(upperMidX, nodeCenter.Y)) {
								isL1Blocked = true
							}
						}
					}

					nodeReplacement, adjacentNodeReplacement = adjacentNodeReplacement, nodeReplacement
					nodeCenter, adjacentNodeCenter = adjacentNodeCenter, nodeCenter
				case geo.Left:
					floor := math.Max(nodeReplacement.TopLeft.Y, adjacentNodeReplacement.TopLeft.Y)
					ceil := math.Min(nodeReplacement.TopLeft.Y+nodeReplacement.Height, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
					if !isL1Blocked {
						minY := math.Min(nodeReplacement.TopLeft.Y, adjacentNodeReplacement.TopLeft.Y)
						lowerMidY := minY + math.Abs(floor-minY)/2.
						// ┌────────┐
						// │        │
						// │        │
						// │        │           ┌─────┐
						// │        │           │     │
						// │        │           │     │
						// │        │           │     │
						// │        │           └─────┘
						// └────────┘
						if nodeReplacement.TopLeft.Y < adjacentNodeReplacement.TopLeft.Y {
							if otherNode.PassesThrough(geo.NewPoint(nodeCenter.X, lowerMidY), geo.NewPoint(adjacentNodeCenter.X, lowerMidY)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(adjacentNodeCenter.X, lowerMidY)) {
								isL1Blocked = true
							}
						}
						//              ┌────────┐
						//              │        │
						//              │        │
						// ┌─────┐      │        │
						// │     │      │        │
						// │     │      │        │
						// │     │      │        │
						// └─────┘      │        │
						//              └────────┘
						if nodeReplacement.TopLeft.Y > adjacentNodeReplacement.TopLeft.Y {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(nodeCenter.X, lowerMidY)) ||
								otherNode.PassesThrough(geo.NewPoint(adjacentNodeCenter.X, lowerMidY), geo.NewPoint(nodeCenter.X, lowerMidY)) {
								isL1Blocked = true
							}
						}
					}
					if !isL2Blocked {
						maxY := math.Max(nodeReplacement.TopLeft.Y+nodeReplacement.Height, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
						upperMidY := maxY - math.Abs(ceil-maxY)/2.
						// ┌────────┐         ┌─────┐
						// │        │         │     │
						// │        │         │     │
						// │        │         │     │
						// │        │         └─────┘
						// │        │
						// │        │
						// │        │
						// └────────┘
						if nodeReplacement.TopLeft.Y+nodeReplacement.Height > adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height {
							if otherNode.PassesThrough(geo.NewPoint(nodeCenter.X, upperMidY), geo.NewPoint(adjacentNodeCenter.X, upperMidY)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(adjacentNodeCenter.X, upperMidY)) {
								isL2Blocked = true
							}
						}
						// ┌─────┐       ┌────────┐
						// │     │       │        │
						// │     │       │        │
						// │     │       │        │
						// └─────┘       │        │
						//               │        │
						//               │        │
						//               │        │
						//               └────────┘
						if nodeReplacement.TopLeft.Y+nodeReplacement.Height < adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(nodeCenter.X, upperMidY)) ||
								otherNode.PassesThrough(geo.NewPoint(adjacentNodeCenter.X, upperMidY), geo.NewPoint(nodeCenter.X, upperMidY)) {
								isL2Blocked = true
							}
						}
					}
				case geo.Right:
					// Use same code as geo.Left but swap, then swap back after
					nodeReplacement, adjacentNodeReplacement = adjacentNodeReplacement, nodeReplacement
					nodeCenter, adjacentNodeCenter = adjacentNodeCenter, nodeCenter

					floor := math.Max(nodeReplacement.TopLeft.Y, adjacentNodeReplacement.TopLeft.Y)
					ceil := math.Min(nodeReplacement.TopLeft.Y+nodeReplacement.Height, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
					if !isL1Blocked {
						minY := math.Min(nodeReplacement.TopLeft.Y, adjacentNodeReplacement.TopLeft.Y)
						lowerMidY := minY + math.Abs(floor-minY)/2.
						if nodeReplacement.TopLeft.Y < adjacentNodeReplacement.TopLeft.Y {
							if otherNode.PassesThrough(geo.NewPoint(nodeCenter.X, lowerMidY), geo.NewPoint(adjacentNodeCenter.X, lowerMidY)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(adjacentNodeCenter.X, lowerMidY)) {
								isL1Blocked = true
							}
						}
						if nodeReplacement.TopLeft.Y > adjacentNodeReplacement.TopLeft.Y {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(nodeCenter.X, lowerMidY)) ||
								otherNode.PassesThrough(geo.NewPoint(adjacentNodeCenter.X, lowerMidY), geo.NewPoint(nodeCenter.X, lowerMidY)) {
								isL1Blocked = true
							}
						}
					}
					if !isL2Blocked {
						maxY := math.Max(nodeReplacement.TopLeft.Y+nodeReplacement.Height, adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height)
						upperMidY := maxY - math.Abs(ceil-maxY)/2.
						if nodeReplacement.TopLeft.Y+nodeReplacement.Height > adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height {
							if otherNode.PassesThrough(geo.NewPoint(nodeCenter.X, upperMidY), geo.NewPoint(adjacentNodeCenter.X, upperMidY)) ||
								otherNode.PassesThrough(adjacentNodeCenter, geo.NewPoint(adjacentNodeCenter.X, upperMidY)) {
								isL2Blocked = true
							}
						}
						if nodeReplacement.TopLeft.Y+nodeReplacement.Height < adjacentNodeReplacement.TopLeft.Y+adjacentNodeReplacement.Height {
							if otherNode.PassesThrough(nodeCenter, geo.NewPoint(nodeCenter.X, upperMidY)) ||
								otherNode.PassesThrough(geo.NewPoint(adjacentNodeCenter.X, upperMidY), geo.NewPoint(nodeCenter.X, upperMidY)) {
								isL2Blocked = true
							}
						}
					}
					nodeReplacement, adjacentNodeReplacement = adjacentNodeReplacement, nodeReplacement
					nodeCenter, adjacentNodeCenter = adjacentNodeCenter, nodeCenter
				default:
					panic("did not expect diagonal")
				}

				if isL1Blocked && isL2Blocked {
					return true
				}

				return false
			}

			bestRouteBlocked := false
			alternateRouteBlocked := !isSemiDiagonal
			// A sweep scorer that already filtered this edge's obstacles does
			// not need the container sets again (they depend on topology only).
			if !s.sweepOn || !s.sweepBuilt(i) {
				clear(s.obstructionSets)
				s.obstructionSets = s.obstructionSets[:0]
				s.resetObstructionKeys()
				ancestor := nodeReplacement.NearestSharedAncestor(adjacentNodeReplacement)

				curr := nodeReplacement.EffectiveContainer()
				for {
					if s.hasObstructionKey(curr) {
						break
					}
					s.obstructionSets = append(s.obstructionSets, node.Graph.Containers[curr])
					s.addObstructionKey(curr)
					if curr == nil {
						break
					}
					curr = curr.EffectiveContainer()
					if curr == ancestor {
						break
					}
				}

				curr = adjacentNodeReplacement.EffectiveContainer()
				for {
					if s.hasObstructionKey(curr) {
						break
					}
					s.obstructionSets = append(s.obstructionSets, node.Graph.Containers[curr])
					s.addObstructionKey(curr)
					if curr == nil {
						break
					}
					curr = curr.EffectiveContainer()
					if curr == ancestor {
						break
					}
				}
			}

			boundsUsable := bounds.usable
			boundsLeft, boundsTop, boundsRight, boundsBottom := bounds.left, bounds.top, bounds.right, bounds.bottom
			if s.sweepOn {
				// A sweep scorer: between its evaluations only s.sweepMover and
				// its descendants move, so this edge's obstacles that pass the
				// filters below that depend only on topology, with the boxes of
				// those that cannot move, were collected once, in scan order.
				// Every check is a skip without side effects, so testing the
				// rest (bounds.excludes, inline as below; TopLeft for movers)
				// on that sequence finds the same obstacles in the same order.
				obstacles := s.sweepObstacles(i, len(node.Edges), nodeReplacement, adjacentNodeReplacement)
				for k := range obstacles {
					if k&(scoringCancellationCheckInterval-1) == 0 {
						if err := p.check(); err != nil {
							return 0, err
						}
					}
					obstacle := &obstacles[k]
					obstruction := obstacle.node
					if obstacle.moving {
						if obstruction.TopLeft == nil {
							continue
						}
						if boundsUsable {
							x, y := obstruction.TopLeft.X, obstruction.TopLeft.Y
							right, bottom := x+obstruction.Width, y+obstruction.Height
							if (x-x)+(y-y)+(right-right)+(bottom-bottom) == 0 &&
								((x < boundsLeft && right < boundsLeft) ||
									(x > boundsRight && right > boundsRight) ||
									(y < boundsTop && bottom < boundsTop) ||
									(y > boundsBottom && bottom > boundsBottom)) {
								continue
							}
						}
					} else if boundsUsable && obstacle.finite &&
						((obstacle.x < boundsLeft && obstacle.right < boundsLeft) ||
							(obstacle.x > boundsRight && obstacle.right > boundsRight) ||
							(obstacle.y < boundsTop && obstacle.bottom < boundsTop) ||
							(obstacle.y > boundsBottom && obstacle.bottom > boundsBottom)) {
						continue
					}
					if !bestRouteBlocked && checkBestRouteBlocked(obstruction) {
						bestRouteBlocked = true
					}
					if bestRouteBlocked && !alternateRouteBlocked && checkAlternateRouteBlocked(obstruction) {
						alternateRouteBlocked = true
					}
					if bestRouteBlocked && alternateRouteBlocked {
						break
					}
				}
			}
			// The loop below tests bounds.excludes and IsDescendantOf inline:
			// the same operations on the same values, without a call per
			// obstacle (excludes is not inlinable).
			var nodeAncestry, adjacentAncestry []*layoutgraph.Node
			var nodeAncestryOK, adjacentAncestryOK bool
			if !s.sweepOn {
				nodeAncestry, nodeAncestryOK = appendScoringAncestry(s.nodeAncestry[:0], nodeReplacement)
				adjacentAncestry, adjacentAncestryOK = appendScoringAncestry(s.adjacentAncestry[:0], adjacentNodeReplacement)
				s.nodeAncestry, s.adjacentAncestry = nodeAncestry, adjacentAncestry
			}
			// reached counts the obstacles the scan reached (it stops at the
			// one that blocks both routes); an EdgeLengthSession records it.
			reached := 0
			for obstructionSetIndex, obstructions := range s.obstructionSets {
				if s.sweepOn {
					break
				}
				if err := p.at(obstructionSetIndex); err != nil {
					return 0, err
				}
				for i := 0; i < len(obstructions); i++ {
					// p.at(i) for i >= 0.
					if i&(scoringCancellationCheckInterval-1) == 0 {
						if err := p.check(); err != nil {
							return 0, err
						}
					}
					obstruction := obstructions[i]
					reached++
					// bounds.excludes(obstruction)
					if boundsUsable && obstruction != nil && obstruction.TopLeft != nil {
						x, y := obstruction.TopLeft.X, obstruction.TopLeft.Y
						right, bottom := x+obstruction.Width, y+obstruction.Height
						if (x-x)+(y-y)+(right-right)+(bottom-bottom) == 0 &&
							((x < boundsLeft && right < boundsLeft) ||
								(x > boundsRight && right > boundsRight) ||
								(y < boundsTop && bottom < boundsTop) ||
								(y > boundsBottom && bottom > boundsBottom)) {
							continue
						}
					}
					// nodeReplacement.IsDescendantOf(obstruction) || adjacentNodeReplacement.IsDescendantOf(obstruction)
					if scoringAncestryHas(nodeAncestry, nodeAncestryOK, nodeReplacement, obstruction) ||
						scoringAncestryHas(adjacentAncestry, adjacentAncestryOK, adjacentNodeReplacement, obstruction) {
						continue
					}
					// Used during initialization, or sibling coming from subgraph that hasn't gone yet
					if obstruction.TopLeft == nil {
						continue
					}
					if obstruction.Graph != nodeReplacement.Graph {
						// not in the same subgraph, can happen during placement
						continue
					}
					if obstruction == nodeReplacement || obstruction == adjacentNodeReplacement {
						continue
					}
					if obstruction.IsClusterVessel() {
						if (nodeReplacement.Cluster != nil && nodeReplacement.Cluster.Vessel == obstruction) || (adjacentNodeReplacement.Cluster != nil && adjacentNodeReplacement.Cluster.Vessel == obstruction) {
							// Penalties are calculated elsewhere below for cluster nodes, so don't double count
							continue
						}
					}

					if !bestRouteBlocked && checkBestRouteBlocked(obstruction) {
						bestRouteBlocked = true
					}
					if bestRouteBlocked && !alternateRouteBlocked && checkAlternateRouteBlocked(obstruction) {
						alternateRouteBlocked = true
					}
					if bestRouteBlocked && alternateRouteBlocked {
						break
					}
				}
				if bestRouteBlocked && alternateRouteBlocked {
					break
				}
			}
			if p.sess != nil {
				p.sess.recordScan(bounds, s.obstructionKeys, reached)
			}
			if bestRouteBlocked {
				var passThroughCost float64
				// Penalize deeply for obstructing cluster
				if nodeReplacement.Cluster != nil || adjacentNodeReplacement.Cluster != nil {
					passThroughCost = graphTurnCost() * 2
				} else if isDiagonal {
					// When it's diagonal, the shortest route would've been an L shaped, so to turn it in an S-shape is only one extra turn
					passThroughCost = graphTurnCost()
				} else if isSemiDiagonal {
					if alternateRouteBlocked {
						passThroughCost = graphTurnCost() * 2
					} else {
						// Just an L-shape
						passThroughCost = graphTurnCost()
					}
				} else {
					// When it's straight, takes 2 extra turns to get around the obstruction
					passThroughCost = graphTurnCost() * 2
				}
				distance += passThroughCost
			}

			badClusterArrangementPenalty := graphTurnCost()

			if nodeReplacement.Cluster != nil {
				switch nodeReplacement.Cluster.Arrangement {
				case layoutgraph.Row:
					edgeDir = getEdgeDirection()
					switch edgeDir {
					case geo.Left, geo.Right:
						distance += badClusterArrangementPenalty * 2
					}
				case layoutgraph.Column:
					edgeDir = getEdgeDirection()
					switch edgeDir {
					case geo.Top, geo.Bottom:
						distance += badClusterArrangementPenalty * 2
					}
				}

				// This is a unique penalty because otherwise, two nodes could sit above the cluster (if it's a row)
				// and avoid all other penalties (including symmetry)
				thisClusterDirection := nodeReplacement.Cluster.Vessel.Orientation(adjacentNodeReplacement)
				firstExternalNode, secondExternalNode, exactlyTwo := clusterExactlyTwoExternalConnectedNodes(nodeReplacement.Cluster)
				if exactlyTwo {
					var otherClusterNode *layoutgraph.Node
					if firstExternalNode == adjacentNodeReplacement {
						otherClusterNode = secondExternalNode
					} else {
						otherClusterNode = firstExternalNode
					}
					otherClusterDirection := nodeReplacement.Cluster.Vessel.Orientation(otherClusterNode)

					// Penalize being on the same side as other external
					switch nodeReplacement.Cluster.Arrangement {
					case layoutgraph.Row:
						switch otherClusterDirection {
						// The other node is below us
						case geo.TopLeft, geo.Top, geo.TopRight:
							switch thisClusterDirection {
							// The other node is below us, so we don't want this node below us
							case geo.TopLeft, geo.Top, geo.TopRight:
								distance += badClusterArrangementPenalty
							}
						case geo.BottomLeft, geo.Bottom, geo.BottomRight:
							switch thisClusterDirection {
							case geo.BottomLeft, geo.Bottom, geo.BottomRight:
								distance += badClusterArrangementPenalty
							}
						}
					case layoutgraph.Column:
						switch otherClusterDirection {
						case geo.TopLeft, geo.Left, geo.BottomLeft:
							switch thisClusterDirection {
							case geo.TopLeft, geo.Left, geo.BottomLeft:
								distance += badClusterArrangementPenalty
							}
						case geo.TopRight, geo.Right, geo.BottomRight:
							switch thisClusterDirection {
							case geo.TopRight, geo.Right, geo.BottomRight:
								distance += badClusterArrangementPenalty
							}
						}
					}
				}
				// Reward being aligned with center when on the right side
				cost := graphTurnCost()
				switch nodeReplacement.Cluster.Arrangement {
				case layoutgraph.Row:
					switch thisClusterDirection {
					case geo.Top, geo.Bottom:
						cost = math.Abs(nodeReplacement.Cluster.Vessel.Center().X - adjacentNodeReplacement.Center().X)
					}
				case layoutgraph.Column:
					switch thisClusterDirection {
					case geo.Right, geo.Left:
						cost = math.Abs(nodeReplacement.Cluster.Vessel.Center().Y - adjacentNodeReplacement.Center().Y)
					}
				}
				distance += cost
			}
			if adjacentNodeReplacement.Cluster != nil {
				switch adjacentNodeReplacement.Cluster.Arrangement {
				case layoutgraph.Row:
					edgeDir = getEdgeDirection()
					switch edgeDir {
					case geo.Left, geo.Right:
						distance += badClusterArrangementPenalty * 2
					}
				case layoutgraph.Column:
					edgeDir = getEdgeDirection()
					switch edgeDir {
					case geo.Top, geo.Bottom:
						distance += badClusterArrangementPenalty * 2
					}
				}

				thisClusterDirection := adjacentNodeReplacement.Cluster.Vessel.Orientation(nodeReplacement)
				firstExternalNode, secondExternalNode, exactlyTwo := clusterExactlyTwoExternalConnectedNodes(adjacentNodeReplacement.Cluster)
				if exactlyTwo {
					var otherClusterNode *layoutgraph.Node
					if firstExternalNode == nodeReplacement {
						otherClusterNode = secondExternalNode
					} else {
						otherClusterNode = firstExternalNode
					}
					otherClusterDirection := adjacentNodeReplacement.Cluster.Vessel.Orientation(otherClusterNode)

					switch adjacentNodeReplacement.Cluster.Arrangement {
					case layoutgraph.Row:
						switch otherClusterDirection {
						case geo.TopLeft, geo.Top, geo.TopRight:
							switch thisClusterDirection {
							case geo.TopLeft, geo.Top, geo.TopRight:
								distance += badClusterArrangementPenalty
							}
						case geo.BottomLeft, geo.Bottom, geo.BottomRight:
							switch thisClusterDirection {
							case geo.BottomLeft, geo.Bottom, geo.BottomRight:
								distance += badClusterArrangementPenalty
							}
						}
					case layoutgraph.Column:
						switch otherClusterDirection {
						case geo.TopLeft, geo.Left, geo.BottomLeft:
							switch thisClusterDirection {
							case geo.TopLeft, geo.Left, geo.BottomLeft:
								distance += badClusterArrangementPenalty
							}
						case geo.TopRight, geo.Right, geo.BottomRight:
							switch thisClusterDirection {
							case geo.TopRight, geo.Right, geo.BottomRight:
								distance += badClusterArrangementPenalty
							}
						}
					}
				}

				cost := graphTurnCost()
				switch adjacentNodeReplacement.Cluster.Arrangement {
				case layoutgraph.Row:
					switch thisClusterDirection {
					case geo.Top, geo.Bottom:
						cost = math.Abs(adjacentNodeReplacement.Cluster.Vessel.Center().X - nodeReplacement.Center().X)
					}
				case layoutgraph.Column:
					switch thisClusterDirection {
					case geo.Right, geo.Left:
						cost = math.Abs(adjacentNodeReplacement.Cluster.Vessel.Center().Y - nodeReplacement.Center().Y)
					}
				}
				distance += cost
			}
		}

		if includeSizes && e.IsBetweenTableColumns() {
			var columnIndex int
			if e.From == nodeReplacement {
				columnIndex = *e.ToTableColumnIndex
			} else {
				columnIndex = *e.FromTableColumnIndex
			}
			for i, e2 := range adjacentNodeReplacement.Edges {
				if err := p.at(i); err != nil {
					return 0, err
				}
				if !e2.IsBetweenTableColumns() {
					continue
				}
				otherTable := adjacentNodeReplacement.Adjacent(e2)
				var otherColumnIndex int
				if e2.From == otherTable {
					otherColumnIndex = *e2.ToTableColumnIndex
				} else {
					otherColumnIndex = *e2.FromTableColumnIndex
				}

				directionToOther := nodeReplacement.Orientation(otherTable)

				sameSide := false
				// The other table has to be on the same side as us for this crossing calculation to matter
				otherEdgeDirection := otherTable.Orientation(adjacentNodeReplacement)
				edgeDir = getEdgeDirection()
				switch edgeDir {
				case geo.BottomRight, geo.Right, geo.TopRight:
					switch otherEdgeDirection {
					case geo.BottomRight, geo.Right, geo.TopRight:
						sameSide = true
					}
				case geo.BottomLeft, geo.Left, geo.TopLeft:
					switch otherEdgeDirection {
					case geo.BottomLeft, geo.Left, geo.TopLeft:
						sameSide = true
					}
				}

				if sameSide {
					switch directionToOther {
					// The other table is below us
					case geo.TopRight, geo.Top, geo.TopLeft:
						// The other table connects to column higher than ours
						if otherColumnIndex < columnIndex {
							distance += node.Graph.CrossingCost()
						}
						// The other table is above us
					case geo.BottomRight, geo.Bottom, geo.BottomLeft:
						// The other table connects to column lower than ours
						if otherColumnIndex > columnIndex {
							distance += node.Graph.CrossingCost()
						}
					}
				}
			}
		}

		// If the distance is less than the minGapSize (and it's enforced), we don't want to reward that
		distance = math.Max(distance, minGapSize)

		// Add a penalty according to edge direction
		outerNode := nodeReplacement
		if nodeReplacement.Container != adjacentNodeReplacement.Container {
			if depth(nodeReplacement) > depth(adjacentNodeReplacement) {
				outerNode = adjacentNodeReplacement
			}
		}
		if directionPenalty && (e.IsDirected() || outerNode.ContainerDirection() != geo.NONE) {
			edgeDir = getEdgeDirection()
			from := e.From
			if semanticFrom, _, directed := e.DirectedEndpoints(); directed {
				from = semanticFrom
			}
			if from == node {
				edgeDir = edgeDir.GetOpposite()
			}

			directionUsed := edgeDir

			// For clusters, we care more about alignment in center than compass direction
			// So coalesce diagonal directions to the same direction
			if nodeReplacement.Cluster != nil || adjacentNodeReplacement.Cluster != nil {
				switch direction {
				case geo.Left:
					switch directionUsed {
					case geo.TopLeft, geo.BottomLeft:
						directionUsed = geo.Left
					}
				case geo.Right:
					switch directionUsed {
					case geo.TopRight, geo.BottomRight:
						directionUsed = geo.Right
					}
				case geo.Top:
					switch directionUsed {
					case geo.TopLeft, geo.TopRight:
						directionUsed = geo.Top
					}
				case geo.Bottom:
					switch directionUsed {
					case geo.BottomLeft, geo.BottomRight:
						directionUsed = geo.Bottom
					}
				}
			}

			dirDelta := math.Abs(float64(compassDelta(compass, directionCompass(directionUsed))))
			if !e.IsDirected() {
				// if there is a container direction set, also apply to bidirectional edges
				// using compassAxisDelta for most of the weight, and still slightly preferring the edge direction as defined
				// this way `direction:right; a <-> b` will weight layout [a]<->[b] slightly better than [b]<->[a]
				dirDelta = 0.1*dirDelta + 0.9*math.Abs(float64(
					compassAxisDelta(compass, directionCompass(directionUsed)),
				))
			}
			baseCost := node.Graph.CellSize
			if !includeSizes {
				baseCost = 1.
			}
			distance += directionFactor * dirDelta * baseCost * sizelessDirectionDeltaFactor
		}

		if includeSizes {
			if e.HasLargeArrowheadLabel() {
				edgeDir = getEdgeDirection()
				if !edgeDir.IsHorizontal() {
					distance += 10 * graphTurnCost()
				}
			} else if e.Label != nil {
				if !directionPenalty || !direction.IsVertical() {
					count := s.labelCounts[i]
					// with 2 vertical edges a label can go on each side without obstructing each other
					if count > 2 {
						edgeDir = getEdgeDirection()
						if !edgeDir.IsHorizontal() {
							// penalize labeled edges with many duplicates if they are not horizontal
							distance += float64(count) * graphTurnCost()
						}
					}
				}
			}
		}

		totalDistance += distance
		if s.exitOn && geo.PrecisionCompare(totalDistance-s.exitOffset, s.exitBound, geo.PRECISION) == 1 {
			// See NodeEdgeLengthScorer.ScoreUnlessAbove: everything still to
			// be added is non-negative, so the full sum compares the same.
			return totalDistance, nil
		}
	}

	// We don't care about getting as close as possible to all Nears, we just want to get close to one of them
	// Since Nears are symmetrical (if node A has node B and C as Nears, nodes B and C will have A too), they'll end up clustering together
	if len(node.Nears) > 0 {
		minDistanceToNear := math.Inf(1)
		nearIndex := 0
		for near := range node.Nears {
			if err := p.at(nearIndex); err != nil {
				return 0, err
			}
			nearIndex++
			// Not initialized
			if near.TopLeft == nil {
				minDistanceToNear = 0
				continue
			}
			distance := node.DistanceTo(near, includeSizes)
			minDistanceToNear = math.Min(minDistanceToNear, distance)
		}
		totalDistance += minDistanceToNear
	}

	if node.HerdAssignment != nil && includeSizes {
		d := 0.0
		switch node.HerdAssignment.Orientation {
		case geo.Bottom:
			d = node.HerdAssignment.Val - (node.TopLeft.Y + node.Height)
		case geo.Top:
			d = node.TopLeft.Y - node.HerdAssignment.Val
		case geo.Left:
			d = node.TopLeft.X - node.HerdAssignment.Val
		case geo.Right:
			d = node.HerdAssignment.Val - (node.TopLeft.X + node.Width)
		}

		if d >= 0 {
			totalDistance += d
		} else {
			// extra penalty since it makes all other sheep further from herd fence
			totalDistance -= (d - node.Graph.CellSize)
		}
	}

	// add a penalty for common uncle siblings not being aligned
	if siblings, ok := node.Graph.CommonUncleSiblings[node]; ok {
		axisScore := AxisScore(siblings)
		cost := node.Graph.CellSize
		if !includeSizes {
			cost = 1.
		}
		totalDistance += cost * (1. - axisScore) * float64(len(siblings)-1)
	}

	if err := p.check(); err != nil {
		return 0, err
	}
	if includeSizes {
		totalDistance += flowContinuityCost(node, s)
	}
	return totalDistance, nil
}

// maxScoringAncestry bounds appendScoringAncestry; deeper (or cyclic) parent
// chains are left to IsDescendantOf itself.
const maxScoringAncestry = 4096

// appendScoringAncestry appends node and the nodes IsDescendantOf visits from
// it: its Container, else its cluster's vessel, else its sequence's vessel,
// repeatedly. ok is false if the chain is longer than maxScoringAncestry.
func appendScoringAncestry(chain []*layoutgraph.Node, node *layoutgraph.Node) ([]*layoutgraph.Node, bool) {
	for depth := 0; node != nil; depth++ {
		if depth == maxScoringAncestry {
			return chain, false
		}
		chain = append(chain, node)
		switch {
		case node.Container != nil:
			node = node.Container
		case node.Cluster != nil:
			node = node.Cluster.Vessel
		case node.Sequence != nil:
			node = node.Sequence.Vessel
		default:
			node = nil
		}
	}
	return chain, true
}

// scoringAncestryHas is node.IsDescendantOf(ancestor) for chain, ok =
// appendScoringAncestry(..., node): IsDescendantOf is true exactly when
// ancestor is a node of the chain, or nil (the walk's end answers
// ancestor == nil).
func scoringAncestryHas(chain []*layoutgraph.Node, ok bool, node, ancestor *layoutgraph.Node) bool {
	if !ok {
		return node.IsDescendantOf(ancestor)
	}
	if ancestor == nil {
		return true
	}
	for _, n := range chain {
		if n == ancestor {
			return true
		}
	}
	return false
}

func (s *edgeScratch) resetObstructionKeys() {
	clear(s.obstructionKeys)
	s.obstructionKeys = s.obstructionKeys[:0]
	if s.obstructionAdded != nil {
		clear(s.obstructionAdded)
	}
}

// hasObstructionKey reports whether container's children were already added
// for this edge (container may be nil, the root).
func (s *edgeScratch) hasObstructionKey(container *layoutgraph.Node) bool {
	if len(s.obstructionKeys) > maxLinearObstructionKeys {
		_, ok := s.obstructionAdded[container]
		return ok
	}
	for _, key := range s.obstructionKeys {
		if key == container {
			return true
		}
	}
	return false
}

func (s *edgeScratch) addObstructionKey(container *layoutgraph.Node) {
	s.obstructionKeys = append(s.obstructionKeys, container)
	if len(s.obstructionKeys) <= maxLinearObstructionKeys {
		return
	}
	if s.obstructionAdded == nil {
		s.obstructionAdded = make(map[*layoutgraph.Node]struct{}, 2*maxLinearObstructionKeys)
	}
	if len(s.obstructionKeys) == maxLinearObstructionKeys+1 {
		for _, key := range s.obstructionKeys {
			s.obstructionAdded[key] = struct{}{}
		}
		return
	}
	s.obstructionAdded[container] = struct{}{}
}

// countLabeledEdgePairs fills s.labelCounts: for each labeled incident edge
// (counted only when sizes are included, as before), the number of labeled
// incident edges whose endpoint replacements have the same pair of IDs.
func countLabeledEdgePairs(node *layoutgraph.Node, nodeReplacements, adjReplacements []*layoutgraph.Node, labeled int, s *edgeScratch) {
	if cap(s.labelCounts) < len(node.Edges) {
		s.labelCounts = make([]int, len(node.Edges))
	} else {
		s.labelCounts = s.labelCounts[:len(node.Edges)]
		clear(s.labelCounts)
	}
	if labeled == 0 {
		return
	}
	isLabeled := func(i int) bool { return node.Edges[i].Label != nil }
	if labeled > maxLinearLabeledEdges {
		if s.labeledEdgeCount == nil {
			s.labeledEdgeCount = make(map[labeledEdgePair]int)
		} else {
			clear(s.labeledEdgeCount)
		}
		for i := range node.Edges {
			if isLabeled(i) {
				s.labeledEdgeCount[labeledEdgePair{from: nodeReplacements[i].ID, to: adjReplacements[i].ID}]++
			}
		}
		for i := range node.Edges {
			if isLabeled(i) {
				s.labelCounts[i] = s.labeledEdgeCount[labeledEdgePair{from: nodeReplacements[i].ID, to: adjReplacements[i].ID}]
			}
		}
		return
	}
	for i := range node.Edges {
		if !isLabeled(i) {
			continue
		}
		from, to := nodeReplacements[i].ID, adjReplacements[i].ID
		count := 0
		for j := range node.Edges {
			if isLabeled(j) && nodeReplacements[j].ID == from && adjReplacements[j].ID == to {
				count++
			}
		}
		s.labelCounts[i] = count
	}
}
