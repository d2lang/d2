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

func TestWrapTimedClipsBetweenSamples(t *testing.T) {
	frames := [][]byte{[]byte("<g></g>"), []byte("<g></g>"), []byte("<g></g>")}
	out, err := WrapTimed(d2target.NewDiagram(), frames, d2svg.RenderOpts{}, []int{7, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range []string{
		"-0{0%{clip-path:none}70%,100%{clip-path:inset(50%)}}",
		"-1{0%{clip-path:inset(50%)}70%{clip-path:none}80%,100%{clip-path:inset(50%)}}",
		"-2{0%{clip-path:inset(50%)}80%{clip-path:none}100%{clip-path:none}}",
	} {
		if !strings.Contains(string(out), rule) {
			t.Errorf("missing frame boundary %q", rule)
		}
	}
	if n := strings.Count(string(out), "10ms steps(1,end) infinite"); n != len(frames) {
		t.Errorf("got %d discrete animations for %d frames", n, len(frames))
	}
}

func TestShortSVGSwitchesOnlyAtBoardBoundaries(t *testing.T) {
	frames := [][]byte{[]byte("<g></g>"), []byte("<g></g>")}
	out, err := Wrap(d2target.NewDiagram(), frames, d2svg.RenderOpts{}, 40)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-0{0%{opacity:1}50%,100%{opacity:0}}",
		"-1{0%{opacity:0}50%{opacity:1}100%{opacity:1}}",
		"80ms steps(1,end) infinite",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("missing frame boundary %q", want)
		}
	}
}

func TestHiddenFrameInteraction(t *testing.T) {
	link := []byte(`<g><a href="https://example.org"><rect/></a></g>`)
	tooltip := []byte(`<g><rect/><title>Next stage</title></g>`)
	for _, tc := range []struct {
		name      string
		durations []int
		frames    [][]byte
		gate      string
	}{
		{"linked board followed by a plain board", []int{1000, 1000}, [][]byte{link, []byte("<g/>")}, "opacity:0"},
		{"links in sampled frames", []int{1000, 25}, [][]byte{link, link}, "clip-path:inset(50%)"},
		{"tooltip without a link", []int{1000, 1000}, [][]byte{tooltip, tooltip}, "opacity:0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := WrapTimed(d2target.NewDiagram(), tc.frames, d2svg.RenderOpts{}, tc.durations)
			if err != nil {
				t.Fatal(err)
			}
			style := string(out)
			for _, rule := range []string{
				" a{visibility:var(--d2-link-visibility,inherit)}",
				tc.gate + ";pointer-events:none;--d2-link-visibility:hidden",
				";pointer-events:inherit;--d2-link-visibility:initial",
			} {
				if !strings.Contains(style, rule) {
					t.Errorf("missing frame interaction rule %q", rule)
				}
			}
		})
	}
}

func TestWrapDefaultsMissingPadding(t *testing.T) {
	diagram := d2target.NewDiagram()
	out, err := Wrap(diagram, [][]byte{[]byte(`<g></g>`)}, d2svg.RenderOpts{}, 1_000)
	if err != nil {
		t.Fatalf("Wrap() error = %v", err)
	}
	if !strings.Contains(string(out), `viewBox="-100 -100 200 200"`) {
		t.Fatalf("Wrap() did not use default padding: %q", out)
	}
}

func TestWrapSVGDocumentOptions(t *testing.T) {
	on := true
	for _, tc := range []struct {
		name                 string
		opts                 d2svg.RenderOpts
		xml, version, center bool
	}{
		{"defaults", d2svg.RenderOpts{}, true, true, false},
		{"center", d2svg.RenderOpts{Center: &on}, true, true, true},
		{"no XML declaration", d2svg.RenderOpts{NoXMLTag: &on}, false, true, false},
		{"omit version", d2svg.RenderOpts{OmitVersion: &on}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := Wrap(d2target.NewDiagram(), [][]byte{[]byte("<g/>"), []byte("<g/>")}, tc.opts, 1000)
			if err != nil {
				t.Fatal(err)
			}
			start, _, _ := strings.Cut(string(out), `><svg class="d2-svg"`)
			if strings.HasPrefix(start, "<?xml") != tc.xml || strings.Contains(start, ` d2Version="`) != tc.version ||
				strings.Contains(start, `preserveAspectRatio="xMidYMid meet"`) != tc.center ||
				strings.Contains(start, `preserveAspectRatio="xMinYMin meet"`) == tc.center {
				t.Errorf("unexpected SVG envelope: %s", start)
			}
		})
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

func TestTimelineWithExplicitSampleTimes(t *testing.T) {
	root := d2target.NewDiagram()
	root.TweenDurationMS = 1000
	root.TweenFrames = []*d2target.Diagram{d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()}
	root.TweenFrameEndsMS = []int{17, 140, 220}
	frames, err := Timeline(root, 1400)
	if err != nil {
		t.Fatal(err)
	}
	var starts, delays []int
	for _, f := range frames {
		starts = append(starts, f.ElapsedMS)
		delays = append(delays, f.DurationMS)
	}
	if !slices.Equal(starts, []int{0, 17, 140, 220}) || !slices.Equal(delays, []int{17, 123, 80, 1180}) {
		t.Fatalf("starts %v, durations %v", starts, delays)
	}
}
