package d2layouts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestExtractSubgraphsMatchesSequentialExtraction(t *testing.T) {
	t.Parallel()
	input := `
gridA: {
  grid-columns: 2
  a -> b -> c
}
sequenceB: {
  shape: sequence_diagram
  x -> y: request
}
ordinary: {
  inner: {
    grid-rows: 1
    p -> q
  }
}
gridA.c -> sequenceB.x
ordinary.inner.q -> gridA.a
noteA: {
  near: center-right
  n1 -> n2
}
noteB: {
  near: bottom-right
  m1 -> m2
}
noteA.n2 -> noteB.m1
`

	sequentialGraph := compileLayoutTestGraph(t, input)
	sequentialSpecs := collectNestedExtractions(sequentialGraph)
	sequential := make(map[string]extractionSignature, len(sequentialSpecs))
	for _, spec := range sequentialSpecs {
		id := spec.container.AbsID()
		nestedGraph, externalEdges, externalEdgeIDs := ExtractSubgraph(spec.container, spec.includeSelf)
		sequential[id] = makeExtractionSignature(nestedGraph, externalEdges, externalEdgeIDs)
	}

	batchedGraph := compileLayoutTestGraph(t, input)
	batchedSpecs := collectNestedExtractions(batchedGraph)
	batchedExtractions := extractSubgraphs(batchedGraph, batchedSpecs).extractions
	batched := make(map[string]extractionSignature, len(batchedSpecs))
	for _, spec := range batchedSpecs {
		extraction := batchedExtractions[spec.container]
		batched[spec.container.AbsID()] = makeExtractionSignature(extraction.nestedGraph, extraction.externalEdges, extraction.externalEdgeIDs)
	}

	if got, want := graphMembershipSignature(batchedGraph), graphMembershipSignature(sequentialGraph); !reflect.DeepEqual(got, want) {
		t.Fatalf("remaining parent graph differs:\n got: %#v\nwant: %#v", got, want)
	}
	if !reflect.DeepEqual(batched, sequential) {
		t.Fatalf("extractions differ:\n got: %#v\nwant: %#v", batched, sequential)
	}
}

func TestLayoutNestedBatchRollsBackLaterSiblingsOnError(t *testing.T) {
	t.Parallel()
	input := `
base
first: {
  near: center-right
  a -> b
}
middle
second: {
  grid-columns: 1
  c -> d
}
third: {
  near: bottom-right
  e -> f
}
last
first.b -> second.c
base -> second.d
first.b -> third.e
base -> third.f
`
	wantErr := errors.New("injected layout failure")
	failingLayout := func(context.Context, *d2graph.Graph) error {
		return wantErr
	}
	ctx := log.WithDefault(context.Background())

	batchedGraph := compileLayoutTestGraph(t, input)
	batchedTracked := trackLayoutObjects(batchedGraph)
	err := LayoutNested(ctx, batchedGraph, GraphInfo{}, failingLayout, DefaultRouter)
	if !errors.Is(err, wantErr) {
		t.Fatalf("batched LayoutNested error = %v, want %v", err, wantErr)
	}
	got := layoutErrorState(batchedGraph, batchedTracked)

	sequentialGraph := compileLayoutTestGraph(t, input)
	sequentialTracked := trackLayoutObjects(sequentialGraph)
	restoreOrder := SaveOrder(sequentialGraph)
	specs := collectNestedExtractions(sequentialGraph)
	if len(specs) < 2 {
		t.Fatalf("test needs at least two nested diagrams, got %d", len(specs))
	}
	first := specs[0]
	nestedGraph, _, _ := ExtractSubgraph(first.container, first.includeSelf)
	nestedInfo := first.graphInfo
	if first.graphInfo.IsConstantNear {
		nestedInfo = GraphInfo{}
		first.container.NearKey = nil
	}
	err = LayoutNested(ctx, nestedGraph, nestedInfo, failingLayout, DefaultRouter)
	if !errors.Is(err, wantErr) {
		t.Fatalf("sequential reference error = %v, want %v", err, wantErr)
	}
	restoreOrder()
	want := layoutErrorState(sequentialGraph, sequentialTracked)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batched error state differs from sequential extraction:\n got: %#v\nwant: %#v", got, want)
	}
}

type layoutErrorGraphState struct {
	parent  graphMembership
	objects []string
}

func trackLayoutObjects(g *d2graph.Graph) map[string]*d2graph.Object {
	tracked := make(map[string]*d2graph.Object, len(g.Objects))
	for _, obj := range g.Objects {
		tracked[obj.AbsID()] = obj
	}
	return tracked
}

func layoutErrorState(parentGraph *d2graph.Graph, tracked map[string]*d2graph.Object) layoutErrorGraphState {
	state := layoutErrorGraphState{parent: graphMembershipSignature(parentGraph)}
	ids := make([]string, 0, len(tracked))
	for id := range tracked {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, originalID := range ids {
		obj := tracked[originalID]
		parentID := "<nil>"
		if obj.Parent != nil {
			parentID = obj.Parent.AbsID()
		}
		var children []string
		for _, child := range obj.ChildrenArray {
			children = append(children, child.AbsID())
		}
		var graphObjects, graphEdges []string
		for _, graphObject := range obj.Graph.Objects {
			graphObjects = append(graphObjects, graphObject.AbsID())
		}
		for _, graphEdge := range obj.Graph.Edges {
			graphEdges = append(graphEdges, graphEdge.AbsID())
		}
		state.objects = append(state.objects, fmt.Sprintf(
			"%s current=%s parent=%s parentGraph=%t rootLevel=%d nearNil=%t children=%v graphObjects=%v graphEdges=%v",
			originalID,
			obj.AbsID(),
			parentID,
			obj.Graph == parentGraph,
			obj.Graph.RootLevel,
			obj.NearKey == nil,
			children,
			graphObjects,
			graphEdges,
		))
	}
	return state
}

type graphMembership struct {
	objects      []string
	edges        []string
	rootChildren []string
}

type extractionSignature struct {
	graphMembership
	externalEdgeCount int
	externalIDs       []string
}

func graphMembershipSignature(g *d2graph.Graph) graphMembership {
	signature := graphMembership{}
	for _, obj := range g.Objects {
		signature.objects = append(signature.objects, obj.AbsID())
	}
	for _, edge := range g.Edges {
		signature.edges = append(signature.edges, edge.AbsID())
	}
	for _, obj := range g.Root.ChildrenArray {
		signature.rootChildren = append(signature.rootChildren, obj.AbsID())
	}
	return signature
}

func makeExtractionSignature(g *d2graph.Graph, externalEdges []*d2graph.Edge, externalEdgeIDs []edgeIDs) extractionSignature {
	signature := extractionSignature{
		graphMembership:   graphMembershipSignature(g),
		externalEdgeCount: len(externalEdges),
	}
	for _, ids := range externalEdgeIDs {
		signature.externalIDs = append(signature.externalIDs, ids.srcID+" -> "+ids.dstID)
	}
	return signature
}

func compileLayoutTestGraph(tb testing.TB, input string) *d2graph.Graph {
	tb.Helper()
	g, _, err := d2compiler.Compile("index.d2", strings.NewReader(input), nil)
	if err != nil {
		tb.Fatal(err)
	}
	return g
}

func nestedPartitionSource(diagrams, children int) string {
	var out strings.Builder
	for diagram := 0; diagram < diagrams; diagram++ {
		fmt.Fprintf(&out, "g%d: {\n  grid-columns: 2\n", diagram)
		for child := 0; child < children; child++ {
			fmt.Fprintf(&out, "  n%d\n", child)
			if child > 0 {
				fmt.Fprintf(&out, "  n%d -> n%d\n", child-1, child)
			}
		}
		out.WriteString("}\n")
		if diagram > 0 {
			fmt.Fprintf(&out, "g%d.n%d -> g%d.n0\n", diagram-1, children-1, diagram)
		}
	}
	return out.String()
}

var layoutBenchmarkGraphSink *d2graph.Graph

func BenchmarkNestedSubgraphPartition(b *testing.B) {
	for _, diagrams := range []int{8, 32, 128} {
		input := nestedPartitionSource(diagrams, 4)
		b.Run(fmt.Sprintf("diagrams-%d", diagrams), func(b *testing.B) {
			b.Run("sequential", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					g := compileLayoutTestGraph(b, input)
					specs := collectNestedExtractions(g)
					b.StartTimer()
					for _, spec := range specs {
						ExtractSubgraph(spec.container, spec.includeSelf)
					}
				}
			})
			b.Run("one-pass", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					g := compileLayoutTestGraph(b, input)
					specs := collectNestedExtractions(g)
					b.StartTimer()
					extractSubgraphs(g, specs)
				}
			})
		})
	}
}

func BenchmarkLayoutNestedPartitions(b *testing.B) {
	ctx := log.WithDefault(context.Background())
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		b.Fatal(err)
	}
	for _, diagrams := range []int{8, 32, 128} {
		input := nestedPartitionSource(diagrams, 4)
		b.Run(fmt.Sprintf("diagrams-%d", diagrams), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				g := compileLayoutTestGraph(b, input)
				if err := g.ApplyTheme(d2themescatalog.NeutralDefault.ID); err != nil {
					b.Fatal(err)
				}
				if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := LayoutNested(ctx, g, GraphInfo{}, d2dagrelayout.DefaultLayout, DefaultRouter); err != nil {
					b.Fatal(err)
				}
				layoutBenchmarkGraphSink = g
			}
		})
	}
}

func TestExtractSubgraphsPreservesIndependentConfigData(t *testing.T) {
	for _, batched := range []bool{false, true} {
		t.Run(fmt.Sprintf("batched=%t", batched), func(t *testing.T) {
			g := compileLayoutTestGraph(t, `grid: {grid-columns: 1; a -> b}
note: {near: top-left; c -> d}`)
			data := map[string]interface{}{
				"tala-seeds": []interface{}{"7", "11"},
				"metadata":   map[string]interface{}{"name": "original"},
			}
			g.Data = map[string]interface{}{
				"tala-seeds": []interface{}{"7", "11"},
				"metadata":   map[string]interface{}{"name": "original"},
			}
			specs := collectNestedExtractions(g)
			var graphs []*d2graph.Graph
			if batched {
				batch := extractSubgraphs(g, specs)
				for _, spec := range specs {
					graphs = append(graphs, batch.extractions[spec.container].nestedGraph)
				}
			} else {
				for _, spec := range specs {
					nested, _, _ := ExtractSubgraph(spec.container, spec.includeSelf)
					graphs = append(graphs, nested)
				}
			}
			for _, graph := range graphs {
				if !reflect.DeepEqual(graph.Data, data) {
					t.Fatalf("nested data = %#v, want %#v", graph.Data, data)
				}
				graph.Data["tala-seeds"].([]interface{})[0] = "changed"
				graph.Data["metadata"].(map[string]interface{})["name"] = "changed"
			}
			if !reflect.DeepEqual(g.Data, data) {
				t.Fatalf("nested layout changed parent data: %#v", g.Data)
			}
		})
	}
}

// shapesRecorder keeps the "shapes" attribute of every record, unformatted.
type shapesRecorder struct {
	mu     sync.Mutex
	shapes []slog.Value
}

func (h *shapesRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (h *shapesRecorder) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *shapesRecorder) WithGroup(string) slog.Handler            { return h }
func (h *shapesRecorder) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "shapes" {
			h.shapes = append(h.shapes, attr.Value)
		}
		return true
	})
	return nil
}

func TestLayoutNestedLogsShapesLazily(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	// A grid, a sequence diagram and ordinary shapes reach every log call.
	g := compileLayoutTestGraph(t, "a -> b\ngrid: {\n  grid-columns: 1\n  c -> d\n}\nsequence: {\n  shape: sequence_diagram\n  e -> f\n}\n")
	if err := g.ApplyTheme(d2themescatalog.NeutralDefault.ID); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	recorder := &shapesRecorder{}
	ctx := log.With(context.Background(), slog.New(recorder))
	if err := LayoutNested(ctx, g, GraphInfo{}, d2dagrelayout.DefaultLayout, DefaultRouter); err != nil {
		t.Fatal(err)
	}
	// Every graph is handed to the handler unformatted, so listing its objects
	// costs nothing unless a handler asks for the text.
	if len(recorder.shapes) < 6 {
		t.Fatalf("LayoutNested logged the shapes of %d graphs, want at least 6", len(recorder.shapes))
	}
	for _, value := range recorder.shapes {
		if value.Kind() != slog.KindLogValuer {
			t.Fatalf("shapes were formatted before a handler asked for them: %v", value.Kind())
		}
	}

	// The text is what was logged eagerly before, for both kinds of handler.
	want := g.PrintString()
	attr := slog.Any("shapes", graphShapes{g})
	// log.PrettyHandler formats attribute values with fmt.
	if got := fmt.Sprintf("%v", attr.Value); got != want {
		t.Fatalf("fmt formatted %q, want %q", got, want)
	}
	// slog's own handlers resolve them.
	var out strings.Builder
	logger := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug}))
	log.Debug(log.With(context.Background(), logger), "layout", attr)
	if !strings.Contains(out.String(), "shapes="+strconv.Quote(want)) {
		t.Fatalf("text handler wrote %q, want shapes=%q", out.String(), want)
	}
}

func TestNestedLayoutMarksOnlyPartialInputs(t *testing.T) {
	g := compileLayoutTestGraph(t, "a\nnote: {near: top-center}\na -> note")
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := g.ApplyTheme(d2themescatalog.NeutralDefault.ID); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	var sawParent, sawNested bool
	core := func(ctx context.Context, part *d2graph.Graph) error {
		if !part.IsPartialLayout {
			t.Fatal("core layout saw a partial graph marked as a whole board")
		}
		if part == g {
			sawParent = true
		} else {
			sawNested = true
		}
		return d2dagrelayout.DefaultLayout(ctx, part)
	}
	router := func(ctx context.Context, board *d2graph.Graph, edges []*d2graph.Edge) error {
		if board.IsPartialLayout {
			t.Fatal("router saw a restored board marked as partial")
		}
		return DefaultRouter(ctx, board, edges)
	}
	if err := LayoutNested(log.WithDefault(t.Context()), g, GraphInfo{}, core, router); err != nil {
		t.Fatal(err)
	}
	if !sawParent || !sawNested || g.IsPartialLayout {
		t.Fatalf("partial input state: parent %v, extracted %v, returned board %v", sawParent, sawNested, g.IsPartialLayout)
	}
}
