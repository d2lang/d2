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
