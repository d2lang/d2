package color

import (
	"encoding/xml"
	"io"
	"math"
	"strings"
	"testing"
)

func TestGradientAngleInNonSquareBox(t *testing.T) {
	for _, size := range [][2]float64{{300, 100}, {100, 300}, {100, 100}} {
		w, h := size[0], size[1]
		g, err := ParseGradient(GradientForBox("linear-gradient(45deg, red, blue)", w, h))
		if err != nil {
			t.Fatal(err)
		}
		x1, y1, x2, y2, err := LinearGradientVector(g.Direction)
		if err != nil {
			t.Fatal(err)
		}
		// Evaluate the same normalized SVG gradient in device pixels. A
		// 45-degree line has equal and opposite horizontal and vertical rates.
		dx, dy := x2-x1, y2-y1
		at := func(x, y float64) float64 {
			return ((x/w-x1)*dx + (y/h-y1)*dy) / (dx*dx + dy*dy)
		}
		for _, pixel := range [][2]float64{{0, h}, {w, 0}, {w / 2, h / 2}, {w/2 + 10, h/2 + 10}} {
			want := (pixel[0] + h - pixel[1]) / (w + h)
			if got := at(pixel[0], pixel[1]); math.Abs(got-want) > 1e-12 {
				t.Errorf("box %v at %v = %g, want %g", size, pixel, got, want)
			}
		}
	}
}

func TestOneStopGradientToSVG(t *testing.T) {
	tests := []string{
		"linear-gradient(red)",
		"radial-gradient(red)",
	}

	for _, cssGradient := range tests {
		t.Run(cssGradient, func(t *testing.T) {
			gradient, err := ParseGradient(cssGradient)
			if err != nil {
				t.Fatalf("ParseGradient() error = %v", err)
			}

			svg := GradientToSVG(gradient)
			if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
				t.Fatalf("GradientToSVG() produced a non-finite offset:\n%s", svg)
			}

			var parsed struct {
				Stops []struct {
					Offset string `xml:"offset,attr"`
				} `xml:"stop"`
			}
			if err := xml.Unmarshal([]byte(svg), &parsed); err != nil {
				t.Fatalf("GradientToSVG() produced invalid XML: %v\n%s", err, svg)
			}
			if len(parsed.Stops) != 1 {
				t.Fatalf("GradientToSVG() produced %d stops, want 1", len(parsed.Stops))
			}
			if got, want := parsed.Stops[0].Offset, "0.00%"; got != want {
				t.Fatalf("stop offset = %q, want %q", got, want)
			}
		})
	}
}

func TestParseGradientValidatesStopPositions(t *testing.T) {
	t.Parallel()

	for _, gradient := range []string{
		`linear-gradient(red 0%"/><script>document.documentElement.dataset.pwned=1</script><!--, blue --><stop/>)`,
		`radial-gradient(red calc(1%), blue)`,
		`linear-gradient(red NaN, blue)`,
		`linear-gradient(red +Inf%, blue)`,
		`linear-gradient(red 20px, blue)`,
	} {
		gradient := gradient
		t.Run(gradient, func(t *testing.T) {
			t.Parallel()
			if parsed, err := ParseGradient(gradient); err == nil {
				t.Fatalf("ParseGradient(%q) = %#v, want an invalid-position error", gradient, parsed)
			}
			if ValidColor(gradient) {
				t.Fatalf("ValidColor(%q) = true", gradient)
			}
		})
	}

	parsed, err := ParseGradient(`linear-gradient(red +.5%, blue 1e2)`)
	if err != nil {
		t.Fatalf("ParseGradient() error = %v", err)
	}
	if got, want := parsed.ColorStops[0].Position, "0.5%"; got != want {
		t.Fatalf("first canonical position = %q, want %q", got, want)
	}
	if got, want := parsed.ColorStops[1].Position, "100"; got != want {
		t.Fatalf("second canonical position = %q, want %q", got, want)
	}
}

func TestGradientToSVGEscapesConstructedAttributes(t *testing.T) {
	t.Parallel()

	wantOffset := `0%"/><script onload="alert(1)">x</script><!--`
	source := GradientToSVG(Gradient{
		Type: "linear",
		ID:   `gradient" onload="alert(1)`,
		ColorStops: []ColorStop{{
			Color:    `red" onload="alert(1)`,
			Position: wantOffset,
		}},
	})

	decoder := xml.NewDecoder(strings.NewReader(source))
	decoder.Strict = true
	foundOffset := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("GradientToSVG() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if strings.EqualFold(start.Name.Local, "script") {
			t.Fatalf("GradientToSVG() emitted an injected script element: %s", source)
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("GradientToSVG() emitted an event-handler attribute %q: %s", attr.Name.Local, source)
			}
			if start.Name.Local == "stop" && attr.Name.Local == "offset" && attr.Value == wantOffset {
				foundOffset = true
			}
		}
	}
	if !foundOffset {
		t.Fatalf("GradientToSVG() did not preserve the escaped offset as data: %s", source)
	}
}
