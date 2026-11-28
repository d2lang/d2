package d2cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
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

func checkProgressStrip(t *testing.T, frame image.Image, extent int) {
	t.Helper()
	bounds := frame.Bounds()
	for y := bounds.Max.Y - 3; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := color.RGBAModel.Convert(frame.At(x, y)) == testProgressInk
			want := y >= bounds.Max.Y-2 && x < bounds.Min.X+extent
			if got != want {
				t.Fatalf("pixel (%d,%d) on %v is %v; expected a two-pixel progress line of width %d", x, y, bounds, frame.At(x, y), extent)
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
				checkProgressStrip(t, im, want)
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
	frame := image.NewRGBA(image.Rect(0, 0, 48, 48))
	original := bytes.Clone(frame.Pix)
	p := &videoProgressOverlay{ink: image.NewUniform(testProgressInk)}
	calls := 0
	err := p.emit(ctx, frame, 5, 50, 60, func(image.Image, int) error {
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || !bytes.Equal(original, frame.Pix) {
		t.Fatalf("cancelled overlay: calls %d, err %v, restored %t", calls, err, bytes.Equal(original, frame.Pix))
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
		name           string
		d              *d2target.Diagram
		ms, durationCS int
	}{
		{"caption", single, 101, (single.ReadingMS() + 9) / 10},
		{"sparse tween", root, 100, 60},
		{"single frame", simpleRasterDiagram(), 1, 1},
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
			elapsed := 0
			for i, frame := range a.Image {
				want := frame.Rect.Dx() * elapsed / tc.durationCS
				if i == len(a.Image)-1 {
					want = frame.Rect.Dx()
				}
				checkProgressStrip(t, frame, want)
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

func TestGIFProgressOffPreservesCaptionedPlayback(t *testing.T) {
	board := simpleRasterDiagram()
	board.Root.Annotation = &d2target.Annotation{Label: "Pause for this caption."}
	opts := progressRenderOpts()
	opts.AnimateProgress = nil
	const intervalMS = 101
	without, _, err := renderGIF(t.Context(), "-", false, board, opts, intervalMS, false, gifLayerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	opts.AnimateProgress = go2.Pointer(false)
	explicit, _, err := renderGIF(t.Context(), "-", false, board, opts, intervalMS, false, gifLayerOptions{})
	if err != nil || !bytes.Equal(without, explicit) {
		t.Fatalf("disabled progress changed the encoded GIF: %v", err)
	}
	animation, err := gif.DecodeAll(bytes.NewReader(without))
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, delay := range animation.Delay {
		total += delay
	}
	// The legacy single-board encoder repeats the 101ms schedule on an
	// integral number of four-frame boards, otherwise it uses one interval.
	want := 11
	if len(animation.Delay)%4 == 0 {
		want *= len(animation.Delay) / 4
	}
	if total != want {
		t.Fatalf("disabled progress changed the prior duration from %dcs to %dcs", want, total)
	}
}

func TestGIFProgressReservesAccentOnFullGrayscalePalette(t *testing.T) {
	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.Gray{Y: uint8(i)}
	}
	im := image.NewPaletted(image.Rect(0, 0, 256, 4), palette)
	for i := range im.Pix {
		im.Pix[i] = uint8(i)
	}
	p := gifProgressOverlay{ink: image.NewUniform(testProgressInk)}
	p.start([]int{3})
	if err := p.paint(t.Context(), im); err != nil {
		t.Fatal(err)
	}
	checkProgressStrip(t, im, 256)
	if got := color.RGBAModel.Convert(im.At(255, 0)); got != (color.RGBA{255, 255, 255, 255}) {
		t.Fatalf("white canvas slot changed to %v", got)
	}
}
