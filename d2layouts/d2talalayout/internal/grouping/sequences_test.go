package grouping

import (
	"context"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/shape"
)

func mustIdentifySequences(t *testing.T, graph *layoutgraph.Graph, nodes []*layoutgraph.Node) [][]*layoutgraph.Node {
	t.Helper()
	guard, err := limits.NewWorkGuard(t.Context(), "IdentifySequences", limits.MaxEngineWorkUnits)
	if err != nil {
		t.Fatal(err)
	}
	active, err := activeSequenceNodes(graph, guard)
	if err != nil {
		t.Fatal(err)
	}
	sequences, err := identifySequences(active, nodes, guard)
	if err != nil {
		t.Fatal(err)
	}
	return sequences
}

func newStepSequenceGraph() (*layoutgraph.Graph, *layoutgraph.Node, *layoutgraph.Node) {
	graph := layoutgraph.NewGraph()
	first := layoutgraph.NewNode(1, 20, 10)
	second := layoutgraph.NewNode(2, 40, 30)
	first.SetShape(shape.STEP_TYPE)
	second.SetShape(shape.STEP_TYPE)
	graph.AddNewNodeToContainer(nil, first)
	graph.AddNewNodeToContainer(nil, second)
	graph.Connect(first, second)
	return graph, first, second
}

func TestSequenceCanBeRebuiltAfterCleanup(t *testing.T) {
	graph, first, second := newStepSequenceGraph()
	random := rand.New(rand.NewSource(1))
	if err := AddSequences(context.Background(), graph, random); err != nil {
		t.Fatal(err)
	}
	var firstVesselID layoutgraph.EntityID
	for vessel := range graph.Sequences {
		firstVesselID = vessel.ID
	}
	Cleanup(graph)
	if first.ConnectionTo(second) != nil {
		t.Fatal("sequence-defining edge survived cleanup")
	}
	if err := AddSequences(context.Background(), graph, rand.New(rand.NewSource(1))); err != nil {
		t.Fatal(err)
	}
	if len(graph.Sequences) != 1 {
		t.Fatalf("sequence count after rebuild = %d, want 1", len(graph.Sequences))
	}
	for vessel := range graph.Sequences {
		if vessel.ID != firstVesselID {
			t.Fatalf("rebuilt vessel ID = %d, want %d", vessel.ID, firstVesselID)
		}
	}
}

func TestIdentifySequencesSkipsRememberedInactiveSteps(t *testing.T) {
	graph, first, second := newStepSequenceGraph()
	remembered := &layoutgraph.Sequence{
		Vessel: layoutgraph.NewNode(3, 0, 0), Nodes: []*layoutgraph.Node{first, second}, Graph: graph,
	}
	first.Sequence = remembered
	second.Sequence = remembered
	if got := mustIdentifySequences(t, graph, []*layoutgraph.Node{first, second}); len(got) != 0 {
		t.Fatalf("remembered steps rediscovered as %d sequences", len(got))
	}
}

func TestRememberedStepsKeepNewTransition(t *testing.T) {
	g, first, second := newStepSequenceGraph()
	r := rand.New(rand.NewSource(2))
	if err := AddSequences(t.Context(), g, r); err != nil {
		t.Fatal(err)
	}
	Cleanup(g)
	added := g.Connect(first, second)
	added.KeepExplicit = true
	if err := AddSequences(t.Context(), g, r); err != nil {
		t.Fatal(err)
	}
	if len(g.Sequences) != 0 || len(g.Edges) != 1 || g.Edges[0] != added {
		t.Fatal("remembered sequence consumed a new explicit transition")
	}
}

func TestAbductSequenceEdgesRetainsGraphOrder(t *testing.T) {
	graph := layoutgraph.NewGraph()
	newNode := func(id int64) *layoutgraph.Node {
		n := layoutgraph.NewNode(id, 100, 60)
		graph.AddNewNodeToContainer(nil, n)
		return n
	}
	first, middle, last := newNode(1), newNode(2), newNode(3)
	left, right, vessel := newNode(4), newNode(5), newNode(6)
	sequence := &layoutgraph.Sequence{Graph: graph, Nodes: []*layoutgraph.Node{first, middle, last}, Vessel: vessel}
	unrelated := graph.Connect(left, right)
	abductSequenceEdges(sequence)
	if len(sequence.EdgeAbductions) != 0 {
		t.Fatal("isolated steps recorded unrelated edges")
	}
	out := graph.Connect(last, right)
	in := graph.Connect(left, first)
	internal := graph.Connect(first, last)
	abductSequenceEdges(sequence)
	if a := sequence.EdgeAbductions; len(a) != 2 || a[0].Edge != out || a[0].OriginallyFrom != last ||
		a[1].Edge != in || a[1].OriginallyTo != first {
		t.Fatalf("external edges lost their graph order or original endpoint: %+v", a)
	}
	if out.From != vessel || in.To != vessel || unrelated.From != left || unrelated.To != right ||
		internal.From != first || internal.To != last {
		t.Fatal("incorrectly transferred a sequence edge")
	}
}
