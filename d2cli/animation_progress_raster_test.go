package d2cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"slices"
	"testing"
	"time"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/util-go/go2"
)

var testProgressInk = color.RGBA{R: 0x12, G: 0x9a, B: 0x4f, A: 255}

func progressRenderOpts() d2svg.RenderOpts {
	return d2svg.RenderOpts{
		Pad: new(int64), AnimateProgress: go2.Pointer(true),
		ThemeOverrides: &d2target.ThemeOverrides{B1: go2.Pointer("#129a4f")},
	}
}

func checkProgressStrip(t *testing.T, frame image.Image, extent, height int) {
	t.Helper()
	bounds := frame.Bounds()
	for y := max(bounds.Min.Y, bounds.Max.Y-height-1); y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := color.RGBAModel.Convert(frame.At(x, y)) == testProgressInk
			want := y >= bounds.Max.Y-height && x < bounds.Min.X+extent
			if got != want {
				t.Fatalf("pixel (%d,%d) on %v is %v; want a %dx%d progress line", x, y, bounds, frame.At(x, y), extent, height)
			}
		}
	}
}

func TestVideoProgressUsesOutputClockAndLetterbox(t *testing.T) {
	root := simpleRasterDiagram()
	root.Steps = []*d2target.Diagram{simpleRasterDiagram()}
	for _, duration := range []time.Duration{time.Millisecond, 200 * time.Millisecond, 20 * time.Second} {
		var tick, runs int
		total := videoFrameCount(duration)
		err := renderVideoFrames(t.Context(), "-", false, root, progressRenderOpts(), 460, gifLayerOptions{}, duration, false, videoSize{48, 72}, func(im image.Image, repeats int) error {
			for range repeats {
				want := 48 * tick / total
				if tick == total-1 {
					want = 48
				}
				checkProgressStrip(t, im, want, 2)
				tick++
			}
			runs++
			return nil
		})
		if err != nil || tick != total {
			t.Fatalf("%s delivered %d/%d ticks: %v", duration, tick, total, err)
		}
		if duration == 20*time.Second && runs >= total/2 {
			t.Fatalf("long clip sent %d runs for %d ticks on a 48-pixel canvas", runs, total)
		}
	}
}

func TestVideoProgressCancellationRestoresBorrowedCanvas(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	frame := image.NewRGBA(image.Rect(3, 7, 51, 11))
	original := bytes.Clone(frame.Pix)
	p := &videoProgressOverlay{ink: image.NewUniform(testProgressInk), steps: frame.Rect.Dx()}
	calls := 0
	err := p.emit(ctx, frame, 5, 50, 60, func(im image.Image, _ int) error {
		checkProgressStrip(t, im, 4, 2)
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || !bytes.Equal(original, frame.Pix) {
		t.Fatalf("cancelled overlay: calls %d, err %v, restored %t", calls, err, bytes.Equal(original, frame.Pix))
	}
}

func TestVideoProgressWithinCodedPixelBudget(t *testing.T) {
	const width, height, ticks = 3840, 2160, 1800
	maxSamples := int(videoMaxTotalPixels / (width * height))
	canvas := image.Rect(0, 0, width, height)
	for _, spare := range []int{0, 1, 20, width} {
		work := videoWork{samples: maxSamples - spare}
		steps := work.progressSteps(canvas)
		coded := 0
		for i := 0; i < ticks; {
			run := videoProgressRun(i, ticks, steps)
			if i != ticks-1 && i+run >= ticks || run < 1 {
				t.Fatalf("invalid run of %d from tick %d", run, i)
			}
			i += run
			coded++
		}
		if coded < 2 || coded > 2+spare {
			t.Fatalf("a final static hold with %d spare samples used %d samples", spare, coded)
		}
	}
}

func TestVideoProgressHeightUsesDeliverySize(t *testing.T) {
	for _, tc := range []struct {
		size videoSize
		want int
	}{{videoSize{250, 480}, 3}, {videoSize{1024, 600}, 6}} {
		err := renderVideoFrames(t.Context(), "-", false, simpleRasterDiagram(), progressRenderOpts(), 100, gifLayerOptions{}, time.Millisecond, false, tc.size, func(im image.Image, _ int) error {
			checkProgressStrip(t, im, im.Bounds().Dx(), tc.want)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestGIFProgressUsesEncodedDelaysOnMixedCanvases(t *testing.T) {
	single := simpleRasterDiagramWithSize(70, 10)
	single.Root.Annotation = &d2target.Annotation{Label: "Pause for this caption."}
	sparse := simpleRasterDiagramWithSize(70, 12)
	sparse.TweenDurationMS = 500
	sparse.TweenFrames = []*d2target.Diagram{simpleRasterDiagramWithSize(20, 40), simpleRasterDiagramWithSize(70, 20)}
	sparse.TweenFrameEndsMS = []int{430, 500}
	root := simpleRasterDiagramWithSize(20, 40)
	root.Steps = []*d2target.Diagram{sparse}
	for _, tc := range []struct {
		name                       string
		d                          *d2target.Diagram
		ms, durationCS, lineHeight int
	}{
		{"caption", single, 101, (single.ReadingMS() + 9) / 10, 2},
		{"sparse tween", root, 100, 60, 2},
		{"single frame", simpleRasterDiagram(), 1, 1, 2},
		{"one row", simpleRasterDiagramWithSize(10, 1), 1, 1, 1},
		{"short canvas", simpleRasterDiagramWithSize(10, 3), 100, 10, 2},
		{"medium portrait", simpleRasterDiagramWithSize(350, 600), 1, 1, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _, err := renderGIF(t.Context(), "-", false, tc.d, progressRenderOpts(), tc.ms, false, gifLayerOptions{})
			if err != nil {
				t.Fatal(err)
			}
			a, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "short canvas" && a.Config.Height != 3 {
				t.Fatalf("short GIF canvas is %d pixels high, want 3", a.Config.Height)
			}
			elapsed := 0
			for i, frame := range a.Image {
				want := frame.Rect.Dx() * elapsed / tc.durationCS
				if i == len(a.Image)-1 {
					want = frame.Rect.Dx()
				}
				checkProgressStrip(t, frame, want, tc.lineHeight)
				if tc.name == "sparse tween" && a.Delay[i] > 6 {
					t.Fatalf("progress froze for %dcs inside a sparse pose", a.Delay[i])
				}
				elapsed += a.Delay[i]
			}
			if elapsed != tc.durationCS {
				t.Fatalf("GIF plays for %dcs, want %dcs", elapsed, tc.durationCS)
			}
		})
	}
}

func TestGIFProgressSpendsOnlySpareFrameAndPixelBudget(t *testing.T) {
	a := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black})
	b := image.NewPaletted(a.Rect, color.Palette{color.White})
	for _, tc := range []struct {
		name               string
		side, limit, spare int
	}{
		{"frame limit", 8, gifMaxFrames, 2},
		{"normalized pixel limit", 512, int(gifMaxNormalizedPixels / (512 * 512)), 2},
		{"no spare slots", 8, gifMaxFrames, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.limit - tc.spare
			poses := make([]*image.Paletted, n)
			s := gifRenderSummary{maxWidth: tc.side, maxHeight: tc.side, totalFrames: n, delays: make([]int, n), offsets: make([]image.Point, n)}
			for i := range poses {
				poses[i] = a
				s.delays[i] = 3
			}
			poses[n-1], s.delays[n-1], s.offsets[n-1] = b, 300, image.Pt(2, 3)
			frames, err := s.splitProgressHolds(t.Context(), poses)
			if err != nil {
				t.Fatal(err)
			}
			if len(frames) != tc.limit || len(s.delays) != tc.limit || len(s.offsets) != tc.limit || s.totalFrames != tc.limit {
				t.Fatalf("expanded schedule has %d pictures, %d delays and %d offsets; want %d", len(frames), len(s.delays), len(s.offsets), tc.limit)
			}
			total := 0
			for i, frame := range frames {
				if i < n-1 {
					if frame != a || s.delays[i] != 3 || s.offsets[i] != (image.Point{}) {
						t.Fatal("changed an authored picture before the hold")
					}
				} else {
					if frame != b || s.offsets[i] != image.Pt(2, 3) {
						t.Fatal("lost the held picture or its placement")
					}
					total += s.delays[i]
				}
			}
			if total != 300 {
				t.Fatalf("hold duration %dcs, want 300cs", total)
			}
		})
	}
}

func TestGIFProgressColorIndependentOfBoardPalette(t *testing.T) {
	board := simpleRasterDiagramWithSize(48, 48)
	board.Shapes[0].Fill = "B1"
	opts := progressRenderOpts()
	opts.AnimateProgressColor = "lime"
	for _, enabled := range []bool{false, true} {
		opts.AnimateProgress = &enabled
		data, _, err := renderGIF(t.Context(), "-", false, board, opts, 100, false, gifLayerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		animation, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		last := animation.Image[len(animation.Image)-1]
		if got := color.RGBAModel.Convert(last.At(24, 24)); got != testProgressInk {
			t.Fatalf("progress %t changed the board's B1 fill to %v", enabled, got)
		}
		want := testProgressInk
		if enabled {
			want = color.RGBA{G: 255, A: 255}
		}
		if got := color.RGBAModel.Convert(last.At(24, 47)); got != want {
			t.Fatalf("progress %t drew %v at the bottom, want %v", enabled, got, want)
		}
	}
}

func TestGIFProgressPreservesCaptionReadingTime(t *testing.T) {
	board := simpleRasterDiagram()
	board.Root.Annotation = &d2target.Annotation{Label: "Pause for this caption."}
	opts := progressRenderOpts()
	var delays []int
	for _, enabled := range []bool{false, true} {
		opts.AnimateProgress = &enabled
		data, _, err := renderGIF(t.Context(), "-", false, board, opts, 101, false, gifLayerOptions{})
		if err != nil {
			t.Fatal(err)
		}
		animation, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		total := 0
		for _, delay := range animation.Delay {
			total += delay
		}
		if want := (board.ReadingMS() + 9) / 10; total != want {
			t.Fatalf("progress %t: caption plays for %dcs, want %dcs", enabled, total, want)
		}
		if delays != nil && !slices.Equal(delays, animation.Delay) {
			t.Fatalf("toggling the progress line changed caption frame delays")
		}
		delays = animation.Delay
	}
}

func TestGIFProgressIntervalDefaults(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		args, env    []string
		want         bool
	}{
		{name: "implicit interval"},
		{name: "authored interval", config: "animate-interval: 100ms", want: true},
		{name: "environment interval", env: []string{"D2_ANIMATE_INTERVAL=100"}, want: true},
		{name: "CLI interval", args: []string{"--animate-interval=100"}, want: true},
		{name: "zero override", config: "animate-interval: 100ms", args: []string{"--animate-interval=0"}},
		{name: "disabled over source interval", config: "animate-interval: 100ms; animate-progress: false"},
		{name: "enabled without an interval", args: []string{"--animate-progress"}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const swatch = `
swatch: "" {width: 48; height: 48; style: {fill: white; stroke-width: 0}}`
			source := "vars.d2-config: {pad: 0; animate-progress-color: lime; " + tc.config + "}" + swatch
			data, err := imageCLIWithEnv(t, t.TempDir(), source, tc.env, append(tc.args, "--stdout-format=gif", "-")...)
			if err != nil {
				t.Fatal(err)
			}
			animation, err := gif.DecodeAll(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			final := animation.Image[len(animation.Image)-1]
			got := color.RGBAModel.Convert(final.At(final.Rect.Dx()/2, final.Rect.Max.Y-2))
			want := color.RGBAModel.Convert(color.White)
			if tc.want {
				want = color.RGBA{G: 255, A: 255}
			}
			if got != want {
				t.Errorf("final bar pixel = %v, want %v", got, want)
			}
		})
	}
}

func TestGIFProgressReservesAccentOnFullGrayscalePalette(t *testing.T) {
	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.Gray{Y: uint8(i)}
	}
	im := image.NewPaletted(image.Rect(0, 0, 256, 7), palette)
	for i := range im.Pix {
		im.Pix[i] = uint8(i)
	}
	p := gifProgressOverlay{ink: image.NewUniform(testProgressInk)}
	p.start([]int{3})
	if err := p.paint(t.Context(), im); err != nil {
		t.Fatal(err)
	}
	checkProgressStrip(t, im, 256, 2)
	if got := color.RGBAModel.Convert(im.At(255, 0)); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("white canvas slot changed to %v", got)
	}
}
