package d2lib

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

const sequenceStepsConfig = `vars: {d2-config: {animate-interval: 1000ms; animate-duration: 700ms}}
shape: sequence_diagram
`

func TestSequenceAppendedRequestAndReturn(t *testing.T) {
	board, _ := compileContinuity(t, sequenceStepsConfig+`a
b
c: C {style.fill: "#EAEAEA"}
a -> b.job: Ask
steps: {
  send: {b.job -> c: Fetch}
  reply: {b.job <- c: Found {style.stroke-dash: 4}}
  self: {c -> c: Log}
}`, d2talalayout.ContinueBoard)
	prev := board
	for i, next := range board.Steps {
		if len(next.TweenFrames) < 3 {
			t.Fatalf("step %d has no trace", i)
		}
		if !next.LayoutContinuity {
			t.Fatalf("step %d fits its raster frames independently of the preceding exchange", i)
		}
		if !reflect.DeepEqual(prev.Shapes, next.TweenFrames[0].Shapes) || !reflect.DeepEqual(prev.Connections, next.TweenFrames[0].Connections) {
			t.Fatalf("step %d changed starting board", i)
		}
		names := []string{"Fetch", "Found", "Log"}
		j := slices.IndexFunc(next.Connections, func(e d2target.Connection) bool { return e.Label == names[i] })
		e := next.Connections[j]
		full := e.GetLabelTopLeft()
		want := d2target.Point{X: int(math.Round(full.X)), Y: int(math.Round(full.Y))}
		lastLength, lastAlpha := 0., 0.
		partial, faded, growingLine, growingBar := false, false, false, false
		oldBar, endBar := shapeByID(t, prev, "b.job"), shapeByID(t, next, "b.job")
		oldLine := continuityWire(t, prev, "a", "a-lifeline-end-2251863791")
		endLine := continuityWire(t, next, oldLine.Src, oldLine.Dst)
		for _, sample := range next.TweenFrames {
			h := shapeByID(t, sample, "b.job").Height
			growingBar = growingBar || h > oldBar.Height && h < endBar.Height
			line := continuityWire(t, sample, oldLine.Src, oldLine.Dst)
			y := line.Route[1].Y
			growingLine = growingLine || y > oldLine.Route[1].Y && y < endLine.Route[1].Y
			if !reflect.DeepEqual(continuityWire(t, prev, "a", "b.job"), continuityWire(t, sample, "a", "b.job")) {
				t.Fatal("prior message changed")
			}
			for _, wire := range sample.Connections {
				if wire.ID != e.ID {
					continue
				}
				length := 0.
				for k := 1; k < len(wire.Route); k++ {
					length += math.Hypot(wire.Route[k].X-wire.Route[k-1].X, wire.Route[k].Y-wire.Route[k-1].Y)
				}
				if length+1e-5 < lastLength {
					t.Fatalf("%s shrank from %v to %v", names[i], lastLength, length)
				}
				lastLength = length
				if reflect.DeepEqual(wire.Route, e.Route) {
					continue
				}
				partial = true
				if i != 1 && *wire.Route[0] != *e.Route[0] || i == 1 && *wire.Route[len(wire.Route)-1] != *e.Route[len(e.Route)-1] {
					t.Fatalf("trace %q detached from sender", wire.Label)
				}
				if wire.SrcArrow != e.SrcArrow || wire.DstArrow != e.DstArrow || wire.StrokeDash != e.StrokeDash {
					t.Fatalf("trace %q changed arrowhead or dashes", wire.Label)
				}
				if wire.MotionLabel == nil || wire.MotionLabel.Point != want || wire.MotionLabel.Opacity < lastAlpha {
					t.Fatal("caption moved or faded backwards")
				}
				lastAlpha = wire.MotionLabel.Opacity
				if lastAlpha > 0 && lastAlpha < 1 && !faded {
					faded = true
					raw, err := json.Marshal(sample)
					if err != nil {
						t.Fatal(err)
					}
					var restored d2target.Diagram
					if err := json.Unmarshal(raw, &restored); err != nil {
						t.Fatal(err)
					}
					if err := d2target.ValidateRenderTarget(&restored); err != nil {
						t.Fatal(err)
					}
					actual, err := d2svg.Render(&restored, nil)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Contains(actual, []byte(">"+wire.Label+"<")) {
						t.Fatalf("rendered caption %q missing", wire.Label)
					}
				}
			}
		}
		if !partial || !faded || !growingLine || (endBar.Height > oldBar.Height && !growingBar) {
			t.Fatalf("%s partial %v caption %v lifeline %v span %v", names[i], partial, faded, growingLine, growingBar)
		}
		prev = next
	}
}

func TestSequenceRevealsPredeclaredMessagesInOrder(t *testing.T) {
	d, _ := compileContinuity(t, sequenceStepsConfig+`a -> b: Ask
b -> a: First {style.opacity: 0}
b -> a: Second {style.opacity: 0}
steps: {replies: {
 (b -> a)[0].style.opacity: 1
 (b -> a)[1].style.opacity: 0.6
}}`, d2talalayout.ContinueBoard)
	n := d.Steps[0]
	startedSecond, seenFirst := false, false
	for i, s := range n.TweenFrames[1:] {
		var firstDrawn bool
		for _, e := range s.Connections {
			switch e.Label {
			case "First":
				seenFirst = seenFirst || e.MotionLabel != nil
				firstDrawn = e.MotionLabel == nil
			case "Second":
				if e.Opacity == 0 {
					continue
				}
				if e.Opacity != .6 || !firstDrawn {
					t.Fatalf("frame %d has second reply before first landed or wrong authored opacity", i)
				}
				startedSecond = startedSecond || e.MotionLabel != nil
			}
		}
	}
	if !seenFirst || !startedSecond {
		t.Fatal("both messages should trace separately")
	}
}

func TestExplicitSequenceStepsKeepTheirOwnTimeline(t *testing.T) {
	// The last message moves from second to eighth on the standalone
	// timeline. In early transition frames most of those edges don't exist.
	source := "a -> b: Ask\n" + strings.Repeat("b -> a: Reply {style.opacity: 0}\n", 6) +
		"a -> a: Finish\nsteps: {replies: {(b -> a)[*].style.opacity: 1}}"
	plain, _ := compileContinuity(t, sequenceStepsConfig+source, d2talalayout.ContinueBoard)
	animated, _ := compileContinuity(t, sequenceStepsConfig+"animated: true\n"+source, d2talalayout.ContinueBoard)
	step := animated.Steps[0]
	if animated.SequencePlaybackDurationMS() != 3000 || step.SequencePlaybackDurationMS() != 9000 {
		t.Fatal("the authored boards lost their standalone timelines")
	}
	if len(step.TweenFrames) == 0 || !reflect.DeepEqual(plain.Steps[0].TweenFrames, step.TweenFrames) {
		t.Fatal("standalone playback changed the explicit transition")
	}
	if err := d2target.ValidateRenderTarget(animated); err != nil {
		t.Fatal(err)
	}
}

func TestSequenceFallbackOnReflow(t *testing.T) {
	d, g := compileContinuity(t, sequenceStepsConfig+`a -> b: Ask
steps: {reply: {b -> a: That extraordinarily long response forces the previous message to move}}`, d2talalayout.ContinueBoard)
	if planSequenceTrace(d, d.Steps[0], g, g.Steps[0]) != nil {
		t.Fatal("prior message moved; expected existing fallback")
	}
}

func TestSelfMessageTracePointsAroundItsBends(t *testing.T) {
	for _, arrow := range []string{"->", "<-"} {
		t.Run(arrow, func(t *testing.T) {
			const config = "vars: {d2-config: {animate-interval: 1800ms; animate-duration: 1200ms}}\nshape: sequence_diagram\n"
			message := "agent " + arrow + " agent"
			board, _ := compileContinuity(t, config+"agent -> peer: Join\n"+message+": Retry {style.opacity: 0}\nsteps: {send: {("+message+")[0].style.opacity: 1}}", d2talalayout.ContinueBoard)
			bends := 0
			for _, frame := range board.Steps[0].TweenFrames {
				for _, e := range frame.Connections {
					if e.Label != "Retry" || e.Opacity == 0 || len(e.Route) < 3 {
						continue
					}
					actor := shapeByID(t, frame, "agent")
					src, dst := e.ArrowheadAdjustmentsForWidths(actor.RenderedStrokeWidth(), actor.RenderedStrokeWidth())
					commands := slices.Collect(e.PathCommands(src, dst))
					i := len(e.Route) - 1
					raw := e.Route[i-1].VectorTo(e.Route[i])
					j := len(commands) - 1
					tangent := commands[j-1].End.VectorTo(&commands[j].End)
					if arrow == "<-" {
						raw = e.Route[0].VectorTo(e.Route[1])
						tangent = commands[0].End.VectorTo(&commands[1].End)
					}
					if raw.Length() < e.BorderRadius {
						bends++
						if raw[0]*tangent[0]+raw[1]*tangent[1] <= 0 {
							t.Fatalf("message head reverses at the bend: route %v, shaft tangent %v", raw, tangent)
						}
					}
				}
			}
			if bends == 0 {
				t.Fatal("expected a sampled tip turning a corner")
			}
		})
	}
}
