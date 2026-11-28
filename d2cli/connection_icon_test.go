package d2cli

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestGIFStopsIconDeparturesAndDrainsTheWire(t *testing.T) {
	const source = `direction: right
vars.d2-config.animate-interval: 1500ms
a -> b: {style.stroke: "#ff0000"; icon.style.animated: 6/s}
steps: {stop: {(a -> b)[0].icon.style.animated: 0/s}}`
	data, err := imageCLI(t, t.TempDir(), source, "--pad=12", "--stdout-format=gif", "-")
	if err != nil {
		t.Fatal(err)
	}
	redPixels := func(frame image.Image) int {
		count := 0
		for y := frame.Bounds().Min.Y; y < frame.Bounds().Max.Y; y++ {
			for x := frame.Bounds().Min.X; x < frame.Bounds().Max.X; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				if r>>8 > 180 && g>>8 < 75 && b>>8 < 75 {
					count++
				}
			}
		}
		return count
	}
	// Only the fixed line and arrowhead remain after the last icon lands.
	after := redPixels(animationFrameAt(t, data, 280))
	before := redPixels(animationFrameAt(t, data, 120))
	stopping := redPixels(animationFrameAt(t, data, 150))
	if after == 0 || before <= after || stopping <= after {
		t.Fatalf("red pixel counts: running=%d just stopped=%d drained=%d", before, stopping, after)
	}
}

func TestStoryboardShowsSparseAndDrainingIcons(t *testing.T) {
	board := func(rate float64) *d2target.Diagram {
		b := animatedConnectionRasterDiagram()
		b.Shapes[1].Pos.X = 42
		wire := &b.Connections[0]
		wire.Route[1].X = 42 // A trip lasts about 110ms.
		wire.Animated = false
		wire.IconAnimation = &d2target.ConnectionIconAnimation{Rate: &rate}
		return b
	}
	for _, tc := range []struct {
		name               string
		interval           int
		nextRate           float64
		boardCount         int
		wantStart, wantEnd int
	}{
		{"late departure", 2200, .5, 2, 1600, 1712},
		{"draining the previous departure", 2050, 0, 2, 0, 62},
		{"one cell per board", 2200, .5, storyboardMaxFrames, 1600, 1712},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, next := board(1), board(tc.nextRate)
			next.Name = "change"
			root.Steps = []*d2target.Diagram{next}
			for len(root.Steps)+1 < tc.boardCount {
				b := board(0)
				b.Name = fmt.Sprintf("hold%d", len(root.Steps))
				root.Steps = append(root.Steps, b)
			}
			plan, err := planStoryboard(t.Context(), root, tc.interval)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan) > storyboardMaxFrames {
				t.Fatalf("exceeded the storyboard limit with %d frames", len(plan))
			}
			for _, sample := range plan {
				local := int(sample.TimeMS) - tc.interval
				if sample.board == next && local >= tc.wantStart && local < tc.wantEnd &&
					len(sample.diagram.Connections[0].IconPositions) > 0 {
					return
				}
			}
			t.Fatal("no sample showed the traveling icon on the changed board")
		})
	}
}

func TestLayerZoomCarriesConnectionIcons(t *testing.T) {
	board := func(x, y int, stroke string) *d2target.Diagram {
		b := animatedConnectionRasterDiagram()
		for i := range b.Shapes {
			b.Shapes[i].Pos.X += x
			b.Shapes[i].Pos.Y += y
		}
		wire := &b.Connections[0]
		for _, p := range wire.Route {
			p.X += float64(x)
			p.Y += float64(y)
		}
		// Only the traveling circle paints the test color.
		wire.Animated = false
		wire.Stroke, wire.StrokeWidth = stroke, 0
		wire.IconAnimation = &d2target.ConnectionIconAnimation{}
		return b
	}
	root, detail := board(0, 0, "#ff0000"), board(10000, 12000, "#0000ff")
	root.Config = &d2target.Config{AnimateDuration: new(int64(400))}
	root.Shapes[1].Link = "root.layers.detail"
	detail.Name = "detail"
	root.Layers = []*d2target.Diagram{detail}
	opts := d2svg.RenderOpts{Pad: new(int64(10))}
	// Use distinct world origins and a zoom that extends the 100ms hold.
	plan, err := planStoryboard(t.Context(), root, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 4 {
		t.Fatalf("zoom produced %d panels, want 4", len(plan))
	}
	data, _, err := renderStoryboard(t.Context(), plan, opts, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}

	// Both zoom panels show the enclosing board. Its circle is in flight on
	// the inward leg and at the source as the camera returns.
	redBounds := func(img image.Image, rect image.Rectangle) image.Rectangle {
		var bounds image.Rectangle
		for y := rect.Min.Y; y < rect.Max.Y; y++ {
			for x := rect.Min.X; x < rect.Max.X; x++ {
				r, g, b, _ := img.At(x, y).RGBA()
				if r > 0xc000 && max(g, b) < 0x5000 {
					bounds = bounds.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		return bounds.Sub(rect.Min)
	}
	panel := image.Rect(0, 0, storyboardWidth, storyboardHeight).Add(image.Pt(2*storyboardGap+storyboardWidth, storyboardGap))
	enter := redBounds(sheet, panel)
	exit := redBounds(sheet, panel.Add(image.Pt(0, storyboardHeight+storyboardCaption+storyboardGap)))
	if enter.Empty() || exit.Empty() || enter.Min.X <= exit.Max.X {
		t.Fatalf("storyboard zooms lost the local icon clock or viewport: entering %v, leaving %v", enter, exit)
	}

	session, err := newGIFRenderSession()
	if err != nil {
		t.Fatal(err)
	}
	var gifFrames []image.Image
	summary, err := renderGIFWithSession(t.Context(), "-", false, root, opts, 100, gifLayerOptions{duration: 400}, session, nil, false, nil,
		func(_ int, f image.Image) error {
			gifFrames = append(gifFrames, f)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	var inGIF, outGIF image.Rectangle
	at := 0
	for i, delay := range summary.delays {
		if at <= 20 && 20 < at+delay {
			inGIF = redBounds(gifFrames[i], gifFrames[i].Bounds())
		}
		if at <= 60 && 60 < at+delay {
			outGIF = redBounds(gifFrames[i], gifFrames[i].Bounds())
		}
		at += delay
	}
	if at != 80 || inGIF.Empty() || outGIF.Empty() || inGIF.Min.X <= outGIF.Max.X {
		t.Fatalf("GIF duration %dcs, inward circle %v, outward circle %v", at, inGIF, outGIF)
	}
}
