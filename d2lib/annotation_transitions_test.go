package d2lib

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func compileAnnotated(t *testing.T, source string) *d2target.Diagram {
	t.Helper()
	calls := 0
	d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := d2target.ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func shapeOf(t *testing.T, d *d2target.Diagram, id string) *d2target.Shape {
	t.Helper()
	for i := range d.Shapes {
		if d.Shapes[i].ID == id {
			return &d.Shapes[i]
		}
	}
	t.Fatalf("no shape %q", id)
	return nil
}

const annotatedSteps = `vars: {d2-config: {animate-interval: 1000ms; animate-duration: 400ms}}
a -> b -> c
steps: {
  one: {
    annotation: |md First [b](b), then [a](a). |
    a.annotation: note
    c.annotation: stays
  }
  two: {
    annotation: |md Only [b](b) now. |
    b.annotation: {shape: box}
    c.annotation: stays
    (a -> b)[0].annotation: fast
  }
}
`

func TestAnnotationsAreDrawnAndErased(t *testing.T) {
	d := compileAnnotated(t, annotatedSteps)
	one, two := d.Steps[0], d.Steps[1]
	want := d2target.TweenFrameCount(400)
	if len(one.TweenFrames) != want || len(two.TweenFrames) != want {
		t.Fatalf("steps have %d and %d frames, want %d each", len(one.TweenFrames), len(two.TweenFrames), want)
	}

	// The endpoint boards hold their marks whole and nothing that is leaving.
	for _, board := range []*d2target.Diagram{one, two} {
		for i := range board.Shapes {
			s := &board.Shapes[i]
			if s.LeavingAnnotation != nil || s.Annotation != nil && s.Annotation.Drawn != nil {
				t.Fatalf("board %s holds a partial mark on %s", board.Name, s.ID)
			}
		}
		if board.Root.LeavingAnnotation != nil || board.Root.Annotation.Drawn != nil {
			t.Fatalf("board %s holds a partial caption", board.Name)
		}
	}

	// Each new mark is drawn from nothing and only ever grows.
	last, halfway := map[string]float64{}, map[string]float64{}
	for i, frame := range one.TweenFrames {
		for _, id := range []string{"a", "b", "c"} {
			a := shapeOf(t, frame, id).Annotation
			if a == nil || a.Drawn == nil || a.Drawn[0] != 0 {
				t.Fatalf("frame %d: %s has annotation %+v", i, id, a)
			}
			if i == 0 && a.Drawn[1] != 0 {
				t.Fatalf("%s starts %v drawn", id, a.Drawn[1])
			}
			if a.Drawn[1] < last[id] {
				t.Fatalf("frame %d: ink on %s went backwards", i, id)
			}
			last[id] = a.Drawn[1]
			if i == want/2 {
				halfway[id] = a.Drawn[1]
			}
		}
	}
	// The caption names b before a, so b's ring leads. c is not named and
	// comes last.
	if !(halfway["b"] > halfway["a"] && halfway["a"] > halfway["c"]) {
		t.Fatalf("marks are drawn in the wrong order: halfway b is %v, a %v, c %v", halfway["b"], halfway["a"], halfway["c"])
	}
	// A cue marks its shape even though nothing else annotates b.
	if b := shapeOf(t, one, "b").Annotation; b == nil || b.Shape != d2target.AnnotationCircle || b.Label != "" {
		t.Fatalf("cue did not mark b: %+v", b)
	}

	// The next step erases what it does not keep, from the start of each
	// mark, before the new ink arrives.
	erased, seen := 0., false
	for i, frame := range two.TweenFrames {
		a := shapeOf(t, frame, "a")
		if a.Annotation != nil {
			t.Fatalf("frame %d: a still has a current annotation", i)
		}
		if leaving := a.LeavingAnnotation; leaving != nil {
			seen = true
			if leaving.Label != "note" || leaving.Drawn[1] != 1 || leaving.Drawn[0] < erased {
				t.Fatalf("frame %d: a is leaving as %+v", i, leaving)
			}
			erased = leaving.Drawn[0]
		} else if !seen {
			t.Fatalf("frame %d: a's mark vanished without being erased", i)
		}
	}
	if final := shapeOf(t, two.TweenFrames[want-1], "a"); final.LeavingAnnotation != nil {
		t.Fatal("a's mark is still being erased on the last frame")
	}

	// b changed from a cue's ring to a box: the ring leaves and the box arrives.
	first := shapeOf(t, two.TweenFrames[0], "b")
	if first.LeavingAnnotation == nil || first.LeavingAnnotation.Shape != d2target.AnnotationCircle ||
		first.Annotation == nil || first.Annotation.Shape != d2target.AnnotationBox || first.Annotation.Drawn[1] != 0 {
		t.Fatalf("b starts step two as %+v leaving and %+v arriving", first.LeavingAnnotation, first.Annotation)
	}

	// c is declared again unchanged, so its ink never leaves the page.
	for i, frame := range two.TweenFrames {
		c := shapeOf(t, frame, "c")
		if c.LeavingAnnotation != nil || c.Annotation == nil || c.Annotation.Drawn != nil {
			t.Fatalf("frame %d: kept mark on c was redrawn", i)
		}
	}

	// The connection's note arrives with the step.
	if a := two.TweenFrames[0].Connections[0].Annotation; a == nil || a.Drawn == nil || a.Drawn[1] != 0 {
		t.Fatalf("connection starts step two annotated as %+v", a)
	}

	// One caption fades out as the next fades in.
	start, end := two.TweenFrames[0].Root, two.TweenFrames[want-1].Root
	if start.LeavingAnnotation == nil || !strings.Contains(start.LeavingAnnotation.Label, "First") || start.Annotation.CaptionOpacity() != 0 {
		t.Fatalf("step two opens with caption %+v over %+v", start.Annotation, start.LeavingAnnotation)
	}
	if end.LeavingAnnotation != nil || end.Annotation.CaptionOpacity() != 1 {
		t.Fatalf("step two closes with caption %+v over %+v", end.Annotation, end.LeavingAnnotation)
	}
}

func TestCaptionsShareOneLowerThird(t *testing.T) {
	// The second step is much taller than the first.
	d := compileAnnotated(t, `vars: {d2-config: {animate-interval: 1000ms; animate-duration: 200ms}}
a -> b
steps: {
  one: {annotation: Short board.}
  two: {b -> c -> d -> e; annotation: Tall board.}
  three: {a.annotation: no caption here}
}
`)
	var anchor *d2target.Point
	lowest := 0
	for _, board := range d.Steps {
		_, br := board.InkBoundingBox()
		lowest = max(lowest, br.Y)
		for _, frame := range append([]*d2target.Diagram{board}, board.TweenFrames...) {
			for _, caption := range []*d2target.Annotation{frame.Root.Annotation, frame.Root.LeavingAnnotation} {
				if caption == nil {
					continue
				}
				if caption.Anchor == nil {
					t.Fatalf("a caption on %s is not pinned", board.Name)
				}
				if anchor == nil {
					anchor = caption.Anchor
				} else if *anchor != *caption.Anchor {
					t.Fatalf("captions are pinned at %v and %v", *anchor, *caption.Anchor)
				}
			}
		}
	}
	if anchor == nil || anchor.Y != lowest+d2target.CaptionGap {
		t.Fatalf("captions are pinned at %v, want below the lowest ink at %d", anchor, lowest)
	}
}

func TestAnnotatedAnimationKeepsOneCanvas(t *testing.T) {
	// Each step's note sticks out on a different side, and the last step is
	// much wider than the first.
	d := compileAnnotated(t, `vars: {d2-config: {animate-interval: 1000ms; animate-duration: 200ms}}
direction: right
a -> b
steps: {
  one: {a.annotation: a note above {near: top-center}}
  two: {b.annotation: a note below {near: bottom-center}}
  three: {b -> c -> d}
}
`)
	wantTL, wantBR := d.NestedBoundingBox()
	frames := 0
	for _, board := range animationBoards(d) {
		frames++
		if tl, br := board.BoundingBox(); tl != wantTL || br != wantBR {
			t.Fatalf("board %q spans (%v)-(%v), want the shared canvas (%v)-(%v)", board.Name, tl, br, wantTL, wantBR)
		}
	}
	if frames < 4+2*d2target.TweenFrameCount(200) {
		t.Fatalf("checked %d boards and frames, expected the transitions too", frames)
	}

	// Without annotations each board keeps its own bounds, as before.
	plain := compileAnnotated(t, `vars: {d2-config: {animate-interval: 1000ms; animate-duration: 200ms}}
direction: right
a -> b
steps: {
  one: {b.style.opacity: 0.5}
  two: {b -> c -> d}
}
`)
	for _, board := range animationBoards(plain) {
		if board.Canvas != nil {
			t.Fatalf("board %q of an animation without annotations was given a canvas", board.Name)
		}
	}

	// A still export is one board; it stays as tight as its own ink.
	still := compileAnnotated(t, `a -> b
steps: {
  one: {a.annotation: a note above {near: top-center}}
  two: {b -> c -> d}
}
`)
	for _, board := range animationBoards(still) {
		if board.Canvas != nil {
			t.Fatalf("board %q of a still export was given a canvas", board.Name)
		}
	}
}

func TestAnnotationsWithoutAnimation(t *testing.T) {
	// With no interval the boards are separate pictures: each keeps its
	// caption under itself and nothing is sampled.
	d := compileAnnotated(t, `a -> b
annotation: Root.
steps: {
  one: {a.annotation: note; annotation: One.}
}
`)
	one := d.Steps[0]
	if len(one.TweenFrames) != 0 || d.Root.Annotation.Anchor != nil || one.Root.Annotation.Anchor != nil {
		t.Fatalf("static boards were prepared for animation: %d frames", len(one.TweenFrames))
	}
	if a := shapeOf(t, one, "a").Annotation; a == nil || a.Drawn != nil || a.LabelWidth <= 0 || a.LabelHeight <= 0 {
		t.Fatalf("static board's annotation is %+v", a)
	}
	if shapeOf(t, d, "a").Annotation != nil {
		t.Fatal("the root board has the step's annotation")
	}

	// An interval alone switches boards without drawing: marks appear whole.
	d = compileAnnotated(t, `vars: {d2-config: {animate-interval: 1000ms}}
a -> b
steps: {
  one: {a.annotation: note; annotation: One.}
}
`)
	one = d.Steps[0]
	if len(one.TweenFrames) != 0 || one.Root.Annotation.Anchor == nil {
		t.Fatalf("interval-only animation has %d frames and anchor %v", len(one.TweenFrames), one.Root.Annotation.Anchor)
	}
}

// Annotations change on every step. The planners that move shapes between
// steps compare boards shape by shape and must not see them.
func TestAnnotationsLeaveOtherTransitionsAlone(t *testing.T) {
	const timing = "vars: {d2-config: {animate-interval: 1000ms; animate-duration: 400ms}}\n"
	annotated := strings.NewReplacer(
		"  clients: {\n", "  clients: {\n    annotation: |md [Customers](clients) arrive. |\n    platform.api.annotation: entry point\n",
		"  transfer: {\n", "  transfer: {\n    annotation: |md Telemetry moves to [Reliability](reliability). |\n    reliability.telemetry.annotation: moved\n",
		"  archive: {\n", "  archive: {\n    annotation: An archive is added.\n    reliability.telemetry.archive.annotation: {shape: box}\n",
	).Replace(nestedContinuityExample)
	if annotated == nestedContinuityExample {
		t.Fatal("the example no longer has the steps this test annotates")
	}

	plain, _ := compileContinuity(t, timing+nestedContinuityExample, d2talalayout.ContinueBoard)
	marked, _ := compileContinuity(t, timing+annotated, d2talalayout.ContinueBoard)
	if err := d2target.ValidateRenderTarget(marked); err != nil {
		t.Fatal(err)
	}

	strip := func(board *d2target.Diagram) d2target.Diagram {
		bare := *board
		bare.Shapes = append([]d2target.Shape(nil), board.Shapes...)
		bare.Connections = append([]d2target.Connection(nil), board.Connections...)
		for i := range bare.Shapes {
			bare.Shapes[i].Annotation, bare.Shapes[i].LeavingAnnotation = nil, nil
		}
		for i := range bare.Connections {
			bare.Connections[i].Annotation, bare.Connections[i].LeavingAnnotation = nil, nil
		}
		bare.Root.Annotation, bare.Root.LeavingAnnotation = nil, nil
		// The shared canvas is there for the notes, like the notes themselves.
		bare.Canvas = nil
		// The sampling is compared on its own below.
		bare.Steps, bare.TweenFrames, bare.TweenFrameEndsMS, bare.TweenDurationMS = nil, nil, nil, 0
		return bare
	}
	sampled, protected := false, 0
	for i, want := range plain.Steps {
		got := marked.Steps[i]
		if !reflect.DeepEqual(strip(got), strip(want)) {
			t.Fatalf("step %s: annotations changed the board beneath them", want.Name)
		}
		if len(want.TweenFrames) == 0 {
			// With nothing else to animate, the marks bring their own frames.
			if len(got.TweenFrames) != d2target.TweenFrameCount(400) {
				t.Fatalf("step %s: marks alone made %d frames", want.Name, len(got.TweenFrames))
			}
			continue
		}
		protected++
		if len(got.TweenFrames) != len(want.TweenFrames) || !reflect.DeepEqual(got.TweenFrameEndsMS, want.TweenFrameEndsMS) || got.TweenDurationMS != want.TweenDurationMS {
			t.Fatalf("step %s: annotations changed the sampling from %d frames to %d", want.Name, len(want.TweenFrames), len(got.TweenFrames))
		}
		for j := range want.TweenFrames {
			if !reflect.DeepEqual(strip(got.TweenFrames[j]), strip(want.TweenFrames[j])) {
				t.Fatalf("step %s frame %d: annotations changed the transition beneath them", want.Name, j)
			}
			for k := range got.TweenFrames[j].Shapes {
				a := got.TweenFrames[j].Shapes[k].Annotation
				sampled = sampled || a != nil && a.Drawn != nil && a.Drawn[1] > 0 && a.Drawn[1] < 1
			}
		}
	}
	if protected == 0 {
		t.Fatal("the example no longer has a transition for annotations to disturb")
	}
	if !sampled {
		t.Fatal("no mark was drawn on the frames of the existing transitions")
	}
}

func TestCaptionPenColorsWhatTheCaptionDraws(t *testing.T) {
	d := compileAnnotated(t, `a -> b -> c
b.annotation: mine {style.stroke: green}
annotation: |md From [a](a) through [b](b) to [c](c). | {style.stroke: "#1971C2"}
`)
	if got := d.Root.Annotation; got == nil || got.Stroke != "#1971C2" || got.CaptionPaint("N1") != "#1971C2" {
		t.Fatalf("caption exported as %+v", got)
	}
	// The rings the caption draws take its pen. A shape with its own
	// annotation keeps its own.
	for id, want := range map[string]string{"a": "#1971C2", "b": "green", "c": "#1971C2"} {
		if got := shapeOf(t, d, id).Annotation; got == nil || got.GetStroke() != want {
			t.Errorf("%s is marked with %+v, want the pen %s", id, got, want)
		}
	}

	// Without a pen the caption is set in the board's text color and its
	// rings use the default pen.
	plain := compileAnnotated(t, `a -> b
annotation: |md Only [a](a). |
`)
	if got := plain.Root.Annotation.CaptionPaint("N1"); got != "N1" {
		t.Errorf("caption without a pen is painted %q", got)
	}
	if got := shapeOf(t, plain, "a").Annotation.GetStroke(); got != d2target.DefaultAnnotationStroke {
		t.Errorf("cue ring without a pen is painted %q", got)
	}
}

func TestAnnotationDefaultMarks(t *testing.T) {
	d := compileAnnotated(t, `box: {
  inner
  inner.annotation: a leaf
}
box.annotation: a container
words: |md Some *text*. |
words.annotation: bare text
chosen.annotation: explicit {shape: cross; near: top-center; style.stroke: blue}
box.inner -> chosen: {annotation: a connection}
chosen -> words: {annotation.shape: cross}
`)
	for id, want := range map[string]string{
		"box.inner": d2target.AnnotationCircle,
		"box":       d2target.AnnotationBrace,
		"words":     d2target.AnnotationUnderline,
		"chosen":    d2target.AnnotationCross,
	} {
		if got := shapeOf(t, d, id).Annotation; got == nil || got.Shape != want {
			t.Errorf("%s is marked with %+v, want a %s", id, got, want)
		}
	}
	chosen := shapeOf(t, d, "chosen").Annotation
	if chosen.Position != "top-center" || chosen.Stroke != "blue" || chosen.GetStroke() != "blue" {
		t.Errorf("explicit annotation exported as %+v", chosen)
	}
	if got := shapeOf(t, d, "box").Annotation.GetStroke(); got != d2target.DefaultAnnotationStroke {
		t.Errorf("default pen is %q", got)
	}
	if got := d.Connections[0].Annotation; got == nil || got.Shape != d2target.AnnotationTrace || got.LabelWidth <= 0 {
		t.Errorf("connection is marked with %+v, want a measured trace", got)
	}
	if got := d.Connections[1].Annotation; got == nil || got.Shape != d2target.AnnotationCross {
		t.Errorf("connection is marked with %+v, want a cross", got)
	}
}

func TestLassoGathersWhatOneDeclarationMarks(t *testing.T) {
	d := compileAnnotated(t, `classes: {
  unit: {
    annotation: deployed together {shape: lasso}
  }
}
a.class: unit
b
c.class: unit
d.class: unit
alone.annotation: by itself {shape: lasso}
s1.shape: cylinder
s2.shape: cylinder
*: {
  &shape: cylinder
  annotation.shape: lasso
}
`)
	// The first shape a class or a glob gives the lasso to carries it for all
	// of them. A lasso declared on one shape holds that shape.
	for id, members := range map[string][]string{"a": {"c", "d"}, "alone": nil, "s1": {"s2"}} {
		got := shapeOf(t, d, id).Annotation
		if got == nil || got.Shape != d2target.AnnotationLasso || !reflect.DeepEqual(got.Members, members) {
			t.Fatalf("%s carries %+v, want a lasso holding %v", id, got, members)
		}
	}
	for _, id := range []string{"b", "c", "d", "s2"} {
		if got := shapeOf(t, d, id).Annotation; got != nil {
			t.Fatalf("%s has an annotation of its own: %+v", id, got)
		}
	}
	if a := shapeOf(t, d, "a").Annotation; a.Label != "deployed together" {
		t.Fatalf("the lasso's note is %q", a.Label)
	}
	marks := 0
	for _, m := range d.AnnotationMarks() {
		marks++
		if len(m.Strokes) != 1 {
			t.Fatalf("%s is %d strokes, want one loop", m.ID, len(m.Strokes))
		}
	}
	if marks != 3 {
		t.Fatalf("got %d loops, want 3", marks)
	}
}
