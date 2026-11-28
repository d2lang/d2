package d2lib

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
)

const scenarioTiming = "vars: {d2-config: {animate-interval: 250ms; animate-duration: 160ms}}\n"

func TestScenarioGridAndOpacityCompareSiblingEndpoints(t *testing.T) {
	source := scenarioTiming + `grid-columns: 2
gateway; worker
scenarios: {
  overflow: {spare; gateway.style.opacity: 0.2}
  normal: {}
  hold: {}
}`
	root, _ := compileGridMotionTest(t, source)
	overflow, normal := root.Scenarios[0], root.Scenarios[1]
	for _, board := range []*d2target.Diagram{overflow, normal} {
		if !board.GridMotion || len(board.TweenFrames) < 2 {
			t.Fatalf("%s has no grid transition", board.Name)
		}
		start := shapeByID(t, board.TweenFrames[0], "gateway").Opacity
		if board == overflow && start != 1 || board == normal && start != .2 {
			t.Fatalf("%s starts at opacity %g", board.Name, start)
		}
		if !slices.ContainsFunc(board.TweenFrames, func(f *d2target.Diagram) bool {
			gateway := shapeByID(t, f, "gateway")
			return gateway.Opacity > .2 && gateway.Opacity < 1
		}) {
			t.Errorf("%s never interpolated the implicit opacity", board.Name)
		}
	}
	if len(normal.Shapes) != 2 || shapeByID(t, normal, "gateway").Opacity != 1 ||
		len(root.Scenarios[2].TweenFrames) != 0 {
		t.Fatal("scenario carried the overflow state into an independent alternative")
	}
	if err := d2target.ValidateRenderTarget(root); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioStructuralAdditionAndRemoval(t *testing.T) {
	source := scenarioTiming + `direction: right
gateway -> queue: {icon.style.animated: 1/s}
scenarios: {
  spillover: {queue -> reserve}
  steady: {}
}`
	root, _ := compileContinuity(t, source, d2talalayout.ContinueBoard)
	prev := root
	for _, scenario := range root.Scenarios {
		if len(scenario.TweenFrames) < 4 || !scenario.LayoutContinuity {
			t.Fatalf("%s missing continuous arrival or departure", scenario.Name)
		}
		first := scenario.TweenFrames[0]
		if !reflect.DeepEqual(first.Shapes, prev.Shapes) || !reflect.DeepEqual(first.Connections, prev.Connections) {
			t.Errorf("%s did not start at the displayed endpoint", scenario.Name)
		}
		prev = scenario
	}
}

func TestScenarioAnchorsSamplesAndEndpointsToParent(t *testing.T) {
	const source = `vars: {n: 100%; d2-config: {animate-interval: 250ms; animate-duration: 160ms}}
a: "Load ${n}"
scenarios: {
  busy: {vars.n: 180%}
  quiet: {vars.n: 20%}
}`
	root, _ := compileContinuity(t, source, func(_ context.Context, prev, next *d2graph.Graph) (bool, error) {
		if got := prev.Objects[0].Label.Value; got != "Load 100%" {
			t.Errorf("laid out %s from %s, want common parent", next.Objects[0].Label.Value, got)
		}
		return true, nil
	})
	quiet := root.Scenarios[1]
	if len(quiet.TweenFrames) != 4 {
		t.Fatalf("got %d paint samples", len(quiet.TweenFrames))
	}
	for i, want := range []string{"Load 180%", "Load 140%", "Load 100%", "Load 60%"} {
		if got := shapeByID(t, quiet.TweenFrames[i], "a").Label; got != want {
			t.Errorf("sample %d label %q, want %q", i, got, want)
		}
	}
}

func TestScenarioDoesNotRevisitGridAcrossInterveningBoards(t *testing.T) {
	const source = `grid-columns: 2
a; b
layers: {separate: {unrelated}}
scenarios: {
  first: {c; layers: {detail: {counter}}}
  second: {d}
  third: {e}
}`
	root, _ := compileGridMotionTest(t, scenarioTiming+source)
	for _, board := range root.Scenarios[:2] {
		if len(board.TweenFrames) != 0 || board.GridMotion {
			t.Fatalf("%s reanimated the previous grid after a different board intervened", board.Name)
		}
	}
	if !root.Scenarios[2].GridMotion {
		t.Fatal("eligible sibling after an interruption did not animate")
	}
}

func TestScenarioReturnsFromAnimatedEmptyBoard(t *testing.T) {
	source := scenarioTiming + `vars.load: 150%
grid-columns: 1
n: "Load ${load}"
scenarios: {
  wide: {vars.load: 250%}
  blank: {n: null}
  restore: {}
}`
	root, _ := compileGridMotionTest(t, source)
	blank, restore := root.Scenarios[1], root.Scenarios[2]
	if blank.IsFolderOnly || !blank.GridMotion || !restore.GridMotion || len(restore.TweenFrames) == 0 {
		t.Fatal("missing empty grid endpoint or transition")
	}
	if got := shapeByID(t, restore, "n").Label; got != "Load 150%" {
		t.Fatalf("restored label %q", got)
	}
	for _, frame := range restore.TweenFrames {
		for _, s := range frame.Shapes {
			if s.Label != "Load 150%" {
				t.Errorf("replayed the wide scenario after the empty board: %q", s.Label)
			}
		}
	}
}

func TestScenarioIconStreamOrigins(t *testing.T) {
	const source = `a -> b: first {icon.style.animated: 1/s}
a -> b: second {icon.style.animated: 2/s}
scenarios: {
  remove: {(a -> b)[0]: null}
  restore: {}
}`
	root, _ := compileGridMotionTest(t, scenarioTiming+source)
	for i, want := range [][]string{{"(a -> b)[1]"}, {"", "(a -> b)[0]"}} {
		for j, id := range want {
			e := root.Scenarios[i].Connections[j]
			if prev := e.IconAnimation.PreviousID; prev == nil || *prev != id {
				t.Errorf("%s %s predecessor %v, want %q", root.Scenarios[i].Name, e.Label, prev, id)
			}
		}
	}
	cuts, _ := compileGridMotionTest(t, "vars: {d2-config.animate-interval: 250ms}\n"+source)
	if cuts.Scenarios[0].Connections[0].IconAnimation.PreviousID != nil {
		t.Fatal("cut-only scenario inherited moving copies")
	}
}

func TestScenarioReusesRepeatedGeometryAfterStructuralEdit(t *testing.T) {
	source := scenarioTiming + `a: {style.fill: red}
a -> b
scenarios: {third: {a.style.fill: blue; b -> c}}`
	optimized, calls := compileForLayoutReuseTest(t, source, true)
	baseline, _ := compileForLayoutReuseTest(t, source, false)
	if calls != 2 || len(optimized.Scenarios[0].TweenFrames) != 4 {
		t.Fatalf("got %d layouts, %d samples; want 2 layouts across 4 samples", calls, len(optimized.Scenarios[0].TweenFrames))
	}
	assertExactDiagramAndSVG(t, optimized, baseline)
}
