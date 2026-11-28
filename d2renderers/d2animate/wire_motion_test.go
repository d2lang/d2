package d2animate

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func TestNativeWireUsesOneClockAndKeepsSampledFallback(t *testing.T) {
	var frames []Frame
	var fragments [][]byte
	id := "(a -> b)[0]"
	key := base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(id)))
	for i := range 3 {
		d := d2target.NewDiagram()
		src := d2target.Shape{ID: "a", Width: 10, Height: 10}
		dst := d2target.Shape{ID: "b", Width: 10, Height: 10, Pos: d2target.Point{X: 20 + i}}
		if i < 2 {
			dst.MotionOffset = geo.NewPoint(0, 0)
		}
		d.Shapes = []d2target.Shape{src, dst}
		d.Connections = []d2target.Connection{{ID: id, Src: "a", Dst: "b", Route: []*geo.Point{geo.NewPoint(10, 5), geo.NewPoint(float64(20+i), 5)}}}
		frames = append(frames, Frame{Diagram: d, TweenSample: i < 2, DurationMS: 10})
		fragments = append(fragments, []byte(fmt.Sprintf(`<g class="d2-test"><g class="%s"><marker id="mk-d2-%d" orient="auto"><path d="M0 0L1 1" fill="red"/></marker><path d="M10 5L%d 5" class="connection" mask="url(#d2-%d)" marker-end="url(#mk-d2-%d)"/></g><mask id="d2-%d"><rect x="0" y="0" width="100" height="10"/></mask></g>`, key, i, 20+i, i, i, i)))
	}
	art, css, clock := nativeMotion(frames, fragments, "d2-test", 30)
	if !clock || bytes.Count(art, []byte("d2-wire-ink")) != 1 || bytes.Count(art, []byte("animation: d2Transition")) != 3 {
		t.Fatalf("persistent and fallback paths: %s", art)
	}
	for _, want := range []string{
		`@supports(d:path("M0 0L1 1"))`, `>g>g{animation:none!important}`,
		`33.333333333%{d:path("M10 5L21 5")}`, `@media(prefers-reduced-motion:reduce)`,
	} {
		if !bytes.Contains(css, []byte(want)) {
			t.Errorf("missing %s in %s", want, css)
		}
	}
	for name, rewrite := range map[string]func(string) string{
		"rerouted port":      func(s string) string { return strings.Replace(s, "M10 5L21 5", "M10 5L21 7", 1) },
		"path topology":      func(s string) string { return strings.Replace(s, "M10 5L21 5", "M10 5L12 7L21 5", 1) },
		"changing mask":      func(s string) string { return strings.Replace(s, `width="100"`, `width="30"`, 1) },
		"changing arrowhead": func(s string) string { return strings.Replace(s, `fill="red"`, `fill="green"`, 1) },
	} {
		t.Run(name, func(t *testing.T) {
			altered := append([][]byte{}, fragments...)
			altered[1] = []byte(rewrite(string(altered[1])))
			if out, _, _ := nativeMotion(frames, altered, "d2-test", 30); out != nil {
				t.Errorf("unexpected continuous path for %s", name)
			}
		})
	}
}

func TestNativeWireKeepsGradientResourceInOriginalFrame(t *testing.T) {
	group := []byte(`<g class="edge"><path class="connection" d="M0 0L1 1" stroke="url(#gradient)"/></g>`)
	if _, ok := readWireLine(group); ok {
		t.Fatal("a frame-local gradient needs its original wire")
	}
}
