package shape_test

import (
	"bufio"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/shape"
)

// TestShapeEquivalence records everything every shape returns, for a set of
// boxes and method inputs, and compares a hash of it per constructor with
// testdata/equivalence.golden. The golden file was generated from the shapes
// before they were merged into fewer types; the results must not change.
//
// Floats are written with %v, the shortest representation that reads back as
// the same float64 (so -0 and 0 differ), which makes the record bit for bit.
//
// SHAPE_EQUIVALENCE_UPDATE=1 rewrites the golden file;
// SHAPE_EQUIVALENCE_DUMP=DIR writes the full records to DIR to compare them.
func TestShapeEquivalence(t *testing.T) {
	got := map[string]string{}
	for _, c := range equivalenceConstructors() {
		var b strings.Builder
		for _, box := range equivalenceBoxes() {
			recordShape(&b, c.new(box))
		}
		got[c.name] = fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
		if dir := os.Getenv("SHAPE_EQUIVALENCE_DUMP"); dir != "" {
			if err := os.WriteFile(filepath.Join(dir, c.name+".txt"), []byte(b.String()), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	golden := filepath.Join("testdata", "equivalence.golden")
	if os.Getenv("SHAPE_EQUIVALENCE_UPDATE") != "" {
		var lines []string
		for name, h := range got {
			lines = append(lines, name+" "+h+"\n")
		}
		sort.Strings(lines)
		if err := os.WriteFile(golden, []byte(strings.Join(lines, "")), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(golden)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, h, _ := strings.Cut(sc.Text(), " ")
		want[name] = h
	}
	if len(want) != len(got) {
		t.Errorf("golden has %d constructors, the test %d", len(want), len(got))
	}
	for name, h := range got {
		if want[name] != h {
			t.Errorf("%s: results changed (hash %s, want %s)", name, h, want[name])
		}
	}
}

type equivalenceConstructor struct {
	name string
	new  func(*geo.Box) shape.Shape
}

var equivalenceTypes = []string{
	shape.SQUARE_TYPE, shape.REAL_SQUARE_TYPE, shape.PARALLELOGRAM_TYPE, shape.DOCUMENT_TYPE,
	shape.CYLINDER_TYPE, shape.QUEUE_TYPE, shape.PAGE_TYPE, shape.PACKAGE_TYPE, shape.STEP_TYPE,
	shape.CALLOUT_TYPE, shape.STORED_DATA_TYPE, shape.PERSON_TYPE, shape.C4_PERSON_TYPE,
	shape.DIAMOND_TYPE, shape.OVAL_TYPE, shape.CIRCLE_TYPE, shape.HEXAGON_TYPE, shape.CLOUD_TYPE,
	shape.TABLE_TYPE, shape.CLASS_TYPE, shape.TEXT_TYPE, shape.CODE_TYPE, shape.IMAGE_TYPE,
	"", "unknown",
}

func equivalenceConstructors() []equivalenceConstructor {
	cs := []equivalenceConstructor{
		{"NewCallout", shape.NewCallout},
		{"NewCircle", shape.NewCircle},
		{"NewClass", shape.NewClass},
		{"NewCloud", shape.NewCloud},
		{"NewCode", shape.NewCode},
		{"NewCylinder", shape.NewCylinder},
		{"NewDiamond", shape.NewDiamond},
		{"NewDocument", shape.NewDocument},
		{"NewHexagon", shape.NewHexagon},
		{"NewImage", shape.NewImage},
		{"NewOval", shape.NewOval},
		{"NewPackage", shape.NewPackage},
		{"NewPage", shape.NewPage},
		{"NewParallelogram", shape.NewParallelogram},
		{"NewPerson", shape.NewPerson},
		{"NewC4Person", shape.NewC4Person},
		{"NewQueue", shape.NewQueue},
		{"NewRealSquare", shape.NewRealSquare},
		{"NewStep", shape.NewStep},
		{"NewStoredData", shape.NewStoredData},
		{"NewSquare", shape.NewSquare},
		{"NewTable", shape.NewTable},
		{"NewText", shape.NewText},
	}
	for _, typ := range equivalenceTypes {
		typ := typ
		cs = append(cs, equivalenceConstructor{"NewShape(" + typ + ")", func(box *geo.Box) shape.Shape {
			return shape.NewShape(typ, box)
		}})
	}
	return cs
}

// equivalenceBoxes: square, wide, tall, tiny, zero-sized, fractional and at
// negative positions.
func equivalenceBoxes() []*geo.Box {
	return []*geo.Box{
		geo.NewBox(geo.NewPoint(0, 0), 100, 100),
		geo.NewBox(geo.NewPoint(10, 20), 300, 80),
		geo.NewBox(geo.NewPoint(5, 7), 60, 240),
		geo.NewBox(geo.NewPoint(1, 1), 2, 3),
		geo.NewBox(geo.NewPoint(0, 0), 0, 0),
		geo.NewBox(geo.NewPoint(12.375, 3.1), 123.456, 78.9),
		geo.NewBox(geo.NewPoint(-500.5, -250.25), 150, 90),
		geo.NewBox(geo.NewPoint(-33.3, -0.7), 47.25, 19.125),
	}
}

// fitInputs are (width, height, paddingX, paddingY).
var fitInputs = [][4]float64{
	{100, 50, 0, 0},
	{100, 50, 40, 20},
	{50, 100, 10, 30},
	{80, 80, 0, 0},
	{0, 0, 0, 0},
	{12.5, 7.25, 3.5, 1.75},
	{1000, 20, 40, 40},
}

func recordShape(b *strings.Builder, s shape.Shape) {
	fmt.Fprintf(b, "shape box=%s\n", fmtBox(s.GetBox()))
	fmt.Fprintf(b, "type=%q ar1=%v rect=%v\n", s.GetType(), s.AspectRatio1(), s.IsRectangular())
	for _, typ := range equivalenceTypes {
		fmt.Fprintf(b, "is(%q)=%v ", typ, s.Is(typ))
	}
	px, py := s.GetDefaultPadding()
	fmt.Fprintf(b, "\npadding=%v,%v\n", px, py)
	for _, in := range fitInputs {
		w, h := s.GetDimensionsToFit(in[0], in[1], in[2], in[3])
		fmt.Fprintf(b, "fit%v=%v,%v ", in, w, h)
		w, h = s.GetDimensionsToFit(in[0], in[1], px, py)
		fmt.Fprintf(b, "fitdefault=%v,%v\n", w, h)
		fmt.Fprintf(b, "content(%v,%v)=%s\n", in[0], in[1], fmtBox(s.GetInnerBoxForContent(in[0], in[1])))
	}
	fmt.Fprintf(b, "svg=%q\n", s.GetSVGPathData())
	_, isProvider := s.(shape.PathCommandProvider)
	fmt.Fprintf(b, "provider=%v commands=%v\n", isProvider, shape.GetPathCommands(s))
	fmt.Fprintf(b, "perimeter=%s\n", fmtPerimeter(s.Perimeter()))
	// first as made, then after each SetInnerBoxAspectRatio
	for i, ar := range []float64{0, 1.7, 0.4, 1, 0} {
		if i > 0 {
			s.SetInnerBoxAspectRatio(ar)
		}
		fmt.Fprintf(b, "aspect %v: inner=%s\n", ar, fmtBox(s.GetInnerBox()))
		for _, in := range fitInputs {
			fmt.Fprintf(b, "place%v=%v\n", in, s.GetInsidePlacement(in[0], in[1], in[2], in[3]))
		}
		for _, p := range tracePoints(s.GetBox()) {
			fmt.Fprintf(b, "trace(%v,%v)=%v\n", *p[0], *p[1], *shape.TraceToShapeBorder(s, p[0], p[1]))
		}
	}
	fmt.Fprintf(b, "box after=%s\n", fmtBox(s.GetBox()))
}

// tracePoints returns pairs (point on the box border, previous point).
func tracePoints(box *geo.Box) [][2]*geo.Point {
	x, y, w, h := box.TopLeft.X, box.TopLeft.Y, box.Width, box.Height
	pair := func(rx, ry, dx, dy float64) [2]*geo.Point {
		return [2]*geo.Point{geo.NewPoint(rx, ry), geo.NewPoint(rx+dx, ry+dy)}
	}
	return [][2]*geo.Point{
		pair(x+w*0.3, y, 0, -50),
		pair(x+w*0.5, y, 0, -20),
		pair(x, y+h*0.6, -40, 0),
		pair(x+w, y+h*0.5, 30, 10),
		pair(x+w*0.7, y+h, -5, 60),
		pair(x+w*0.1, y+h, 0, 25),
		pair(x, y+h*0.05, -10, -10),
		pair(x+w, y+h*0.95, 7, 0.5),
	}
}

func fmtBox(b *geo.Box) string {
	if b == nil {
		return "nil"
	}
	return fmt.Sprintf("(%v,%v %vx%v)", b.TopLeft.X, b.TopLeft.Y, b.Width, b.Height)
}

func fmtPerimeter(p []geo.Intersectable) string {
	if p == nil {
		return "nil"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d:", len(p))
	for _, it := range p {
		switch it := it.(type) {
		case *geo.Segment:
			fmt.Fprintf(&b, " L(%v %v)", *it.Start, *it.End)
		case *geo.Ellipse:
			fmt.Fprintf(&b, " E(%v %v %v)", *it.Center, it.Rx, it.Ry)
		case *geo.BezierCurve:
			// its control points are unexported
			points := reflect.ValueOf(it).Elem().FieldByName("points")
			b.WriteString(" C(")
			for i := 0; i < points.Len(); i++ {
				p := points.Index(i).Elem()
				fmt.Fprintf(&b, "{%v %v}", p.FieldByName("X").Float(), p.FieldByName("Y").Float())
			}
			b.WriteString(")")
		default:
			fmt.Fprintf(&b, " %T", it)
		}
	}
	return b.String()
}
