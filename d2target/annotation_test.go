package d2target

import (
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

func annotatedShape(id string, x, y, w, h int, a *Annotation) Shape {
	s := *BaseShape()
	s.ID, s.Type = id, ShapeRectangle
	s.Pos, s.Width, s.Height = Point{X: x, Y: y}, w, h
	s.Annotation = a
	return s
}

func straight(id string, x1, y1, x2, y2 float64, a *Annotation) Connection {
	c := *BaseConnection()
	c.ID = id
	c.Route = []*geo.Point{{X: x1, Y: y1}, {X: x2, Y: y2}}
	c.Annotation = a
	return c
}

func inkBounds(m AnnotationMark) (x1, y1, x2, y2 float64) { return m.Bounds() }

func TestAnnotationMarksSurroundTheirTarget(t *testing.T) {
	for _, tc := range []struct {
		shape   string
		strokes int
		// encloses reports whether the ink reaches past every side of the box.
		encloses bool
	}{
		{AnnotationCircle, 1, true},
		{AnnotationBox, 1, true},
		{AnnotationCross, 2, false},
		{AnnotationUnderline, 1, false},
		{AnnotationBrace, 1, false},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			d := &Diagram{Shapes: []Shape{annotatedShape("a", 100, 200, 160, 70, &Annotation{Shape: tc.shape})}}
			marks := d.AnnotationMarks()
			if len(marks) != 1 || len(marks[0].Strokes) != tc.strokes {
				t.Fatalf("got %d marks with strokes %v", len(marks), marks)
			}
			x1, y1, x2, y2 := inkBounds(marks[0])
			encloses := x1 < 100 && y1 < 200 && x2 > 260 && y2 > 270
			if encloses != tc.encloses {
				t.Fatalf("ink (%v,%v)-(%v,%v) encloses the box: %v, want %v", x1, y1, x2, y2, encloses, tc.encloses)
			}
			// No mark strays far from what it points at.
			if x1 < 40 || y1 < 150 || x2 > 320 || y2 > 320 {
				t.Fatalf("ink (%v,%v)-(%v,%v) wanders from its target", x1, y1, x2, y2)
			}
		})
	}
}

func TestAnnotationRingIsDeterministicPerTarget(t *testing.T) {
	ring := func(id string) [][]geo.Point {
		d := &Diagram{Shapes: []Shape{annotatedShape(id, 0, 0, 120, 60, &Annotation{Shape: AnnotationCircle})}}
		return d.AnnotationMarks()[0].Strokes
	}
	a, again, b := ring("a"), ring("a"), ring("b")
	if len(a[0]) != len(again[0]) {
		t.Fatal("the same target drew two different rings")
	}
	for i := range a[0] {
		if a[0][i] != again[0][i] {
			t.Fatal("the same target drew two different rings")
		}
	}
	same := len(a[0]) == len(b[0])
	for i := 0; same && i < len(a[0]); i++ {
		same = a[0][i] == b[0][i]
	}
	if same {
		t.Fatal("two targets drew the same ring; each should have its own hand")
	}
}

func TestAnnotationDrawnShowsPartOfTheMark(t *testing.T) {
	length := func(drawn *[2]float64) float64 {
		d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 200, 80, &Annotation{Shape: AnnotationCross, Drawn: drawn})}}
		total := 0.
		for _, stroke := range d.AnnotationMarks()[0].Strokes {
			total += strokeLength(stroke)
		}
		return total
	}
	whole := length(nil)
	for _, tc := range []struct {
		drawn [2]float64
		want  float64
	}{
		{[2]float64{0, 0.25}, 0.25},
		{[2]float64{0, 0.5}, 0.5},
		{[2]float64{0, 0.75}, 0.75},
		{[2]float64{0.4, 1}, 0.6},
		{[2]float64{0, 1}, 1},
	} {
		got := length(&tc.drawn) / whole
		if math.Abs(got-tc.want) > 0.02 {
			t.Errorf("drawn %v shows %.3f of the mark, want %.2f", tc.drawn, got, tc.want)
		}
	}
	if length(&[2]float64{0, 0}) != 0 || length(&[2]float64{1, 1}) != 0 {
		t.Error("an undrawn or fully erased mark still has ink")
	}

	// The second stroke of a cross starts only after the first is finished.
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 200, 80, &Annotation{Shape: AnnotationCross, Drawn: &[2]float64{0, 0.4}})}}
	if strokes := d.AnnotationMarks()[0].Strokes; len(strokes) != 1 {
		t.Errorf("got %d strokes at 40%%, want only the first", len(strokes))
	}
}

func TestAnnotationNoteAvoidsObstacles(t *testing.T) {
	note := &Annotation{Shape: AnnotationCircle, Label: "note", LabelWidth: 60, LabelHeight: 20}
	target := annotatedShape("a", 200, 200, 100, 60, note)

	side := func(d *Diagram) string {
		box := d.AnnotationMarks()[0].LabelBox
		switch {
		case box.TopLeft.X >= 300:
			return "right"
		case box.TopLeft.X+box.Width <= 200:
			return "left"
		case box.TopLeft.Y+box.Height <= 200:
			return "top"
		}
		return "bottom"
	}

	// With room everywhere the note goes where a hand would put it.
	if got := side(&Diagram{Shapes: []Shape{target}}); got != "right" {
		t.Fatalf("free note went %s, want right", got)
	}
	// A neighbor on the right pushes it above.
	crowded := &Diagram{Shapes: []Shape{target, annotatedShape("b", 320, 200, 100, 60, nil)}}
	if got := side(crowded); got != "top" {
		t.Fatalf("note beside a neighbor went %s, want top", got)
	}
	// A connection running over the top pushes it below.
	crowded.Connections = []Connection{straight("c", 150, 170, 450, 170, nil)}
	if got := side(crowded); got != "bottom" {
		t.Fatalf("note under a connection went %s, want bottom", got)
	}
	// The author's choice is final.
	pinned := *note
	pinned.Position = "center-left"
	crowded.Shapes[0].Annotation = &pinned
	if got := side(crowded); got != "left" {
		t.Fatalf("pinned note went %s, want left", got)
	}
	// A container around the target is where the note is expected to sit.
	inside := &Diagram{Shapes: []Shape{annotatedShape("box", 0, 0, 800, 600, nil), target}}
	if got := side(inside); got != "right" {
		t.Fatalf("note inside a container went %s, want right", got)
	}
}

func TestAnnotationNoteHoldsStillWhileDrawn(t *testing.T) {
	box := func(drawn *[2]float64) geo.Box {
		a := &Annotation{Shape: AnnotationCircle, Label: "note", LabelWidth: 60, LabelHeight: 20, Drawn: drawn}
		d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, a)}}
		return *d.AnnotationMarks()[0].LabelBox
	}
	whole := box(nil)
	for _, p := range []float64{0, 0.1, 0.5, 0.9} {
		if got := box(&[2]float64{0, p}); got.TopLeft.X != whole.TopLeft.X || got.TopLeft.Y != whole.TopLeft.Y {
			t.Fatalf("note moved to %v at progress %v, want %v", got.TopLeft, p, whole.TopLeft)
		}
	}
}

func TestAnnotationOnConnection(t *testing.T) {
	trace := &Annotation{Shape: AnnotationTrace, Label: "2 ms", LabelWidth: 30, LabelHeight: 20}
	d := &Diagram{
		Shapes:      []Shape{annotatedShape("a", 0, 0, 100, 60, nil), annotatedShape("b", 300, 0, 100, 60, nil)},
		Connections: []Connection{straight("(a -> b)[0]", 100, 30, 300, 30, trace)},
	}
	mark := d.AnnotationMarks()[0]
	// A trace is a highlighter over the whole route.
	if mark.Opacity >= 1 || mark.Width <= float64(d.Connections[0].StrokeWidth) {
		t.Fatalf("trace has width %v and opacity %v", mark.Width, mark.Opacity)
	}
	if first, last := mark.Strokes[0][0], mark.Strokes[0][len(mark.Strokes[0])-1]; first.X != 100 || last.X != 300 {
		t.Fatalf("trace runs from %v to %v", first, last)
	}
	// Its note sits off the route, beside its middle.
	note := mark.LabelBox
	if note.TopLeft.Y < 30 && note.TopLeft.Y+note.Height > 30 {
		t.Fatalf("note at %v lies on the route", note.TopLeft)
	}
	if cx := note.TopLeft.X + note.Width/2; cx < 150 || cx > 250 {
		t.Fatalf("note at %v is not beside the middle of the route", note.TopLeft)
	}

	d.Connections[0].Annotation = &Annotation{Shape: AnnotationCross}
	cross := d.AnnotationMarks()[0]
	x1, y1, x2, y2 := cross.Bounds()
	if len(cross.Strokes) != 2 || x1 < 180 || x2 > 220 || y1 < 10 || y2 > 50 {
		t.Fatalf("cross is not a small mark on the middle of the route: (%v,%v)-(%v,%v)", x1, y1, x2, y2)
	}
}

func TestAnnotationsGrowTheBoard(t *testing.T) {
	plain := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, nil)}}
	ptl, pbr := plain.BoundingBox()

	marked := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationCircle, Label: "note", LabelWidth: 60, LabelHeight: 20})}}
	tl, br := marked.BoundingBox()
	if tl.X >= ptl.X || tl.Y >= ptl.Y || br.X < pbr.X+60 || br.Y <= pbr.Y {
		t.Fatalf("board (%v)-(%v) does not hold the ring and its note", tl, br)
	}

	// A frame that has drawn none of the mark reserves the same room.
	marked.Shapes[0].Annotation.Drawn = &[2]float64{0, 0}
	if ftl, fbr := marked.BoundingBox(); ftl != tl || fbr != br {
		t.Fatalf("undrawn frame is (%v)-(%v), want (%v)-(%v)", ftl, fbr, tl, br)
	}
}

func TestCanvasWidensTheBoard(t *testing.T) {
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, nil)}}
	tl, br := d.BoundingBox()

	d.Canvas = &MotionBox{Point: Point{X: -40, Y: tl.Y + 5}, Width: 300, Height: 10}
	ctl, cbr := d.BoundingBox()
	if ctl.X != -40 || cbr.X != 260 {
		t.Fatalf("board spans x %d to %d, want the canvas's -40 to 260", ctl.X, cbr.X)
	}
	// The canvas is a floor: it never trims what the board draws.
	if ctl.Y != tl.Y || cbr.Y != br.Y {
		t.Fatalf("board spans y %d to %d, want its own %d to %d", ctl.Y, cbr.Y, tl.Y, br.Y)
	}

	d.Canvas = &MotionBox{Width: 0, Height: 10}
	if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), "canvas") {
		t.Fatalf("an empty canvas passed validation: %v", err)
	}
}

func TestCaptionBox(t *testing.T) {
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, nil)}}
	d.Root.Annotation = &Annotation{Label: "A caption wider than the board.", LabelWidth: 300, LabelHeight: 30}

	inkTL, inkBR := d.InkBoundingBox()
	box := d.CaptionBox(inkTL, inkBR)
	if box.TopLeft.Y != float64(inkBR.Y+CaptionGap) {
		t.Fatalf("caption top is %v, want it under the ink at %v", box.TopLeft.Y, inkBR.Y+CaptionGap)
	}
	if center := box.TopLeft.X + box.Width/2; math.Abs(center-float64(inkTL.X+inkBR.X)/2) > 1 {
		t.Fatalf("caption is centered at %v", center)
	}
	// The board widens to hold it.
	tl, br := d.BoundingBox()
	if br.X-tl.X < 300 || br.Y < inkBR.Y+CaptionGap+30 {
		t.Fatalf("board (%v)-(%v) does not hold its caption", tl, br)
	}

	// An animation pins every board's caption to one place.
	d.Root.Annotation.Anchor = &Point{X: 500, Y: 400}
	box = d.CaptionBox(inkTL, inkBR)
	if box.TopLeft.X != 350 || box.TopLeft.Y != 400 {
		t.Fatalf("anchored caption is at %v", box.TopLeft)
	}
}

func TestReadingTime(t *testing.T) {
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, nil)}}
	if ms := d.ReadingMS(); ms != 0 {
		t.Fatalf("a board with nothing to read holds for %dms", ms)
	}
	d.Root.Annotation = &Annotation{Label: "One **replica** has [fallen](db.r2) behind."}
	short := d.ReadingMS()
	// Markdown punctuation and link destinations are not read.
	if want := 700 + 58*len("One replica has fallen behind."); short != want {
		t.Fatalf("got %dms, want %dms", short, want)
	}
	d.Shapes[0].Annotation = &Annotation{Shape: AnnotationCircle, Label: "4 s behind"}
	if long := d.ReadingMS(); long <= short {
		t.Fatalf("a note did not add reading time: %d then %d", short, long)
	}
	d.Root.Annotation.Label = strings.Repeat("word ", 5000)
	if ms := d.ReadingMS(); ms != 20_000 {
		t.Fatalf("an essay holds for %dms, want the cap", ms)
	}
}

func TestValidateRenderAnnotation(t *testing.T) {
	valid := func() *Diagram {
		d := &Diagram{
			Shapes:      []Shape{annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationCircle}), annotatedShape("b", 300, 0, 100, 60, nil)},
			Connections: []Connection{straight("(a -> b)[0]", 100, 30, 300, 30, &Annotation{Shape: AnnotationTrace})},
		}
		d.Root.Annotation = &Annotation{Label: "caption"}
		return d
	}
	if err := ValidateRenderTarget(valid()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Diagram)
		want   string
	}{
		{"trace on a shape", func(d *Diagram) { d.Shapes[0].Annotation.Shape = AnnotationTrace }, "shapes[0].annotation.shape"},
		{"ring on a connection", func(d *Diagram) { d.Connections[0].Annotation.Shape = AnnotationCircle }, "connections[0].annotation.shape"},
		{"caption with a mark", func(d *Diagram) { d.Root.Annotation.Shape = AnnotationBox }, "root.annotation.shape"},
		{"unknown mark", func(d *Diagram) { d.Shapes[0].Annotation.Shape = "squiggle" }, "shapes[0].annotation.shape"},
		{"unknown side", func(d *Diagram) { d.Shapes[0].Annotation.Position = "top-left" }, "annotation.position"},
		{"backward ink", func(d *Diagram) { d.Shapes[0].Annotation.Drawn = &[2]float64{0.6, 0.2} }, "annotation.drawn"},
		{"ink past the end", func(d *Diagram) { d.Shapes[0].Annotation.Drawn = &[2]float64{0, 1.5} }, "annotation.drawn"},
		{"NaN ink", func(d *Diagram) { d.Shapes[0].Annotation.Drawn = &[2]float64{math.NaN(), 1} }, "annotation.drawn"},
		{"negative note", func(d *Diagram) { d.Shapes[0].Annotation.LabelWidth = -1 }, "annotation.labelWidth"},
		{"leaving mark", func(d *Diagram) { d.Shapes[1].LeavingAnnotation = &Annotation{Shape: AnnotationTrace} }, "shapes[1].leavingAnnotation.shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := valid()
			tc.mutate(d)
			err := ValidateRenderTarget(d)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error naming %s", err, tc.want)
			}
		})
	}
}

// CI renders on amd64 and laptops on arm64. Their sines, powers and fused
// multiply-adds differ in the last bit, which once made the simplifier keep
// different points of the same stroke on each. Noise far larger than that
// difference must not change a mark.
func TestAnnotationStrokesIgnoreFloatNoise(t *testing.T) {
	shape := annotatedShape("client", 0, 0, 87, 66, nil)
	box := geo.NewBox(geo.NewPoint(0, 0), 87, 66)
	dense := map[string][]geo.Point{}
	// The same curves the marks are built from, before they are simplified.
	// A bowed stroke is symmetric about its middle, so its two halves tie.
	for i := 0; i <= 12; i++ {
		u := float64(i) / 12
		dense["bow"] = append(dense["bow"], geo.Point{X: 6.6 + 73.8*u + 1.5*math.Sin(math.Pi*u), Y: 6.6 + 52.8*u - 2.1*math.Sin(math.Pi*u)})
	}
	for i := 0; i <= 220; i++ {
		a := 2 * math.Pi * float64(i) / 220
		dense["ring"] = append(dense["ring"], geo.Point{X: 43.5 + 60*math.Cos(a), Y: 33 + 45*math.Sin(a)})
	}
	for i := 0; i <= 40; i++ {
		u := float64(i) / 40
		dense["halves"] = append(dense["halves"], geo.Point{X: 10 * u, Y: 0.25 + 2.5*u})
	}
	noise := []float64{3e-13, -7e-13, 1e-12, -2e-13, 5e-13, -9e-13, 0}
	for name, pts := range dense {
		want := simplify(append([]geo.Point(nil), pts...))
		if len(want) < 2 || len(want) >= len(pts) && name != "halves" {
			t.Fatalf("%s: simplified %d points to %d", name, len(pts), len(want))
		}
		for shift := range noise {
			noisy := make([]geo.Point, len(pts))
			for i, p := range pts {
				noisy[i] = geo.Point{X: p.X + noise[(i+shift)%len(noise)], Y: p.Y - noise[(2*i+shift)%len(noise)]}
			}
			got := simplify(noisy)
			if len(got) != len(want) {
				t.Fatalf("%s: noise changed %d points to %d", name, len(want), len(got))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s: noise moved point %d from %v to %v", name, i, want[i], got[i])
				}
			}
		}
	}

	// Every coordinate a mark emits is a whole number of tenths.
	d := &Diagram{Shapes: []Shape{shape}}
	for _, mark := range []string{AnnotationCircle, AnnotationBox, AnnotationUnderline, AnnotationBrace, AnnotationCross} {
		d.Shapes[0].Annotation = &Annotation{Shape: mark}
		for _, stroke := range shapeMarkStrokes(&d.Shapes[0], d.Shapes[0].Annotation, box) {
			for _, p := range stroke {
				if x, y := p.X*10, p.Y*10; math.Abs(x-math.Round(x)) > 1e-9 || math.Abs(y-math.Round(y)) > 1e-9 {
					t.Fatalf("%s: point %v is not on the tenth-of-a-pixel grid", mark, p)
				}
			}
		}
	}
}
