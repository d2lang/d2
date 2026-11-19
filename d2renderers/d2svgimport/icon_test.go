package d2svgimport

import (
	"context"
	"image/color"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

func TestIconGradientCoordinatesAndPixels(t *testing.T) {
	const stops = `<stop stop-color="red"/><stop offset="100%" stop-color="blue"/>`
	for _, units := range []string{"", ` gradientUnits="objectBoundingBox"`} {
		result := mustImport(t, `<svg viewBox="0 0 40 20"><linearGradient id="g"`+units+`>`+stops+`</linearGradient>
            <rect x="4" y="2" width="32" height="16" fill="url(#g)"/></svg>`)
		frame := renderImportedRaster(t, result)
		left, right := frame.NRGBAAt(4, 10), frame.NRGBAAt(35, 10)
		if left.R < 245 || left.B > 10 || right.B < 245 || right.R > 10 || left.A != 255 || right.A != 255 {
			t.Fatalf("gradient units %q: left %v, right %v", units, left, right)
		}
	}

	result := mustImport(t, `<svg viewBox="0 0 40 20"><defs>
        <linearGradient id="g" gradientUnits="userSpaceOnUse" x1="25%" spreadMethod="reflect">`+stops+`</linearGradient></defs>
        <rect width="40" height="20" fill="url(#g)"/>
        <svg width="10" height="10" viewBox="0 0 8 4"><rect width="8" height="4" fill="url(#g)"/></svg></svg>`)
	outer := result.Root.Children[0].Primitive.(d2scene.Rect).Fill.(d2scene.LinearGradient)
	inner := result.Root.Children[1].Children[0].Children[0].Primitive.(d2scene.Rect).Fill.(d2scene.LinearGradient)
	if outer.Start.X != 10 || outer.End.X != 40 || inner.Start.X != 2 || inner.End.X != 8 || inner.Spread != d2scene.SpreadReflect {
		t.Fatalf("user-space gradient in outer and nested viewports: %+v, %+v", outer, inner)
	}

	radial := mustImport(t, `<svg viewBox="0 0 40 40"><radialGradient id="r">`+stops+`</radialGradient>
        <rect x="4" y="4" width="32" height="32" fill="url(#r)"/></svg>`)
	frame := renderImportedRaster(t, radial)
	center, rim := frame.NRGBAAt(20, 20), frame.NRGBAAt(35, 20)
	if center.R < 235 || rim.B < 240 || center.A != 255 || rim.A != 255 {
		t.Fatalf("radial gradient center %v, rim %v", center, rim)
	}
}

func TestIconGradientStopDefaultsAndDegeneratePaint(t *testing.T) {
	result := mustImport(t, `<svg viewBox="0 0 4 2"><defs>
        <linearGradient/><linearGradient id="empty"/><linearGradient id="step" x2="0" spreadMethod="repeat">
        <stop offset="-20%"/><stop offset=".75"/><stop offset=".25"/><stop offset="150%" stop-color="red"/></linearGradient>
        </defs><rect width="2" height="2" fill="url(#empty)"/><rect x="2" width="2" height="2" fill="url(#step)"/></svg>`)
	gradient := result.Root.Children[len(result.Root.Children)-1].Primitive.(d2scene.Rect).Fill.(d2scene.LinearGradient)
	for index, want := range []float64{0, .75, .75, 1} {
		if gradient.Stops[index].Offset != want {
			t.Fatalf("stop %d = %g, want %g", index, gradient.Stops[index].Offset, want)
		}
	}
	frame := renderImportedRaster(t, result)
	if frame.NRGBAAt(0, 0).A != 0 || frame.NRGBAAt(2, 0) != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("empty and zero-length gradients painted %v, %v", frame.NRGBAAt(0, 0), frame.NRGBAAt(2, 0))
	}
}

func TestIconMaskRegionContentAndType(t *testing.T) {
	for _, test := range []struct {
		mode  string
		alpha uint8
	}{{"", 0}, {` mask-type="alpha"`, 255}} {
		result := mustImport(t, `<svg viewBox="0 0 32 16"><mask id="m" x="25%" width="50%" maskContentUnits="objectBoundingBox"`+test.mode+`>
            <rect width=".5" height="1" fill="white"/><rect x=".5" width=".5" height="1" fill="black"/></mask>
            <g transform="translate(4 0)" mask="url(#m)"><rect x="2" y="2" width="16" height="12" fill="red"/></g></svg>`)
		frame := renderImportedRaster(t, result)
		if frame.NRGBAAt(9, 8).A != 0 || frame.NRGBAAt(11, 8) != (color.NRGBA{R: 255, A: 255}) || frame.NRGBAAt(16, 8).A != test.alpha || frame.NRGBAAt(18, 8).A != 0 {
			t.Fatalf("mask %q alpha at x=9,11,16,18: %d %d %d %d", test.mode, frame.NRGBAAt(9, 8).A, frame.NRGBAAt(11, 8).A, frame.NRGBAAt(16, 8).A, frame.NRGBAAt(18, 8).A)
		}
	}
}

func TestIconNestedClipTransformsIntersect(t *testing.T) {
	result := mustImport(t, `<svg viewBox="0 0 24 24">
        <clipPath id="upper"><rect x="0" y="0" width="20" height="7"/></clipPath>
        <clipPath id="both" transform="translate(3 4)" clip-path="url(#upper)"><rect width="9" height="18"/></clipPath>
        <rect transform="translate(2 1)" width="20" height="20" clip-path="url(#both)" fill="red"/></svg>`)
	frame := renderImportedRaster(t, result)
	if frame.NRGBAAt(6, 6).A != 255 || frame.NRGBAAt(13, 11).A != 255 || frame.NRGBAAt(4, 7).A != 0 || frame.NRGBAAt(8, 12).A != 0 || frame.NRGBAAt(14, 6).A != 0 {
		t.Fatal("chained clips did not intersect in the clipPath's transformed coordinate system")
	}
}

func TestIconGaussianBlurColorSpaceAndRegion(t *testing.T) {
	var linear, srgb color.NRGBA
	for _, space := range []string{"", ` color-interpolation-filters="sRGB"`} {
		result := mustImport(t, `<svg viewBox="0 0 36 24">
        <filter id="blur" x="0" y="-50%" width="1" height="200%"`+space+`><feGaussianBlur stdDeviation="2"/></filter>
        <g filter="url(#blur)"><rect x="6" y="6" width="12" height="12" fill="red"/>
        <rect x="18" y="6" width="12" height="12" fill="blue"/></g></svg>`)
		frame := renderImportedRaster(t, result)
		if edge := frame.NRGBAAt(12, 4); edge.R < 240 || edge.A == 0 || edge.A == 255 {
			t.Fatalf("blurred edge %v", edge)
		}
		if outside := frame.NRGBAAt(5, 12); outside.A != 0 {
			t.Fatalf("blur leaked beyond filter region: %v", outside)
		}
		if space == "" {
			linear = frame.NRGBAAt(17, 12)
		} else {
			srgb = frame.NRGBAAt(17, 12)
		}
	}
	if linear.R <= srgb.R+20 || linear.B <= srgb.B+20 {
		t.Fatalf("blurred transition in linearRGB %v, sRGB %v", linear, srgb)
	}
}

func TestIconEditorMetadataClassListsAndPositionedText(t *testing.T) {
	result := mustImport(t, `<svg viewBox="0 0 48 24" version="1.2" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
      xmlns:cc="http://creativecommons.org/ns#" xmlns:dc="http://purl.org/dc/elements/1.1/">
      <desc>Editor description</desc><metadata><rdf:RDF><cc:Work><dc:format>image/svg+xml</dc:format><dc:title>Logo</dc:title></cc:Work></rdf:RDF></metadata>
      <style>.one,.two{fill:red;isolation:isolate}.two{fill:blue}</style><g style="enable-background:new 0 0 48 24;mix-blend-mode:normal">
      <text x="2" y="18" font-size="16" class="one">A</text>
      <text font-size="16"><tspan x="26" y="18" class="two">B</tspan></text></g></svg>`)
	frame := renderImportedRaster(t, result)
	var red, blue int
	for y := 2; y < 18; y++ {
		for x := 0; x < 48; x++ {
			p := frame.NRGBAAt(x, y)
			if x < 20 && p.R > 200 && p.A > 128 {
				red++
			}
			if x >= 26 && p.B > 200 && p.A > 128 {
				blue++
			}
		}
	}
	if red < 10 || blue < 10 {
		t.Fatalf("positioned red and blue glyphs cover %d and %d pixels", red, blue)
	}
}

func TestIconResourceLimitsAndVisibility(t *testing.T) {
	const hidden = `<style>.archive{display:none}</style><g class="archive"><image id="old" width="8" height="8" href="unused.jpg"/></g>`
	const root = `<svg width="8" height="8">`
	mustImport(t, root+hidden+`<g id="unused"/><g id="unused"/><clipPath><rect width="2" height="2"/></clipPath><rect width="8" height="8"/></svg>`)
	for _, test := range []struct{ body, want string }{
		{hidden + `<use href="#old"/>`, "external"},
		{`<filter id="f"><feGaussianBlur in="BackgroundImage"/></filter>`, "SourceGraphic"},
		{`<filter id="f"><feGaussianBlur stdDeviation="-1"/></filter>`, "deviation"},
		{`<mask id="m"><rect width="8" height="8" mask="url(#m)"/></mask><rect width="8" height="8" mask="url(#m)"/>`, "nested mask"},
		{`<text>left<tspan x="1" y="1">right</tspan></text>`, "mixed or flowing"},
	} {
		if _, err := ImportNode(context.Background(), "icon.svg", []byte(root+test.body+"</svg>"), generousImportLimits()); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("expected %q, got %v", test.want, err)
		}
	}
	uri := embeddedRasterDataURI("image/png", embeddedRasterPNG(t, 16, 16, color.NRGBA{G: 255, A: 255}))
	comma := strings.IndexByte(uri, ',')
	svg := []byte(root + `<image width="8" height="8" href="` + uri[:comma+5] + " \n" + uri[comma+5:] + `"/></svg>`)
	limits := generousImportLimits()
	limits.MaxBytes = len(svg)
	limits.MaxDecodedRasterBytes = 16 * 16 * 4
	result, err := ImportNode(context.Background(), "icon.svg", svg, limits)
	if err != nil {
		t.Fatal(err)
	}
	if frame := renderImportedRaster(t, result); frame.NRGBAAt(4, 4) != (color.NRGBA{G: 255, A: 255}) {
		t.Fatalf("embedded raster pixel %v", frame.NRGBAAt(4, 4))
	}
	limits.MaxDecodedRasterBytes--
	if _, err = ImportNode(context.Background(), "icon.svg", svg, limits); err == nil {
		t.Fatal("expected decoded raster budget to be enforced")
	}
}
