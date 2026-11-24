package d2isometricimg

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2renderers/d2scene"
)

func TestSVGSurfaceResourcesAreSharedAcrossPaintPasses(t *testing.T) {
	ctx := nativeVectorContext(context.Background())
	p, _ := newTextPainter(ctx, 1)
	texture, _, err := p.texture("shared face", normalPrintStyle())
	if err != nil {
		t.Fatal(err)
	}
	surface := nativeVectorForTexture(ctx, texture)
	writer := &nativeSVGWriter{ctx: ctx}
	first := writer.surfaceDefinition(surface)
	size := writer.buf.Len()
	for i := 0; i < 1000; i++ {
		if got := writer.surfaceDefinition(surface); got != first || writer.buf.Len() != size {
			t.Fatal("mesh or shadow pass duplicated retained vector source")
		}
	}
	if writer.err != nil {
		t.Fatal(writer.err)
	}
	other := &nativeSVGWriter{ctx: ctx}
	if other.surfaceDefinition(surface) != first || other.buf.Len() != size {
		t.Fatal("surface definitions leaked between exports")
	}
}

func TestNativeSVGLargeRichTextAndPaperStayWithinBudget(t *testing.T) {
	for _, fixture := range []string{"stable/giant_markdown_test/dagre/board.exp.json", "patterns/paper/dagre/board.exp.json"} {
		t.Run(fixture, func(t *testing.T) {
			diagram := sourcePanelFixture(t, fixture)
			svg, err := Render(context.Background(), diagram, &Options{Format: SVG, Width: 1200, Height: 1200, FitContent: true, Render: d2isometric.RenderOpts{}})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d vector SVG bytes", fixture, len(svg))
			if strings.Contains(string(svg), "data:image") {
				t.Fatal("large vector source was replaced by raster pixels")
			}
			if strings.Contains(fixture, "giant_markdown") {
				uses := strings.Count(string(svg), `href="#surface-`)
				definitions := strings.Count(string(svg), `id="surface-`)
				if uses < definitions*4 {
					t.Fatalf("large rich text did not reuse glyph definitions: uses=%d definitions=%d", uses, definitions)
				}
			}
		})
	}
}

func TestIdenticalPatternArtworkSharedBetweenSurfaces(t *testing.T) {
	writer := &nativeSVGWriter{ctx: context.Background()}
	tile := d2scene.Box{Width: 2, Height: 2}
	for _, red := range []uint8{0, 255, 0} {
		pattern := d2scene.PatternPaint{Tile: tile, Transform: d2scene.Identity(), Root: d2scene.NewNode(d2scene.Rect{Box: tile, Fill: d2scene.SolidPaint{Color: color.NRGBA{R: red, A: 255}}})}
		writer.surfaceDefinition(&nativeVectorSurface{document: nativePaintDocument(pattern)})
	}
	if writer.err != nil {
		t.Fatal(writer.err)
	}
	markup := writer.buf.String()
	if strings.Count(markup, "<pattern ") != 2 || strings.Count(markup, "url(#surface-0--paint-1)") != 2 || strings.Count(markup, "url(#surface-1--paint-1)") != 1 {
		t.Fatal("expected two identical tiles to reference the same paint server and the distinct tile to have its own", markup)
	}
}
