package d2cli

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/xmain"
)

func TestTALALayoutOptions(t *testing.T) {
	state := layoutOptionsState(t, "--tala-seeds=1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17")
	layout, err := LayoutResolver(t.Context(), state)("tala")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout(t.Context(), simpleLayoutGraph()); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("command-line seeds were not applied: %v", err)
	}
	defaultLayout, err := LayoutResolver(t.Context(), nil)("tala")
	if err != nil {
		t.Fatal(err)
	}
	if err := defaultLayout(t.Context(), simpleLayoutGraph()); err != nil {
		t.Fatalf("another resolver changed fresh defaults: %v", err)
	}
	graph := simpleLayoutGraph()
	graph.Data = map[string]any{"tala-seeds": []int64{7}}
	if err := layout(t.Context(), graph); err != nil {
		t.Fatalf("diagram seeds did not override command-line seeds: %v", err)
	}
}

func TestTALALayoutSnapshotsSeeds(t *testing.T) {
	// Seventeen copies of one seed are valid; seventeen distinct seeds are not.
	seeds := make([]int64, 17)
	state := &xmain.State{Opts: xmain.NewOpts(nil, nil)}
	state.Opts.Flags.Int64SliceVar(&seeds, "tala-seeds", seeds, "seeds")
	resolve := LayoutResolver(t.Context(), state)
	for i := range seeds {
		seeds[i] = int64(i)
	}
	layout, err := resolve("tala")
	if err != nil {
		t.Fatal(err)
	}
	if err := layout(t.Context(), simpleLayoutGraph()); err != nil {
		t.Fatalf("changing the original seed slice changed a resolved layout: %v", err)
	}
	other, err := LayoutResolver(t.Context(), state)("tala")
	if err != nil {
		t.Fatal(err)
	}
	if err := other(t.Context(), simpleLayoutGraph()); err == nil || !strings.Contains(err.Error(), "at most 16") {
		t.Fatalf("new resolver did not capture changed seeds: %v", err)
	}
	if err := layout(t.Context(), simpleLayoutGraph()); err != nil {
		t.Fatalf("creating another resolver changed the first: %v", err)
	}
}

func TestConcurrentResolvedLayouts(t *testing.T) {
	for _, name := range builtinLayoutNames() {
		t.Run(name, func(t *testing.T) {
			resolve := LayoutResolver(t.Context(), layoutOptionsState(t))
			var wait sync.WaitGroup
			for range 8 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					layout, err := resolve(name)
					if err != nil {
						t.Error(err)
						return
					}
					if err := layout(t.Context(), simpleLayoutGraph()); err != nil {
						t.Error(err)
					}
				}()
			}
			wait.Wait()
		})
	}
}

func layoutOptionsState(t *testing.T, args ...string) *xmain.State {
	t.Helper()
	state := &xmain.State{Opts: xmain.NewOpts(nil, nil)}
	registerDagreFlags(state.Opts)
	registerELKFlags(state.Opts)
	registerTALAFlags(state.Opts)
	if err := state.Opts.Flags.Parse(args); err != nil {
		t.Fatal(err)
	}
	return state
}

func simpleLayoutGraph() *d2graph.Graph {
	graph := d2graph.NewGraph()
	object := &d2graph.Object{
		Graph:    graph,
		Parent:   graph.Root,
		ID:       "a",
		IDVal:    "a",
		Box:      geo.NewBox(geo.NewPoint(0, 0), 100, 60),
		Children: make(map[string]*d2graph.Object),
	}
	graph.Root.Children[object.ID] = object
	graph.Root.ChildrenArray = append(graph.Root.ChildrenArray, object)
	graph.Objects = append(graph.Objects, object)
	return graph
}

func layoutGraphJSON(t *testing.T, layout d2graph.LayoutGraph) string {
	t.Helper()
	graph, _, err := d2compiler.Compile("test.d2", strings.NewReader("a -> b\na -> c\nb -> d\nc -> d\nb -> b"), nil)
	if err != nil {
		t.Fatal(err)
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := layout(context.Background(), graph); err != nil {
		t.Fatal(err)
	}
	data, err := d2graph.SerializeGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
