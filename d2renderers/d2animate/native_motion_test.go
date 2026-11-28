package d2animate

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/animation"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func TestNativeMotionUsesOneShapeAndTheAuthoredClock(t *testing.T) {
	var frames []Frame
	var fragments [][]byte
	for i, x := range []int{0, 0, 0, 1, 1, 9} {
		board := d2target.NewDiagram()
		s := d2target.Shape{ID: "a", Pos: d2target.Point{X: x}}
		tx := ""
		if i == 1 || i == 2 {
			s.MotionOffset = geo.NewPoint(0, 0)
		}
		if i == 2 {
			s.MotionOffset.X = 0.5
			tx = ` transform="translate(0.5 0)"`
		}
		board.Shapes = []d2target.Shape{s}
		f := Frame{Diagram: board, DurationMS: 10, TweenSample: i == 1 || i == 2}
		if i == 2 {
			f.start = frameStart{fraction: time.Millisecond / 2}
		}
		frames = append(frames, f)
		fragments = append(fragments, []byte(fmt.Sprintf(`<g class="d2-test "><rect x="0" y="0"/><g class="YQ=="%s><g class="shape"><rect x="%d" y="0" width="10" height="5"/></g><text x="%d" y="3">A</text></g></g>`, tx, x, x+2)))
	}
	art, css, frameClock := nativeMotion(frames, fragments, "d2-test", 60)
	if frameClock || bytes.Count(art, []byte(`class="YQ=="`)) != 1 || bytes.Count(art, []byte(">A</text>")) != 1 {
		t.Fatalf("unexpected artwork %s", art)
	}
	for _, want := range []string{
		"16.666666667%{transform:translate(0px,0px)}",
		"34.166666667%{transform:translate(0.5px,0px)}",
		"50%{transform:translate(1px,0px);animation-timing-function:steps(1,end)}",
		"66.666666667%{transform:translate(1px,0px);animation-timing-function:steps(1,end)}",
		"@media(prefers-reduced-motion:reduce)",
	} {
		if !bytes.Contains(css, []byte(want)) {
			t.Errorf("missing %s in %s", want, css)
		}
	}
}

func TestNativeMotionKeepsLegacyPositionJumpsAndNamedPaint(t *testing.T) {
	var frames []Frame
	var fragments [][]byte
	for i, paint := range []string{"#000000", "#808080", "#ffffff"} {
		board := d2target.NewDiagram()
		board.Shapes = []d2target.Shape{{ID: "a", Pos: d2target.Point{X: i}}}
		frames = append(frames, Frame{Diagram: board, TweenSample: i < 2, DurationMS: 10})
		fragments = append(fragments, []byte(fmt.Sprintf(`<g class="d2-test"><g class="YQ==" style="opacity:0.%d"><rect x="%d" y="0" fill="%s" class="stroke-B1" stroke="#123456"/></g></g>`, i+1, i, paint)))
	}
	art, css, _ := nativeMotion(frames, fragments, "d2-test", 30)
	if art == nil {
		t.Fatal("expected native paint track")
	}
	for _, want := range []string{"0%{transform:translate(0px,0px);animation-timing-function:steps(1,end)}", "33.333333333%{opacity:0.2}", "33.333333333%{fill:#808080}"} {
		if !bytes.Contains(css, []byte(want)) {
			t.Errorf("missing %s in %s", want, css)
		}
	}
	if bytes.Contains(css, []byte("stroke:")) {
		t.Fatal("unexpected override of adaptive theme stroke")
	}
}

func TestNativeMotionIsolatesChangingArtworkAndResourceOrder(t *testing.T) {
	var frames []Frame
	for _, x := range []int{0, 1} {
		board := d2target.NewDiagram()
		board.Shapes = []d2target.Shape{{ID: "a", Pos: d2target.Point{X: x}, MotionOffset: geo.NewPoint(0, 0)}}
		frames = append(frames, Frame{Diagram: board, TweenSample: x == 0, DurationMS: 10})
	}
	frame := func(x int, before, after string) []byte {
		return []byte(fmt.Sprintf(`<g class="d2-test">%s<g class="YQ=="><rect x="%d" y="0" width="10" height="4"/></g>%s</g>`, before, x, after))
	}
	mask := `<mask id="d2-123"><rect x="0" y="0" fill="white"/></mask>`
	changing := `<g><path d="M0 0L10 0" mask="url(#d2-123)"/></g>`
	out, _, clock := nativeMotion(frames, [][]byte{frame(0, mask, changing), frame(1, mask, `<ellipse cx="3" cy="2"/>`)}, "d2-test", 20)
	if !clock || !bytes.Contains(out, []byte("d2Motion-")) || bytes.Count(out, []byte("d2Transition-")) != 2 {
		t.Fatalf("mixed scene %s", out)
	}
	for name, bad := range map[string][]byte{
		"duplicate resource moved across paint slots": frame(1, "", mask+changing),
		"local animation":      frame(1, "", `<rect><animate attributeName="x"/></rect>`),
		"nested SVG":           frame(1, "", `<svg><rect/></svg>`),
		"authored style":       frame(1, "", `<style>.shape{opacity:0}</style>`),
		"link":                 frame(1, "", `<a href="dest"><rect/></a>`),
		"unsupported geometry": []byte(strings.Replace(string(frame(1, mask, "")), `width="10"`, `width="11"`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if out, _, _ := nativeMotion(frames, [][]byte{frame(0, mask, changing), bad}, "d2-test", 20); out != nil {
				t.Fatalf("expected original frame rendering, got %s", out)
			}
		})
	}
}

func TestNativeStrokeWidthKeepsFractionalInk(t *testing.T) {
	var frames []Frame
	var parts [][]byte
	for i, w := range []string{"1", "1.25", "1.5"} {
		board := d2target.NewDiagram()
		board.Shapes = []d2target.Shape{{ID: "s"}}
		frames = append(frames, Frame{Diagram: board, TweenSample: i != 2, DurationMS: 10})
		parts = append(parts, []byte(`<g class="d2-test"><g class="cw=="><rect x="0" y="0" style="stroke-width:`+w+`;stroke-dasharray:2 2"/></g></g>`))
	}
	out, css, clock := nativeMotion(frames, parts, "d2-test", 30)
	if clock || bytes.Count(out, []byte("<rect ")) != 1 || !bytes.Contains(css, []byte("stroke-width:1.25px")) || !bytes.Contains(out, []byte("stroke-dasharray:2 2")) {
		t.Fatalf("fractional stroke not preserved in native playback: %s %s", out, css)
	}
}

func TestNativeCubicTranslation(t *testing.T) {
	full := make([]float64, 61)
	for i := range full {
		full[i] = float64(i) / 60
	}
	for _, tc := range []struct {
		name                      string
		times                     []float64
		arrival                   int
		unverified, perturb, want bool
	}{
		{"full", full, 1000, false, false, true},
		{"projected", []float64{0, .25, .5, .75, 1}, 1000, false, false, true},
		{"nonuniform", []float64{0, .2, .5, .8, 1}, 1000, false, false, true},
		{"missing midpoint", []float64{0, .2, .4, .6, .8, 1}, 1000, false, false, false},
		{"missing first half", []float64{0, .5, .6, .8, 1}, 1000, false, false, false},
		{"short final period", full, 992, false, false, false},
		{"nonlinear deviation", full, 1000, false, true, false},
		{"unverified integer samples", full, 1000, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var frames []Frame
			var imgs [][]byte
			for i, f := range tc.times {
				u := animation.EaseInOut(f)
				x, y := -2-5*u, 3+9*u
				if tc.perturb && i == len(tc.times)/3 {
					y += .01
				}
				s := d2target.Shape{ID: "a", Pos: d2target.Point{X: int(math.Round(x)), Y: int(math.Round(y))}}
				if !tc.unverified && i < len(tc.times)-1 {
					s.MotionOffset = geo.NewPoint(x-float64(s.Pos.X), y-float64(s.Pos.Y))
				}
				board := d2target.NewDiagram()
				board.Shapes = []d2target.Shape{s}
				frame := Frame{Diagram: board}
				offset := ""
				if s.MotionOffset != nil {
					offset = ` transform="translate(` + svg.FormatFloat(s.MotionOffset.X) + " " + svg.FormatFloat(s.MotionOffset.Y) + `)"`
				}
				if i+1 < len(tc.times) {
					frame.TweenSample = true
					start := time.Duration(f * 1000 * float64(time.Millisecond))
					end := time.Duration(tc.times[i+1] * 1000 * float64(time.Millisecond))
					if i+2 == len(tc.times) {
						end = time.Duration(tc.arrival) * time.Millisecond
					}
					frame.DurationMS = int(end/time.Millisecond - start/time.Millisecond)
					frame.start = frameStart{fraction: start % time.Millisecond}
				} else {
					board.TweenDurationMS = 1000
					frame.DurationMS = 1100 - tc.arrival
				}
				frames = append(frames, frame)
				imgs = append(imgs, []byte(fmt.Sprintf(`<g class="d2-test"><g class="YQ=="%s><rect x="%d" y="%d" width="10" height="5"/></g></g>`, offset, s.Pos.X, s.Pos.Y)))
			}
			art, css, frameClock := nativeMotion(frames, imgs, "d2-test", 1100)
			if art == nil || frameClock {
				t.Fatalf("unexpected shape fallback")
			}
			first, _, _ := strings.Cut(string(css), "}@keyframes")
			in := strings.Count(first, "cubic-bezier(.3333333333,0,.6666666667,0)")
			out := strings.Count(first, "cubic-bezier(.3333333333,1,.6666666667,1)")
			if tc.want {
				if in != 1 || out != 1 || strings.Count(first, "transform:") != 4 ||
					!strings.Contains(first, "45.454545455%{transform:translate(-2.5px,4.5px)") ||
					!strings.Contains(first, "90.909090909%{transform:translate(-5px,9px);animation-timing-function:steps(1,end)") {
					t.Fatalf("expected exact cubic halves and arrival, got %s", first)
				}
			} else if in != 0 || out != 0 {
				t.Fatalf("unexpected inferred curve: %s", first)
			}
			if strings.Count(string(css), "cubic-bezier(") != in+out {
				t.Fatal("reduced-motion track contains easing")
			}
		})
	}
}
