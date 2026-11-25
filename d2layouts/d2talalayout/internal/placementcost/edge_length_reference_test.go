package placementcost

// referenceEvaluateNodeEdgeLength is evaluateNodeEdgeLength before its obstacle loop was inlined, kept verbatim apart from its name: the obstacle loop called
// bounds.excludes and IsDescendantOf per obstacle and polled with p.at.

import (
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func referenceEvaluateNodeEdgeLength(p *scoringPoll, node *layoutgraph.Node, options EdgeLengthOptions, s *edgeScratch, setup edgeLengthStatic) (float64, error) {
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

		// Lazy calculation of centers and edge direction - only when needed
		var nodeCenter, adjacentNodeCenter *geo.Point
		var edgeDirection geo.Orientation
		var edgeDirectionCalculated bool
		var edgeDir geo.Orientation

		getCenters := func() {
			if nodeCenter == nil {
				nodeCenter = nodeReplacement.Center()
				adjacentNodeCenter = adjacentNodeReplacement.Center()
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
					distance += node.Graph.TurnCost()
				}
			} else {
				distance = placementDistance(nodeReplacement, adjacentNodeReplacement, includeSizes)
			}
		}

		if includeSizes && node.IsContainer() && adjacentNode.IsContainer() && nodeContainer == adjacentNode.EffectiveContainer() {
			containerAlignmentPenalty := 0.
			// If the widths are roughly equal in size, we want alignment on either one of the sides or the center
			if math.Min(node.Width, adjacentNode.Width) > math.Max(node.Width, adjacentNode.Width)*0.75 {
				if (node.TopLeft.X != adjacentNode.TopLeft.X) && (node.TopLeft.X+node.Width != adjacentNode.TopLeft.X+adjacentNode.Width) && ((node.TopLeft.X+node.Width)/2 != (adjacentNode.TopLeft.X+adjacentNode.Width)/2) {
					containerAlignmentPenalty = node.Graph.TurnCost()
				}
			}

			if math.Min(node.Height, adjacentNode.Height) > math.Max(node.Height, adjacentNode.Height)*0.75 {
				if (node.TopLeft.Y != adjacentNode.TopLeft.Y) && (node.TopLeft.Y+node.Height != adjacentNode.TopLeft.Y+adjacentNode.Height) && ((node.TopLeft.Y+node.Height)/2 != (adjacentNode.TopLeft.Y+adjacentNode.Height)/2) {
					containerAlignmentPenalty = node.Graph.TurnCost()
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

			for obstructionSetIndex, obstructions := range s.obstructionSets {
				if err := p.at(obstructionSetIndex); err != nil {
					return 0, err
				}
				for i := 0; i < len(obstructions); i++ {
					if err := p.at(i); err != nil {
						return 0, err
					}
					obstruction := obstructions[i]
					if bounds.excludes(obstruction) {
						continue
					}
					if nodeReplacement.IsDescendantOf(obstruction) || adjacentNodeReplacement.IsDescendantOf(obstruction) {
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
			if bestRouteBlocked {
				var passThroughCost float64
				// Penalize deeply for obstructing cluster
				if nodeReplacement.Cluster != nil || adjacentNodeReplacement.Cluster != nil {
					passThroughCost = node.Graph.TurnCost() * 2
				} else if isDiagonal {
					// When it's diagonal, the shortest route would've been an L shaped, so to turn it in an S-shape is only one extra turn
					passThroughCost = node.Graph.TurnCost()
				} else if isSemiDiagonal {
					if alternateRouteBlocked {
						passThroughCost = node.Graph.TurnCost() * 2
					} else {
						// Just an L-shape
						passThroughCost = node.Graph.TurnCost()
					}
				} else {
					// When it's straight, takes 2 extra turns to get around the obstruction
					passThroughCost = node.Graph.TurnCost() * 2
				}
				distance += passThroughCost
			}

			badClusterArrangementPenalty := node.Graph.TurnCost()

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
				cost := node.Graph.TurnCost()
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

				cost := node.Graph.TurnCost()
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
					distance += 10 * node.Graph.TurnCost()
				}
			} else if e.Label != nil {
				if !directionPenalty || !direction.IsVertical() {
					count := s.labelCounts[i]
					// with 2 vertical edges a label can go on each side without obstructing each other
					if count > 2 {
						edgeDir = getEdgeDirection()
						if !edgeDir.IsHorizontal() {
							// penalize labeled edges with many duplicates if they are not horizontal
							distance += float64(count) * node.Graph.TurnCost()
						}
					}
				}
			}
		}

		totalDistance += distance
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
