package d2svg_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func annotationDiagram() *d2target.Diagram {
	shape := func(id string, x int, a *d2target.Annotation) d2target.Shape {
		s := *d2target.BaseShape()
		s.ID, s.Type = id, d2target.ShapeRectangle
		s.Pos, s.Width, s.Height = d2target.Point{X: x, Y: 0}, 100, 60
		s.Fill, s.Stroke = "#fff", "#000"
		s.Annotation = a
		return s
	}
	d := d2target.NewDiagram()
	d.Shapes = []d2target.Shape{
		shape("a", 0, &d2target.Annotation{Shape: d2target.AnnotationCircle, Label: "**late**", LabelWidth: 30, LabelHeight: 21}),
		shape("b", 300, &d2target.Annotation{Shape: d2target.AnnotationCross, Stroke: "#0000ff"}),
	}
	c := *d2target.BaseConnection()
	c.ID, c.Stroke = "(a -> b)[0]", "#000"
	c.Route = []*geo.Point{{X: 100, Y: 30}, {X: 300, Y: 30}}
	c.Annotation = &d2target.Annotation{Shape: d2target.AnnotationTrace}
	d.Connections = []d2target.Connection{c}
	d.Root.Annotation = &d2target.Annotation{Label: "A caption.", LabelWidth: 110, LabelHeight: 31}
	return d
}

var annotationPath = regexp.MustCompile(`<path d="([^"]+)"[^>]*stroke-linecap:round`)

func annotationInk(svg string) (paths int, bytes int) {
	for _, m := range annotationPath.FindAllStringSubmatch(svg, -1) {
		paths++
		bytes += len(m[1])
	}
	return paths, bytes
}

func TestRenderAnnotations(t *testing.T) {
	t.Parallel()

	out, err := d2svg.Render(annotationDiagram(), nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out)
	// One group per mark, written after every shape and connection so that
	// nothing covers them, then the caption.
	if n := strings.Count(svg, `<g class="annotation">`); n != 3 {
		t.Fatalf("got %d marks, want 3", n)
	}
	if strings.Count(svg, `<g class="annotation caption">`) != 1 {
		t.Fatal("caption is missing")
	}
	if last := strings.LastIndex(svg, `class="shape"`); last > strings.Index(svg, `<g class="annotation">`) {
		t.Fatal("a shape is painted over an annotation")
	}
	// A ring, the two strokes of a cross, and a trace.
	if paths, _ := annotationInk(svg); paths != 4 {
		t.Fatalf("got %d pen strokes, want 4", paths)
	}
	for _, want := range []string{
		d2target.DefaultAnnotationStroke, // the ring and its note
		"#0000ff",                        // the cross's own color
		"late",                           // the note
		"A caption.",
		"opacity:0.38", // a trace is a translucent highlighter
	} {
		if !strings.Contains(svg, want) {
			t.Errorf("output is missing %q", want)
		}
	}
	// The note is Markdown.
	if strings.Contains(svg, "**late**") {
		t.Error("note was not rendered as Markdown")
	}

	// The viewport grows to hold marks, notes and the caption.
	plain := annotationDiagram()
	for i := range plain.Shapes {
		plain.Shapes[i].Annotation = nil
	}
	plain.Connections[0].Annotation, plain.Root.Annotation = nil, nil
	bare, err := d2svg.Render(plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bare), "annotation") {
		t.Fatal("a board without annotations mentions them")
	}
	viewBox := regexp.MustCompile(`class="[^"]*d2-svg" width="(\d+)" height="(\d+)"`)
	if a, b := viewBox.FindStringSubmatch(svg), viewBox.FindStringSubmatch(string(bare)); a == nil || b == nil || len(a[2]) < len(b[2]) || a[2] <= b[2] && len(a[2]) == len(b[2]) {
		t.Fatalf("annotated board %v is not taller than the bare one %v", a, b)
	}
}

func TestRenderPartlyDrawnAnnotations(t *testing.T) {
	t.Parallel()

	render := func(drawn *[2]float64) string {
		d := annotationDiagram()
		d.Shapes[0].Annotation.Drawn = drawn
		d.Shapes[1].Annotation, d.Connections[0].Annotation = nil, nil
		out, err := d2svg.Render(d, nil)
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	whole, half, none := render(nil), render(&[2]float64{0, 0.5}), render(&[2]float64{0, 0})
	_, wholeInk := annotationInk(whole)
	_, halfInk := annotationInk(half)
	if halfInk == 0 || halfInk >= wholeInk {
		t.Fatalf("half a ring has %d bytes of path against %d for the whole", halfInk, wholeInk)
	}
	// The note comes in as the pen finishes.
	if !strings.Contains(whole, "late") || strings.Contains(half, "late") {
		t.Fatal("the note should appear only once the mark is nearly drawn")
	}
	// A mark that has not started leaves nothing behind, and every frame
	// reserves the same room so the picture does not jump.
	if strings.Contains(none, `<g class="annotation">`) {
		t.Fatal("an undrawn mark still wrote a group")
	}
	size := regexp.MustCompile(`viewBox="[^"]+"`)
	if a, b := size.FindAllString(whole, 2), size.FindAllString(none, 2); len(a) != 2 || a[1] != b[1] {
		t.Fatalf("frames have different viewports: %v and %v", a, b)
	}
}

func TestRenderCaptionInItsPen(t *testing.T) {
	t.Parallel()

	d := annotationDiagram()
	d.Shapes[0].Annotation, d.Shapes[1].Annotation, d.Connections[0].Annotation = nil, nil, nil
	plain, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	d.Root.Annotation.Stroke = "#1971c2"
	penned, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "#1971c2") || !strings.Contains(string(penned), "#1971c2") {
		t.Fatal("a caption's stroke should paint its text, and only then")
	}
}

func TestRenderCaptionCrossFade(t *testing.T) {
	t.Parallel()

	d := annotationDiagram()
	d.Root.Annotation.Drawn = &[2]float64{0, 0.25}
	d.Root.LeavingAnnotation = &d2target.Annotation{Label: "The one before.", LabelWidth: 150, LabelHeight: 31, Drawn: &[2]float64{0.5, 1}}
	out, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out)
	leaving, arriving := strings.Index(svg, "The one before."), strings.Index(svg, "A caption.")
	if leaving < 0 || arriving < leaving {
		t.Fatal("both captions should be painted, the leaving one underneath")
	}
	if !strings.Contains(svg, `<g opacity="0.5">`) || !strings.Contains(svg, `<g opacity="0.25">`) {
		t.Fatal("captions are not cross-faded")
	}
}

func TestRenderHighlightAndNoteColor(t *testing.T) {
	t.Parallel()

	d := annotationDiagram()
	d.Shapes[0].Annotation = &d2target.Annotation{Shape: d2target.AnnotationHighlight, Label: "late", LabelWidth: 30, LabelHeight: 21}
	d.Shapes[1].Annotation = &d2target.Annotation{Shape: d2target.AnnotationArrow, Label: "here", LabelWidth: 30, LabelHeight: 21, Stroke: "#0000ff", FontColor: "#008000"}
	out, err := d2svg.Render(d, nil)
	if err != nil {
		t.Fatal(err)
	}
	svg := string(out)
	// A highlighter ends square, in its own yellow, and its note is in the pen.
	if !regexp.MustCompile(`<path d="[^"]+"[^>]*stroke="` + d2target.DefaultHighlightStroke + `"[^>]*stroke-linecap:butt`).MatchString(svg) {
		t.Fatal("no square-ended highlighter stroke in the default yellow")
	}
	if !strings.Contains(svg, "color:"+d2target.DefaultAnnotationStroke) && !strings.Contains(svg, `fill="`+d2target.DefaultAnnotationStroke+`"`) {
		t.Fatal("the highlight's note is not written in the default pen")
	}
	// An arrow is a shaft and two barbs in the pen, and font-color colors its note alone.
	if n := len(regexp.MustCompile(`stroke="#0000ff"[^>]*stroke-linecap:round`).FindAllString(svg, -1)); n != 3 {
		t.Fatalf("arrow drew %d strokes in its pen, want 3", n)
	}
	if !strings.Contains(svg, "#008000") {
		t.Fatal("font-color did not reach the note")
	}
}
