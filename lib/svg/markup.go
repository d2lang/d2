package svg

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Attribute is an SVG attribute whose name has been validated and whose value
// is escaped when rendered. Its fields are intentionally private so callers
// cannot accidentally construct an unescaped attribute fragment.
type Attribute struct {
	name  string
	value string
}

// Name returns the validated attribute name. Values remain private so they
// cannot be serialized without escaping.
func (attribute Attribute) Name() (string, error) {
	if !validAttributeName(attribute.name) {
		return "", fmt.Errorf("invalid zero-value SVG attribute")
	}
	return attribute.name, nil
}

// Attr constructs an SVG attribute. The name must be a static XML attribute
// name and must not be an event-handler attribute. Invalid names panic because
// attribute names are a programmer-controlled part of the renderer, never
// diagram input.
func Attr(name, value string) Attribute {
	if !validAttributeName(name) {
		panic(fmt.Sprintf("invalid SVG attribute name %q", name))
	}
	return Attribute{name: name, value: value}
}

func IntAttr(name string, value int) Attribute {
	return Attr(name, strconv.Itoa(value))
}

func FloatAttr(name string, value float64) Attribute {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		panic(fmt.Sprintf("non-finite SVG attribute %q", name))
	}
	return Attr(name, FormatFloat(value))
}

// RenderAttributes serializes attributes in the supplied order. The returned
// string is empty or begins with one space, so it can be appended directly to
// an opening element name.
func RenderAttributes(attributes ...Attribute) string {
	if len(attributes) == 0 {
		return ""
	}

	var out strings.Builder
	seen := make(map[string]struct{}, len(attributes))
	for _, attribute := range attributes {
		if !validAttributeName(attribute.name) {
			panic(fmt.Sprintf("invalid zero-value SVG attribute"))
		}
		if _, ok := seen[attribute.name]; ok {
			panic(fmt.Sprintf("duplicate SVG attribute %q", attribute.name))
		}
		seen[attribute.name] = struct{}{}
		out.WriteByte(' ')
		out.WriteString(attribute.name)
		out.WriteString(`="`)
		out.WriteString(EscapeAttribute(attribute.value))
		out.WriteByte('"')
	}
	return out.String()
}

// ParseAttributes converts the legacy textual attribute-list form into typed
// attributes. It rejects malformed XML, duplicate attributes, and event
// handlers instead of copying the fragment into renderer output.
func ParseAttributes(fragment string) ([]Attribute, error) {
	if strings.TrimSpace(fragment) == "" {
		return nil, nil
	}
	decoder := xml.NewDecoder(strings.NewReader("<d2 " + fragment + "></d2>"))
	decoder.Strict = true
	token, err := decoder.RawToken()
	if err != nil {
		return nil, fmt.Errorf("invalid SVG attributes: %w", err)
	}
	start, ok := token.(xml.StartElement)
	if !ok || start.Name.Local != "d2" || start.Name.Space != "" {
		return nil, fmt.Errorf("invalid SVG attributes")
	}
	attributes := make([]Attribute, 0, len(start.Attr))
	seen := make(map[string]struct{}, len(start.Attr))
	for _, raw := range start.Attr {
		name := raw.Name.Local
		if raw.Name.Space != "" {
			name = raw.Name.Space + ":" + name
		}
		if !validAttributeName(name) {
			return nil, fmt.Errorf("invalid SVG attribute name %q", name)
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate SVG attribute %q", name)
		}
		seen[name] = struct{}{}
		attributes = append(attributes, Attribute{name: name, value: raw.Value})
	}
	token, err = decoder.RawToken()
	if err != nil {
		return nil, fmt.Errorf("invalid SVG attributes: %w", err)
	}
	end, ok := token.(xml.EndElement)
	if !ok || end.Name.Local != "d2" || end.Name.Space != "" {
		return nil, fmt.Errorf("SVG attribute fragment contains markup")
	}
	if _, err := decoder.RawToken(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("SVG attribute fragment contains trailing markup")
		}
		return nil, fmt.Errorf("invalid SVG attributes: %w", err)
	}
	return attributes, nil
}

// HrefAttributes returns the SVG 2 href attribute and its SVG 1.1
// compatibility equivalent. This function only serializes the URL; callers
// must continue to apply the appropriate link or asset policy first.
func HrefAttributes(value string) []Attribute {
	return []Attribute{
		Attr("href", value),
		Attr("xlink:href", value),
	}
}

// OpenElement and EmptyElement serialize element boundaries while validating
// the renderer-controlled tag name.
func OpenElement(name string, attributes ...Attribute) string {
	validateElementName(name)
	return "<" + name + RenderAttributes(attributes...) + ">"
}

func EmptyElement(name string, attributes ...Attribute) string {
	validateElementName(name)
	return "<" + name + RenderAttributes(attributes...) + " />"
}

// CompactEmptyElement is equivalent to EmptyElement but preserves the compact
// `/>` syntax used by older renderer output.
func CompactEmptyElement(name string, attributes ...Attribute) string {
	validateElementName(name)
	return "<" + name + RenderAttributes(attributes...) + "/>"
}

func CloseElement(name string) string {
	validateElementName(name)
	return "</" + name + ">"
}

func validateElementName(name string) {
	if !validXMLName(name) || strings.EqualFold(name, "script") {
		panic(fmt.Sprintf("invalid SVG element name %q", name))
	}
}

func validAttributeName(name string) bool {
	if !validXMLName(name) {
		return false
	}
	localName := name
	if colon := strings.LastIndexByte(name, ':'); colon >= 0 {
		localName = name[colon+1:]
	}
	return !strings.HasPrefix(strings.ToLower(localName), "on")
}

func validXMLName(name string) bool {
	if name == "" || strings.Count(name, ":") > 1 {
		return false
	}
	for _, part := range strings.Split(name, ":") {
		if !validXMLNamePart(part) {
			return false
		}
	}
	return true
}

func validXMLNamePart(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !isASCIIAlpha(c) && c != '_' {
				return false
			}
			continue
		}
		if !isASCIIAlpha(c) && !isASCIIDigit(c) && c != '_' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// ID is an identifier that is safe to use both as an XML id and in an
// unquoted local IRI such as url(#identifier).
type ID struct {
	value string
}

// LiteralID constructs an ID from a renderer-generated value. It panics if the
// value contains syntax that could change the meaning of a local IRI.
func LiteralID(value string) ID {
	if !validID(value) {
		panic(fmt.Sprintf("invalid SVG id %q", value))
	}
	return ID{value: value}
}

// EncodedID encodes an arbitrary non-empty value using SVGID's stable base32
// representation.
func EncodedID(value string) ID {
	encoded := SVGID(value)
	if encoded == "" {
		panic("cannot encode an empty SVG id")
	}
	return ID{value: encoded}
}

const scopedIDEncodingPrefix = "d2e-"

// ScopedID builds a stable identifier from trusted prefix/suffix strings and
// an arbitrary component. Ordinary safe components retain their spelling.
// Unsafe values and values beginning with the reserved encoding prefix are
// base32 encoded, keeping the mapping injective without changing common IDs.
func ScopedID(prefix, component, suffix string) ID {
	if !validIDPart(prefix) || !validIDPart(suffix) {
		panic("invalid SVG id prefix or suffix")
	}
	encodedComponent := component
	if component == "" || !validIDPart(component) || strings.HasPrefix(component, scopedIDEncodingPrefix) {
		encodedComponent = scopedIDEncodingPrefix + SVGID(component)
	}
	return LiteralID(prefix + encodedComponent + suffix)
}

func (id ID) String() string {
	if !validID(id.value) {
		panic("invalid zero-value SVG id")
	}
	return id.value
}

// LocalIRI returns an SVG local-resource reference for id.
func LocalIRI(id ID) string {
	return "url(#" + id.String() + ")"
}

// ParseLocalIRI validates the deprecated textual local-reference form.
func ParseLocalIRI(value string) (ID, error) {
	if !strings.HasPrefix(value, "url(#") || !strings.HasSuffix(value, ")") {
		return ID{}, fmt.Errorf("invalid SVG local IRI %q", value)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(value, "url(#"), ")")
	if !validID(id) {
		return ID{}, fmt.Errorf("invalid SVG local IRI %q", value)
	}
	return ID{value: id}, nil
}

func validID(value string) bool {
	return value != "" && validIDPart(value)
}

func validIDPart(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isASCIIAlpha(c) && !isASCIIDigit(c) && c != '_' && c != '-' && c != '.' && c != ':' {
			return false
		}
	}
	return true
}

// Fragment is content that is already safe to place between SVG element tags.
// Use Text for untrusted strings and TrustedFragment only for
// renderer-generated SVG.
type Fragment struct {
	value string
}

func Text(value string) Fragment {
	return Fragment{value: EscapeText(value)}
}

func TrustedFragment(value string) Fragment {
	validateXMLString(value)
	return Fragment{value: value}
}

func Join(fragments ...Fragment) Fragment {
	var out strings.Builder
	for _, fragment := range fragments {
		out.WriteString(fragment.value)
	}
	return Fragment{value: out.String()}
}

func (fragment Fragment) String() string {
	return fragment.value
}
