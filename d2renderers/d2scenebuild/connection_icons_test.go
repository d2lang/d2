package d2scenebuild

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/d2lang/d2/d2renderers/d2animate"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestConnectionIconStreamSamples(t *testing.T) {
	diagram := validDiagram()
	diagram.Root.Fill = "#fff"
	c := &diagram.Connections[0]
	c.Stroke, c.IconShape = "#ff0000", d2target.ShapeCircle
	c.IconAnimation = &d2target.ConnectionIconAnimation{Rate: floatPointer(5)}
	// Travel from (22,10) to (78,10) takes .311 seconds. A 5/s stream
	// has two dots at t=0, one at .15s, and two after the next departure.
	for _, tc := range []struct {
		name   string
		at     time.Duration
		sample []geo.Point
		dots   []int
		clear  []int
	}{
		{"steady opening", 0, nil, []int{22, 58}, []int{40}},
		{"second dot in flight", 50 * time.Millisecond, nil, []int{31, 67}, []int{45}},
		{"one dot has landed", 150 * time.Millisecond, nil, []int{49}, []int{30, 70}},
		{"new departure", 200 * time.Millisecond, nil, []int{22, 58}, []int{40}},
		{"timeline sample", 500 * time.Millisecond, []geo.Point{{X: 41, Y: 10}}, []int{41}, []int{22, 58}},
		{"empty timeline sample", 0, []geo.Point{}, nil, []int{22, 58}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.IconPositions = tc.sample
			doc, err := Build(context.Background(), diagram, Options{})
			if err != nil {
				t.Fatal(err)
			}
			opts := shapeEffectFrameOptions()
			opts.Time = tc.at
			opts.MaxAnimationTracks = 4
			img, err := d2raster.Render(context.Background(), doc, opts)
			if err != nil {
				t.Fatal(err)
			}
			pixel := func(x int) color.NRGBA { return img.NRGBAAt(x-int(doc.ViewBox.X), 13-int(doc.ViewBox.Y)) }
			for _, x := range tc.dots {
				if got := pixel(x); got.R != 255 || got.G > 10 {
					t.Errorf("expected red dot at %d, got %v", x, got)
				}
			}
			for _, x := range tc.clear {
				if got := pixel(x); got.G < 240 {
					t.Errorf("expected clear background at %d, got %v", x, got)
				}
			}
		})
	}
}

func TestConnectionIconPolygonPixels(t *testing.T) {
	// Both the static icon and the midpoint of a 200ms trip are centered at
	// (50,10). Sample away from the wire to distinguish all three silhouettes.
	for _, tc := range []struct {
		shape                 string
		upperLeft, lowerRight bool
	}{
		{d2target.ShapeSquare, true, true},
		{d2target.ConnectionIconTriangle, false, true},
		{d2target.ShapeDiamond, false, false},
	} {
		for _, mode := range []string{"static", "moving"} {
			t.Run(tc.shape+"/"+mode, func(t *testing.T) {
				d := validDiagram()
				c := &d.Connections[0]
				c.IconShape, c.Stroke, c.IconPosition = tc.shape, "#ff0000", "INSIDE_MIDDLE_CENTER"
				if mode == "moving" {
					c.IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: 200}
				}
				doc, err := Build(context.Background(), d, Options{})
				if err != nil {
					t.Fatal(err)
				}
				opts := shapeEffectFrameOptions()
				opts.Time = 100 * time.Millisecond
				img, err := d2raster.Render(context.Background(), doc, opts)
				if err != nil {
					t.Fatal(err)
				}
				for _, sample := range []struct {
					x, y   int
					filled bool
				}{{46, 6, tc.upperLeft}, {53, 13, tc.lowerRight}, {50, 7, true}} {
					pixel := img.NRGBAAt(sample.x-int(doc.ViewBox.X), sample.y-int(doc.ViewBox.Y))
					if pixel.R != 255 || (sample.filled && pixel.G > 25) || (!sample.filled && pixel.G < 230) {
						t.Errorf("at (%d,%d) got %v, want red fill: %t", sample.x, sample.y, pixel, sample.filled)
					}
				}
			})
		}
	}
}

// The image is intentionally asymmetric: it should remain upright as the
// arrow points left, and all repeated copies should use the same asset.
func TestConnectionIconImageFollowsReverseArrow(t *testing.T) {
	diagram := validDiagram()
	c := &diagram.Connections[0]
	c.Icon = testRasterAssetURL(t)
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.SrcArrow = d2target.TriangleArrowhead
	doc, err := Build(context.Background(), diagram, Options{Assets: testAssetOptions(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Assets) != 1 {
		t.Fatalf("loaded %d assets for a moving image", len(doc.Assets))
	}
	for _, sample := range []struct {
		at       time.Duration
		wantRedX int
	}{{50 * time.Millisecond, 59}, {150 * time.Millisecond, 41}} {
		opts := shapeEffectFrameOptions()
		opts.Time = sample.at
		img, err := d2raster.Render(context.Background(), doc, opts)
		if err != nil {
			t.Fatal(err)
		}
		pixel := func(x int) color.NRGBA { return img.NRGBAAt(x-int(doc.ViewBox.X), 13-int(doc.ViewBox.Y)) }
		red := pixel(sample.wantRedX)
		blue := pixel(sample.wantRedX + 15)
		if red.R < 210 || red.R < red.B || blue.B < blue.R+30 {
			t.Errorf("at %s, expected red then blue across the moving image, got %v and %v", sample.at, red, blue)
		}
	}
}

func TestOutsideCaptionLeavesMovingArtworkIntact(t *testing.T) {
	diagram := validDiagram()
	c := &diagram.Connections[0]
	c.Icon = testRasterAssetURL(t)
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.IconPositions = []geo.Point{{X: 50, Y: 10}}
	c.LabelPosition = "OUTSIDE_TOP_CENTER"
	renderIcon := func() []color.NRGBA {
		t.Helper()
		doc, err := Build(context.Background(), diagram, Options{Assets: testAssetOptions(t)})
		if err != nil {
			t.Fatal(err)
		}
		img, err := d2raster.Render(context.Background(), doc, shapeEffectFrameOptions())
		if err != nil {
			t.Fatal(err)
		}
		var pixels []color.NRGBA
		for y := 10 - c.IconSize()/2; y < 10+c.IconSize()/2; y++ {
			for x := 50 - c.IconSize()/2; x < 50+c.IconSize()/2; x++ {
				pixels = append(pixels, img.NRGBAAt(x-int(doc.ViewBox.X), y-int(doc.ViewBox.Y)))
			}
		}
		return pixels
	}
	uncaptioned := renderIcon()
	c.Text = d2target.Text{Label: "Push bytes", LabelWidth: 72, LabelHeight: 18, FontSize: 14}
	if !slices.Equal(uncaptioned, renderIcon()) {
		t.Fatal("outside caption covers part of the moving image")
	}
}

// Both streams pass underneath the shared on-edge caption. In particular,
// the second wire is drawn after the caption and has no label of its own.
func TestConnectionIconStreamPassesBehindNeighborLabel(t *testing.T) {
	diagram := validDiagram()
	c := &diagram.Connections[0]
	c.Text = d2target.Text{Label: "95%", LabelWidth: 30, LabelHeight: 18, FontSize: 14, Color: "#111111"}
	c.LabelPosition = "INSIDE_MIDDLE_CENTER"
	c.IconAnimation = &d2target.ConnectionIconAnimation{}
	c.IconShape = d2target.ShapeCircle
	c.Stroke = "#ff0000"
	n := *c
	n.ID, n.Label, n.Stroke = "parallel", "", "#ff0000"
	n.Route = []*geo.Point{{X: 20, Y: 12}, {X: 80, Y: 12}}
	diagram.Connections = append(diagram.Connections, n)
	render := func(icons []geo.Point) *image.NRGBA {
		t.Helper()
		for i := range diagram.Connections {
			diagram.Connections[i].IconPositions = icons
		}
		doc, err := Build(context.Background(), diagram, Options{})
		if err != nil {
			t.Fatal(err)
		}
		img, err := d2raster.Render(context.Background(), doc, shapeEffectFrameOptions())
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	without := render([]geo.Point{})
	during := render([]geo.Point{{X: 50, Y: 10}})
	if !bytes.Equal(without.Pix, during.Pix) {
		t.Fatal("dots obscured the on-edge caption")
	}
	aside := render([]geo.Point{{X: 28, Y: 12}})
	if bytes.Equal(aside.Pix, without.Pix) {
		t.Fatal("mask hid a passing dot outside the caption")
	}
}

func TestSparseIconStoryboardUsesTravelWindow(t *testing.T) {
	d := validDiagram()
	c := &d.Connections[0]
	c.IconAnimation = &d2target.ConnectionIconAnimation{Rate: floatPointer(.25)}
	want := time.Duration(math.Round(56 / d2target.IconMotionSpeed * float64(time.Second)))
	if got := ShortestAnimationPeriod(d); got != want {
		t.Fatalf("sparse icon phase = %v, want %v", got, want)
	}
	c.IconAnimation.Rate = floatPointer(10)
	if got := ShortestAnimationPeriod(d); got != 100*time.Millisecond {
		t.Fatalf("dense icon phase = %v", got)
	}
	c.IconAnimation.Rate = floatPointer(0)
	if got := ShortestAnimationPeriod(d); got != 0 {
		t.Fatalf("stopped icon phase = %v", got)
	}
}

func TestConnectionScheduledSceneMatchesSampledFrames(t *testing.T) {
	for _, tc := range []struct {
		name      string
		durations [3]int64
	}{
		{"default speed", [3]int64{}},
		{"switch from rate to timed loop, then drain", [3]int64{0, 550, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) { checkConnectionScheduledFrames(t, tc.durations) })
	}
}

func checkConnectionScheduledFrames(t *testing.T, durations [3]int64) {
	t.Helper()
	root := validDiagram()
	step := validDiagram()
	stop := validDiagram()
	root.Steps = []*d2target.Diagram{step, stop}
	for i, board := range []*d2target.Diagram{root, step, stop} {
		c := &board.Connections[0]
		c.IconShape, c.Stroke = d2target.ShapeCircle, "#bc3415"
		if durations[i] > 0 {
			c.IconAnimation = &d2target.ConnectionIconAnimation{DurationMS: durations[i]}
		} else {
			c.IconAnimation = &d2target.ConnectionIconAnimation{Rate: floatPointer([]float64{5, 11, 0}[i])}
		}
	}
	tl, err := d2animate.NewIconTimeline(root, 310)
	if err != nil {
		t.Fatal(err)
	}
	for _, board := range []*d2target.Diagram{root, step, stop} {
		for _, start := range []int{0, 117} {
			frame := d2animate.Frame{Diagram: board, ElapsedMS: start, DurationMS: 310 - start}
			bake := func(d *d2target.Diagram) *d2scene.Document {
				t.Helper()
				one := *d
				one.Steps = nil
				doc, err := Build(context.Background(), &one, Options{})
				if err != nil {
					t.Fatal(err)
				}
				return doc
			}
			doc := bake(tl.ScenePicture(board, frame))
			for at := start; at < 310; at += 17 {
				opts := shapeEffectFrameOptions()
				opts.Time = time.Duration(at) * time.Millisecond
				opts.MaxAnimationTracks, opts.MaxAnimationKeyframes = 100, 1000
				got, err := d2raster.Render(context.Background(), doc, opts)
				if err != nil {
					t.Fatal(err)
				}
				want, err := d2raster.Render(context.Background(), bake(tl.Sample(board, board, at)), opts)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(got.Pix, want.Pix) {
					t.Fatalf("segment %d, sample %d differs", start, at)
				}
			}
		}
	}
}
