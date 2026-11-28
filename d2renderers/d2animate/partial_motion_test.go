package d2animate

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func stepMotionFixture(persistent bool) ([]Frame, [][]byte) {
	holds := []int{7, 10, 10, 5, 11, 11, 6}
	var frames []Frame
	var svgs [][]byte
	for i, ms := range holds {
		b := d2target.NewDiagram()
		sample := i == 1 || i == 2 || i == 4 || i == 5
		var raw strings.Builder
		raw.WriteString(`<g class="d2-test">`)
		shape := func(id string, x int) {
			s := d2target.Shape{ID: id, Pos: d2target.Point{X: x}}
			if sample {
				s.MotionOffset = geo.NewPoint(0, 0)
			}
			b.Shapes = append(b.Shapes, s)
			fmt.Fprintf(&raw, `<g class="%s"><rect x="%d" y="0" width="10" height="4"/></g>`, base64.URLEncoding.EncodeToString([]byte(id)), x)
		}
		if persistent {
			shape("guide", i)
		}
		if i < 4 {
			shape("a", i)
		} else {
			shape("b", i)
		}
		raw.WriteString("</g>")
		f := Frame{Diagram: b, DurationMS: ms, TweenSample: sample}
		if i == 2 {
			f.start = frameStart{fraction: time.Millisecond / 2}
		}
		frames = append(frames, f)
		svgs = append(svgs, []byte(raw.String()))
	}
	return frames, svgs
}

func TestNativeStepMotionAcrossChangingArtwork(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		frames, svgs := stepMotionFixture(persistent)
		whole, _, _ := nativeMotion(frames, svgs, "d2-test", 60)
		if (whole != nil) != persistent {
			t.Fatal("fixture did not exercise the expected whole-film anchors")
		}
		art, css, clock := nativeTimelineMotion(frames, svgs, "d2-test", 60)
		if clock || bytes.Count(art, []byte("d2MotionStep-d2-test-")) != 2 || bytes.Count(art, []byte(`class="YQ=="`)) != 2 || bytes.Count(art, []byte(`class="Yg=="`)) != 1 {
			t.Fatalf("bad mixed artwork: %s", art)
		}
		keys := []string{
			"@keyframes d2MotionStep-d2-test-1{0%{opacity:0}11.666666667%{opacity:1}53.333333333%,100%{opacity:0}}",
			"@keyframes d2MotionStep-d2-test-4{0%{opacity:0}53.333333333%{opacity:1}100%{opacity:1}}",
			"29.166666667%{transform:translate(1px,0px)}",
			"53.333333333%{transform:translate(0px,0px)}",
			"@media(prefers-reduced-motion:reduce)",
		}
		for _, key := range keys {
			if !bytes.Contains(css, []byte(key)) {
				t.Errorf("persistent=%t: missing %s in %s", persistent, key, css)
			}
		}
	}
}

func TestNativeStepMotionKeepsOriginalResourcesAndStyles(t *testing.T) {
	frames, svgs := stepMotionFixture(false)
	for i := range svgs {
		// The same generated ID has two different definitions. A later board
		// can still reference the first one in document order.
		width := 1
		if i >= 4 {
			width = 2
		}
		res := fmt.Sprintf(`<mask id="d2-17"><rect x="0" y="0" width="%d" height="1"/></mask>`, width)
		svgs[i] = bytes.Replace(svgs[i], []byte(`<g class="d2-test">`), []byte(`<g class="d2-test">`+res), 1)
	}
	art, _, _ := nativeTimelineMotion(frames, svgs, "d2-test", 60)
	before := bytes.Index(art, []byte(`width="1" height="1"`))
	after := bytes.Index(art, []byte(`width="2" height="1"`))
	if before < 0 || after <= before || !bytes.Contains(art, []byte(`id="d2-17"`)) {
		t.Fatalf("original resource order lost: %s", art)
	}
	svgs[0] = bytes.Replace(svgs[0], []byte("</g>"), []byte("<style>.shape{opacity:0}</style></g>"), 1)
	if output, _, _ := nativeTimelineMotion(frames, svgs, "d2-test", 60); output != nil {
		t.Fatal("transformed artwork in the presence of an authored stylesheet")
	}
}

func TestNativeStepMotionKeepsGlobalIndicesForSampledGaps(t *testing.T) {
	frames, svgs := stepMotionFixture(false)
	for i := 1; i <= 3; i++ {
		// This background changes geometry and must be held on the original
		// sample clock underneath the locally continuous shape.
		gap := fmt.Sprintf(`<rect x="0" y="0" width="%d"/>`, i)
		svgs[i] = bytes.Replace(svgs[i], []byte(`<g class="d2-test">`), []byte(`<g class="d2-test">`+gap), 1)
	}
	art, _, clock := nativeTimelineMotion(frames, svgs, "d2-test", 60)
	if !clock {
		t.Fatal("step lost its sampled gap clock")
	}
	for i := 1; i <= 3; i++ {
		want := fmt.Sprintf("d2Transition-d2-test-%d 60ms", i)
		if !bytes.Contains(art, []byte(want)) {
			t.Fatalf("missing global frame %d in %s", i, art)
		}
	}
}
