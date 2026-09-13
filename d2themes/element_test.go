package d2themes

import (
	"encoding/xml"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/lib/svg"
)

func TestThemableElementEscapesPaintAttributes(t *testing.T) {
	t.Parallel()

	const value = `red" onload="alert(1)`
	element := NewThemableElement("rect", nil)
	element.Fill = value
	element.Stroke = value
	element.BackgroundColor = value
	element.Color = value
	source := "<svg>" + element.Render() + "</svg>"

	want := map[string]bool{
		"fill":             false,
		"stroke":           false,
		"background-color": false,
		"color":            false,
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		for _, attr := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attr.Name.Local), "on") {
				t.Fatalf("Render() emitted an event-handler attribute %q: %s", attr.Name.Local, source)
			}
			if _, ok := want[attr.Name.Local]; ok && attr.Value == value {
				want[attr.Name.Local] = true
			}
		}
	}
	for attribute, found := range want {
		if !found {
			t.Errorf("Render() did not preserve %s value %q after XML decoding: %s", attribute, value, source)
		}
	}
}

func TestThemableElementEscapesAllStringAttributes(t *testing.T) {
	t.Parallel()

	const value = `x" onload="alert(1)&<`
	element := NewThemableElement("path", nil)
	element.Href = value
	element.D = value
	element.Points = value
	element.Transform = value
	element.Xmlns = value
	element.StrokeDashArray = value
	element.ClassName = value
	element.Style = value
	source := "<svg>" + element.Render() + "</svg>"

	want := map[string]string{
		"href":             value,
		"d":                value,
		"points":           value,
		"transform":        value,
		"xmlns":            value,
		"stroke-dasharray": value,
		"class":            value,
		"style":            value,
	}
	decoder := xml.NewDecoder(strings.NewReader(source))
	decoder.Strict = true
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Render() emitted invalid XML: %v\n%s", err, source)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "path" {
			continue
		}
		for _, attribute := range start.Attr {
			if strings.HasPrefix(strings.ToLower(attribute.Name.Local), "on") {
				t.Fatalf("Render() emitted event attribute %q: %s", attribute.Name.Local, source)
			}
			if expected, exists := want[attribute.Name.Local]; exists {
				if attribute.Value != expected {
					t.Errorf("decoded %s = %q, want %q", attribute.Name.Local, attribute.Value, expected)
				}
				delete(want, attribute.Name.Local)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing attributes after decoding: %#v\n%s", want, source)
	}
}

func TestThemableElementLegacyAttributesAreChecked(t *testing.T) {
	t.Parallel()

	element := NewThemableElement("path", nil)
	element.Attributes = `data-value="a&amp;b" aria-label="hello"`
	if got, want := element.Render(), `<path data-value="a&amp;b" aria-label="hello" />`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}

	for _, attributes := range []string{
		`onload="alert(1)"`,
		`x="1"><script>alert(1)</script><path x="`,
		`class="one" class="two"`,
	} {
		element := NewThemableElement("path", nil)
		element.Attributes = attributes
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Render() accepted legacy attributes %q", attributes)
				}
			}()
			_ = element.Render()
		}()
	}
}

func TestThemableElementLegacyLocalReferencesAreChecked(t *testing.T) {
	t.Parallel()

	element := NewThemableElement("rect", nil)
	element.Mask = "url(#mask-id)"
	element.ClipPath = "clip-id"
	if got, want := element.Render(), `<rect mask="url(#mask-id)" clip-path="url(#clip-id)" />`; got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}

	for _, configure := range []func(*ThemableElement){
		func(element *ThemableElement) { element.Mask = "url(#x),url(https://example.invalid/a))" },
		func(element *ThemableElement) { element.ClipPath = `x)" onload="alert(1)` },
		func(element *ThemableElement) { element.SetMaskUrl(`x),url(https://example.invalid/a)`) },
	} {
		element := NewThemableElement("rect", nil)
		func() {
			defer func() {
				if recover() == nil {
					t.Error("legacy local-reference boundary accepted an unsafe identifier")
				}
			}()
			configure(element)
			_ = element.Render()
		}()
	}
}

func TestThemableElementTextAndTrustedContentBoundaries(t *testing.T) {
	t.Parallel()

	text := NewThemableElement("text", nil)
	text.SetText(`<script>alert("x")</script>&`)
	if got, want := text.Render(), `<text>&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;&amp;</text>`; got != want {
		t.Fatalf("SetText Render() = %q, want %q", got, want)
	}

	group := NewThemableElement("g", nil)
	group.SetInnerSVG(svg.TrustedFragment(`<title>renderer generated</title>`))
	if got, want := group.Render(), `<g><title>renderer generated</title></g>`; got != want {
		t.Fatalf("SetInnerSVG Render() = %q, want %q", got, want)
	}
}

func TestThemableElementRejectsNonFiniteNumbers(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		element := NewThemableElement("rect", nil)
		element.X = value
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Render() accepted non-finite x=%v", value)
				}
			}()
			_ = element.Render()
		}()
	}
}
