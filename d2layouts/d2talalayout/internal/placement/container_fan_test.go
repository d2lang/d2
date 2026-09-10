package placement

import (
	"context"
	"fmt"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/stretchr/testify/require"
)

func TestContainerOrientationExplicitFan(t *testing.T) {
	for _, direction := range []geo.Orientation{geo.Right, geo.Left, geo.Bottom, geo.Top} {
		for _, fanIn := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/in=%v", direction, fanIn), func(t *testing.T) {
				positions := []geo.Point{{X: 0, Y: 0}, {X: 400, Y: 0}, {X: 800, Y: 0}}
				if direction.IsHorizontal() {
					positions = []geo.Point{{X: 0, Y: 0}, {X: 0, Y: 400}, {X: 0, Y: 800}}
				}
				f := newContainerOrientationFixture(t, direction.GetOpposite(), positions, false)
				f.graph.Directions[f.root] = direction
				f.graph.ReplaceEdgesUnchecked(nil)
				for _, arm := range []*layoutgraph.Node{f.nodes[0], f.nodes[2]} {
					edge := f.graph.Connect(f.nodes[1], arm)
					if fanIn {
						edge.SourceArrowhead = "triangle"
					} else {
						edge.TargetArrowhead = "triangle"
					}
				}
				originalEdges := append([]*layoutgraph.Edge(nil), f.graph.Edges...)
				require.NoError(t, orientSourceInterior(context.Background(), f.graph, f.root, nil))
				require.Equal(t, originalEdges, f.graph.Edges)
				for _, edge := range f.graph.Edges {
					from, to, ok := edge.DirectedEndpoints()
					require.True(t, ok)
					require.True(t, fanForwardSeparated(from, to, direction))
				}
				require.Equal(t, 100.0, f.graph.CellSize)
				require.NoError(t, layoutgraph.Validate(context.Background(), "ExplicitFan", f.graph))
			})
		}
	}
}

func TestContainerOrientationFanPreservesNestedGeometryAndExternalNeighbors(t *testing.T) {
	f, child := newExplicitNestedContainerOrientationFixture(t)
	hub := f.nodes[1]
	second := f.graph.Containers[hub][1]
	originalDelta := geo.Point{X: second.TopLeft.X - child.TopLeft.X, Y: second.TopLeft.Y - child.TopLeft.Y}
	f.graph.ReplaceEdgesUnchecked(nil)
	f.graph.Connect(child, second).TargetArrowhead = "triangle"
	f.graph.Connect(f.nodes[0], child).TargetArrowhead = "triangle"
	f.graph.Connect(f.nodes[2], second).TargetArrowhead = "triangle"
	// Each arm has a distinct external predecessor; their difference must not
	// be erased by treating the arms as equivalent cluster members.
	internalNodes := append([]*layoutgraph.Node(nil), f.graph.Nodes...)
	for i, arm := range []*layoutgraph.Node{f.nodes[0], f.nodes[2]} {
		outside := layoutgraph.NewNode(layoutgraph.EntityID(200+i), 100, 60)
		outside.TopLeft = geo.NewPoint(float64(50000+i*400), 50000)
		f.graph.AddNewNodeToContainer(nil, outside)
		f.graph.Connect(outside, arm).TargetArrowhead = "triangle"
	}
	f.graph.Nodes = internalNodes
	type endpoints struct{ from, to *layoutgraph.Node }
	originalEdges := map[*layoutgraph.Edge]endpoints{}
	for _, edge := range f.graph.Edges {
		originalEdges[edge] = endpoints{edge.From, edge.To}
	}
	require.NoError(t, orientSourceInterior(context.Background(), f.graph, f.root, nil))
	require.True(t, fanForwardSeparated(f.nodes[0], hub, geo.Right))
	require.True(t, fanForwardSeparated(f.nodes[2], hub, geo.Right))
	require.Equal(t, originalDelta, geo.Point{X: second.TopLeft.X - child.TopLeft.X, Y: second.TopLeft.Y - child.TopLeft.Y})
	for edge, ends := range originalEdges {
		require.Same(t, ends.from, edge.From)
		require.Same(t, ends.to, edge.To)
	}
	require.Nil(t, f.nodes[0].Cluster)
	require.Nil(t, f.nodes[2].Cluster)
	require.Equal(t, 260.0, hub.Width)
	require.Equal(t, 350.0, hub.Height)
	require.NoError(t, layoutgraph.Validate(context.Background(), "NestedExplicitFan", f.graph))
}

func TestExplicitFanRequiresDirectedLinksOnlyWithinItsScope(t *testing.T) {
	for _, bidirectional := range []bool{false, true} {
		for _, external := range []bool{false, true} {
			t.Run(fmt.Sprintf("bidirectional=%v/external=%v", bidirectional, external), func(t *testing.T) {
				f := newContainerOrientationFixture(t, geo.Bottom, []geo.Point{{X: 0, Y: 0}, {X: 0, Y: 400}, {X: 0, Y: 800}}, false)
				f.graph.Directions[f.root] = geo.Right
				f.graph.ReplaceEdgesUnchecked(nil)
				for _, arm := range []*layoutgraph.Node{f.nodes[0], f.nodes[2]} {
					f.graph.Connect(f.nodes[1], arm).TargetArrowhead = layoutgraph.TriangleArrowhead
				}
				other := f.nodes[2]
				if external {
					other = layoutgraph.NewNode(200, 100, 60)
					other.TopLeft = geo.NewPoint(50000, 50000)
					f.graph.AddNewNodeToContainer(nil, other)
					// Match a combined interior: outside endpoints remain in the
					// shared topology, but are not local placement blocks.
					f.graph.Nodes = f.nodes
				}
				link := f.graph.Connect(f.nodes[0], other)
				if bidirectional {
					link.SourceArrowhead = layoutgraph.TriangleArrowhead
					link.TargetArrowhead = layoutgraph.TriangleArrowhead
				}
				states := captureContainerOrientationNodes(append(append([]*layoutgraph.Node(nil), f.nodes...), f.root, other))
				local := make(map[*layoutgraph.Node]bool)
				for _, node := range f.nodes {
					local[node] = true
				}
				guard, err := limits.NewWorkGuard(t.Context(), "ExplicitFanTest", limits.MaxEngineWorkUnits)
				require.NoError(t, err)

				changed, err := orientExplicitFan(t.Context(), f.graph, f.graph, f.root, local, geo.Right, guard)

				require.NoError(t, err)
				require.Equal(t, external, changed, "only connections outside the scope may be ignored")
				if !external {
					assertContainerOrientationNodesRestored(t, states)
				} else {
					for _, arm := range []*layoutgraph.Node{f.nodes[0], f.nodes[2]} {
						require.True(t, fanForwardSeparated(f.nodes[1], arm, geo.Right))
					}
					assertContainerOrientationNodesRestored(t, map[*layoutgraph.Node]containerOrientationNodeState{other: states[other]})
				}
				require.NoError(t, layoutgraph.Validate(t.Context(), "ExplicitFanLinks", f.graph))
			})
		}
	}
}
