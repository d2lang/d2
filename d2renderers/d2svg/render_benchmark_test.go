package d2svg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/d2lang/d2/d2target"
)

// BenchmarkSVGMetadata renders already laid-out diagrams with warm font caches.
// The opaque and late-fallback cases exercise costs hidden by plain text-only benchmarks.
func BenchmarkSVGMetadata(b *testing.B) {
	run := func(b *testing.B, d *d2target.Diagram, opts *RenderOpts) {
		b.Helper()
		if _, err := Render(d, opts); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Render(d, opts); err != nil {
				b.Fatal(err)
			}
		}
	}
	sketch := true
	dark := int64(200)
	for _, tc := range []struct {
		name, fixture string
		opts          RenderOpts
	}{
		{"Empty", "sanity/empty", RenderOpts{}},
		{"Sequence", "stable/sequence_diagram_real", RenderOpts{}},
		{"Architecture", "real_world/tpmjs_architecture", RenderOpts{}},
		{"DarkTheme", "real_world/queue_workers", RenderOpts{DarkThemeID: &dark}},
		{"Markdown", "stable/giant_markdown_test", RenderOpts{}},
		{"Code", "stable/code_snippet", RenderOpts{}},
		{"LaTeX", "stable/latex", RenderOpts{}},
		{"Sketch", "real_world/queue_workers", RenderOpts{Sketch: &sketch}},
		{"EmbeddedBoard", "real_world/queue_workers", RenderOpts{MasterID: "d2-master"}},
		{"CustomStylesheet", "real_world/queue_workers", RenderOpts{}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			data, err := os.ReadFile(filepath.Join("..", "..", "e2etests", "testdata", tc.fixture, "dagre", "board.exp.json"))
			if err != nil {
				b.Fatal(err)
			}
			var d d2target.Diagram
			if err := json.Unmarshal(data, &d); err != nil {
				b.Fatal(err)
			}
			if tc.name == "CustomStylesheet" {
				original := BaseStylesheet
				BaseStylesheet += "\n.text { font-variant: small-caps }"
				b.Cleanup(func() { BaseStylesheet = original })
			}
			run(b, &d, &tc.opts)
		})
	}
	for _, tc := range []struct {
		name, shapeType      string
		threeD, lateFallback bool
	}{
		{"Rectangles", d2target.ShapeRectangle, false, false},
		{"Ovals", d2target.ShapeOval, false, false},
		{"RectanglesLateFallback", d2target.ShapeRectangle, false, true},
		{"Opaque3D", d2target.ShapeRectangle, true, false},
		{"Opaque3DLateFallback", d2target.ShapeRectangle, true, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			d := d2target.NewDiagram()
			for i := range 256 {
				s := d2target.BaseShape()
				s.ID = "node" + strconv.Itoa(i)
				s.Type = tc.shapeType
				s.ThreeDee = tc.threeD
				s.Pos = d2target.Point{X: (i % 16) * 120, Y: (i / 16) * 80}
				s.Width, s.Height = 100, 60
				s.Label, s.LabelPosition = s.ID, "INSIDE_MIDDLE_CENTER"
				s.LabelWidth, s.LabelHeight, s.FontSize = 55, 18, 16
				s.Fill, s.Stroke = "B5", "B1"
				d.Shapes = append(d.Shapes, *s)
			}
			if tc.lateFallback {
				// The font and palette collectors tokenize nonbreaking spaces differently.
				d.Shapes[len(d.Shapes)-1].Classes = []string{"fill-B5\u00a0stroke-B2"}
			}
			run(b, d, &RenderOpts{})
		})
	}
}
