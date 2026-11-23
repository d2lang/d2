package d2lib

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/lib/geo"
	d2log "github.com/d2lang/d2/lib/log"
)

func rankCompileOptions(engine string) *CompileOptions {
	return &CompileOptions{
		Layout: &engine,
		Ruler:  layoutReuseTestRuler,
		LayoutResolver: func(name string) (d2graph.LayoutGraph, error) {
			switch name {
			case "dagre":
				return d2dagrelayout.DefaultLayout, nil
			case "elk":
				return d2elklayout.DefaultLayout, nil
			case "tala":
				return d2talalayout.DefaultLayout, nil
			default:
				return nil, fmt.Errorf("unknown layout %s", name)
			}
		},
		RouterResolver: func(name string) (d2graph.RouteEdges, error) {
			if name == "tala" {
				return d2talalayout.RouteEdges, nil
			}
			return nil, nil
		},
	}
}

func TestPaymentRank(t *testing.T) {
	const payment = `
merchant: {
  checkout: Checkout {rank: payment; width: 162; height: 62}
}
channel: HTTPS {rank: payment; shape: circle; width: 52; height: 52}
gateway: Payment gateway {rank: payment; width: 176; height: 84}
shopper -> merchant.checkout
merchant.checkout -> channel: authorize
channel -> gateway
gateway -> receipt
`
	for _, engine := range []string{"dagre", "elk", "tala"} {
		for _, direction := range []string{"down", "up", "right", "left"} {
			t.Run(engine+"/"+direction, func(t *testing.T) {
				d, g, err := Compile(d2log.WithDefault(t.Context()), "direction: "+direction+"\n"+payment, rankCompileOptions(engine), nil)
				require.NoError(t, err)
				nodes := make(map[string]*d2graph.Object)
				for _, obj := range g.Objects {
					nodes[obj.AbsID()] = obj
				}
				axis := func(obj *d2graph.Object) float64 {
					switch direction {
					case "down":
						return obj.Center().Y
					case "up":
						return -obj.Center().Y
					case "right":
						return obj.Center().X
					default:
						return -obj.Center().X
					}
				}
				mid := axis(nodes["channel"])
				require.InDelta(t, mid, axis(nodes["merchant.checkout"]), 0.01)
				require.InDelta(t, mid, axis(nodes["gateway"]), 0.01)
				require.Less(t, axis(nodes["shopper"]), mid)
				require.Greater(t, axis(nodes["receipt"]), mid)
				child, parent := nodes["merchant.checkout"], nodes["merchant"]
				require.True(t, parent.Contains(child.TopLeft))
				require.True(t, parent.Contains(geo.NewPoint(child.TopLeft.X+child.Width, child.TopLeft.Y+child.Height)))
				for _, other := range []string{"channel", "gateway", "shopper", "receipt"} {
					require.False(t, parent.Overlaps(*nodes[other].Box), "merchant overlaps %s", other)
				}

				wanted := [][2]string{{"shopper", "merchant.checkout"}, {"merchant.checkout", "channel"}, {"channel", "gateway"}, {"gateway", "receipt"}}
				require.Len(t, d.Connections, len(wanted))
				for i, edge := range d.Connections {
					require.Equal(t, wanted[i], [2]string{edge.Src, edge.Dst})
					require.GreaterOrEqual(t, len(edge.Route), 2)
					checkAttachment(t, nodes[edge.Src], edge.Route[0])
					checkAttachment(t, nodes[edge.Dst], edge.Route[len(edge.Route)-1])
					if i != 1 && i != 2 {
						continue
					}
					for _, node := range g.Objects {
						if node.IsContainer() || node == nodes[edge.Src] || node == nodes[edge.Dst] {
							continue
						}
						for j := 1; j < len(edge.Route); j++ {
							segment := geo.Segment{Start: edge.Route[j-1], End: edge.Route[j]}
							require.False(t, node.Box.Intersects(segment, -1), "%s crosses %s", edge.ID, node.AbsID())
						}
					}
					if edge.Label != "" {
						box := geo.NewBox(edge.GetLabelTopLeft(), float64(edge.LabelWidth), float64(edge.LabelHeight))
						for _, node := range g.Objects {
							if !node.IsContainer() {
								require.False(t, box.Overlaps(*node.Box), "%s label overlaps %s", edge.ID, node.AbsID())
							}
						}
					}
				}
			})
		}
	}
}

func TestELKRankedIncomingEdgeClearsContainerTitle(t *testing.T) {
	const source = `direction: down
feed: Feed
batch: Region {
  label.near: top-center
  worker: Worker
}
feed -> batch.worker -> peer
batch.worker.rank: handoff
peer.rank: handoff`
	d, g, err := Compile(d2log.WithDefault(t.Context()), source, rankCompileOptions("elk"), nil)
	require.NoError(t, err)
	batch := g.Root.Children["batch"]
	title := geo.NewBox(batch.GetLabelTopLeft(), float64(batch.LabelDimensions.Width), float64(batch.LabelDimensions.Height))
	route := d.Connections[0].Route
	for i := 1; i < len(route); i++ {
		a, b := route[i-1], route[i]
		require.True(t, a.X == b.X || a.Y == b.Y)
		require.False(t, title.Intersects(geo.Segment{Start: a, End: b}, -1), "incoming edge crosses Region")
	}
	checkAttachment(t, g.Root.Children["feed"], route[0])
	checkAttachment(t, batch.Children["worker"], route[len(route)-1])
}

func TestELKRankPreservesStraightUpstreamEdge(t *testing.T) {
	const payment = `
merchant: {
  checkout: {width: 80; height: 60}
  commerce: {width: 180; height: 120}
  checkout -> commerce
}
merchant.commerce -> channel -> payments
merchant.commerce.rank: payment
channel.rank: payment
payments.rank: payment
`
	for _, direction := range []string{"down", "up", "right", "left"} {
		t.Run(direction, func(t *testing.T) {
			d, _, err := Compile(d2log.WithDefault(t.Context()), "direction: "+direction+"\n"+payment, rankCompileOptions("elk"), nil)
			require.NoError(t, err)
			require.Len(t, d.Connections, 3)
			upstream := d.Connections[0]
			require.Equal(t, "merchant.checkout", upstream.Src)
			require.Equal(t, "merchant.commerce", upstream.Dst)
			require.GreaterOrEqual(t, len(upstream.Route), 2)
			first := upstream.Route[0]
			for _, point := range upstream.Route[1:] {
				if direction == "down" || direction == "up" {
					require.InDelta(t, first.X, point.X, 0.001)
				} else {
					require.InDelta(t, first.Y, point.Y, 0.001)
				}
			}
		})
	}
}

func TestTALARankedRouteClearsFixedOutsideLabel(t *testing.T) {
	const source = `direction: down
shape: hierarchy
feed -> dot -> sink
dot: An external label {shape: circle; width: 32; height: 32; label.near: outside-right-center}
dot.rank: peers
sink.rank: peers`
	d, g, err := Compile(d2log.WithDefault(t.Context()), source, rankCompileOptions("tala"), nil)
	require.NoError(t, err)
	dot := g.Root.Children["dot"]
	box := geo.NewBox(dot.GetLabelTopLeft(), float64(dot.LabelDimensions.Width), float64(dot.LabelDimensions.Height))
	require.Len(t, d.Connections, 2)
	edge := d.Connections[1]
	require.Equal(t, "dot", edge.Src)
	require.Equal(t, "sink", edge.Dst)
	require.GreaterOrEqual(t, len(edge.Route), 2)
	checkAttachment(t, dot, edge.Route[0])
	for i, p := range edge.Route[1:] {
		require.False(t, box.Intersects(geo.Segment{Start: edge.Route[i], End: p}, -1), "route passes through the fixed caption")
	}
}

func checkAttachment(t *testing.T, node *d2graph.Object, point *geo.Point) {
	t.Helper()
	dx := math.Abs(point.X - node.Center().X)
	dy := math.Abs(point.Y - node.Center().Y)
	var distance float64
	if node.Shape.Value == "circle" {
		distance = math.Abs(math.Hypot(dx, dy) - node.Width/2)
	} else {
		// Signed distance to the nearest point on an axis-aligned rectangle.
		x, y := dx-node.Width/2, dy-node.Height/2
		distance = math.Abs(math.Hypot(max(x, 0), max(y, 0)) + min(max(x, y), 0))
	}
	require.LessOrEqual(t, distance, 2.0, "%s at %v ends %.2fpx from the shape boundary", node.AbsID(), point, distance)
}

func TestPipelineRankLayoutReuse(t *testing.T) {
	const pipeline = `build -> test -> deploy
scenarios.parallel: {
  build.rank: checks
  test.rank: checks
}
`
	for _, engine := range []string{"dagre", "elk"} {
		t.Run(engine, func(t *testing.T) {
			opts := rankCompileOptions(engine)
			opts.LayoutReuse = true
			_, g, err := Compile(d2log.WithDefault(t.Context()), pipeline, opts, nil)
			require.NoError(t, err)
			root := g.Root.Children
			require.Less(t, root["build"].Center().Y, root["test"].Center().Y)
			scenario := g.Scenarios[0].Root.Children
			require.InDelta(t, scenario["build"].Center().Y, scenario["test"].Center().Y, 0.01)
			require.Less(t, scenario["test"].Center().Y, scenario["deploy"].Center().Y)
		})
	}
}

func TestRankLayoutBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, source, message string }{
		{"member_container", "box: {child; rank: peers}\nx.rank: peers", "is a container"},
		{"grid_members", "box: {grid-columns: 2; x.rank: peers; y.rank: peers}", "controls the position"},
		{"separate_grid_cells", "grid-columns: 2\na.x.rank: peers\nb.y.rank: peers", "independent layouts"},
		{"sequence_actors", "shape: sequence_diagram\na.rank: peers\nb.rank: peers\na -> b", "controls the position"},
		{"constant_near", "a: {near: top-left; rank: peers}\nb.rank: peers", "fixed near position"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := Compile(d2log.WithDefault(t.Context()), tc.source, rankCompileOptions("dagre"), nil)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestRankInNestedLayouts(t *testing.T) {
	const source = `
direction: right
panel: {
  rank: overview
  grid-columns: 1
  cell: {
    a.rank: stages
    b.rank: stages
    a -> b -> c
  }
}
dialog: {rank: overview; shape: sequence_diagram; user -> server}
panel -> dialog
`
	for _, engine := range []string{"dagre", "elk", "tala"} {
		t.Run(engine, func(t *testing.T) {
			_, g, err := Compile(d2log.WithDefault(t.Context()), source, rankCompileOptions(engine), nil)
			require.NoError(t, err)
			panel, dialog := g.Root.Children["panel"], g.Root.Children["dialog"]
			require.InDelta(t, panel.Center().X, dialog.Center().X, 0.01)
			require.False(t, panel.Overlaps(*dialog.Box))
			inside := panel.Children["cell"].Children
			require.InDelta(t, inside["a"].Center().Y, inside["b"].Center().Y, 0.01)
			require.Less(t, inside["b"].Center().Y, inside["c"].Center().Y)
		})
	}
}
