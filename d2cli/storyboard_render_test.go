package d2cli

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
)

func TestStoryboardSeeksNativeTimeline(t *testing.T) {
	board := func(fill string) *d2target.Diagram {
		b := simpleRasterDiagramWithSize(100, 50)
		b.Shapes[0].Fill = fill
		return b
	}
	start, midway, end := board("#0000ff"), board("#800080"), board("#ff0000")
	end.Name = "turn red"
	end.TweenFrames = []*d2target.Diagram{board("#0000ff"), midway}
	end.TweenFrameEndsMS = []int{24, 80}
	end.TweenDurationMS = 80
	start.Steps = []*d2target.Diagram{end}

	plan, err := planStoryboard(t.Context(), start, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, got, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []storyboardFrameInfo{{"root", 50}, {"steps.turn red", 140}, {"steps.turn red", 190}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frames = %+v, want %+v", got, want)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// The contact sheet contains the renderer's actual intermediate paint.
	offsets := []image.Point{{0, 0}, {1, 0}, {0, 1}}
	for i, want := range []color.NRGBA{{B: 255, A: 255}, {R: 128, B: 128, A: 255}, {R: 255, A: 255}} {
		x := storyboardGap + offsets[i].X*(storyboardWidth+storyboardGap)
		y := storyboardGap + offsets[i].Y*(storyboardHeight+storyboardCaption+storyboardGap)
		pixel := color.NRGBAModel.Convert(img.At(x+storyboardWidth/2, y+storyboardHeight/2))
		if pixel != want {
			t.Errorf("panel %d center = %v, want %v", i, pixel, want)
		}
		for _, span := range []image.Rectangle{
			image.Rect(0, storyboardHeight, storyboardWidth/2, storyboardHeight+storyboardCaption),
			image.Rect(storyboardWidth/2, storyboardHeight, storyboardWidth, storyboardHeight+storyboardCaption),
		} {
			rect := span.Add(image.Pt(x, y))
			found := false
			for p := rect.Min; p.Y < rect.Max.Y && !found; p.Y++ {
				for p.X = rect.Min.X; p.X < rect.Max.X; p.X++ {
					r, _, _, _ := img.At(p.X, p.Y).RGBA()
					if r < 0x8888 {
						found = true
						break
					}
				}
			}
			if !found {
				t.Errorf("missing printed caption ink in %v", rect)
			}
		}
	}
}

func TestStoryboardIndependentAnimation(t *testing.T) {
	shape := simpleRasterDiagram()
	shape.Shapes[0].Animated = true
	edge := animatedConnectionRasterDiagram()
	edge.Connections[0].StrokeWidth = 1 // The visible dash pattern repeats every 250ms.
	for _, tc := range []struct {
		name     string
		board    *d2target.Diagram
		interval int
		secondMS int64
	}{
		{"shape", shape, 2000, 500},
		{"connection", edge, 1000, 125},
		{"short board", edge, 100, 50},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planStoryboard(t.Context(), tc.board, tc.interval)
			if err != nil {
				t.Fatal(err)
			}
			data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(info) != 2 || info[0].TimeMS != 0 || info[1].TimeMS != tc.secondMS {
				t.Fatalf("animation samples = %+v", info)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			// Compare only the picture. Different timestamp labels don't prove
			// that we showed a different phase of the shape or dash animation.
			for y := storyboardGap; y < storyboardGap+storyboardHeight; y++ {
				for x := storyboardGap; x < storyboardGap+storyboardWidth; x++ {
					if img.At(x, y) != img.At(x+storyboardWidth+storyboardGap, y) {
						return
					}
				}
			}
			t.Fatal("the two seek times painted the same picture")
		})
	}
}

func TestStoryboardFolderPathsAndReadingHolds(t *testing.T) {
	first, step := simpleRasterDiagram(), simpleRasterDiagram()
	first.Name = "part.one"
	first.Root.Annotation = &d2target.Annotation{Label: "This sentence takes longer than the interval to read."}
	step.Name = "next"
	first.Steps = []*d2target.Diagram{step}
	plan, err := planStoryboard(t.Context(), &d2target.Diagram{IsFolderOnly: true, Layers: []*d2target.Diagram{first}}, 80)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[1].Board != `layers."part.one".steps.next` ||
		plan[1].TimeMS != int64(first.ReadingMS()+40) {
		t.Fatalf("nested plan = %+v", plan)
	}
}

func TestStoryboardLimitsKeepOnePanelPerBoard(t *testing.T) {
	root := &d2target.Diagram{IsFolderOnly: true}
	for range storyboardMaxFrames {
		b := animatedConnectionRasterDiagram()
		root.Steps = append(root.Steps, b)
	}
	plan, err := planStoryboard(t.Context(), root, 100)
	if err != nil || len(plan) != storyboardMaxFrames {
		t.Fatalf("full plan has %d frames, err %v", len(plan), err)
	}
	for i, sample := range plan {
		if sample.diagram != root.Steps[i] {
			t.Fatalf("board %d was omitted", i)
		}
	}
	root.Steps = append(root.Steps, simpleRasterDiagram())
	if _, err := planStoryboard(t.Context(), root, 100); err == nil {
		t.Fatal("expected error for excessive board count")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := planStoryboard(ctx, root, 100); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled plan: %v", err)
	}
}

func TestStoryboardPreservesPinnedMotion(t *testing.T) {
	root, next := simpleRasterDiagramWithSize(20, 20), simpleRasterDiagramWithSize(20, 20)
	next.Name = "右"
	next.LayoutContinuity = true
	next.Shapes[0].Pos.X = 150
	root.Steps = []*d2target.Diagram{next}
	plan, err := planStoryboard(t.Context(), root, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 2 || info[1].Board != "steps.右" {
		t.Fatalf("lost Unicode board path: %+v", info)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var left []int
	for i := range info {
		x0 := storyboardGap + i*(storyboardWidth+storyboardGap)
		y := storyboardGap + storyboardHeight/2
		for dx := 0; dx < storyboardWidth; dx++ {
			c := color.NRGBAModel.Convert(img.At(x0+dx, y)).(color.NRGBA)
			if c.R > 245 && c.G < 10 && c.B < 10 {
				left = append(left, dx)
				break
			}
		}
	}
	if len(left) != 2 || left[1]-left[0] != 300 {
		t.Fatalf("red object offsets = %v; want 150 diagram units of travel at 2x", left)
	}
}
