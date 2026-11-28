package d2cli

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestVideoStaticHoldAndStablePlacement(t *testing.T) {
	// The additional box grows a related board to the left. Both the first
	// red box and the new padding column must stay in place across this cut.
	root, next := simpleRasterDiagramWithSize(11, 11), simpleRasterDiagramWithSize(11, 11)
	blue := next.Shapes[0]
	blue.ID, blue.Fill, blue.Pos.X = "other", "#0000ff", -22
	next.Shapes = append(next.Shapes, blue)
	next.LayoutContinuity = true
	root.Steps = []*d2target.Diagram{next}
	var red []image.Rectangle
	opts := d2svg.RenderOpts{Pad: new(int64)}
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 60_000, gifLayerOptions{}, 0, false, videoSize{}, func(im image.Image, repeats int) error {
		if want := []int{1800, 1799, 1}; len(red) >= len(want) || repeats != want[len(red)] || im.Bounds().Dx()%2 != 0 || im.Bounds().Dy()%2 != 0 {
			t.Fatalf("frame repeated %d times with bounds %v", repeats, im.Bounds())
		}
		var box image.Rectangle
		for y := range im.Bounds().Dy() {
			for x := range im.Bounds().Dx() {
				c := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
				if c.A != 255 {
					t.Fatalf("transparent output at (%d,%d)", x, y)
				}
				if c.R == 255 && c.G == 0 && c.B == 0 {
					box = box.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		red = append(red, box)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(red) != 3 || red[0].Empty() || red[0] != red[1] || red[1] != red[2] {
		t.Fatalf("expected stationary red box through the final tick, got %v", red)
	}
}

func TestVideoSingleBoardContinuousMotion(t *testing.T) {
	board := animatedConnectionRasterDiagram()
	opts := d2svg.RenderOpts{Pad: new(int64)}
	var first []byte
	moved := false
	var count int
	err := renderVideoFrames(t.Context(), "-", false, board, opts, 1, gifLayerOptions{}, 300*time.Millisecond, false, videoSize{}, func(im image.Image, repeats int) error {
		if repeats != 1 {
			t.Fatalf("animated frame repeated %d times", repeats)
		}
		var raw bytes.Buffer
		if err := writeVideoFrame(&raw, im); err != nil {
			t.Fatal(err)
		}
		pixels := raw.Bytes()
		if first == nil {
			first = bytes.Clone(pixels)
		} else if !bytes.Equal(pixels, first) {
			moved = true
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 9 || !moved {
		t.Fatalf("%d samples, motion = %t", count, moved)
	}
}

func TestVideoLayerCameraAndFinalHold(t *testing.T) {
	root, child := simpleRasterDiagramWithSize(21, 21), simpleRasterDiagramWithSize(51, 31)
	root.Shapes[0].Link = "root.layers.detail"
	child.Name, child.Shapes[0].Fill = "detail", "#0000ff"
	root.Layers = []*d2target.Diagram{child}
	opts := d2svg.RenderOpts{Pad: new(int64)}
	var count int
	var size image.Point
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 200, gifLayerOptions{duration: 100}, 0, false, videoSize{}, func(im image.Image, repeats int) error {
		if count == 0 {
			size = im.Bounds().Size()
		}
		if im.Bounds().Size() != size {
			t.Fatalf("canvas jumps from %v to %v", size, im.Bounds())
		}
		count += repeats
		if count == 12 {
			c := color.NRGBAModel.Convert(im.At(size.X/2, size.Y/2)).(color.NRGBA)
			if repeats != 1 || c.B != 255 || c.R != 0 {
				t.Fatalf("final hold repeated %d times, center %v", repeats, c)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 12 {
		t.Fatalf("got %d frames", count)
	}
	p, err := planVideo(root, 200, gifLayerOptions{duration: 100}, 800*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, leg, _ := p.sample(11)
	if leg == nil || leg.Zoom.Inward {
		t.Fatalf("repeated clip lost its return zoom: %v", leg)
	}
}

func TestVideoClockRoundsOnceAndHonorsCaptions(t *testing.T) {
	root := simpleRasterDiagram()
	root.Root.Annotation = &d2target.Annotation{Label: "Give the replica time to catch up."}
	captionMS := root.ReadingMS()
	step := simpleRasterDiagram()
	root.Steps = []*d2target.Diagram{step}
	p, err := planVideo(root, 9, gifLayerOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Duration(captionMS+9) * time.Millisecond; p.duration != want {
		t.Fatalf("duration %s, want %s", p.duration, want)
	}
	last, _, _, _, _ := p.sample(p.frames - 1)
	if p.segments[last].Diagram != step {
		t.Fatal("last frame missed the final board")
	}

	root.Root.Annotation = nil
	root.Steps = append(root.Steps, simpleRasterDiagram())
	p, err = planVideo(root, 39, gifLayerOptions{}, 300*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int{0, 0, 1, 2, 0, 1, 2, 2, 0} {
		got, _, _, _, _ := p.sample(i)
		if got != want {
			t.Fatalf("at frame %d got segment %d, want %d", i, got, want)
		}
	}
}

func TestVideoPropagatesConsumerError(t *testing.T) {
	stop := errors.New("consumer stopped")
	err := renderVideoFrames(t.Context(), "-", false, simpleRasterDiagram(), d2svg.RenderOpts{}, 1000, gifLayerOptions{}, 0, false, videoSize{}, func(image.Image, int) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("got %v", err)
	}
}

func TestVideoDenseTourBudgets(t *testing.T) {
	// Six incoming transitions contribute 600 native tween scenes, plus
	// seven endpoint boards. A shared SVG tests resolver reuse in both passes.
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10" fill="#00ff00"/></svg>`))
	}))
	defer server.Close()
	asset, err := url.Parse(server.URL + "/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	root := rasterImageDiagram(asset)
	for range 6 {
		step := simpleRasterDiagram()
		step.TweenDurationMS = 4000
		for range 100 {
			step.TweenFrames = append(step.TweenFrames, simpleRasterDiagram())
		}
		root.Steps = append(root.Steps, step)
	}
	var frames, runs int
	err = renderVideoFrames(trustedAssetContext(), "-", false, root, d2svg.RenderOpts{Pad: new(int64)}, 18_000, gifLayerOptions{}, 120*time.Second, false, videoSize{}, func(im image.Image, n int) error {
		if frames == 0 {
			c := color.NRGBAModel.Convert(im.At(im.Bounds().Dx()/2, im.Bounds().Dy()/2)).(color.NRGBA)
			if c.G != 255 || c.R != 0 || c.B != 0 {
				t.Fatalf("first board lost its SVG: %v", c)
			}
		}
		frames += n
		runs++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || frames != 3600 || runs != 608 {
		t.Fatalf("got %d asset requests, %d frames in %d runs", requests.Load(), frames, runs)
	}
}

func TestVideoWorkUsesActualPaintsAndPreservesTrimFraming(t *testing.T) {
	root, next := simpleRasterDiagram(), simpleRasterDiagramWithSize(241, 151)
	root.Layers = []*d2target.Diagram{next}
	opts := d2svg.RenderOpts{Pad: new(int64)}
	var full image.Rectangle
	for _, duration := range []time.Duration{0, 100 * time.Millisecond} {
		calls := 0
		ctx := t.Context()
		plan, err := planVideo(root, 60_000, gifLayerOptions{}, duration)
		if err != nil {
			t.Fatal(err)
		}
		work, err := plan.work(ctx)
		if err != nil {
			t.Fatal(err)
		}
		err = renderVideoFrames(ctx, "-", false, root, opts, 60_000, gifLayerOptions{}, duration, false, videoSize{}, func(im image.Image, repeats int) error {
			calls++
			if duration == 0 {
				full = im.Bounds()
			} else if im.Bounds() != full {
				t.Fatalf("trimmed canvas %v, full canvas %v", im.Bounds(), full)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if work.builds != 2 || work.samples != calls || work.paints != calls-1 || videoWorkDivisor(work.paints) != 1 {
			t.Fatalf("unexpected work on %v clip: %+v for %d emitted runs", duration, work, calls)
		}
	}
	p, err := planVideo(animatedConnectionRasterDiagram(), 1000, gifLayerOptions{}, 120*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	work, err := p.work(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if work.builds != 1 || work.paints != 3600 || videoWorkDivisor(work.paints) <= 1 {
		t.Fatalf("unbudgeted continuous motion: %+v", work)
	}
}

func TestVideoLong4KBudget(t *testing.T) {
	const duration = 30 * time.Minute
	uhd := image.Rect(0, 0, 3840, 2160)
	for _, tc := range []struct {
		diagram         *d2target.Diagram
		paints, samples int
		exceeds         bool
	}{
		{simpleRasterDiagram(), 1, 2, false},
		{animatedConnectionRasterDiagram(), 54_000, 54_000, true},
	} {
		p, err := planVideo(tc.diagram, 1000, gifLayerOptions{}, duration)
		if err != nil {
			t.Fatal(err)
		}
		work, err := p.work(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if work.paints != tc.paints || work.samples != tc.samples || (work.checkPixels(uhd) != nil) != tc.exceeds {
			t.Fatalf("unexpected 30-minute UHD work %+v, budget error %v", work, work.checkPixels(uhd))
		}
	}
}
