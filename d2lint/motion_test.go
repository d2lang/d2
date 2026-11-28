package d2lint

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func compile(t *testing.T, source string, files fs.FS) (*d2target.Diagram, *d2graph.Graph) {
	t.Helper()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	opts := &d2lib.CompileOptions{
		InputPath: "film.d2", FS: files, AnimateTweens: new(true), Layout: new("dagre"), Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error { return d2dagrelayout.Layout(ctx, g, nil) }, nil
		},
	}
	d, g, err := d2lib.Compile(log.WithDefault(t.Context()), source, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return d, g
}

func TestReviewAuthoredMotion(t *testing.T) {
	d, g := compile(t, `anchor
redis: Redis {slot: empty {style.stroke-dash: 4}}
steps: {
  fill: {
    redis.slot: null
    redis.order: "order:481"
    a; b; c; d; e; f
  }
  hit: {
    latency: 2 ms
    a -> b: hit
  }
  clear: {
    latency: null
    (a -> b)[0]: null
  }
  read: {
    redis.style.opacity: 0
    annotation: |md Read [the cache](redis). |
  }
}`, nil)
	got, err := Motion(t.Context(), d, g, 400)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		code, board, object string
		line                int
		text                string
	}{
		{"crowded-step", "steps.fill", "", 4, "within 200 ms"},
		{"replaced-id", "steps.fill", "redis.slot", 5, "redis.order"},
		{"short-label", "steps.hit", "latency", 10, `"2 ms" is visible for 0.4 s`},
		{"short-label", "steps.hit", "(a -> b)[0]", 11, `"hit" is visible for 0.4 s`},
		{"hidden-cue", "steps.read", "redis", 19, "caption names redis"},
	} {
		if !slices.ContainsFunc(got, func(x Diagnostic) bool {
			return x.Path == "film.d2" && x.Line == want.line && x.Column > 0 && x.Code == want.code && x.Board == want.board && x.Object == want.object && strings.Contains(x.Message, want.text)
		}) {
			t.Errorf("missing %+v among %+v", want, got)
		}
	}
	if slices.ContainsFunc(got, func(x Diagnostic) bool { return x.Code == "short-label" && (x.Object == "anchor" || x.Object == "a") }) {
		t.Errorf("persistent labels should not flash: %+v", got)
	}
}

func TestLabelContinuityAndImports(t *testing.T) {
	const source = `stable
status: Up
steps: {
  off: {status.style.opacity: 0}
  alert: { ...@note }
  hold: {stable.style.fill: red}
  clear: {alert: null; status.style.opacity: 1}
}`
	files := fstest.MapFS{"note.d2": {Data: []byte("# inherited through an import\nalert: 2 ms\n")}}
	d, g := compile(t, source, files)
	// The alert spans two 500ms boards. "Up" spans the final and opening
	// boards of the playback loop. Each has time to be read.
	got, err := Motion(t.Context(), d, g, 500)
	if err != nil || len(got) != 0 {
		t.Fatalf("unexpected findings: %+v, %v", got, err)
	}
	got, err = Motion(t.Context(), d, g, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(got, func(x Diagnostic) bool {
		return x.Path == "note.d2" && x.Line == 2 && x.Object == "alert" && strings.Contains(x.Message, "0.4 s")
	}) {
		t.Fatalf("expected source location and time across inherited boards: %+v", got)
	}
}

func TestSampledOnsetsAndCaptionHold(t *testing.T) {
	d, g := compile(t, `grid-columns: 6
a; b; c; d; e; f
steps: {
  changes: {
    *.style.fill: red
    annotation: |md The six services are now ready to accept requests. |
    message: completed
  }
}`, nil)
	step := d.Steps[0]
	// Use deliberately uneven sample ends: the first three color changes are
	// 300 ms apart from the last three. Endpoint-only lint would group all six.
	copySample := func() *d2target.Diagram {
		s := *d
		s.Steps = nil
		s.Shapes = slices.Clone(d.Shapes)
		return &s
	}
	step.TweenDurationMS = 400
	left, right := copySample(), copySample()
	for i := 0; i < 3; i++ {
		right.Shapes[i].Fill = step.Shapes[i].Fill
	}
	step.TweenFrames = []*d2target.Diagram{left, right}
	step.TweenFrameEndsMS = []int{25, 325}
	got, err := Motion(t.Context(), d, g, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range got {
		if x.Code == "crowded-step" || x.Code == "short-label" && x.Object == "message" {
			t.Fatalf("bad timing: %+v", got)
		}
	}
}

func TestIndependentLayersAndSingleBoards(t *testing.T) {
	d, g := compile(t, `layers: {
  one: {a; b; c; d; e; f}
  two: {g; h; i; j; k; l}
}`, nil)
	got, err := Motion(t.Context(), d, g, 1500)
	if err != nil || len(got) > 0 {
		t.Fatalf("independent boards: %+v, %v", got, err)
	}
	d, g = compile(t, "camera: narration", nil)
	got, err = Motion(t.Context(), d, g, 50)
	if err != nil || len(got) > 0 {
		t.Fatalf("single static board: %+v, %v", got, err)
	}
}

func TestMotionPositionsForDottedBoards(t *testing.T) {
	d, g := compile(t, "base\nsteps.burst: {\n a; b; c; d; e; f\n}", nil)
	got, err := Motion(t.Context(), d, g, 2000)
	if err != nil || len(got) != 1 || got[0].Code != "crowded-step" || got[0].Line != 2 || got[0].Column != 7 {
		t.Fatalf("wrong dotted board location: %+v, %v", got, err)
	}
}

func TestFlashedOpacityPointsToRevealAndNestedPath(t *testing.T) {
	d, g := compile(t, `layers: {
  "cache.v2": {
    redis: Cached {style.opacity: 0}
    steps: {
      show: {redis.style.opacity: 1}
      hide: {redis.style.opacity: 0}
    }
  }
}`, nil)
	got, err := Motion(t.Context(), d, g, 200)
	if err != nil || len(got) != 1 || got[0].Board != `layers."cache.v2".steps.show` || got[0].Line != 5 || got[0].Column != 14 {
		t.Fatalf("expected the visibility change in the nested step: %+v, %v", got, err)
	}
}

func TestHiddenAndIntermediateOnlyObjects(t *testing.T) {
	d, g := compile(t, `anchor
steps: {change: {anchor.style.fill: red}}`, nil)
	step := d.Steps[0]
	sample := *step
	sample.Shapes = slices.Clone(step.Shapes)
	for _, id := range []string{"1", "2", "3", "4", "5", "6"} {
		s := step.Shapes[0]
		s.ID, s.Label = "sample-"+id, "generated"
		sample.Shapes = append(sample.Shapes, s)
	}
	step.TweenDurationMS, step.TweenFrames = 300, []*d2target.Diagram{&sample}
	got, err := Motion(t.Context(), d, g, 400)
	if err != nil || len(got) != 0 {
		t.Fatalf("sample-only shapes should have no authoring warnings: %+v, %v", got, err)
	}
}

func TestCueTargetFadesDuringCaption(t *testing.T) {
	d, g := compile(t, `vars: {d2-config: {animate-interval: 2s; animate-duration: 400ms}}
db
steps: {
  read: {
    annotation: |md Read the [database](db). |
    db.style.opacity: 0
  }
  show: {
    annotation: |md See the [database](db). |
    db.style.opacity: 1
  }
}`, nil)
	got, err := Motion(t.Context(), d, g, 2000)
	if err != nil || len(got) != 1 || got[0].Code != "hidden-cue" || got[0].Board != "steps.read" || got[0].Line != 5 {
		t.Fatalf("only the fade-out should obscure the cue: %+v, %v", got, err)
	}
}
