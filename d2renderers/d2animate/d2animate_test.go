package d2animate

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestWrapRejectsTimelineOverflow(t *testing.T) {
	boards := [][]byte{[]byte("<g/>"), []byte("<g/>")}
	if _, err := Wrap(d2target.NewDiagram(), boards, d2svg.RenderOpts{}, math.MaxInt); err == nil {
		t.Fatal("expected the two-board animation duration to overflow")
	}
}

func TestWrapDefaultsMissingPadding(t *testing.T) {
	diagram := d2target.NewDiagram()
	out, err := Wrap(diagram, [][]byte{[]byte(`<g></g>`)}, d2svg.RenderOpts{}, 1_000)
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	if !strings.Contains(string(out), `viewBox="0 0 200 200"`) {
		t.Fatalf("Wrap() did not use default padding: %q", out)
	}
}

func TestWrapRejectsMalformedTargetBeforeTraversal(t *testing.T) {
	diagram := d2target.NewDiagram()
	diagram.Layers = []*d2target.Diagram{nil}

	if _, err := Wrap(diagram, [][]byte{[]byte(`<g></g>`)}, d2svg.RenderOpts{}, 1_000); err == nil || !strings.Contains(err.Error(), "root.layers[0] is nil") {
		t.Fatalf("Wrap() error = %v, want malformed-target error", err)
	}
}

func TestTimelineSampleBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		interval, duration, samples int
		stepDelays                  []int
	}{
		{"shorter than interval", 400, 101, 3, []int{33, 34, 34, 299}},
		{"same as interval", 101, 101, 3, []int{33, 34, 17, 17}},
		{"longer than interval", 30, 101, 3, []int{33, 34, 17, 17}},
		{"one millisecond", 1, 1, 1, []int{1}},
		{"without transition", 35, 0, 0, []int{35}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, step := d2target.NewDiagram(), d2target.NewDiagram()
			root.Steps = []*d2target.Diagram{step}
			for i := 0; i < tc.samples; i++ {
				step.TweenFrames = append(step.TweenFrames, d2target.NewDiagram())
			}
			step.TweenDurationMS = tc.duration
			frames, err := Timeline(root, tc.interval)
			if err != nil {
				t.Fatal(err)
			}
			if frames[0].Diagram != root || frames[0].DurationMS != tc.interval || frames[0].ElapsedMS != 0 {
				t.Fatalf("unexpected initial board: %+v", frames[0])
			}
			var delays []int
			elapsed := 0
			for i, frame := range frames[1:] {
				if frame.ElapsedMS != elapsed {
					t.Fatalf("frame %d starts at %dms, want %dms", i, frame.ElapsedMS, elapsed)
				}
				if i < len(frames)-2 && (!frame.TweenSample || frame.Diagram != step.TweenFrames[i]) {
					t.Fatalf("expected step sample %d", i)
				}
				delays = append(delays, frame.DurationMS)
				elapsed += frame.DurationMS
			}
			if !slices.Equal(delays, tc.stepDelays) {
				t.Fatalf("step delays %v, want %v", delays, tc.stepDelays)
			}
			last := frames[len(frames)-1]
			if last.Diagram != step || last.TweenSample || last.DurationMS < 1 {
				t.Fatal("final frame must display the exact authored endpoint")
			}
		})
	}
}
