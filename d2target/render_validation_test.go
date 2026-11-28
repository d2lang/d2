package d2target

import (
	"encoding/json"
	"math"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func TestValidateConnectionIconAnimation(t *testing.T) {
	c := *BaseConnection()
	c.IconAnimation = &ConnectionIconAnimation{}
	for _, rate := range []float64{math.Inf(1), math.NaN(), -1} {
		c.IconAnimation.Rate = &rate
		if err := validateRenderConnectionNumbers(c, "edge"); err == nil || !strings.Contains(err.Error(), "iconAnimation.rate") {
			t.Errorf("rate %g: %v", rate, err)
		}
	}
	c.IconAnimation.Rate = nil
	for _, ms := range []int64{-1, math.MaxInt64} {
		c.IconAnimation.DurationMS = ms
		if err := validateRenderConnectionNumbers(c, "edge"); err == nil || !strings.Contains(err.Error(), "iconAnimation.durationMs") {
			t.Errorf("duration %dms: %v", ms, err)
		}
	}
	c.IconAnimation.DurationMS = 200
	for _, rate := range []float64{0, 2} {
		c.IconAnimation.Rate = &rate
		if err := validateRenderConnectionNumbers(c, "edge"); err == nil || !strings.Contains(err.Error(), "either rate or durationMs") {
			t.Errorf("accepted rate %g with a 200ms duration: %v", rate, err)
		}
	}
	c.IconAnimation.Rate = nil
	if c.IconSize() != 10 {
		t.Fatal("moving icons without artwork must use the native dot")
	}
	c.IconPositions = make([]geo.Point, MaxMovingIconsPerConnection+1)
	if err := validateRenderConnectionNumbers(c, "edge"); err == nil || !strings.Contains(err.Error(), "iconPositions") {
		t.Errorf("too many sampled icons: %v", err)
	}
}

func TestValidateConnectionIconShapes(t *testing.T) {
	c := BaseConnection()
	c.IconPosition = "INSIDE_MIDDLE_CENTER"
	for _, shape := range []string{"circle", "square", "triangle", "diamond"} {
		c.IconShape = shape
		if err := c.ValidateIconMotion("edge"); err != nil {
			t.Errorf("shape %q: %v", shape, err)
		}
	}
	c.IconShape = "rectangle"
	if err := c.ValidateIconMotion("edge"); err == nil || !strings.Contains(err.Error(), "iconShape") {
		t.Errorf("unsupported shape: %v", err)
	}
	c.IconShape, c.Icon = ShapeSquare, &url.URL{Path: "image.svg"}
	if err := c.ValidateIconMotion("edge"); err == nil || !strings.Contains(err.Error(), "image URL") {
		t.Errorf("shape and image: %v", err)
	}
}

func TestAnimatedStrokeBounds(t *testing.T) {
	d := NewDiagram()
	s := BaseShape()
	s.ID, s.Pos, s.Width, s.Height = "box", Point{10, 20}, 30, 40
	s.StrokeWidth, s.StrokeWidthOffset = 2, .25
	d.Shapes = []Shape{*s}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	if tl, br := d.BoundingBox(); tl != (Point{8, 18}) || br != (Point{42, 62}) {
		t.Fatalf("bounds of 2.25 px stroke = %v ... %v", tl, br)
	}
	for _, tc := range []struct {
		width  int
		offset float64
	}{{0, -.25}, {2, math.NaN()}, {2, math.Inf(1)}, {2, 1}, {math.MaxInt, .25}} {
		d.Shapes[0].StrokeWidth, d.Shapes[0].StrokeWidthOffset = tc.width, tc.offset
		if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "strokeWidthOffset") {
			t.Errorf("width %d with offset %g: got %v", tc.width, tc.offset, err)
		}
	}
}

func TestValidateTraceEndpoint(t *testing.T) {
	c := *BaseConnection()
	for _, test := range []struct {
		tip   TraceEndpoint
		curve bool
		valid bool
	}{
		{TraceSource, false, true},
		{TraceDestination, false, true},
		{"arbitrary", false, false},
		{TraceDestination, true, false},
	} {
		c.MotionTip, c.IsCurve = test.tip, test.curve
		err := validateRenderConnectionNumbers(c, "edge")
		if (err == nil) != test.valid {
			t.Fatalf("endpoint %q, curve %t: %v", test.tip, test.curve, err)
		}
	}
}

func TestValidateSQLRowReveal(t *testing.T) {
	d := NewDiagram()
	s := BaseShape()
	s.ID, s.Type, s.Width, s.Height = "tracks", ShapeSQLTable, 160, 109
	s.Columns = make([]SQLColumn, 2)
	d.Shapes = []Shape{*s}
	d.Shapes[0].RowReveal = &SQLRowReveal{FullHeight: 120}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []SQLRowReveal{
		{FullHeight: 100}, {FullHeight: 400}, {FullHeight: 0},
	} {
		d.Shapes[0].RowReveal = &invalid
		if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "rowReveal") {
			t.Fatalf("accepted invalid row reveal %+v: %v", invalid, err)
		}
	}
}

func TestValidateRenderTargetBoundsBoardTree(t *testing.T) {
	t.Parallel()

	t.Run("maximum depth", func(t *testing.T) {
		root := NewDiagram()
		cursor := root
		for range maxRenderTargetBoardDepth {
			child := NewDiagram()
			cursor.Layers = []*Diagram{child}
			cursor = child
		}
		if err := ValidateRenderTarget(root); err != nil {
			t.Fatalf("ValidateRenderTarget() at maximum depth: %v", err)
		}
		if _, err := root.HashID(nil); err != nil {
			t.Fatalf("HashID() at maximum validated depth: %v", err)
		}

		cursor.Layers = []*Diagram{NewDiagram()}
		err := ValidateRenderTarget(root)
		if err == nil || !strings.Contains(err.Error(), "exceeds depth") {
			t.Fatalf("ValidateRenderTarget() error = %v, want depth error", err)
		}
	})

	t.Run("maximum board count", func(t *testing.T) {
		root := NewDiagram()
		root.Layers = make([]*Diagram, MaxRenderTargetBoards-1)
		for i := range root.Layers {
			root.Layers[i] = NewDiagram()
		}
		if err := ValidateRenderTarget(root); err != nil {
			t.Fatalf("ValidateRenderTarget() at maximum board count: %v", err)
		}
		if _, err := root.HashID(nil); err != nil {
			t.Fatalf("HashID() at maximum validated board count: %v", err)
		}

		root.Layers = append(root.Layers, NewDiagram())
		err := ValidateRenderTarget(root)
		if err == nil || !strings.Contains(err.Error(), "total boards") {
			t.Fatalf("ValidateRenderTarget() error = %v, want board-count error", err)
		}
	})
}

func TestValidateRenderTargetRejectsReusedBoard(t *testing.T) {
	t.Parallel()

	root := NewDiagram()
	root.Layers = []*Diagram{root}
	err := ValidateRenderTarget(root)
	if err == nil || !strings.Contains(err.Error(), "reuses board") {
		t.Fatalf("ValidateRenderTarget() error = %v, want reused-board error", err)
	}
}

func TestValidateRenderTargetTweenFrames(t *testing.T) {
	invalid := []*Diagram{
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}}, TweenFrameEndsMS: []int{0}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{0}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{3, 3}},
		{TweenDurationMS: 20, TweenFrames: []*Diagram{{}, {}}, TweenFrameEndsMS: []int{2, 21}},
		{TweenDurationMS: 20},
		{TweenDurationMS: -1},
		{TweenDurationMS: 600001, TweenFrames: []*Diagram{{}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{}, {}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{nil}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{Layers: []*Diagram{{}}}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{Shapes: []Shape{{Width: -1}}}}},
		{TweenDurationMS: 1, TweenFrames: []*Diagram{{Shapes: []Shape{{MotionLabelBox: &MotionBox{Width: 20, Height: 0}}}}}},
	}
	for i, diagram := range invalid {
		if err := ValidateRenderTarget(diagram); err == nil {
			t.Errorf("accepted invalid tween target %d", i)
		}
	}
}

func TestValidateRenderInk(t *testing.T) {
	progress := 0.5
	shape := *BaseShape()
	shape.SketchFillTransition = &SketchFillTransition{From: "N2", To: "transparent", Progress: progress}
	shape.UnderlineProgress = &progress
	shape.Label = "previous"
	shape.MarkdownStrike = &MarkdownStrikeTransition{From: "~~previous~~", Progress: progress, PlainRanges: [][2]float64{{0, 1}}}
	target := &Diagram{Shapes: []Shape{shape}}
	raw, err := json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip Diagram
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRenderTarget(&roundTrip); err != nil {
		t.Fatal(err)
	}
	restored := roundTrip.Shapes[0]
	if restored.UnderlineProgress == nil || *restored.UnderlineProgress != progress || restored.MarkdownStrike == nil || restored.MarkdownStrike.From != shape.MarkdownStrike.From || restored.MarkdownStrike.Progress != progress || !slices.Equal(restored.MarkdownStrike.PlainRanges, shape.MarkdownStrike.PlainRanges) || restored.SketchFillTransition == nil || *restored.SketchFillTransition != *shape.SketchFillTransition {
		t.Fatal("lost ink in render target JSON")
	}
	for _, edit := range []func(*Shape){
		func(s *Shape) { s.SketchFillTransition.Progress = math.NaN() },
		func(s *Shape) { s.SketchFillTransition.To = "bogus" },
		func(s *Shape) { *s.UnderlineProgress = math.Inf(-1) },
		func(s *Shape) { s.MarkdownStrike.Progress = 1.01 },
		func(s *Shape) { s.MarkdownStrike.PlainRanges[0][0] = math.NaN() },
		func(s *Shape) { s.Label = "" },
	} {
		if err := json.Unmarshal(raw, &roundTrip); err != nil {
			t.Fatal(err)
		}
		edit(&roundTrip.Shapes[0])
		if err := ValidateRenderTarget(&roundTrip); err == nil {
			t.Error("invalid ink sample accepted")
		}
	}
}

func TestRenderTargetProgressColor(t *testing.T) {
	d := NewDiagram()
	d.Config = &Config{AnimateProgressColor: "DarkOrange"}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	d.Config.AnimateProgressColor = "red;stroke:blue"
	if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "root.config.animateProgressColor") {
		t.Fatalf("expected invalid serialized progress color diagnostic, got %v", err)
	}
}

func TestMotionOffsetBoundsAndValidation(t *testing.T) {
	d := NewDiagram()
	s := BaseShape()
	s.ID, s.Width, s.Height, s.StrokeWidth = "moving", 10, 5, 0
	s.MotionOffset = geo.NewPoint(-.25, .5)
	d.Shapes = []Shape{*s}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
	if tl, br := d.InkBoundingBox(); tl != (Point{-1, 0}) || br != (Point{10, 6}) {
		t.Fatalf("subpixel ink bounds = %v ... %v", tl, br)
	}
	s.Annotation = &Annotation{Shape: AnnotationCircle}
	d.Shapes[0] = *s
	withMotion := d.AnnotationMarks()[0]
	d.Shapes[0].MotionOffset = nil
	atRest := d.AnnotationMarks()[0]
	a, b := withMotion.Strokes[0][0], atRest.Strokes[0][0]
	// Mark strokes are snapped to tenths after their geometry is built.
	if math.Abs((a.X-b.X)+.25) > .051 || math.Abs((a.Y-b.Y)-.5) > .051 {
		t.Fatalf("annotation did not follow the moving shape: %v, %v", a, b)
	}
	for _, bad := range []float64{1, -1, math.NaN(), math.Inf(1)} {
		d.Shapes[0].MotionOffset = geo.NewPoint(0, bad)
		if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "motionOffset") {
			t.Fatalf("accepted invalid offset %v: %v", bad, err)
		}
	}
}
