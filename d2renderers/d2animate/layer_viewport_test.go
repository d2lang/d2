package d2animate

import (
	"bytes"
	"image"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestLinkedLayerViewport(t *testing.T) {
	root, child, step := d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()
	child.Name, step.Name = "detail", "move"
	root.Layers = []*d2target.Diagram{child}
	root.Steps = []*d2target.Diagram{step}
	root.Shapes = []d2target.Shape{{ID: "portal", Pos: d2target.NewPoint(1100, 500), Width: 20, Height: 20, Link: "root.layers.detail"}}
	svgs := [][]byte{
		[]byte(`<g viewBox="1000 400 200 200"><a href="root.layers.detail" xlink:href="root.layers.detail"><g class="cG9ydGFs"><rect/></g></a></g>`),
		[]byte(`<g viewBox="4000 2000 100 80"><circle/></g>`),
		// The step's appendix extends past the root's original right edge.
		[]byte(`<g viewBox="1100 480 140 140"><path/></g>`),
	}
	for _, clickOnly := range []bool{false, true} {
		input := svgs
		if clickOnly {
			spacer := d2target.NewDiagram()
			spacer.Name = "unrelated"
			root.Layers = []*d2target.Diagram{spacer, child}
			input = [][]byte{svgs[0], []byte(`<g viewBox="7000 100 50 50"/>`), svgs[1], svgs[2]}
		}
		frames, err := Timeline(root, 1000)
		if err != nil {
			t.Fatal(err)
		}
		out, err := WrapTimeline(root, nil, frames, input, d2svg.RenderOpts{}, 400)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{
			`class="d2-svg" width="240" height="220" viewBox="0 0 240 220"`,
			`translate(-3930 -1930)`,             // Center the distant child's 100×80 artwork.
			`40%{transform:matrix(1,0,0,1,10,0)`, // Click camera pans to the translated portal.
		} {
			if !bytes.Contains(out, []byte(want)) {
				t.Errorf("clickOnly=%t: missing %s", clickOnly, want)
			}
		}
		if got := bytes.Count(out, []byte("translate(-1000 -400)")); got != 2 {
			t.Errorf("root and step need the same placement, got %d matching translations", got)
		}

	}
	// The unrelated absolute coordinates still share one world canvas when
	// there is no supported camera move to play or follow.
	root.Shapes[0].Link = ""
	root.Layers = []*d2target.Diagram{child}
	frames, err := Timeline(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	out, err := WrapTimeline(root, nil, frames, svgs, d2svg.RenderOpts{}, 400)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(`translate(-3930 -1930)`)) || bytes.Contains(out, []byte(`viewBox="0 0 240 220"`)) {
		t.Fatal("recentered an export without linked layer navigation")
	}
}

func TestLayerViewportRawGroupAndTween(t *testing.T) {
	root, child, sample, scenario := d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram(), d2target.NewDiagram()
	child.Name, scenario.Name = "inner", "new"
	root.Layers = []*d2target.Diagram{child}
	child.Scenarios = []*d2target.Diagram{scenario}
	root.Shapes = []d2target.Shape{{Pos: d2target.NewPoint(50, 50), Width: 100, Height: 100}}
	scenario.TweenFrames = []*d2target.Diagram{sample}
	frames := []Frame{{Diagram: root}, {Diagram: child}, {Diagram: sample, TweenSample: true}, {Diagram: scenario}}
	svgs := [][]byte{[]byte("<g/>"), []byte(`<g viewBox="8000 9000 100 150"/>`), []byte(`<g viewBox="7980 9000 110 200"/>`), []byte(`<g viewBox="8000 9000 95 120"/>`)}
	viewport, offsets, err := layerViewport(NewLayerNavigation(root, nil), frames, svgs, 10)
	if err != nil {
		t.Fatal(err)
	}
	tl, br := root.BoundingBox()
	raw := image.Rect(tl.X-10, tl.Y-10, br.X+10, br.Y+10)
	if viewport.Dx() != max(raw.Dx(), 120) || viewport.Dy() != max(raw.Dy(), 200) ||
		offsets[1] != offsets[2] || offsets[1] != offsets[3] ||
		offsets[0].X != (viewport.Dx()-raw.Dx())/2-raw.Min.X {
		t.Errorf("raw frame bounds %v, group viewport %v, placements %v", raw, viewport, offsets)
	}
}

func TestClickCameraPreservesRawFrameTransform(t *testing.T) {
	child := &d2target.Diagram{Name: "detail"}
	root := &d2target.Diagram{
		Layers: []*d2target.Diagram{child},
		Shapes: []d2target.Shape{{ID: "portal", Width: 20, Height: 20, Link: "root.layers.detail"}},
	}
	frames, err := Timeline(root, 1000)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`<g viewBox="0 0 100 100" transform="translate(3 4)"><a href="root.layers.detail" xlink:href="root.layers.detail"><g class="cG9ydGFs"><rect/></g></a></g>`)
	out, err := WrapTimeline(root, nil, frames, [][]byte{raw, raw}, d2svg.RenderOpts{}, 400)
	if err != nil {
		t.Fatal(err)
	}
	// Even at a zero placement offset the camera has its own group, so its
	// CSS transform cannot override the caller's transform on the artwork.
	if !bytes.Contains(out, []byte(`<g><g transform="translate(0 0)"><g viewBox="0 0 100 100" transform="translate(3 4)">`)) {
		t.Fatal("missing separate camera and artwork transforms")
	}
}
