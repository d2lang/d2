package d2cli

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func copyVideoRegion(src image.Image, area image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rectangle{Max: area.Size()})
	draw.Draw(dst, dst.Bounds(), src, area.Min, draw.Src)
	return dst
}

func TestVideoAutomaticCameraUsesEvenCanvas(t *testing.T) {
	p := &videoTimeline{segments: []videoSegment{{}, {}}}
	boxes := []d2scene.Box{{Width: 1239, Height: 603}, {Width: 1239, Height: 673}}
	framing, err := p.frameSizes(boxes, 1, videoSize{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if framing.camera != framing.output || framing.output.Size() != image.Pt(1240, 674) || p.segments[0].bounds.Size() != image.Pt(1239, 603) {
		t.Fatalf("automatic scene %v, camera %v, output %v", p.segments[0].bounds, framing.camera, framing.output)
	}
}

func TestVideoSizeValidation(t *testing.T) {
	for _, valid := range []string{"48x48", "1920X1080", "1080x1920", "3840x2160", "4096x2048", "3584x2304"} {
		if _, err := parseVideoSize(valid); err != nil {
			t.Errorf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "1920", "480p", "2x2", "1281x720", "4098x48", "48x2306", "4096x2160", "2160x3840", "2147483648x50"} {
		if _, err := parseVideoSize(invalid); err == nil {
			t.Errorf("accepted invalid size %q", invalid)
		}
	}
}

// Use a single static shape so the fitted, centered ink can be identified
// independently of the encoder. The panorama exceeds the native raster limit
// at its original width, but can be measured and painted into a normal MP4.
func TestVideoSizeFitsWideSource(t *testing.T) {
	root := simpleRasterDiagramWithSize(40000, 4000)
	opts := d2svg.RenderOpts{Pad: new(int64)}
	var region image.Rectangle
	var ticks int
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, 0, false, videoSize{200, 100}, func(im image.Image, n int) error {
		ticks += n
		if im.Bounds().Size() != image.Pt(200, 100) {
			t.Fatalf("size %v, repeats %d", im.Bounds(), n)
		}
		for y := range im.Bounds().Dy() {
			for x := range im.Bounds().Dx() {
				c := color.NRGBAModel.Convert(im.At(x, y)).(color.NRGBA)
				if c.R == 255 && c.G < 5 && c.B < 5 {
					region = region.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		return nil
	})
	if err != nil || region != image.Rect(0, 40, 200, 60) || ticks != videoFPS {
		t.Fatalf("fitted red shape = %v, error %v", region, err)
	}
	err = renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, 0, false, videoSize{}, func(im image.Image, n int) error {
		t.Fatal("encoded a frame exceeding native decoder dimensions")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "--video-size") || !strings.Contains(err.Error(), "--scale") {
		t.Fatalf("automatic size error = %v", err)
	}
}

func TestVideoSizeRendersVectorsAtOutputResolution(t *testing.T) {
	root := simpleRasterDiagramWithLabel()
	root.Shapes[0].Text.Color = "#000000"
	root.Shapes[0].LabelPosition = "INSIDE_MIDDLE_CENTER"
	opts := d2svg.RenderOpts{Pad: new(int64)}
	render := func(size videoSize) *image.NRGBA {
		t.Helper()
		var still *image.NRGBA
		err := renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, 0, false, size, func(im image.Image, _ int) error {
			still = copyVideoRegion(im, im.Bounds())
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return still
	}
	// Render a labeled 10x10 board natively at 8x. An exact 160x80 video
	// should contain precisely that rasterization in its central 80x80 square.
	scale := 8.
	opts.Scale = &scale
	native := render(videoSize{})
	opts.Scale = nil
	wide := render(videoSize{160, 80})
	if native.Bounds().Size() != image.Pt(80, 80) || wide.Bounds().Size() != image.Pt(160, 80) {
		t.Fatalf("native %v, wide %v", native.Bounds(), wide.Bounds())
	}
	if !bytes.Equal(native.Pix, copyVideoRegion(wide, image.Rect(40, 0, 120, 80)).Pix) {
		t.Fatal("fixed-size vector render differs from native scale")
	}
}

func TestVideoSizeTinyBoardAndTrimmedGroup(t *testing.T) {
	root := simpleRasterDiagram()
	opts := d2svg.RenderOpts{Pad: new(int64)}
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, 0, false, videoSize{}, func(im image.Image, _ int) error {
		if im.Bounds().Size() != image.Pt(48, 48) {
			t.Fatalf("tiny board video bounds = %v", im.Bounds())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	next := simpleRasterDiagramWithSize(321, 161)
	next.LayoutContinuity = true
	root.Steps = []*d2target.Diagram{next}
	var reference *image.NRGBA
	for _, duration := range []time.Duration{0, 10 * time.Millisecond} {
		first := true
		err := renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, duration, false, videoSize{256, 192}, func(im image.Image, n int) error {
			if first {
				first = false
				actual := copyVideoRegion(im, im.Bounds())
				if reference == nil {
					reference = actual
				} else if !bytes.Equal(reference.Pix, actual.Pix) {
					t.Fatal("truncated clip changed the first frame")
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestVideoSizePreservesLayerCameraUnderLetterbox(t *testing.T) {
	root, child := simpleRasterDiagramWithSize(48, 48), simpleRasterDiagramWithSize(72, 72)
	root.Shapes[0].Link = "root.layers.detail"
	child.Name, child.Shapes[0].Fill = "detail", "#0000ff"
	root.Layers = []*d2target.Diagram{child}
	pad := int64(6)
	opts := d2svg.RenderOpts{Pad: &pad}
	var reference [][]byte
	for _, size := range []videoSize{{96, 96}, {192, 96}} {
		pos := 0
		err := renderVideoFrames(t.Context(), "-", false, root, opts, 200, gifLayerOptions{duration: 100}, 0, false, size, func(im image.Image, _ int) error {
			margin := (size.width - 96) / 2
			center := copyVideoRegion(im, image.Rect(margin, 0, margin+96, 96))
			if margin == 0 {
				reference = append(reference, center.Pix)
			} else if pos >= len(reference) || !bytes.Equal(reference[pos], center.Pix) {
				t.Fatalf("letterbox changed camera pose %d", pos)
			}
			pos++
			return nil
		})
		if err != nil || pos != len(reference) || pos < 4 {
			t.Fatalf("sampled %d poses, error %v", pos, err)
		}
	}
}

func TestVideoProgressReservesSpaceForArtwork(t *testing.T) {
	for _, tc := range []struct {
		name          string
		board, output image.Point
		size          videoSize
		art           image.Rectangle
		line          int
	}{
		{"natural", image.Pt(622, 84), image.Pt(622, 94), videoSize{}, image.Rect(0, 0, 622, 84), 2},
		{"original height chooses line", image.Pt(450, 248), image.Pt(450, 258), videoSize{}, image.Rect(0, 0, 450, 248), 2},
		{"odd footer becomes even", image.Pt(300, 250), image.Pt(300, 262), videoSize{}, image.Rect(0, 0, 300, 250), 3},
		{"native minimum keeps centering", image.Pt(10, 10), image.Pt(48, 58), videoSize{}, image.Rect(19, 19, 29, 29), 2},
		{"existing letterbox", image.Pt(200, 40), image.Pt(200, 100), videoSize{200, 100}, image.Rect(0, 30, 200, 70), 2},
		{"shift before shrinking", image.Pt(200, 90), image.Pt(200, 100), videoSize{200, 100}, image.Rect(0, 0, 200, 90), 2},
		{"fit square at fixed size", image.Pt(100, 100), image.Pt(100, 100), videoSize{100, 100}, image.Rect(5, 0, 95, 90), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := simpleRasterDiagramWithSize(tc.board.X, tc.board.Y)
			root.Root.Fill = "#123456"
			matte := color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 255}
			err := renderVideoFrames(t.Context(), "-", false, root, progressRenderOpts(), 100, gifLayerOptions{}, time.Millisecond, false, tc.size, func(frame image.Image, ticks int) error {
				if frame.Bounds().Size() != tc.output || ticks != 1 {
					t.Fatalf("canvas %v for %d ticks, want %v", frame.Bounds(), ticks, tc.output)
				}
				for y := range tc.output.Y {
					for x := range tc.output.X {
						want := matte
						switch {
						case y >= tc.output.Y-tc.line:
							want = color.NRGBA(testProgressInk)
						case image.Pt(x, y).In(tc.art):
							want = color.NRGBA{R: 255, A: 255}
						}
						if got := color.NRGBAModel.Convert(frame.At(x, y)); got != want {
							t.Fatalf("pixel (%d,%d) is %v, want %v", x, y, got, want)
						}
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVideoProgressFooterRespectsDecoderLimits(t *testing.T) {
	p := &videoTimeline{segments: []videoSegment{{}}}
	boxes := []d2scene.Box{{Width: 3584, Height: 2304}}
	if _, err := p.frameSizes(boxes, 1, videoSize{}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.frameSizes(boxes, 1, videoSize{}, true); err == nil || !strings.Contains(err.Error(), "--video-size") {
		t.Fatalf("natural footer exceeds decoder limit: %v", err)
	}
	if got, err := p.frameSizes(boxes, 1, videoSize{3584, 2304}, true); err != nil || got.output.Size() != image.Pt(3584, 2304) {
		t.Fatalf("explicit maximum size: got %v, error %v", got, err)
	}
}

func TestPlaceVideoFrameClearsPreviousPoseAndMatte(t *testing.T) {
	canvas := image.NewRGBA(image.Rect(0, 0, 20, 16))
	backing := image.NewNRGBA(image.Rect(0, 0, 12, 8))
	for y := range 8 {
		for x := range 12 {
			backing.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 20), G: uint8(y * 30), A: 255})
		}
	}
	cropped := backing.SubImage(image.Rect(2, 3, 8, 8)).(*image.NRGBA)
	camera := image.NewRGBA(image.Rect(0, 0, 11, 9))
	draw.Draw(camera, camera.Rect, image.NewUniform(color.RGBA{R: 255, A: 255}), image.Point{}, draw.Src)
	for _, tc := range []struct {
		src    draw.Image
		offset image.Point
		matte  color.RGBA
	}{
		{camera, image.Pt(0, 0), color.RGBA{B: 80, A: 255}},
		{cropped, image.Pt(10, -2), color.RGBA{G: 160, A: 255}},
		{camera, image.Pt(5, 7), color.RGBA{B: 225, A: 255}},
	} {
		ref := image.NewRGBA(canvas.Rect)
		draw.Draw(ref, ref.Rect, image.NewUniform(tc.matte), image.Point{}, draw.Src)
		draw.Draw(ref, tc.src.Bounds().Add(tc.offset), tc.src, tc.src.Bounds().Min, draw.Src)
		placeVideoFrame(canvas, tc.src, tc.offset, tc.matte)
		if !bytes.Equal(canvas.Pix, ref.Pix) {
			t.Fatalf("%T at offset %v left incorrect pixels", tc.src, tc.offset)
		}
	}
}

func TestVideoProgressCompositesTransparentArtworkBeforePlacement(t *testing.T) {
	png := testSolidPNG(t, color.NRGBA{R: 220, G: 50, B: 90, A: 110})
	asset, err := url.Parse("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))
	if err != nil {
		t.Fatal(err)
	}
	for _, fill := range []string{"transparent", "#12345680", "#123456"} {
		root := rasterImageDiagram(asset)
		root.Root.Fill = fill
		root.Shapes[0].Width, root.Shapes[0].Height = 64, 64
		opts := progressRenderOpts()
		var original *image.NRGBA
		for _, enabled := range []bool{false, true} {
			opts.AnimateProgress = &enabled
			err := renderVideoFrames(t.Context(), "-", false, root, opts, 1, gifLayerOptions{}, time.Millisecond, false, videoSize{}, func(im image.Image, _ int) error {
				art := copyVideoRegion(im, image.Rect(0, 0, 64, 64))
				for i := 3; i < len(art.Pix); i += 4 {
					if art.Pix[i] != 255 {
						t.Fatalf("root %s emits alpha %d", fill, art.Pix[i])
					}
				}
				if !enabled {
					original = art
					center := art.NRGBAAt(32, 32)
					if center.R <= center.G || center.R <= center.B {
						t.Fatalf("expected translucent red artwork above %s: %v", fill, center)
					}
				} else if im.Bounds().Dy() != 74 || !bytes.Equal(art.Pix, original.Pix) {
					t.Fatalf("placing translucent PNG above the footer changed artwork for root %s", fill)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
