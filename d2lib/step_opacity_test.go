package d2lib

import (
	"context"
	"math"
	"testing"

	"github.com/d2lang/d2/d2target"
	d2log "github.com/d2lang/d2/lib/log"
)

func TestImplicitStepOpacity(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 64ms}; alpha: 0.4}
classes: {
  bright: {style.opacity: 0.8}
  muted: {style.opacity: ${alpha}}
}
plain
a.style.opacity: 0.8
card: {class: [bright; muted]; style.opacity: 0.2}
alias: {style.opacity: ${alpha}}
a -> b
a -> b: {style.opacity: 0.6}
a -> b: {style.opacity: 0.95}
removed: {style.opacity: 0.2}
steps: {
  fade: {
    plain.style.opacity: 0
    a.style.opacity: 0
    (a -> b)[2].style.opacity: 0
    card.style.opacity: null
    vars: {alpha: 0.6}
    (a -> b)[0].style.opacity: 0.5
    (a -> b)[1].style.opacity: null
    removed: null
    added.style.opacity: 0.3
  }
  restore: {
    plain.style: null
    a.style.opacity: 0.8
    (a -> b)[2].style.opacity: 0.95
    card.class: bright
    (a -> b)[0].style: null
  }
}`
	calls := 0
	board, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, step := range board.Steps {
		if len(step.TweenFrames) != 4 {
			t.Fatalf("step %d has %d paint samples", i, len(step.TweenFrames))
		}
		for j, frame := range append(append([]*d2target.Diagram(nil), step.TweenFrames...), step) {
			check := func(name string, value, start, end float64) {
				t.Helper()
				fraction := float64(j) / 4
				if start == 0 || end == 0 {
					fraction = [...]float64{0, .0625, .5, .9375, 1}[j]
				}
				want := (1-fraction)*start + fraction*end
				if math.Abs(value-want) > 1e-12 {
					t.Errorf("%s, sample %d of step %s: opacity %g, want %g", name, j, step.Name, value, want)
				}
			}
			plain := shapeByID(t, frame, "plain").Opacity
			card := shapeByID(t, frame, "card").Opacity
			alias := shapeByID(t, frame, "alias").Opacity
			if len(frame.Connections) != 3 {
				t.Fatalf("step %s sample %d: got %d connections; root %d, authored %d", step.Name, j, len(frame.Connections), len(board.Connections), len(step.Connections))
			}
			first, second := frame.Connections[0].Opacity, frame.Connections[1].Opacity
			if i == 0 {
				check("implicit shape", plain, 1, 0)
				check("hide explicit shape", shapeByID(t, frame, "a").Opacity, .8, 0)
				check("hide explicit edge", frame.Connections[2].Opacity, .95, 0)
				check("reset to inherited variable", card, 0.2, 0.6)
				check("unchanged binding", alias, 0.4, 0.6)
				check("implicit edge", first, 1, 0.5)
				check("reset edge", second, 0.6, 1)
			} else {
				check("reset shape style", plain, 0, 1)
				check("show explicit shape", shapeByID(t, frame, "a").Opacity, 0, .8)
				check("show explicit edge", frame.Connections[2].Opacity, 0, .95)
				check("changed class", card, 0.6, 0.8)
				check("unchanged alias", alias, 0.6, 0.6)
				check("reset edge style", first, 0.5, 1)
				check("unchanged parallel edge", second, 1, 1)
			}
			check("newly authored shape", shapeByID(t, frame, "added").Opacity, 0.3, 0.3)
			for _, s := range frame.Shapes {
				if s.ID == "removed" {
					t.Fatal("paint sample resurrected a removed shape")
				}
			}
		}
	}
}

func TestOpacityMembershipDoesNotCreatePaint(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
a: {style.opacity: 0.2}
steps: {swap: {a: null; b.style.opacity: 0.8}; same: {b.style.opacity: 0.80}}`
	calls := 0
	board, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range board.Steps {
		if len(step.TweenFrames) != 0 {
			t.Fatalf("step %s unexpectedly produced %d paint samples", step.Name, len(step.TweenFrames))
		}
	}
}

func TestImplicitOpacityAfterUnrelatedLayer(t *testing.T) {
	const source = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
a
layers: {alternate: {a.style.opacity: 0.15}}
steps: {hide: {a.style.opacity: 0}}`
	calls := 0
	d, _, err := Compile(d2log.WithDefault(context.Background()), source, dagreCompileOptions(&calls), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Steps[0].TweenFrames) != 0 || shapeByID(t, d.Steps[0], "a").Opacity != 0 {
		t.Fatal("an opacity animation restarted from an offscreen board")
	}
}

func TestOpacityAfterParallelEdgeDeletion(t *testing.T) {
	for _, add := range []string{"", "; a -> b: newest {style.opacity: 0.9}"} {
		const start = `vars: {d2-config: {animate-interval: 200ms; animate-duration: 160ms}}
a -> b: reliable {style.opacity: 0.24}
a -> b: debug {style.opacity: 0.65}
steps: {remove: {(a -> b)[0]: null`
		calls := 0
		board, _, err := Compile(d2log.WithDefault(context.Background()), start+add+"}}", dagreCompileOptions(&calls), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range board.Steps[0].TweenFrames {
			for _, e := range f.Connections {
				switch e.Label {
				case "debug":
					if e.Opacity != .65 {
						t.Errorf("survivor adopted deleted edge opacity %g", e.Opacity)
					}
				case "newest":
					if e.Opacity != .9 {
						t.Errorf("new edge adopted prior edge opacity %g", e.Opacity)
					}
				default:
					t.Fatalf("unexpected edge %s", e.Label)
				}
			}
		}
	}
}
