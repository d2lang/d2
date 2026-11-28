package d2cli

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/gif"
	"math"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestRasterInterpolationOfSparseSubpixelMotion(t *testing.T) {
	pose := func(x float64, continuous bool) *d2target.Diagram {
		d := simpleRasterDiagramWithSize(14, 12)
		d.Shapes[0].Pos.X = int(math.Round(x))
		if continuous {
			d.Shapes[0].MotionOffset = geo.NewPoint(x-float64(d.Shapes[0].Pos.X), 0)
		}
		return d
	}
	root, step := pose(0, false), pose(5, false)
	step.LayoutContinuity = true
	step.TweenDurationMS = 1000
	// Initial and final authored boards are at integer positions. Two samples
	// would otherwise provide just one interior pose for a whole second.
	step.TweenFrames = []*d2target.Diagram{pose(0, false), pose(2.5, true)}
	root.Steps = []*d2target.Diagram{step}
	plan, err := planVideo(root, 1200, gifLayerOptions{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.segments) != 3 || len(plan.segments[1].translations["box"].Keyframes) != 2 {
		t.Fatal("expected one linear motion scene between the two static boards")
	}
	opts := d2svg.RenderOpts{Pad: new(int64)}
	var ticks int
	seen := map[[32]byte]bool{}
	err = renderVideoFrames(t.Context(), "-", false, root, opts, 1200, gifLayerOptions{}, 0, false, videoSize{}, func(im image.Image, repeats int) error {
		if ticks >= 72 && ticks < 132 {
			var raw bytes.Buffer
			if err := writeVideoFrame(&raw, im); err != nil {
				return err
			}
			seen[sha256.Sum256(raw.Bytes())] = true
		}
		ticks += repeats
		return nil
	})
	if err != nil || len(seen) != 60 {
		t.Fatalf("video displayed %d unique tween images, want 60: %v", len(seen), err)
	}
	data, _, err := renderGIF(t.Context(), "-", false, root, opts, 1200, false, gifLayerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	film, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	clear(seen)
	cs := 0
	for i, im := range film.Image {
		if cs >= 120 && cs < 220 {
			var raw bytes.Buffer
			if err := writeVideoFrame(&raw, im); err != nil {
				t.Fatal(err)
			}
			seen[sha256.Sum256(raw.Bytes())] = true
			if film.Delay[i] != 2 {
				t.Fatalf("GIF tween frame has %d cs delay, want 2", film.Delay[i])
			}
		}
		cs += film.Delay[i]
	}
	if len(seen) != 50 || cs != 240 {
		t.Fatalf("GIF displayed %d unique tween images over %d cs, want 50 and 240", len(seen), cs)
	}

	// The same samples with explicit ends are authored holds.
	step.TweenFrameEndsMS = []int{500, 1000}
	plan, err = planVideo(root, 1200, gifLayerOptions{}, 0)
	if err != nil || len(plan.segments) != 4 || plan.segments[1].translations != nil {
		t.Fatalf("explicit pauses were interpolated: %v", err)
	}
}
