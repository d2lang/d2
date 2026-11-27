package d2scenebuild_test

import (
	"bytes"
	"context"
	"image/color"
	"image/png"
	"math"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2raster"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2scenebuild"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestPackageCodeLabelPNGKeepsNotchClear(t *testing.T) {
	t.Parallel()

	talaLayout := func(ctx context.Context, g *d2graph.Graph) error {
		return d2talalayout.Layout(ctx, g, nil)
	}

	tests := []struct {
		name   string
		script string
		layout d2graph.LayoutGraph
		sketch bool
	}{
		{
			name: "tala top-center go code label keeps package notch clear",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`,
			layout: talaLayout,
		},
		{
			name: "tala top-left go code label keeps package notch clear",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-left
  width: 250
  height: 250
}`,
			layout: talaLayout,
		},
		{
			name: "dagre top-center go code label keeps package notch clear",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`,
			layout: d2dagrelayout.DefaultLayout,
		},
		{
			name: "tala sketch top-center go code label keeps package notch clear",
			script: `x: |go
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`,
			layout: talaLayout,
			sketch: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}

			ctx := log.WithTB(context.Background(), t)
			diagram, _, err := d2lib.Compile(ctx, tc.script, &d2lib.CompileOptions{
				Ruler: ruler,
				LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
					return tc.layout, nil
				},
			}, &d2svg.RenderOpts{})
			if err != nil {
				t.Fatalf("compile failed: %v", err)
			}

			buildOpts := d2scenebuild.Options{Sketch: tc.sketch}
			if tc.sketch {
				buildOpts.SketchBudget = d2scenebuild.SketchBudget{
					MaxOperationSets: 100_000,
					MaxOperations:    1_000_000,
					MaxPathCommands:  1_000_000,
				}
			}
			doc, err := d2scenebuild.Build(ctx, diagram, buildOpts)
			if err != nil {
				t.Fatalf("d2scenebuild.Build failed: %v", err)
			}

			s := diagram.Shapes[0]
			topWidth := float64(s.Width) * 0.5
			topHeight := math.Min(55, float64(s.Height)*0.2)

			bgNode := findSceneNodeByID(doc.Root, s.ID+":code-background")
			if bgNode == nil {
				t.Fatalf("missing %q code background node in scene", s.ID+":code-background")
			}
			bgRect, ok := bgNode.Primitive.(d2scene.Rect)
			if !ok {
				t.Fatalf("code background primitive = %T, want d2scene.Rect", bgNode.Primitive)
			}
			if bgRect.Box.X != float64(s.Pos.X) || bgRect.Box.Y != float64(s.Pos.Y)+topHeight {
				t.Fatalf("code background origin = (%v, %v), want (%v, %v)",
					bgRect.Box.X, bgRect.Box.Y, float64(s.Pos.X), float64(s.Pos.Y)+topHeight)
			}

			frame, err := d2raster.Render(ctx, doc, d2raster.FrameOptions{
				Scale: 1, Background: color.White,
				MaxWidth: 2_000, MaxHeight: 2_000, MaxPixels: 4_000_000,
				MaxNodes: 100_000, MaxDepth: 1_000, MaxPathCommands: 1_000_000,
				MaxTextRunesPerRun: 100_000, MaxFontFacesPerText: 16,
				MaxTextCoverageChecks: 1_000_000, MaxTextShapingRuns: 10_000,
				MaxAnimationTracks: 1_000, MaxAnimationKeyframes: 10_000,
				MaxAssets: 100, MaxAssetBytes: 64 << 20, MaxDecodedAssetBytes: 64 << 20,
				MaxImportDepth: 64, MaxOffscreenBytes: 64 << 20, MaxEvenOddClipWork: 1_000_000_000,
			})
			if err != nil {
				t.Fatalf("d2raster.Render failed: %v", err)
			}
			pngBytes, err := d2raster.EncodePNG(ctx, frame)
			if err != nil {
				t.Fatalf("d2raster.EncodePNG failed: %v", err)
			}
			decoded, err := png.Decode(bytes.NewReader(pngBytes))
			if err != nil {
				t.Fatalf("png.Decode failed: %v", err)
			}

			canvasBg := color.NRGBAModel.Convert(decoded.At(2, 2)).(color.NRGBA)
			codeBg := color.NRGBA{R: 0xf7, G: 0xf7, B: 0xf7, A: 0xff}

			// Every interior pixel of the upper-right folder notch must remain
			// the canvas background color rather than the code block fill.
			notchMinX := int(math.Ceil(float64(s.Pos.X)+topWidth-doc.ViewBox.X)) + 6
			notchMaxX := int(math.Floor(float64(s.Pos.X)+float64(s.Width)-doc.ViewBox.X)) - 6
			notchMinY := int(math.Ceil(float64(s.Pos.Y)-doc.ViewBox.Y)) + 6
			notchMaxY := int(math.Floor(float64(s.Pos.Y)+topHeight-doc.ViewBox.Y)) - 6
			for y := notchMinY; y <= notchMaxY; y++ {
				for x := notchMinX; x <= notchMaxX; x++ {
					got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					if got != canvasBg {
						t.Fatalf("notch pixel (%d, %d) = %v, want canvas background %v (code bg=%v)",
							x, y, got, canvasBg, codeBg)
					}
				}
			}

			// The upper-left folder tab must stay visible above the code block
			// (painted with the package fill, not covered by the code background).
			tabSampleX := int(math.Round(float64(s.Pos.X) + topWidth*0.5 - doc.ViewBox.X))
			tabSampleY := int(math.Round(float64(s.Pos.Y) + topHeight*0.5 - doc.ViewBox.Y))
			if got := color.NRGBAModel.Convert(decoded.At(tabSampleX, tabSampleY)).(color.NRGBA); got == codeBg {
				t.Fatalf("folder tab pixel (%d, %d) = %v, unexpectedly covered by code background", tabSampleX, tabSampleY, got)
			}

			// The package body below the tab must paint the code background and
			// syntax-highlighted text ("hi") inside the body padding region.
			bodySampleX := int(math.Round(float64(s.Pos.X) + float64(s.Width)*0.75 - doc.ViewBox.X))
			bodySampleY := int(math.Round(float64(s.Pos.Y) + topHeight + float64(s.Height)*0.4 - doc.ViewBox.Y))
			if got := color.NRGBAModel.Convert(decoded.At(bodySampleX, bodySampleY)).(color.NRGBA); got != codeBg {
				t.Fatalf("body sample pixel (%d, %d) = %v, want code background %v", bodySampleX, bodySampleY, got, codeBg)
			}

			textMinX := int(math.Floor(float64(s.Pos.X) - doc.ViewBox.X + 4))
			textMaxX := int(math.Ceil(float64(s.Pos.X) - doc.ViewBox.X + 60))
			textMinY := int(math.Floor(float64(s.Pos.Y) + topHeight - doc.ViewBox.Y + 4))
			textMaxY := int(math.Ceil(float64(s.Pos.Y) + topHeight - doc.ViewBox.Y + 40))
			textInk := 0
			for y := textMinY; y <= textMaxY; y++ {
				for x := textMinX; x <= textMaxX; x++ {
					got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
					if got != codeBg && got != canvasBg {
						textInk++
					}
				}
			}
			if textInk == 0 {
				t.Fatalf("expected rendered code text ink inside package body [%d..%d, %d..%d]",
					textMinX, textMaxX, textMinY, textMaxY)
			}
		})
	}
}

func TestPackageMarkdownLabelPNGFitsInTabAndKeepsNotchClear(t *testing.T) {
	t.Parallel()

	script := `x: |md
  hi
| {
  shape: package
  label.near: top-center
  width: 250
  height: 250
}`
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	ctx := log.WithTB(context.Background(), t)
	diagram, _, err := d2lib.Compile(ctx, script, &d2lib.CompileOptions{
		Ruler: ruler,
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) {
			return func(ctx context.Context, g *d2graph.Graph) error {
				return d2talalayout.Layout(ctx, g, nil)
			}, nil
		},
	}, &d2svg.RenderOpts{})
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	doc, err := d2scenebuild.Build(ctx, diagram, d2scenebuild.Options{})
	if err != nil {
		t.Fatalf("d2scenebuild.Build failed: %v", err)
	}
	frame, err := d2raster.Render(ctx, doc, d2raster.FrameOptions{
		Scale: 1, Background: color.White,
		MaxWidth: 2_000, MaxHeight: 2_000, MaxPixels: 4_000_000,
		MaxNodes: 100_000, MaxDepth: 1_000, MaxPathCommands: 1_000_000,
		MaxTextRunesPerRun: 100_000, MaxFontFacesPerText: 16,
		MaxTextCoverageChecks: 1_000_000, MaxTextShapingRuns: 10_000,
		MaxAnimationTracks: 1_000, MaxAnimationKeyframes: 10_000,
		MaxAssets: 100, MaxAssetBytes: 64 << 20, MaxDecodedAssetBytes: 64 << 20,
		MaxImportDepth: 64, MaxOffscreenBytes: 64 << 20, MaxEvenOddClipWork: 1_000_000_000,
	})
	if err != nil {
		t.Fatalf("d2raster.Render failed: %v", err)
	}
	pngBytes, err := d2raster.EncodePNG(ctx, frame)
	if err != nil {
		t.Fatalf("d2raster.EncodePNG failed: %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("png.Decode failed: %v", err)
	}

	s := diagram.Shapes[0]
	topWidth := float64(s.Width) * 0.5
	topHeight := math.Min(55, float64(s.Height)*0.2)
	canvasBg := color.NRGBAModel.Convert(decoded.At(2, 2)).(color.NRGBA)

	notchMinX := int(math.Ceil(float64(s.Pos.X)+topWidth-doc.ViewBox.X)) + 6
	notchMaxX := int(math.Floor(float64(s.Pos.X)+float64(s.Width)-doc.ViewBox.X)) - 6
	notchMinY := int(math.Ceil(float64(s.Pos.Y)-doc.ViewBox.Y)) + 6
	notchMaxY := int(math.Floor(float64(s.Pos.Y)+topHeight-doc.ViewBox.Y)) - 6
	for y := notchMinY; y <= notchMaxY; y++ {
		for x := notchMinX; x <= notchMaxX; x++ {
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if got != canvasBg {
				t.Fatalf("markdown notch pixel (%d, %d) = %v, want canvas background %v", x, y, got, canvasBg)
			}
		}
	}

	// Markdown text ink should render inside the upper-left folder tab.
	tabMinX := int(math.Ceil(float64(s.Pos.X)-doc.ViewBox.X)) + 6
	tabMaxX := int(math.Floor(float64(s.Pos.X)+topWidth-doc.ViewBox.X)) - 6
	tabMinY := int(math.Ceil(float64(s.Pos.Y)-doc.ViewBox.Y)) + 6
	tabMaxY := int(math.Floor(float64(s.Pos.Y)+topHeight-doc.ViewBox.Y)) - 6
	tabBg := color.NRGBAModel.Convert(decoded.At(tabMinX, tabMinY)).(color.NRGBA)
	tabInk := 0
	for y := tabMinY; y <= tabMaxY; y++ {
		for x := tabMinX; x <= tabMaxX; x++ {
			if color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA) != tabBg {
				tabInk++
			}
		}
	}
	if tabInk == 0 {
		t.Fatalf("expected rendered markdown ink inside package tab [%d..%d, %d..%d]",
			tabMinX, tabMaxX, tabMinY, tabMaxY)
	}
}

func findSceneNodeByID(root *d2scene.Node, id string) *d2scene.Node {
	if root == nil {
		return nil
	}
	if root.ID == id {
		return root
	}
	for _, child := range root.Children {
		if found := findSceneNodeByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
