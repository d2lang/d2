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
	data, got, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
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
			data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
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
	// Independent TALA layers can use different pins. Animated SVG displays
	// them in one viewport, even when the layouts have no continuity flag.
	first, next := simpleRasterDiagramWithSize(20, 20), simpleRasterDiagramWithSize(20, 20)
	first.Name, next.Name = "left", "右"
	first.Shapes[0].Pos.X = 10
	next.Shapes[0].Pos.X = 160
	root := &d2target.Diagram{IsFolderOnly: true, Layers: []*d2target.Diagram{first, next}}
	plan, err := planStoryboard(t.Context(), root, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 2 || info[1].Board != "layers.右" {
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

func TestStoryboardBoundsIncludeUnsampledTween(t *testing.T) {
	board := func(x int) *d2target.Diagram {
		b := simpleRasterDiagramWithSize(20, 20)
		b.Shapes[0].Pos.X = x
		return b
	}
	root, next := board(0), board(100)
	next.Name = "return"
	next.TweenDurationMS = 400
	next.TweenFrames = []*d2target.Diagram{board(500), board(50)}
	root.Steps = []*d2target.Diagram{next}
	plan, err := planStoryboard(t.Context(), root, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[1].diagram != next.TweenFrames[1] {
		t.Fatalf("unexpected transition sample: %+v", plan)
	}
	data, _, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// A 100-unit move is about 80 pixels in the full 520-unit timeline.
	// Omitting the unsampled excursion to x=500 would stretch it to 200.
	redLeft := func(origin image.Point) int {
		for x := 0; x < storyboardWidth; x++ {
			c := color.NRGBAModel.Convert(img.At(origin.X+x, origin.Y+storyboardHeight/2)).(color.NRGBA)
			if c.R > 245 && c.G < 10 && c.B < 10 {
				return x
			}
		}
		t.Fatal("red shape missing from panel")
		return 0
	}
	a := redLeft(image.Pt(storyboardGap, storyboardGap))
	b := redLeft(image.Pt(storyboardGap, 2*storyboardGap+storyboardHeight+storyboardCaption))
	if dx := b - a; dx < 79 || dx > 81 {
		t.Fatalf("100 world units moved %d pixels on the sheet, want about 80", dx)
	}
}

func TestStoryboardUsesAbsoluteEffectClock(t *testing.T) {
	root, step := simpleRasterDiagram(), simpleRasterDiagram()
	root.Shapes[0].Animated, step.Shapes[0].Animated = true, true
	step.Name = "next"
	root.Steps = []*d2target.Diagram{step}
	plan, err := planStoryboard(t.Context(), root, 1250)
	if err != nil {
		t.Fatal(err)
	}
	data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []storyboardFrameInfo{{"root", 0}, {"root", 500}, {"steps.next", 1250}, {"steps.next", 1500}}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("sample times = %v, want %v", info, want)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	samePanel := func(a, b int) bool {
		at := func(i int) image.Point {
			return image.Pt(storyboardGap+(i%2)*(storyboardWidth+storyboardGap), storyboardGap+(i/2)*(storyboardHeight+storyboardCaption+storyboardGap))
		}
		pa, pb := at(a), at(b)
		for y := 0; y < storyboardHeight; y++ {
			for x := 0; x < storyboardWidth; x++ {
				if img.At(pa.X+x, pa.Y+y) != img.At(pb.X+x, pb.Y+y) {
					return false
				}
			}
		}
		return true
	}
	if !samePanel(1, 3) || samePanel(2, 3) {
		t.Fatal("a one-second pulse must peak at both 500ms and 1500ms, with a distinct phase at 1250ms")
	}
}

func TestStoryboardLinkedLayerCamera(t *testing.T) {
	// A narrow red portal opens a layer pinned far from its overview. The
	// automatic return exits through that same portal.
	root, detail := simpleRasterDiagramWithSize(20, 30), simpleRasterDiagramWithSize(60, 40)
	root.Config = &d2target.Config{AnimateDuration: new(int64(400))}
	root.Shapes[0].Link = "root.layers.detail"
	anchor := root.Shapes[0]
	anchor.ID, anchor.Link, anchor.Fill, anchor.Pos.X = "anchor", "", "#00ff00", -100
	root.Shapes = append(root.Shapes, anchor)
	detail.Name = "detail"
	detail.Shapes[0].Pos = d2target.Point{X: 10000, Y: 12000}
	detail.Shapes[0].Fill = "#0000ff"
	root.Layers = []*d2target.Diagram{detail}

	plan, err := planStoryboard(t.Context(), root, 100)
	if err != nil {
		t.Fatal(err)
	}
	data, info, err := renderStoryboard(t.Context(), plan, d2svg.RenderOpts{Pad: new(int64(0))}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []storyboardFrameInfo{{"root", 0}, {"root", 200}, {"layers.detail", 400}, {"root", 600}}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("linked camera timeline = %+v, want %+v", info, want)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bounds := func(index int, blue bool) image.Rectangle {
		origin := image.Pt(storyboardGap+(index%2)*(storyboardWidth+storyboardGap), storyboardGap+(index/2)*(storyboardHeight+storyboardCaption+storyboardGap))
		var rect image.Rectangle
		for y := range storyboardHeight {
			for x := range storyboardWidth {
				r, g, b, _ := img.At(origin.X+x, origin.Y+y).RGBA()
				if blue {
					r, b = b, r
				}
				if r > 0xd000 && max(g, b) < 0x3000 {
					rect = rect.Union(image.Rect(x, y, x+1, y+1))
				}
			}
		}
		return rect
	}
	portal, entering, child, leaving := bounds(0, false), bounds(1, false), bounds(2, true), bounds(3, false)
	if portal.Dx() < 30 || child.Dx() < 100 || entering.Empty() || leaving.Empty() {
		t.Fatalf("distant, independently framed boards lost their artwork: %+v %+v %+v %+v", portal, entering, child, leaving)
	}
	if portal.Min.X-entering.Min.X < 75 || entering != leaving {
		t.Fatalf("zoom should pan to the red portal and return through it: start %v, inward %v, outward %v", portal, entering, leaving)
	}
}
