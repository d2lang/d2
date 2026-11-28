package d2svg_test

import (
	"context"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/go2"
)

func assertStyleLocalIRIsResolve(t *testing.T, source []byte) {
	t.Helper()
	ids := make(map[string]int)
	var styleText strings.Builder
	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	inStyle := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("rendered SVG is not strict XML: %v", err)
		}
		switch tok := token.(type) {
		case xml.StartElement:
			for _, attr := range tok.Attr {
				if attr.Name.Local == "id" {
					ids[attr.Value]++
				}
			}
			if tok.Name.Local == "style" {
				inStyle = true
			}
		case xml.EndElement:
			if tok.Name.Local == "style" {
				inStyle = false
			}
		case xml.CharData:
			if inStyle {
				styleText.Write(tok)
			}
		}
	}
	for id, count := range ids {
		if count != 1 {
			t.Fatalf("SVG id %q occurs %d times, want 1", id, count)
		}
	}
	refs := localIRIRegexp.FindAllStringSubmatch(styleText.String(), -1)
	if len(refs) == 0 {
		t.Fatal("expected local IRI references in <style>")
	}
	for _, match := range refs {
		if ids[match[1]] != 1 {
			t.Fatalf("style IRI %q resolved to %d definitions, want 1", match[1], ids[match[1]])
		}
	}
}

func TestFillPatternColor(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		script string
		sketch bool
		checks func(t *testing.T, svg string)
	}{
		{
			name: "shape_and_root_custom_colors",
			script: `
style.fill-pattern: dots
style.fill: "black"
style.fill-pattern.color: white
a: {
  style.fill: "black"
  style.fill-pattern: dots
  style.fill-pattern.color: "#ff0000"
}
b: {
  style.fill: "#112233"
  style.fill-pattern: lines
  style.fill-pattern.color: cyan
}
c: {
  style.fill-pattern: dots
}
d: {
  shape: circle
  style.fill: "black"
  style.fill-pattern: dots
  style.fill-pattern.color: "linear-gradient(red, blue)"
}
e: {
  shape: class
  style.fill: "black"
  style.fill-pattern: dots
  style.fill-pattern.color: "#00ff00"
  +field: string
}
`,
			checks: func(t *testing.T, svg string) {
				t.Helper()
				for _, want := range []string{
					`fill="white"`,
					`fill="#ff0000"`,
					`fill="cyan"`,
					`fill="#00ff00"`,
					`class="dots-overlay"`,
					`<linearGradient`,
				} {
					if !strings.Contains(svg, want) {
						t.Fatalf("expected rendered SVG to contain %q", want)
					}
				}
				for _, cls := range []string{"dots-overlay-", "lines-overlay-"} {
					idx := strings.Index(svg, "."+cls)
					if idx < 0 {
						t.Fatalf("missing custom overlay CSS class prefix %q", cls)
					}
					ruleEnd := strings.Index(svg[idx:], "}")
					if ruleEnd < 0 {
						t.Fatalf("unclosed CSS rule for %q", cls)
					}
					rule := svg[idx : idx+ruleEnd+1]
					if strings.Contains(rule, "mix-blend-mode") {
						t.Fatalf("custom color overlay rule %q must not apply mix-blend-mode", rule)
					}
				}
			},
		},
		{
			name: "sketch_mode",
			script: `
a: {
  style.fill: "black"
  style.fill-pattern: dots
  style.fill-pattern.color: "#abcdef"
}
`,
			sketch: true,
			checks: func(t *testing.T, svg string) {
				t.Helper()
				if !strings.Contains(svg, `fill="#abcdef"`) {
					t.Fatal("expected sketch custom dots pattern with fill=\"#abcdef\"")
				}
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ruler, err := textmeasure.NewRuler()
			assert.Success(t, err)
			renderOpts := &d2svg.RenderOpts{}
			if tc.sketch {
				renderOpts.Sketch = go2.Pointer(true)
			}
			layoutResolver := func(string) (d2graph.LayoutGraph, error) {
				return d2dagrelayout.DefaultLayout, nil
			}
			diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), tc.script, &d2lib.CompileOptions{
				Ruler:          ruler,
				LayoutResolver: layoutResolver,
			}, renderOpts)
			assert.Success(t, err)

			out, err := d2svg.Render(diagram, renderOpts)
			assert.Success(t, err)
			var parsed any
			assert.Success(t, xml.Unmarshal(out, &parsed))
			assertStyleLocalIRIsResolve(t, out)
			tc.checks(t, string(out))
		})
	}
}

func assertAllLocalIRIsResolve(t *testing.T, source []byte) {
	t.Helper()
	assertStyleLocalIRIsResolve(t, source)
	ids := make(map[string]int)
	var attrIRIs []string
	decoder := xml.NewDecoder(strings.NewReader(string(source)))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("rendered SVG is not strict XML: %v", err)
		}
		if tok, ok := token.(xml.StartElement); ok {
			for _, attr := range tok.Attr {
				if attr.Name.Local == "id" {
					ids[attr.Value]++
				} else {
					for _, match := range localIRIRegexp.FindAllStringSubmatch(attr.Value, -1) {
						attrIRIs = append(attrIRIs, match[1])
					}
				}
			}
		}
	}
	for _, id := range attrIRIs {
		if ids[id] != 1 {
			t.Fatalf("attribute IRI %q resolved to %d definitions, want 1", id, ids[id])
		}
	}
}

func TestFillPatternColorCSSScopedByDiagramHash(t *testing.T) {
	t.Parallel()

	script := `
direction: right
style.fill: black
style.fill-pattern: lines
style.fill-pattern.color: red
Client -> Server
`
	renderWithSalt := func(salt string) (string, string) {
		ruler, err := textmeasure.NewRuler()
		assert.Success(t, err)
		renderOpts := &d2svg.RenderOpts{
			NoXMLTag: go2.Pointer(true),
			Salt:     go2.Pointer(salt),
		}
		layoutResolver := func(string) (d2graph.LayoutGraph, error) {
			return d2dagrelayout.DefaultLayout, nil
		}
		diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
			Ruler:          ruler,
			LayoutResolver: layoutResolver,
		}, renderOpts)
		assert.Success(t, err)
		hashID, err := diagram.HashID(renderOpts.Salt)
		assert.Success(t, err)
		out, err := d2svg.Render(diagram, renderOpts)
		assert.Success(t, err)
		assertAllLocalIRIsResolve(t, out)
		return hashID, string(out)
	}

	hashA, svgA := renderWithSalt("a")
	hashB, svgB := renderWithSalt("b")
	if hashA == hashB {
		t.Fatalf("expected distinct hashes for distinct salts, got %q", hashA)
	}

	for _, pair := range []struct {
		name string
		hash string
		svg  string
	}{
		{"salt_a", hashA, svgA},
		{"salt_b", hashB, svgB},
	} {
		scopedPrefix := "." + pair.hash + " .lines-overlay-"
		if !strings.Contains(pair.svg, scopedPrefix) {
			t.Fatalf("%s: expected diagram-scoped CSS selector %q in SVG", pair.name, scopedPrefix)
		}
		if strings.Contains(pair.svg, "\n.lines-overlay-") {
			t.Fatalf("%s: found unscoped .lines-overlay- CSS selector in SVG", pair.name)
		}
	}
}

func TestFillPatternColorCSSScopedAcrossVariants(t *testing.T) {
	t.Parallel()

	script := `
vars: {
  d2-legend: {
    item: "Legend" {
      style.fill-pattern: dots
      style.fill-pattern.color: "#00ff00"
    }
  }
}
style.fill-pattern: lines
style.fill-pattern.color: "#ff0000"
a: {
  style.fill: black
  style.fill-pattern: dots
  style.fill-pattern.color: white
}
`
	for _, tc := range []struct {
		name           string
		sketch         bool
		dark           bool
		themeRootColor bool
	}{
		{name: "dark_theme", dark: true},
		{name: "dark_theme_token_color", dark: true, themeRootColor: true},
		{name: "sketch_mode", sketch: true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ruler, err := textmeasure.NewRuler()
			assert.Success(t, err)
			renderOpts := &d2svg.RenderOpts{
				NoXMLTag: go2.Pointer(true),
				Salt:     go2.Pointer(tc.name),
			}
			if tc.sketch {
				renderOpts.Sketch = go2.Pointer(true)
			}
			if tc.dark {
				renderOpts.DarkThemeID = go2.Pointer(int64(200))
			}
			layoutResolver := func(string) (d2graph.LayoutGraph, error) {
				return d2dagrelayout.DefaultLayout, nil
			}
			diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
				Ruler:          ruler,
				LayoutResolver: layoutResolver,
			}, renderOpts)
			assert.Success(t, err)
			if tc.themeRootColor {
				diagram.Root.FillPatternColor = "B1"
			}
			hashID, err := diagram.HashID(renderOpts.Salt)
			assert.Success(t, err)
			out, err := d2svg.Render(diagram, renderOpts)
			assert.Success(t, err)
			assertAllLocalIRIsResolve(t, out)
			svgStr := string(out)
			for _, prefix := range []string{"dots-overlay-", "lines-overlay-"} {
				if !strings.Contains(svgStr, "."+hashID+" ."+prefix) {
					t.Fatalf("expected scoped selector %q in SVG", "."+hashID+" ."+prefix)
				}
				if strings.Contains(svgStr, "\n."+prefix) {
					t.Fatalf("found unscoped selector %q in SVG", "."+prefix)
				}
			}
			if tc.themeRootColor {
				if !strings.Contains(svgStr, "."+hashID+" .fill-B1") {
					t.Fatalf("expected diagram-scoped theme color rule %q for pattern token", "."+hashID+" .fill-B1")
				}
			}
		})
	}
}

func TestFillPatternColorGradientOnlyInLegendDefinesGradient(t *testing.T) {
	t.Parallel()

	script := `
vars: {
  d2-legend: {
    dotted_grad: "Pattern gradient" {
      style.fill-pattern: dots
      style.fill-pattern.color: "linear-gradient(red, blue)"
    }
    fill_grad: "Fill gradient" {
      style.fill: "radial-gradient(yellow, green)"
    }
  }
}
a -> b
`
	ruler, err := textmeasure.NewRuler()
	assert.Success(t, err)
	renderOpts := &d2svg.RenderOpts{}
	layoutResolver := func(string) (d2graph.LayoutGraph, error) {
		return d2dagrelayout.DefaultLayout, nil
	}
	diagram, _, err := d2lib.Compile(log.WithTB(context.Background(), t), script, &d2lib.CompileOptions{
		Ruler:          ruler,
		LayoutResolver: layoutResolver,
	}, renderOpts)
	assert.Success(t, err)

	out, err := d2svg.Render(diagram, renderOpts)
	assert.Success(t, err)
	var parsed any
	assert.Success(t, xml.Unmarshal(out, &parsed))
	assertAllLocalIRIsResolve(t, out)

	svgStr := string(out)
	if !strings.Contains(svgStr, "<linearGradient") {
		t.Fatal("expected <linearGradient> definition for legend fill-pattern.color")
	}
	if !strings.Contains(svgStr, "<radialGradient") {
		t.Fatal("expected <radialGradient> definition for legend shape fill")
	}
}
