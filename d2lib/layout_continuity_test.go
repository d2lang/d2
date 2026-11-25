package d2lib

import (
	"context"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func compileContinuity(t *testing.T, source string, adjust d2graph.LayoutContinuation) (*d2target.Diagram, *d2graph.Graph) {
	t.Helper()
	engine := "TALA"
	d, g, err := Compile(d2log.WithDefault(t.Context()), source, &CompileOptions{
		Ruler: layoutReuseTestRuler, Layout: &engine,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return d2talalayout.DefaultLayout, nil
		},
		LayoutContinuations: map[string]d2graph.LayoutContinuation{"tala": adjust},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return d, g
}

func TestTALABoardContinuity(t *testing.T) {
	const src = `direction: right
"a.with.dots" -> gateway -> queue -> worker -> ledger
scenarios: {
  secondary: {queue -> monitor}
  incoming: {worker <- audit}
}
steps: {
  audit: {worker -> audit}
  recolor: {audit.style.fill: red}
  remove: {audit: null}
}
layers: {
  separate: {direction: right; "a.with.dots" -> gateway -> queue -> worker -> ledger -> audit}
}`
	board, graph := compileContinuity(t, src, d2talalayout.ContinueBoard)
	fresh, _ := compileContinuity(t, src, nil)
	if board.LayoutContinuity || board.Layers[0].LayoutContinuity ||
		!reflect.DeepEqual(diagramGeometryOf(board.Layers[0]), diagramGeometryOf(fresh.Layers[0])) {
		t.Fatal("independent layer inherited geometry from its parent")
	}
	if reflect.DeepEqual(diagramGeometryOf(board.Steps[0]), diagramGeometryOf(fresh.Steps[0])) {
		t.Fatal("fixture no longer differentiates ordinary and incremental layout")
	}
	for _, step := range append(board.Scenarios, board.Steps...) {
		if !step.LayoutContinuity {
			t.Fatalf("eligible board %s lost its predecessor's coordinate frame", step.Name)
		}
		common := diagramGeometryOf(step)
		base := diagramGeometryOf(board)
		if !reflect.DeepEqual(common.shapes[:len(base.shapes)], base.shapes) ||
			!reflect.DeepEqual(common.connections[:len(base.connections)], base.connections) {
			t.Fatalf("board %s moved existing geometry", step.Name)
		}
	}
	if shapeByID(t, board.Steps[1], "audit").Fill != "red" ||
		!reflect.DeepEqual(diagramGeometryOf(board.Steps[0]), diagramGeometryOf(board.Steps[1])) {
		t.Fatal("paint-only cumulative step lost its style or preceding layout")
	}
	if len(board.Steps[2].Shapes) != len(board.Shapes) {
		t.Fatal("deleted leaf retained")
	}
	if graph.Steps[0].Objects[0].TopLeft == graph.Objects[0].TopLeft ||
		graph.Steps[0].Edges[0].Route[0] == graph.Edges[0].Route[0] {
		t.Fatal("prior and current graph have shared mutable geometry")
	}
}

func TestTALABoardContinuityUsesOrdinaryLayoutForUnsupportedEdits(t *testing.T) {
	const src = `direction: right
A -> B -> C -> D
scenarios: {
  two: {B -> E; B -> F}
  replacement: {C: null; B -> E}
  pinned: {E: {left: 17; top: 40}; B -> E}
  wider: {B.width: 250; B -> E}
  new-caption: {B -> E: Notify}
  changed-caption: {B.label: Z; B -> E}
  changed-flow: {direction: left; B -> E}
  hidden: {E.style.opacity: 0.0; B -> E}
  unpainted: {E.style.fill: transparent; E.style.stroke: transparent; E.style.font-color: transparent; B -> E}
  map: {group: {E -> F}; B -> group.E}
  independent: {E}
}`
	board, _ := compileContinuity(t, src, d2talalayout.ContinueBoard)
	fresh, _ := compileContinuity(t, src, nil)
	for i, scenario := range board.Scenarios {
		if !reflect.DeepEqual(scenario, fresh.Scenarios[i]) {
			t.Errorf("scenario %s differs from ordinary layout", scenario.Name)
		}
	}
}

func TestLayoutContinuityAcrossSampledTransition(t *testing.T) {
	const src = `vars: {n: 100%; d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
a: "Load ${n}"
steps: {
  drain: {vars: {n: 0%}}
  done: {a.style.fill: red}
}`
	base, current := []string{}, []string{}
	board, _ := compileContinuity(t, src, func(_ context.Context, a, b *d2graph.Graph) (bool, error) {
		base = append(base, a.Objects[0].Label.Value)
		current = append(current, b.Objects[0].Label.Value)
		return b.Objects[0].Label.Value != "Load 50%", nil
	})
	if len(board.Steps[0].TweenFrames) != 4 || board.Steps[0].LayoutContinuity || !board.Steps[1].LayoutContinuity {
		t.Fatalf("continuity across a transition with an independent sample: %v, %v", board.Steps[0].LayoutContinuity, board.Steps[1].LayoutContinuity)
	}
	if !reflect.DeepEqual(base[1:], current[:len(current)-1]) {
		t.Fatalf("wrong previous board in sample sequence: previous=%v current=%v", base, current)
	}
}

func TestTALABoardContinuityRejectsCanceledOrReconfiguredBoard(t *testing.T) {
	_, graph := compileContinuity(t, "direction: right\nA -> B -> C\nsteps: {branch: {B -> D}}", nil)
	step := graph.Steps[0]
	initial := step.Objects[0].TopLeft.Copy()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if accepted, err := d2talalayout.ContinueBoard(ctx, graph, step); accepted || err != context.Canceled {
		t.Fatalf("canceled board accepted=%t err=%v", accepted, err)
	}
	step.Data = map[string]interface{}{"tala-seeds": []int64{7}}
	if accepted, err := d2talalayout.ContinueBoard(t.Context(), graph, step); accepted || err != nil {
		t.Fatalf("explicit new seeds accepted=%t err=%v", accepted, err)
	}
	if *step.Objects[0].TopLeft != *initial {
		t.Fatal("declined continuation changed the current board")
	}
}

func TestTALABoardContinuationTranslatesWithoutMutatingInputGeometry(t *testing.T) {
	_, graph := compileContinuity(t, "A -> B\nsteps: {recolor: {A.style.fill: red}}", nil)
	step := graph.Steps[0]
	for _, n := range step.Objects {
		n.TopLeft.X += 100
	}
	step.Edges[0].Move(100, 0)
	externalBox, externalPoint := step.Objects[0].Box, step.Edges[0].Route[0]
	x, portX := externalBox.TopLeft.X, externalPoint.X
	ok, err := d2talalayout.ContinueBoard(t.Context(), graph, step)
	if !ok || err != nil {
		t.Fatalf("uniform translation rejected: %t, %v", ok, err)
	}
	if step.Objects[0].TopLeft.X != x-100 || step.Edges[0].Route[0].X != portX-100 {
		t.Fatal("geometry did not return to its preceding coordinates")
	}
	if externalBox.TopLeft.X != x || externalPoint.X != portX {
		t.Fatal("translation mutated aliased input geometry")
	}
}
