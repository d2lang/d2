package svg

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func TestEscapeAttribute(t *testing.T) {
	t.Parallel()

	const value = "url('#gradient') & \" < > '\t\n\r"
	const want = "url('#gradient') &amp; &#34; &lt; &gt; '&#x9;&#xA;&#xD;"
	if got := EscapeAttribute(value); got != want {
		t.Fatalf("EscapeAttribute(%q) = %q, want %q", value, got, want)
	}
}

func TestRenderAttributesRoundTrip(t *testing.T) {
	t.Parallel()

	const value = "double\" single' amp& less< greater> tab\t newline\n carriage\r"
	source := `<svg` + RenderAttributes(Attr("data-value", value)) + `/>`

	var element struct {
		Value string `xml:"data-value,attr"`
	}
	if err := xml.Unmarshal([]byte(source), &element); err != nil {
		t.Fatalf("xml.Unmarshal(%q) error = %v", source, err)
	}
	if element.Value != value {
		t.Fatalf("decoded attribute = %q, want %q", element.Value, value)
	}
}

func TestRenderAttributesPreservesOrder(t *testing.T) {
	t.Parallel()

	got := RenderAttributes(Attr("href", "image.png"), Attr("x", "1"), Attr("y", "2"))
	want := ` href="image.png" x="1" y="2"`
	if got != want {
		t.Fatalf("RenderAttributes() = %q, want %q", got, want)
	}
}

func TestHrefAttributes(t *testing.T) {
	t.Parallel()

	got := RenderAttributes(HrefAttributes(`https://example.com/?a=1&b="2"`)...)
	want := ` href="https://example.com/?a=1&amp;b=&#34;2&#34;" xlink:href="https://example.com/?a=1&amp;b=&#34;2&#34;"`
	if got != want {
		t.Fatalf("HrefAttributes() = %q, want %q", got, want)
	}
}

func TestAttrRejectsUnsafeNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", ":", ":x", "x:", "a:b:c", "bad name", `x\" onclick`, "1st", "onload", "ONCLICK", "svg:onfocus"} {
		name := name
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Attr(%q) did not panic", name)
				}
			}()
			_ = Attr(name, "value")
		})
	}
}

func TestRenderAttributesRejectsDuplicates(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("RenderAttributes() did not reject duplicate attributes")
		}
	}()
	_ = RenderAttributes(Attr("class", "one"), Attr("class", "two"))
}

func TestParseAttributes(t *testing.T) {
	t.Parallel()

	attributes, err := ParseAttributes(`stroke-width="2" mask="url(#safe)" xlink:href="a&amp;b"`)
	if err != nil {
		t.Fatalf("ParseAttributes() error = %v", err)
	}
	got := RenderAttributes(attributes...)
	want := ` stroke-width="2" mask="url(#safe)" xlink:href="a&amp;b"`
	if got != want {
		t.Fatalf("parsed attributes = %q, want %q", got, want)
	}
}

func TestParseAttributesRejectsMarkupAndEvents(t *testing.T) {
	t.Parallel()

	for _, fragment := range []string{
		`onload="alert(1)"`,
		`x="1"><script>bad</script><d2 x="`,
		`x="1" x="2"`,
		`bad name="value"`,
	} {
		if attributes, err := ParseAttributes(fragment); err == nil {
			t.Fatalf("ParseAttributes(%q) = %#v, want error", fragment, attributes)
		}
	}
}

func TestScopedIDAndLocalIRI(t *testing.T) {
	t.Parallel()

	plain := ScopedID("mask-", "shape.one", "-clip")
	if got, want := plain.String(), "mask-shape.one-clip"; got != want {
		t.Fatalf("ScopedID safe value = %q, want %q", got, want)
	}
	unsafe := ScopedID("mask-", `x),url(https://example.invalid/a)`, "-clip")
	if strings.ContainsAny(unsafe.String(), `(),/'\"`) {
		t.Fatalf("ScopedID unsafe value = %q", unsafe.String())
	}
	if got, want := LocalIRI(unsafe), "url(#"+unsafe.String()+")"; got != want {
		t.Fatalf("LocalIRI() = %q, want %q", got, want)
	}
	encodedBang := ScopedID("mask-", "!", "-clip")
	reservedLiteral := ScopedID("mask-", scopedIDEncodingPrefix+SVGID("!"), "-clip")
	if encodedBang.String() == reservedLiteral.String() {
		t.Fatalf("ScopedID collision: unsafe %q and reserved literal %q", encodedBang.String(), reservedLiteral.String())
	}
}

func TestParseLocalIRI(t *testing.T) {
	t.Parallel()

	id, err := ParseLocalIRI("url(#safe-id)")
	if err != nil {
		t.Fatalf("ParseLocalIRI() error = %v", err)
	}
	if got, want := id.String(), "safe-id"; got != want {
		t.Fatalf("ParseLocalIRI() = %q, want %q", got, want)
	}
	for _, value := range []string{"https://example.invalid/a", "url(#x),url(https://example.invalid/a))", "url(#)", "url('#quoted')"} {
		if id, err := ParseLocalIRI(value); err == nil {
			t.Errorf("ParseLocalIRI(%q) = %q, want error", value, id.String())
		}
	}
}

func TestXMLValuesNormalizeInvalidEncodingAndCharacters(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"bad\x00value", "bad\x01value", string([]byte{0xff})} {
		value := value
		t.Run(fmt.Sprintf("%q", value), func(t *testing.T) {
			normalized := normalizeXMLString(value)
			source := `<svg` + RenderAttributes(Attr("data-value", value)) + `>` + Text(value).String() + `</svg>`
			var decoded struct {
				Value string `xml:"data-value,attr"`
				Text  string `xml:",chardata"`
			}
			if err := xml.Unmarshal([]byte(source), &decoded); err != nil {
				t.Fatalf("normalized output is not XML: %v\n%s", err, source)
			}
			if decoded.Value != normalized || decoded.Text != normalized {
				t.Fatalf("decoded value/text = %q/%q, want normalized %q", decoded.Value, decoded.Text, normalized)
			}

			defer func() {
				if recover() == nil {
					t.Fatalf("TrustedFragment accepted invalid XML value %q", value)
				}
			}()
			_ = TrustedFragment(value)
		})
	}
}

func normalizeXMLString(value string) string {
	var normalized strings.Builder
	for _, r := range value {
		if !validXMLRune(r) {
			r = '\uFFFD'
		}
		normalized.WriteRune(r)
	}
	return normalized.String()
}

func FuzzRenderAttributes(f *testing.F) {
	f.Add("class", `evil\" onload=\"alert(1)`, "text<&\n")
	f.Fuzz(func(t *testing.T, attributeValue, textValue, idComponent string) {
		attributes := RenderAttributes(Attr("data-value", attributeValue))
		id := ScopedID("d2-", idComponent, "-id")
		source := `<svg` + attributes + ` id="` + EscapeAttribute(id.String()) + `">` + Text(textValue).String() + `</svg>`
		var decoded struct {
			ID    string `xml:"id,attr"`
			Value string `xml:"data-value,attr"`
			Text  string `xml:",chardata"`
		}
		if err := xml.Unmarshal([]byte(source), &decoded); err != nil {
			t.Fatalf("generated invalid XML: %v\n%s", err, source)
		}
		wantAttribute := normalizeXMLString(attributeValue)
		wantText := normalizeXMLString(textValue)
		if decoded.ID != id.String() || decoded.Value != wantAttribute || decoded.Text != wantText {
			t.Fatalf("round trip = %#v, want id/value/text %q/%q/%q", decoded, id.String(), wantAttribute, wantText)
		}
	})
}
