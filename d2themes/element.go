package d2themes

import (
	"fmt"
	"math"
	"strings"

	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/svg"
)

// ThemableElement is a helper class for creating new XML elements.
// This should be preferred over formatting and must be used
// whenever Fill, Stroke, BackgroundColor or Color contains a color from a theme.
// i.e. N[1-7] | B[1-6] | AA[245] | AB[45]
type ThemableElement struct {
	tag string

	X      float64
	X1     float64
	X2     float64
	Y      float64
	Y1     float64
	Y2     float64
	Width  float64
	Height float64
	R      float64
	Rx     float64
	Ry     float64
	Cx     float64
	Cy     float64

	D         string
	Points    string
	Transform string
	Href      string
	Xmlns     string

	Fill            string
	Stroke          string
	StrokeDashArray string
	BackgroundColor string
	Color           string

	ClassName string
	Style     string
	// Mask is retained for source compatibility and must be a local IRI in the
	// form url(#id). Deprecated: use SetMaskID.
	Mask string
	// ClipPath is retained for source compatibility and must contain a local
	// identifier. Deprecated: use SetClipPathID.
	ClipPath string
	// Attributes is retained for source compatibility. Values are parsed and
	// re-serialized through lib/svg; malformed or event attributes panic.
	// Deprecated: use AddAttributes.
	Attributes string

	// Content is retained as a trusted-markup compatibility escape hatch.
	// Deprecated: use SetText or SetInnerSVG.
	Content string

	FillPattern string

	inlineTheme     *Theme
	extraAttributes []svg.Attribute
	inner           *svg.Fragment
	mask            string
	clipPath        string
}

func NewThemableElement(tag string, inlineTheme *Theme) *ThemableElement {
	xmlns := ""
	if tag == "div" {
		xmlns = "http://www.w3.org/1999/xhtml"
	}

	return &ThemableElement{
		tag:             tag,
		X:               math.MaxFloat64,
		X1:              math.MaxFloat64,
		X2:              math.MaxFloat64,
		Y:               math.MaxFloat64,
		Y1:              math.MaxFloat64,
		Y2:              math.MaxFloat64,
		Width:           math.MaxFloat64,
		Height:          math.MaxFloat64,
		R:               math.MaxFloat64,
		Rx:              math.MaxFloat64,
		Ry:              math.MaxFloat64,
		Cx:              math.MaxFloat64,
		Cy:              math.MaxFloat64,
		Xmlns:           xmlns,
		Fill:            color.Empty,
		Stroke:          color.Empty,
		StrokeDashArray: color.Empty,
		BackgroundColor: color.Empty,
		inlineTheme:     inlineTheme,
	}
}

func (el *ThemableElement) Copy() *ThemableElement {
	tmp := *el
	tmp.extraAttributes = append([]svg.Attribute(nil), el.extraAttributes...)
	return &tmp
}

func (el *ThemableElement) AddAttributes(attributes ...svg.Attribute) {
	el.extraAttributes = append(el.extraAttributes, attributes...)
}

func (el *ThemableElement) SetText(text string) {
	content := svg.Text(text)
	el.inner = &content
}

func (el *ThemableElement) SetInnerSVG(content svg.Fragment) {
	el.inner = &content
}

func (el *ThemableElement) SetTranslate(x, y float64) {
	el.Transform = fmt.Sprintf("translate(%s %s)", svg.FormatFloat(x), svg.FormatFloat(y))
}

func (el *ThemableElement) SetMaskID(id svg.ID) {
	el.mask = svg.LocalIRI(id)
}

// SetMaskUrl is retained for source compatibility.
// Deprecated: use SetMaskID.
func (el *ThemableElement) SetMaskUrl(id string) {
	el.SetMaskID(svg.LiteralID(id))
}

func (el *ThemableElement) SetClipPathID(id svg.ID) {
	el.clipPath = id.String()
}

func (el *ThemableElement) Render() string {
	attributes := make([]svg.Attribute, 0, 24+len(el.extraAttributes))
	// href has to be at the top for the img bundler to detect <image> tags correctly
	if len(el.Href) > 0 {
		attributes = append(attributes, svg.Attr("href", el.Href))
	}
	if el.X != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("x", el.X))
	}
	if el.X1 != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("x1", el.X1))
	}
	if el.X2 != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("x2", el.X2))
	}
	if el.Y != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("y", el.Y))
	}
	if el.Y1 != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("y1", el.Y1))
	}
	if el.Y2 != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("y2", el.Y2))
	}
	if el.Width != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("width", el.Width))
	}
	if el.Height != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("height", el.Height))
	}
	if el.R != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("r", el.R))
	}
	if el.Rx != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("rx", calculateAxisRadius(el.Rx, el.Width, el.Height)))
	}
	if el.Ry != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("ry", calculateAxisRadius(el.Ry, el.Width, el.Height)))
	}
	if el.Cx != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("cx", el.Cx))
	}
	if el.Cy != math.MaxFloat64 {
		attributes = append(attributes, svg.FloatAttr("cy", el.Cy))
	}
	if el.StrokeDashArray != "" {
		attributes = append(attributes, svg.Attr("stroke-dasharray", el.StrokeDashArray))
	}

	if len(el.D) > 0 {
		attributes = append(attributes, svg.Attr("d", el.D))
	}
	mask := el.mask
	if mask == "" && el.Mask != "" {
		id, err := svg.ParseLocalIRI(el.Mask)
		if err != nil {
			panic(err)
		}
		mask = svg.LocalIRI(id)
	}
	if len(mask) > 0 {
		attributes = append(attributes, svg.Attr("mask", mask))
	}
	if len(el.Points) > 0 {
		attributes = append(attributes, svg.Attr("points", el.Points))
	}
	if len(el.Transform) > 0 {
		attributes = append(attributes, svg.Attr("transform", el.Transform))
	}
	if len(el.Xmlns) > 0 {
		attributes = append(attributes, svg.Attr("xmlns", el.Xmlns))
	}

	class := el.ClassName
	style := el.Style

	// Add class {property}-{theme color} if the color is from a theme, set the property otherwise
	if color.IsThemeColor(el.Stroke) {
		class += fmt.Sprintf(" stroke-%s", el.Stroke)
		if el.inlineTheme != nil {
			attributes = append(attributes, svg.Attr("stroke", ResolveThemeColor(*el.inlineTheme, el.Stroke)))
		}
	} else if len(el.Stroke) > 0 {
		if color.IsGradient(el.Stroke) {
			el.Stroke = fmt.Sprintf("url('#%s')", color.UniqueGradientID(el.Stroke))
		}
		attributes = append(attributes, svg.Attr("stroke", el.Stroke))
	}
	if color.IsThemeColor(el.Fill) {
		class += fmt.Sprintf(" fill-%s", el.Fill)
		if el.inlineTheme != nil {
			attributes = append(attributes, svg.Attr("fill", ResolveThemeColor(*el.inlineTheme, el.Fill)))
		}
	} else if len(el.Fill) > 0 {
		if color.IsGradient(el.Fill) {
			el.Fill = fmt.Sprintf("url('#%s')", color.UniqueGradientID(el.Fill))
		}
		attributes = append(attributes, svg.Attr("fill", el.Fill))
	}
	if color.IsThemeColor(el.BackgroundColor) {
		class += fmt.Sprintf(" background-color-%s", el.BackgroundColor)
		if el.inlineTheme != nil {
			attributes = append(attributes, svg.Attr("background-color", ResolveThemeColor(*el.inlineTheme, el.BackgroundColor)))
		}
	} else if len(el.BackgroundColor) > 0 {
		attributes = append(attributes, svg.Attr("background-color", el.BackgroundColor))
	}
	if color.IsThemeColor(el.Color) {
		class += fmt.Sprintf(" color-%s", el.Color)
		if el.inlineTheme != nil {
			attributes = append(attributes, svg.Attr("color", ResolveThemeColor(*el.inlineTheme, el.Color)))
		}
	} else if len(el.Color) > 0 {
		attributes = append(attributes, svg.Attr("color", el.Color))
	}

	if len(class) > 0 {
		attributes = append(attributes, svg.Attr("class", class))
	}
	if len(style) > 0 {
		attributes = append(attributes, svg.Attr("style", style))
	}
	attributes = append(attributes, el.extraAttributes...)

	if el.Attributes != "" {
		legacyAttributes, err := svg.ParseAttributes(el.Attributes)
		if err != nil {
			panic(err)
		}
		attributes = append(attributes, legacyAttributes...)
	}
	clipPath := el.clipPath
	if clipPath == "" && el.ClipPath != "" {
		clipPath = svg.LiteralID(el.ClipPath).String()
	}
	if len(clipPath) > 0 {
		attributes = append(attributes, svg.Attr("clip-path", svg.LocalIRI(svg.LiteralID(clipPath))))
	}

	out := svg.OpenElement(el.tag, attributes...)
	inner := el.inner
	if inner == nil && el.Content != "" {
		compatibilityContent := svg.TrustedFragment(el.Content)
		inner = &compatibilityContent
	}
	if inner != nil && len(inner.String()) > 0 {
		return strings.TrimSuffix(out, ">") + ">" + inner.String() + svg.CloseElement(el.tag)
	}

	out = strings.TrimSuffix(out, ">") + " />"
	fillPattern := strings.ToLower(el.FillPattern)
	if fillPattern != "" && fillPattern != "none" {
		patternEl := el.Copy()
		patternEl.Fill = ""
		patternEl.Stroke = ""
		patternEl.BackgroundColor = ""
		patternEl.Color = ""
		patternEl.ClassName = fmt.Sprintf("%s-overlay", fillPattern)
		patternEl.FillPattern = ""
		out += patternEl.Render()
	}
	return out
}

func calculateAxisRadius(borderRadius, width, height float64) float64 {
	minimumSideSize := math.Min(width, height)
	maximumBorderRadiusValue := minimumSideSize / 2.0
	return math.Min(borderRadius, maximumBorderRadiusValue)
}
