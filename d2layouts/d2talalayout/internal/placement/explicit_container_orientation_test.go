package placement

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

func TestContainerOrientationFollowsExplicitLocalDirection(t *testing.T) {
	for _, direction := range []geo.Orientation{geo.Right, geo.Left, geo.Bottom, geo.Top} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/reverse=%v", direction, reverse), func(t *testing.T) {
				positions := []geo.Point{{X: 0, Y: 0}, {X: 400, Y: 0}, {X: 800, Y: 0}}
				if direction.IsHorizontal() {
					positions = []geo.Point{{X: 0, Y: 0}, {X: 0, Y: 400}, {X: 0, Y: 800}}
				}
				f := newContainerOrientationFixture(t, direction.GetOpposite(), positions, reverse)
				f.graph.Directions[f.root] = direction
				// Explicit local flow does not require an outward-only source.
				f.root.Edges[0].SourceArrowhead = "triangle"
				f.root.Edges[0].TargetArrowhead = ""
				outside := make([]*layoutgraph.Node, 0, len(f.root.Edges))
				for _, edge := range f.root.Edges {
					outside = append(outside, edge.To)
				}
				outsideState := captureContainerOrientationNodes(outside)

				require.NoError(t, orientSourceInterior(context.Background(), f.graph, f.root, nil))

				for _, edge := range f.graph.Edges {
					from, to, directed := edge.DirectedEndpoints()
					require.True(t, directed)
					delta, cross := to.Center().Y-from.Center().Y, to.Center().X-from.Center().X
					if direction.IsHorizontal() {
						delta, cross = cross, delta
					}
					if direction == geo.Left || direction == geo.Top {
						delta = -delta
					}
					require.Greater(t, delta, 0.0)
					require.InDelta(t, 0.0, cross, 0.01)
				}
				for _, node := range f.nodes {
					require.Equal(t, 100.0, node.Width)
					require.Equal(t, 60.0, node.Height)
				}
				assertContainerOrientationNodesRestored(t, outsideState)
				require.NoError(t, layoutgraph.Validate(context.Background(), "ExplicitContainerDirection", f.graph))
			})
		}
	}
}

func newExplicitNestedContainerOrientationFixture(t *testing.T) (containerOrientationFixture, *layoutgraph.Node) {
	t.Helper()
	f := newContainerOrientationFixture(t, geo.Bottom, []geo.Point{{X: 0, Y: 0}, {X: 0, Y: 500}, {X: 0, Y: 1000}}, false)
	f.graph.Directions[f.root] = geo.Right
	container := f.nodes[1]
	container.SetContainer(true)
	container.Width, container.Height = 260, 350
	child := layoutgraph.NewNode(50, 100, 60)
	child.TopLeft = geo.NewPoint(80, 570)
	f.graph.AddNewNodeToContainer(container, child)
	second := layoutgraph.NewNode(51, 100, 60)
	second.TopLeft = geo.NewPoint(80, 760)
	f.graph.AddNewNodeToContainer(container, second)
	f.graph.Connect(child, second).TargetArrowhead = "triangle"
	f.graph.Directions[container] = geo.Bottom
	// CombineSubgraphs includes every descendant alongside its parent block.
	// Keep both children and their internal edge in this view.
	f.graph.SyncNestedGeometry()
	require.NoError(t, layoutgraph.Validate(context.Background(), "ExplicitNestedOrientationFixture", f.graph))
	return f, child
}

func TestContainerOrientationKeepsNestedContainerUpright(t *testing.T) {
	f, child := newExplicitNestedContainerOrientationFixture(t)
	container := f.nodes[1]
	dx, dy := child.TopLeft.X-container.TopLeft.X, child.TopLeft.Y-container.TopLeft.Y
	second := f.graph.Containers[container][1]
	relative := geo.Point{X: second.TopLeft.X - child.TopLeft.X, Y: second.TopLeft.Y - child.TopLeft.Y}
	require.Contains(t, f.graph.Nodes, child)
	require.Contains(t, f.graph.Nodes, second)

	require.NoError(t, orientSourceInterior(context.Background(), f.graph, f.root, nil))

	require.Greater(t, f.nodes[1].Center().X, f.nodes[0].Center().X)
	require.Greater(t, f.nodes[2].Center().X, f.nodes[1].Center().X)
	require.Equal(t, 260.0, container.Width)
	require.Equal(t, 350.0, container.Height)
	require.Equal(t, 100.0, child.Width)
	require.Equal(t, 60.0, child.Height)
	require.InDelta(t, dx, child.TopLeft.X-container.TopLeft.X, 0.01)
	require.InDelta(t, dy, child.TopLeft.Y-container.TopLeft.Y, 0.01)
	require.Same(t, container, child.Container)
	require.Equal(t, relative, geo.Point{X: second.TopLeft.X - child.TopLeft.X, Y: second.TopLeft.Y - child.TopLeft.Y})
	require.Equal(t, geo.Bottom, f.graph.Direction(container))
	require.Same(t, f.root, container.Container)
	require.NoError(t, layoutgraph.Validate(context.Background(), "ExplicitNestedContainerDirection", f.graph))
}

func TestContainerOrientationExplicitDirectionPreservesFixedDescendant(t *testing.T) {
	f, child := newExplicitNestedContainerOrientationFixture(t)
	child.FixedTopLeft = child.TopLeft.Copy()
	states := captureContainerOrientationNodes(append(append([]*layoutgraph.Node(nil), f.graph.Nodes...), f.root))

	require.NoError(t, orientSourceInterior(context.Background(), f.graph, f.root, nil))

	assertContainerOrientationNodesRestored(t, states)
}

type cancelAfterNestedOrientation struct {
	context.Context
	child    *layoutgraph.Node
	original geo.Point
	observed bool
}

func (ctx *cancelAfterNestedOrientation) Err() error {
	if *ctx.child.TopLeft != ctx.original {
		ctx.observed = true
		return context.Canceled
	}
	return ctx.Context.Err()
}

func TestContainerOrientationExplicitCancellationRestoresNestedChild(t *testing.T) {
	f, child := newExplicitNestedContainerOrientationFixture(t)
	states := captureContainerOrientationNodes(append(append([]*layoutgraph.Node(nil), f.graph.Nodes...), f.root))
	ctx := &cancelAfterNestedOrientation{Context: context.Background(), child: child, original: *child.TopLeft}

	require.ErrorIs(t, orientSourceInterior(ctx, f.graph, f.root, nil), context.Canceled)

	require.True(t, ctx.observed)
	assertContainerOrientationNodesRestored(t, states)
}
