package d2elklayout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestConfigurableOptsSerializeELK012Keys(t *testing.T) {
	opts := ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     123,
		Padding:         "[top=11,left=22,bottom=33,right=44]",
		EdgeNodeSpacing: 67,
		EdgeEdgeSpacing: go2.Pointer(78),
		SelfLoopSpacing: 89,
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"elk.algorithm":                 "layered",
		"spacing.nodeNodeBetweenLayers": float64(123),
		"elk.padding":                   "[top=11,left=22,bottom=33,right=44]",
		"spacing.edgeNodeBetweenLayers": float64(67),
		"spacing.edgeEdgeBetweenLayers": float64(78),
		"elk.spacing.nodeSelfLoop":      float64(89),
	}
	if len(got) != len(want) {
		t.Fatalf("serialized options = %s", raw)
	}
	for key, wantValue := range want {
		if gotValue := got[key]; gotValue != wantValue {
			t.Errorf("%s = %#v, want %#v (serialized %s)", key, gotValue, wantValue, raw)
		}
	}
}

func TestUnsetEdgeEdgeSpacingKeepsPreviousDefault(t *testing.T) {
	// A Go API caller predating the EdgeEdgeSpacing option leaves the field nil, which
	// must keep the spacing D2 sent before the option existed rather than ELK's own.
	opts := ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     70,
		Padding:         "[top=50,left=50,bottom=50,right=50]",
		EdgeNodeSpacing: 40,
		SelfLoopSpacing: 50,
	}
	for name, got := range map[string]*elkOpts{
		"root":      newRootLayoutOptions(&opts),
		"container": newContainerLayoutOptions(&opts),
	} {
		if got.EdgeEdgeSpacing == nil || *got.EdgeEdgeSpacing != default_edge_edge_spacing {
			t.Errorf("%s edge-edge spacing = %v, want %d", name, got.EdgeEdgeSpacing, default_edge_edge_spacing)
		}
	}
}

func TestExplicitZeroEdgeEdgeSpacingIsHonored(t *testing.T) {
	// Unset is nil, so an explicit zero is a real request for no extra spacing
	// and must reach ELK rather than being read back as the default.
	opts := DefaultOpts
	opts.EdgeEdgeSpacing = go2.Pointer(0)
	for name, got := range map[string]*elkOpts{
		"root":      newRootLayoutOptions(&opts),
		"container": newContainerLayoutOptions(&opts),
	} {
		if got.EdgeEdgeSpacing == nil || *got.EdgeEdgeSpacing != 0 {
			t.Fatalf("%s edge-edge spacing = %v, want 0", name, got.EdgeEdgeSpacing)
		}
		raw, err := json.Marshal(got.ConfigurableOpts)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if fields["spacing.edgeEdgeBetweenLayers"] != float64(0) {
			t.Errorf("%s serialized %s, want an explicit zero for spacing.edgeEdgeBetweenLayers", name, raw)
		}
	}
}

func TestUnsetEdgeEdgeSpacingIsOmitted(t *testing.T) {
	// ConfigurableOpts is embedded in elkOpts, which is also built bare for
	// leaf nodes, ports and edge labels. ELK only applies edgeEdgeBetweenLayers
	// to parents, so those elements must not assert a spacing of their own.
	for name, opts := range map[string]*elkOpts{
		"leaf node":  {SelfLoopDistribution: "EQUALLY"},
		"port":       {PortSide: South},
		"edge label": {InlineEdgeLabels: true},
	} {
		raw, err := json.Marshal(opts)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if _, ok := got["spacing.edgeEdgeBetweenLayers"]; ok {
			t.Errorf("%s options carry spacing.edgeEdgeBetweenLayers, want it omitted (serialized %s)", name, raw)
		}
	}
}

func TestNativeELKMigrationProfile(t *testing.T) {
	root := newRootLayoutOptions(&DefaultOpts)
	if root.ConsiderModelOrder != "NODES_AND_EDGES" || root.CycleBreakingStrategy != "GREEDY_MODEL_ORDER" || root.ForceNodeModelOrder {
		t.Fatalf("root migration profile = consider %q, cycle %q, force model order %t; want NODES_AND_EDGES/GREEDY_MODEL_ORDER/false",
			root.ConsiderModelOrder, root.CycleBreakingStrategy, root.ForceNodeModelOrder)
	}

	container := newContainerLayoutOptions(&DefaultOpts)
	if container.ConsiderModelOrder != "NONE" || container.CycleBreakingStrategy != "GREEDY" || container.ForceNodeModelOrder {
		t.Fatalf("container migration profile = consider %q, cycle %q, force model order %t; want NONE/GREEDY/false",
			container.ConsiderModelOrder, container.CycleBreakingStrategy, container.ForceNodeModelOrder)
	}
}

func TestConfigurableOptsAffectLayout(t *testing.T) {
	t.Run("node spacing", func(t *testing.T) {
		compact := layoutOptionFixture(t, "a -> b", func(opts *ConfigurableOpts) {
			opts.NodeSpacing = 20
		})
		spacious := layoutOptionFixture(t, "a -> b", func(opts *ConfigurableOpts) {
			opts.NodeSpacing = 160
		})
		compactGap := verticalGap(objectByID(t, compact, "a"), objectByID(t, compact, "b"))
		spaciousGap := verticalGap(objectByID(t, spacious, "a"), objectByID(t, spacious, "b"))
		if spaciousGap-compactGap < 100 {
			t.Fatalf("node gap changed from %g to %g, want a material increase", compactGap, spaciousGap)
		}
	})

	t.Run("padding", func(t *testing.T) {
		compact := layoutOptionFixture(t, "container: {\n  a\n}", func(opts *ConfigurableOpts) {
			opts.Padding = "[top=10,left=10,bottom=10,right=10]"
		})
		spacious := layoutOptionFixture(t, "container: {\n  a\n}", func(opts *ConfigurableOpts) {
			opts.Padding = "[top=100,left=100,bottom=100,right=100]"
		})
		compactContainer := objectByID(t, compact, "container")
		spaciousContainer := objectByID(t, spacious, "container")
		if spaciousContainer.Width-compactContainer.Width < 150 || spaciousContainer.Height-compactContainer.Height < 150 {
			t.Fatalf("container size changed from %gx%g to %gx%g, want padding to materially increase both dimensions",
				compactContainer.Width, compactContainer.Height, spaciousContainer.Width, spaciousContainer.Height)
		}
	})

	t.Run("edge-node spacing", func(t *testing.T) {
		compact := layoutOptionFixture(t, "a -> c\nb -> c", func(opts *ConfigurableOpts) {
			opts.EdgeNodeSpacing = 5
		})
		spacious := layoutOptionFixture(t, "a -> c\nb -> c", func(opts *ConfigurableOpts) {
			opts.EdgeNodeSpacing = 200
		})
		compactGap := verticalGap(objectByID(t, compact, "a"), objectByID(t, compact, "c"))
		spaciousGap := verticalGap(objectByID(t, spacious, "a"), objectByID(t, spacious, "c"))
		if spaciousGap-compactGap < 300 {
			t.Fatalf("edge-node layer gap changed from %g to %g, want a material increase", compactGap, spaciousGap)
		}
	})

	t.Run("edge-edge spacing between layers", func(t *testing.T) {
		const src = "a -> x\na -> y\nb -> x\nb -> y\nc -> x\nc -> y"
		compact := layoutOptionFixture(t, src, func(opts *ConfigurableOpts) {
			opts.EdgeEdgeSpacing = go2.Pointer(5)
		})
		spacious := layoutOptionFixture(t, src, func(opts *ConfigurableOpts) {
			opts.EdgeEdgeSpacing = go2.Pointer(200)
		})
		compactGap := verticalGap(objectByID(t, compact, "a"), objectByID(t, compact, "x"))
		spaciousGap := verticalGap(objectByID(t, spacious, "a"), objectByID(t, spacious, "x"))
		if spaciousGap-compactGap < 300 {
			t.Fatalf("edge-edge layer gap changed from %g to %g, want a material increase", compactGap, spaciousGap)
		}
	})

	t.Run("self-loop spacing", func(t *testing.T) {
		compact := layoutOptionFixture(t, "a -> a", func(opts *ConfigurableOpts) {
			opts.SelfLoopSpacing = 20
		})
		spacious := layoutOptionFixture(t, "a -> a", func(opts *ConfigurableOpts) {
			opts.SelfLoopSpacing = 140
		})
		compactExtent := routeExtent(t, compact.Edges[0])
		spaciousExtent := routeExtent(t, spacious.Edges[0])
		if spaciousExtent-compactExtent < 80 {
			t.Fatalf("self-loop route extent changed from %g to %g, want a material increase", compactExtent, spaciousExtent)
		}
	})
}

func TestMergeEdgesKeepsSharedAttachmentsDuringBendCleanup(t *testing.T) {
	const fan = "direction: right\nsource -> a\nsource -> b\nsource -> c\nsource -> d\na -> sink\nb -> sink\nc -> sink\nd -> sink"
	for _, merged := range []bool{false, true} {
		g := layoutOptionFixture(t, fan, func(o *ConfigurableOpts) { o.MergeEdges = merged })
		starts, ends := map[geo.Point]bool{}, map[geo.Point]bool{}
		for _, e := range g.Edges {
			if e.Src.ID == "source" {
				starts[*e.Route[0]] = true
			}
			if e.Dst.ID == "sink" {
				ends[*e.Route[len(e.Route)-1]] = true
			}
		}
		want := 4
		if merged {
			want = 1
		}
		if len(starts) != want || len(ends) != want {
			t.Errorf("merge=%t: %d departures and %d arrivals, want %d each", merged, len(starts), len(ends), want)
		}
	}
	table := layoutOptionFixture(t, "table: {shape: sql_table; id: int; name: text}\ntable.id -> a\ntable.name -> a", func(o *ConfigurableOpts) { o.MergeEdges = true })
	if *table.Edges[0].Route[0] == *table.Edges[1].Route[0] {
		t.Fatal("distinct SQL columns share an attachment")
	}
}

func TestLayeredOptionScopesAndZero(t *testing.T) {
	opts, err := graphOptions(&d2graph.Graph{Data: map[string]any{
		"elk-nodeNode": "0", "elk-edgeEdge": "2.5", "elk-edgeNode": 0,
		"elk-forceNodeModelOrder": true, "elk-wrappingStrategy": "single_edge", "elk-edgeRouting": "polyline",
		"elk-feedbackEdges": "TrUe", "elk-mergeEdges": true,
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := newRootLayoutOptions(opts)
	nested := newContainerLayoutOptions(opts)
	if !root.ForceNodeModelOrder || nested.ForceNodeModelOrder {
		t.Fatal("declaration order should be forced at the root only")
	}
	for _, o := range []*elkOpts{root, nested} {
		if o.NodeNode == nil || *o.NodeNode != 0 || o.EdgeNode == nil || *o.EdgeNode != 0 ||
			o.EdgeEdge == nil || *o.EdgeEdge != 2.5 || o.WrappingStrategy != "SINGLE_EDGE" ||
			o.EdgeRouting != "POLYLINE" || !o.FeedbackEdges || !o.MergeEdges {
			t.Fatalf("incorrect parent options: %+v", o.LayeredOptions)
		}
	}
	for key, val := range map[string]string{
		"elk-nodeNode": "-1", "elk-edgeEdge": "NaN", "elk-edgeNode": "+Inf", "elk-edgeRouting": "SPLINES",
		"elk-wrappingStrategy": "sideways", "elk-mergeEdges": "sometimes",
	} {
		if _, err := graphOptions(&d2graph.Graph{Data: map[string]any{key: val}}, nil); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("expected an error naming %s for %q, got %v", key, val, err)
		}
	}
}

func TestAlgorithmOptionsAtRoot(t *testing.T) {
	defaults := DefaultOpts
	defaults.Algorithm = "force"
	defaults.ForceModel = "eAdEs"
	defaults.StressDesiredEdgeLength = go2.Pointer(60.)
	data := map[string]any{
		"elk-algorithm": "OrG.ECLIPSE.Elk.StReSs", "elk-stressDesiredEdgeLength": 200,
		"elk-forceRepulsion": 0, "elk-mrtreeSearchOrder": "BfS", "elk-mrtreeWeighting": "FaN",
		"elk-radialRadius": 0, "elk-radialWedgeCriteria": "LeAf_NuMbEr", "elk-radialCompactor": "WeDgE_CoMpAcTiOn",
	}
	opts, err := graphOptions(&d2graph.Graph{Data: data}, &defaults)
	if err != nil {
		t.Fatal(err)
	}
	root := newRootLayoutOptions(opts)
	if root.Algorithm != "org.eclipse.elk.stress" || defaults.Algorithm != "force" || *defaults.StressDesiredEdgeLength != 60 {
		t.Fatalf("unexpected algorithm or mutation of caller options: %q, %+v", root.Algorithm, defaults)
	}
	raw, err := json.Marshal(root.AlgorithmOptions)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"elk.force.model": "EADES", "elk.force.repulsion": 0., "elk.stress.desiredEdgeLength": 200.,
		"elk.mrtree.searchOrder": "BFS", "elk.mrtree.weighting": "FAN",
		"elk.radial.radius": 0., "elk.radial.wedgeCriteria": "LEAF_NUMBER", "elk.radial.compactor": "WEDGE_COMPACTION",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("serialized algorithm options = %s, want %v", raw, want)
	}
	for _, settings := range []*elkOpts{newContainerLayoutOptions(opts), newRootLayoutOptions(&DefaultOpts)} {
		if settings.AlgorithmOptions != (AlgorithmOptions{}) {
			t.Errorf("unexpected explicit algorithm settings: %+v", settings.AlgorithmOptions)
		}
	}
}

func TestMrTreeEdgeClearance(t *testing.T) {
	for _, name := range []string{"mrtree", "elk.mrtree", "org.eclipse.elk.mrtree"} {
		opts := DefaultOpts
		opts.Algorithm = name
		if v := newRootLayoutOptions(&opts).EdgeNode; v != nil {
			t.Errorf("%s root specifies %g instead of using the provider default", name, *v)
		}
		if v := newContainerLayoutOptions(&opts).EdgeNode; v == nil || *v != edge_node_spacing {
			t.Errorf("nested layered graph lost its default edge clearance")
		}
		for _, spacing := range []float64{0, 12} {
			opts.EdgeNode = &spacing
			if v := newRootLayoutOptions(&opts).EdgeNode; v == nil || *v != spacing {
				t.Errorf("%s lost explicit edge clearance %g", name, spacing)
			}
		}
	}
}

func TestMrTreeRoutesAvoidShapes(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"right", "left", "down", "up"} {
		for _, order := range []string{"dfs", "bfs"} {
			t.Run(direction+"/"+order, func(t *testing.T) {
				const graph = "Root -> API -> Cache -> Store\nRoot -> Store\nRoot -> Web\n"
				source := fmt.Sprintf("direction: %s\n%s", direction, graph)
				g, _, err := d2compiler.Compile("", strings.NewReader(source), nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
					t.Fatal(err)
				}
				opts := DefaultOpts
				opts.Algorithm = "mrtree"
				opts.MrTreeSearchOrder = order
				if err := Layout(t.Context(), g, &opts); err != nil {
					t.Fatal(err)
				}
				for _, edge := range g.Edges {
					last := len(edge.Route) - 1
					if last < 1 {
						t.Fatalf("%s has no route", edge.AbsID())
					}
					if edge.Src.Box.Contains(edge.Route[1]) || edge.Dst.Box.Contains(edge.Route[last-1]) {
						t.Errorf("%s approaches an attachment from inside or along its border", edge.AbsID())
					}
					for i := 1; i <= last; i++ {
						segment := geo.NewSegment(edge.Route[i-1], edge.Route[i])
						if segment.Start.Equals(segment.End) {
							t.Errorf("%s repeats a bend at %v", edge.AbsID(), segment.Start)
						}
						for _, obj := range g.Objects {
							// Allow an edge to meet the border, including subpixel rounding
							// when tracing from ELK's coordinates onto a D2 shape.
							if obj.Box.Intersects(*segment, -1) {
								t.Errorf("%s segment %v to %v crosses %s at %v (%gx%g)", edge.AbsID(), segment.Start, segment.End, obj.ID, obj.TopLeft, obj.Width, obj.Height)
							}
						}
					}
				}
			})
		}
	}
}

func TestAlgorithmOptionValidation(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"elk-stressDesiredEdgeLength", "0"}, {"elk-stressDesiredEdgeLength", "-1"},
		{"elk-forceRepulsion", "NaN"}, {"elk-radialRadius", "+Inf"}, {"elk-radialRadius", "-1"},
		{"elk-forceModel", "spring"}, {"elk-mrtreeSearchOrder", "shortest"}, {"elk-mrtreeWeighting", "constraint"},
		{"elk-radialWedgeCriteria", "area"}, {"elk-radialCompactor", "tight"},
	} {
		opts := DefaultOpts
		if err := opts.SetOption(tc.key, tc.value); err == nil || !strings.Contains(err.Error(), tc.key) {
			t.Errorf("%s=%s: expected a diagnostic naming the option, got %v", tc.key, tc.value, err)
		}
	}
	opts := DefaultOpts
	opts.StressDesiredEdgeLength = go2.Pointer(0.)
	if _, err := graphOptions(&d2graph.Graph{}, &opts); err == nil || !strings.Contains(err.Error(), "elk-stressDesiredEdgeLength") {
		t.Errorf("invalid typed stress length: %v", err)
	}
	for input, want := range map[string]string{
		"RaDiAl": "radial", "SPoReOvErLaP": "sporeOverlap", "ORG.ECLIPSE.ELK.SPORECOMPACTION": "org.eclipse.elk.sporeCompaction",
	} {
		if got := algorithmName(input); got != want {
			t.Errorf("algorithm %q = %q, want %q", input, got, want)
		}
	}
}

func TestELKPaddingWithOmittedSides(t *testing.T) {
	for _, input := range []string{"[top=12.5,left=9]", "(top=12.5; left=9)", "top=12.5,left=9", "[ToP=12.5,LeFt=9]"} {
		opts := DefaultOpts
		if err := opts.SetOption("elk-padding", input); err != nil || opts.Padding != "[top=12.5,left=9,bottom=0,right=0]" {
			t.Errorf("partial padding %q: %s, %v", input, opts.Padding, err)
		}
	}
}

func TestShapeLayerConstraint(t *testing.T) {
	const src = "direction: right\nstart -> read -> build -> ship\nstart -> audit\n"
	layout := func(extra string) *d2graph.Graph {
		return layoutOptionFixture(t, src+extra, func(*ConfigurableOpts) {})
	}
	before := layout("")
	if objectByID(t, before, "audit").TopLeft.X >= objectByID(t, before, "ship").TopLeft.X {
		t.Fatal("audit should precede ship by default")
	}
	for _, last := range []string{"last", "LAST", "lAsT"} {
		after := layout("audit.elk-layer-constraint: " + last)
		if objectByID(t, after, "audit").TopLeft.X != objectByID(t, after, "ship").TopLeft.X {
			t.Errorf("%q should move audit into the final layer", last)
		}
	}
	for _, src := range []string{"elk-layer-constraint: first", "a -> b: {elk-layer-constraint: last}", "a.elk-layer-constraint: middle"} {
		if _, _, err := d2compiler.Compile("", strings.NewReader(src), nil); err == nil {
			t.Errorf("expected invalid shape constraint in %q", src)
		}
	}
}

func layoutOptionFixture(t *testing.T, input string, configure func(*ConfigurableOpts)) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("compile fixture: %v", err)
	}
	for i, obj := range g.Objects {
		obj.Box = geo.NewBox(geo.NewPoint(float64(i*100), float64(i*50)), 60, 40)
	}
	opts := DefaultOpts
	configure(&opts)
	if err := Layout(log.WithTB(context.Background(), t), g, &opts); err != nil {
		t.Fatalf("layout fixture: %v", err)
	}
	return g
}

func objectByID(t *testing.T, g *d2graph.Graph, id string) *d2graph.Object {
	t.Helper()
	for _, obj := range g.Objects {
		if obj.AbsID() == id {
			return obj
		}
	}
	t.Fatalf("object %q not found", id)
	return nil
}

func verticalGap(a, b *d2graph.Object) float64 {
	if a.TopLeft.Y > b.TopLeft.Y {
		a, b = b, a
	}
	return b.TopLeft.Y - a.TopLeft.Y - a.Height
}

func routeExtent(t *testing.T, edge *d2graph.Edge) float64 {
	t.Helper()
	if len(edge.Route) < 2 {
		t.Fatalf("edge %q route has %d points, want at least 2", edge.AbsID(), len(edge.Route))
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, point := range edge.Route {
		minX = math.Min(minX, point.X)
		minY = math.Min(minY, point.Y)
		maxX = math.Max(maxX, point.X)
		maxY = math.Max(maxY, point.Y)
	}
	return math.Max(maxX-minX, maxY-minY)
}

func TestNestedEdgeRoutesWithoutLayered(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	for _, algorithm := range []string{"layered", "stress", "force", "mrtree", "radial"} {
		t.Run(algorithm, func(t *testing.T) {
			g, _, err := d2compiler.Compile("", strings.NewReader("outer: {inner: {a}}\nexternal -> outer.inner.a\n"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
				t.Fatal(err)
			}
			opts := DefaultOpts
			opts.Algorithm = algorithm
			if err := Layout(t.Context(), g, &opts); err != nil {
				t.Fatal(err)
			}
			if len(g.Edges[0].Route) < 2 {
				t.Fatalf("edge has route %v", g.Edges[0].Route)
			}
		})
	}
}
