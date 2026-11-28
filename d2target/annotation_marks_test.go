package d2target

import (
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func markOf(t *testing.T, d *Diagram, id string) AnnotationMark {
	t.Helper()
	for _, m := range d.AnnotationMarks() {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("no mark for %q", id)
	return AnnotationMark{}
}

func labelled(s Shape, text string, w, h int) Shape {
	s.Label, s.LabelWidth, s.LabelHeight = text, w, h
	s.LabelPosition = label.InsideMiddleCenter.String()
	return s
}

// inLoop reports whether the point is inside the closed loop the stroke makes.
func inLoop(stroke []geo.Point, x, y float64) bool {
	inside := false
	for i, j := 0, len(stroke)-1; i < len(stroke); j, i = i, i+1 {
		a, b := stroke[i], stroke[j]
		if (a.Y > y) != (b.Y > y) && x < (b.X-a.X)*(y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
	}
	return inside
}

func TestHighlightRunsOverTheLabel(t *testing.T) {
	note := &Annotation{Shape: AnnotationHighlight, Label: "note", LabelWidth: 40, LabelHeight: 20}
	d := &Diagram{Shapes: []Shape{labelled(annotatedShape("a", 100, 100, 120, 66, note), "API", 30, 21)}}
	m := markOf(t, d, "a")
	if len(m.Strokes) != 1 || !m.Flat || m.Opacity != AnnotationHighlightOpacity || m.Paint != DefaultHighlightStroke {
		t.Fatalf("highlight is %d strokes, flat %v, opacity %v, paint %q", len(m.Strokes), m.Flat, m.Opacity, m.Paint)
	}
	// As tall as the line of text, and a little longer than it at each end.
	pass := m.Strokes[0]
	from, to := pass[0], pass[len(pass)-1]
	if m.Width != 21 || from.X != 140 || to.X != 180 || math.Abs(from.Y-133) > 3 || math.Abs(to.Y-133) > 3 {
		t.Fatalf("highlight is %v wide from %v to %v, want it along the label at (145,122.5) 30x21", m.Width, from, to)
	}
	// A highlighter is too pale to write with: the note is in the default
	// pen, and beside the shape, not beside the label inside it.
	if m.NotePaint != DefaultAnnotationStroke {
		t.Fatalf("note is written in %q", m.NotePaint)
	}
	shape := geo.NewBox(geo.NewPoint(100, 100), 120, 66)
	if m.LabelBox == nil || overlapArea(m.LabelBox, shape) > 0 {
		t.Fatalf("note at %v covers the shape it describes", m.LabelBox)
	}

	// The pen colors the highlighter and font-color the note, separately.
	note.Stroke, note.FontColor = "#74C0FC", "navy"
	if m := markOf(t, d, "a"); m.Paint != "#74C0FC" || m.NotePaint != "navy" {
		t.Fatalf("mark %q and note %q, want the pen and the font color", m.Paint, m.NotePaint)
	}

	// One pass per line of text.
	d.Shapes[0] = labelled(d.Shapes[0], "two\nlines", 40, 42)
	if m := markOf(t, d, "a"); len(m.Strokes) != 2 || m.Width != 21 {
		t.Fatalf("two lines got %d passes %v wide", len(m.Strokes), m.Width)
	}

	// With no label it runs across the middle of the shape.
	d.Shapes[0] = annotatedShape("a", 100, 100, 120, 66, &Annotation{Shape: AnnotationHighlight})
	m = markOf(t, d, "a")
	if _, y1, _, y2 := m.Bounds(); len(m.Strokes) != 1 || y1 < 110 || y2 > 156 {
		t.Fatalf("unlabelled highlight spans y %v to %v", y1, y2)
	}

	// On a connection it runs over the connection's label.
	c := straight("(a -> b)[0]", 0, 0, 200, 0, &Annotation{Shape: AnnotationHighlight})
	c.Label, c.LabelWidth, c.LabelHeight, c.LabelPosition = "40 ms", 40, 21, label.InsideMiddleCenter.String()
	d = &Diagram{Connections: []Connection{c}}
	m = markOf(t, d, "(a -> b)[0]")
	pass = m.Strokes[0]
	if x1, x2 := pass[0].X, pass[len(pass)-1].X; !m.Flat || x1 != 75 || x2 != 125 {
		t.Fatalf("connection highlight runs from x %v to %v, flat %v; want it along the label in the middle", x1, x2, m.Flat)
	}
}

func TestArrowRunsFromItsNoteToItsTarget(t *testing.T) {
	a := &Annotation{Shape: AnnotationArrow, Label: "look", LabelWidth: 40, LabelHeight: 20}
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 100, 100, 100, 60, a)}}
	shape := geo.NewBox(geo.NewPoint(100, 100), 100, 60)
	m := markOf(t, d, "a")
	if len(m.Strokes) != 3 {
		t.Fatalf("arrow is %d strokes, want a shaft and two barbs", len(m.Strokes))
	}
	shaft := m.Strokes[0]
	from, tip := shaft[0], shaft[len(shaft)-1]
	// It leaves the note and stops just short of the shape.
	if d := math.Hypot(from.X-nearestOnBox(m.LabelBox, from).X, from.Y-nearestOnBox(m.LabelBox, from).Y); d > 6 {
		t.Fatalf("arrow starts %v from its note", d)
	}
	near := nearestOnBox(shape, tip)
	if d := math.Hypot(tip.X-near.X, tip.Y-near.Y); d < 2 || d > 10 {
		t.Fatalf("arrow stops %v from its target", d)
	}
	if overlapArea(m.LabelBox, growBox(shape, 20)) > 0 {
		t.Fatalf("note at %v is not standing off from its target", m.LabelBox)
	}
	for _, barb := range m.Strokes[1:] {
		if barb[0] != tip || len(barb) != 2 || math.Hypot(barb[1].X-tip.X, barb[1].Y-tip.Y) < 8 {
			t.Fatalf("barb %v does not run back from the tip %v", barb, tip)
		}
	}

	// The same arrow whether it is being drawn or erased.
	d.Shapes[0].LeavingAnnotation = a
	if leaving := markOf(t, d, "a:leaving"); len(leaving.Strokes) != 3 || leaving.Strokes[0][3] != shaft[3] {
		t.Fatal("the leaving arrow bows the other way")
	}
	d.Shapes[0].LeavingAnnotation = nil

	// near picks the side the arrow comes from.
	a.Position = "bottom-center"
	if m := markOf(t, d, "a"); m.LabelBox.TopLeft.Y < 160 {
		t.Fatalf("note at %v, want it below the shape", m.LabelBox)
	}

	// An arrow with no words still points.
	d.Shapes[0].Annotation = &Annotation{Shape: AnnotationArrow}
	if m := markOf(t, d, "a"); len(m.Strokes) != 3 || m.LabelBox != nil {
		t.Fatalf("wordless arrow is %d strokes with note %v", len(m.Strokes), m.LabelBox)
	}

	// On a connection it points at the middle of the route.
	d = &Diagram{Connections: []Connection{straight("(a -> b)[0]", 0, 0, 200, 0, &Annotation{Shape: AnnotationArrow, Label: "slow", LabelWidth: 30, LabelHeight: 20})}}
	m = markOf(t, d, "(a -> b)[0]")
	tip = m.Strokes[0][len(m.Strokes[0])-1]
	if math.Hypot(tip.X-100, tip.Y) > 12 {
		t.Fatalf("arrow tip %v is not at the middle of the route", tip)
	}
}

func TestLassoLoopsRoundEveryMember(t *testing.T) {
	lasso := &Annotation{Shape: AnnotationLasso, Members: []string{"b", "c"}, Label: "together", LabelWidth: 60, LabelHeight: 20}
	d := &Diagram{Shapes: []Shape{
		annotatedShape("a", 0, 0, 100, 60, lasso),
		annotatedShape("b", 400, 0, 100, 60, nil),
		annotatedShape("c", 200, 300, 100, 60, nil),
		annotatedShape("far", 800, 0, 100, 60, nil),
	}}
	m := markOf(t, d, "a")
	if len(m.Strokes) != 1 {
		t.Fatalf("lasso is %d strokes, want one loop", len(m.Strokes))
	}
	loop := m.Strokes[0]
	for _, center := range [][2]float64{{50, 30}, {450, 30}, {250, 330}} {
		if !inLoop(loop, center[0], center[1]) {
			t.Fatalf("the loop leaves out the member at %v", center)
		}
	}
	if inLoop(loop, 850, 30) {
		t.Fatal("the loop holds a shape that is not a member")
	}
	// It runs past its own start, as a hand does, without meeting it.
	first, last := loop[0], loop[len(loop)-1]
	if gap := math.Hypot(first.X-last.X, first.Y-last.Y); gap < 3 {
		t.Fatalf("the loop closes exactly on itself (gap %v)", gap)
	}

	// A shape that pokes into the loop from the side is gone round.
	d = &Diagram{Shapes: []Shape{
		annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationLasso, Members: []string{"b"}}),
		annotatedShape("b", 400, 0, 100, 60, nil),
		annotatedShape("poking", 200, 40, 100, 100, nil),
	}}
	loop = markOf(t, d, "a").Strokes[0]
	if inLoop(loop, 250, 48) || inLoop(loop, 205, 45) || inLoop(loop, 295, 45) {
		t.Fatal("the loop cuts through a shape that pokes into it")
	}
	for _, center := range [][2]float64{{50, 30}, {450, 30}, {250, 10}} {
		if !inLoop(loop, center[0], center[1]) {
			t.Fatalf("going round a shape left out %v", center)
		}
	}
	// A shape that sits right among the members cannot be left out.
	d.Shapes[2] = annotatedShape("among", 220, 10, 60, 30, nil)
	if loop = markOf(t, d, "a").Strokes[0]; !inLoop(loop, 250, 25) {
		t.Fatal("the loop bent round a shape that sits among its members")
	}

	// A lasso on one shape holds that shape, and a container holds its children.
	d = &Diagram{Shapes: []Shape{
		annotatedShape("box", 0, 0, 300, 200, &Annotation{Shape: AnnotationLasso}),
		annotatedShape("box.child", 40, 60, 100, 60, nil),
	}}
	loop = markOf(t, d, "box").Strokes[0]
	if !inLoop(loop, 90, 90) || !inLoop(loop, 5, 5) || !inLoop(loop, 295, 195) {
		t.Fatal("a lasso on a container does not hold all of it")
	}
}

func TestSegmentCrossesBox(t *testing.T) {
	box := geo.NewBox(geo.NewPoint(10, 10), 20, 20)
	for _, tc := range []struct {
		ax, ay, bx, by float64
		want           bool
	}{
		{0, 20, 40, 20, true},    // straight through
		{0, 0, 40, 40, true},     // corner to corner
		{0, 10, 40, 10, false},   // along the top edge
		{0, 0, 9, 40, false},     // passes to the left
		{15, 15, 25, 25, true},   // wholly inside
		{0, 0, 10, 10, false},    // ends on a corner
		{30, 0, 30, 40, false},   // along the right edge
		{-10, 35, 35, -10, true}, // clips the corner
	} {
		if got := segmentCrossesBox(geo.Point{X: tc.ax, Y: tc.ay}, geo.Point{X: tc.bx, Y: tc.by}, box); got != tc.want {
			t.Errorf("(%v,%v)-(%v,%v): got %v, want %v", tc.ax, tc.ay, tc.bx, tc.by, got, tc.want)
		}
	}
}

func TestNewMarksStayOnTheTenthGrid(t *testing.T) {
	d := &Diagram{
		Shapes: []Shape{
			labelled(annotatedShape("a", 3, 7, 87, 66, &Annotation{Shape: AnnotationHighlight}), "API", 31, 21),
			annotatedShape("b", 211, 13, 87, 66, &Annotation{Shape: AnnotationArrow, Label: "n", LabelWidth: 33, LabelHeight: 21}),
			annotatedShape("c", 97, 201, 87, 66, &Annotation{Shape: AnnotationLasso, Members: []string{"a", "b"}}),
		},
		Connections: []Connection{straight("(a -> b)[0]", 90, 40, 211, 46, &Annotation{Shape: AnnotationArrow})},
	}
	for _, m := range d.AnnotationMarks() {
		if len(m.Strokes) == 0 {
			t.Fatalf("%s drew nothing", m.ID)
		}
		for _, stroke := range m.Strokes {
			for _, p := range stroke {
				if x, y := p.X*10, p.Y*10; math.Abs(x-math.Round(x)) > 1e-9 || math.Abs(y-math.Round(y)) > 1e-9 {
					t.Fatalf("%s: point %v is not on the tenth-of-a-pixel grid", m.ID, p)
				}
			}
		}
	}
}

func TestValidateNewMarks(t *testing.T) {
	ok := &Diagram{
		Shapes: []Shape{
			annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationLasso, Members: []string{"b"}}),
			annotatedShape("b", 200, 0, 100, 60, &Annotation{Shape: AnnotationHighlight}),
		},
		Connections: []Connection{straight("(a -> b)[0]", 100, 30, 200, 30, &Annotation{Shape: AnnotationArrow})},
	}
	if err := ValidateRenderTarget(ok); err != nil {
		t.Fatal(err)
	}
	for name, breakIt := range map[string]func(*Diagram){
		"shape": func(d *Diagram) { d.Connections[0].Annotation = &Annotation{Shape: AnnotationLasso} },
		"members": func(d *Diagram) {
			d.Shapes[1].Annotation = &Annotation{Shape: AnnotationHighlight, Members: []string{"a"}}
		},
	} {
		d := &Diagram{Shapes: append([]Shape(nil), ok.Shapes...), Connections: append([]Connection(nil), ok.Connections...)}
		breakIt(d)
		if err := ValidateRenderTarget(d); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestLassoDoesNotCrossItself(t *testing.T) {
	// The shape between the two members is taller than the loop, so it reaches
	// across both of the loop's long edges and cannot be left out.
	d := &Diagram{Shapes: []Shape{
		annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationLasso, Members: []string{"b"}}),
		annotatedShape("b", 400, 0, 100, 60, nil),
		annotatedShape("tall", 200, -100, 100, 300, nil),
	}}
	loop := markOf(t, d, "a").Strokes[0]
	if !inLoop(loop, 250, 30) {
		t.Fatal("the loop bent round a shape that reaches right across it")
	}
	for _, p := range loop {
		if p.Y < -25 || p.Y > 85 {
			t.Fatalf("the loop leaves its hull at %v", p)
		}
	}
}

func TestMarksFindCenteredMarkdown(t *testing.T) {
	// Markdown sits in the middle of a shape that is larger than its text.
	s := annotatedShape("m", 0, 0, 400, 120, &Annotation{Shape: AnnotationHighlight})
	s.Type, s.Language = ShapeText, "markdown"
	s.Label, s.LabelWidth, s.LabelHeight = "some markdown", 100, 24
	d := &Diagram{Shapes: []Shape{s}}
	pass := markOf(t, d, "m").Strokes[0]
	if from, to := pass[0], pass[len(pass)-1]; from.X != 145 || to.X != 255 || math.Abs(from.Y-60) > 3 {
		t.Fatalf("highlight runs from %v to %v, want it over the text in the middle", from, to)
	}
	d.Shapes[0].Annotation = &Annotation{Shape: AnnotationUnderline}
	if _, y1, _, y2 := markOf(t, d, "m").Bounds(); y1 < 70 || y2 > 90 {
		t.Fatalf("underline spans y %v to %v, want it under the text in the middle", y1, y2)
	}
}

func TestReplacedNoteDoesNotCrowdItsSuccessor(t *testing.T) {
	now := &Annotation{Shape: AnnotationCircle, Label: "second", LabelWidth: 50, LabelHeight: 20}
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, now)}}
	alone := *markOf(t, d, "a").LabelBox
	// While the old note is erased, the new one is already where it will be.
	d.Shapes[0].LeavingAnnotation = &Annotation{Shape: AnnotationCircle, Label: "first", LabelWidth: 40, LabelHeight: 20}
	if got := *markOf(t, d, "a").LabelBox; got.TopLeft.X != alone.TopLeft.X || got.TopLeft.Y != alone.TopLeft.Y {
		t.Fatalf("the new note is at %v while the old one leaves, and at %v after", got.TopLeft, alone.TopLeft)
	}
}

func TestAnnotationPenIsOneColor(t *testing.T) {
	for _, paint := range []string{"url(https://example.com/paint.svg#p)", "linear-gradient(#f00, #00f)", "notacolor"} {
		for _, set := range []func(*Annotation){
			func(a *Annotation) { a.Stroke = paint },
			func(a *Annotation) { a.FontColor = paint },
		} {
			a := &Annotation{Shape: AnnotationCircle}
			set(a)
			d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, a)}}
			if err := ValidateRenderTarget(d); err == nil {
				t.Fatalf("%q passed as an annotation's paint", paint)
			}
		}
	}
	d := &Diagram{Shapes: []Shape{annotatedShape("a", 0, 0, 100, 60, &Annotation{Shape: AnnotationCircle, Stroke: "royalblue", FontColor: "#0A0F25"})}}
	if err := ValidateRenderTarget(d); err != nil {
		t.Fatal(err)
	}
}
