package d2cli

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
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
	camera, output, err := p.frameSizes(boxes, 1, videoSize{})
	if err != nil {
		t.Fatal(err)
	}
	if camera != output || output.Size() != image.Pt(1240, 674) || p.segments[0].bounds.Size() != image.Pt(1239, 603) {
		t.Fatalf("automatic scene %v, camera %v, output %v", p.segments[0].bounds, camera, output)
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
	err := renderVideoFrames(t.Context(), "-", false, root, opts, 1000, gifLayerOptions{}, 0, false, videoSize{200, 100}, func(im image.Image, n int) error {
		if im.Bounds().Size() != image.Pt(200, 100) || n != videoFPS {
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
	if err != nil || region != image.Rect(0, 40, 200, 60) {
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
