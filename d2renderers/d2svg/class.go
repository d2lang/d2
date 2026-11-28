package d2svg

import (
	"fmt"
	"math"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/svg"
)

func classHeader(writer *objectWriter, diagramHash string, shape d2target.Shape, box *geo.Box, text string, textWidth, textHeight, fontSize float64, inlineTheme *d2themes.Theme) {
	rectEl := d2themes.NewThemableElement("rect", inlineTheme)
	rectEl.X, rectEl.Y = box.TopLeft.X, box.TopLeft.Y
	rectEl.Width, rectEl.Height = box.Width, box.Height
	rectEl.Fill = shape.Fill
	rectEl.FillPattern = shape.FillPattern
	rectEl.FillPatternColor = shape.FillPatternColor
	rectEl.ClassName = "class_header"
	if shape.BorderRadius != 0 {
		rectEl.SetClipPathID(borderRadiusClipPathID(diagramHash, shape.ID))
	}
	writer.element(rectEl)

	if text != "" {
		tl := label.InsideMiddleCenter.GetPointOnBox(
			box,
			0,
			textWidth,
			textHeight,
		)

		textEl := d2themes.NewThemableElement("text", inlineTheme)
		textEl.X = tl.X + textWidth/2
		textEl.Y = tl.Y + fontSize
		textEl.Fill = shape.GetFontColor()
		textEl.ClassName = "text-mono"
		textEl.Style = fmt.Sprintf(`text-anchor:%s;font-size:%vpx;`,
			"middle", 4+fontSize,
		)
		writer.label(textEl, text, textHeight)
	}
}

func classRow(writer *objectWriter, shape d2target.Shape, box *geo.Box, prefix, nameText, typeText string, fontSize float64, underline bool, inlineTheme *d2themes.Theme) {
	prefixTL := label.InsideMiddleLeft.GetPointOnBox(
		box,
		d2target.PrefixPadding,
		box.Width,
		fontSize,
	)
	typeTR := label.InsideMiddleRight.GetPointOnBox(
		box,
		d2target.TypePadding,
		0,
		fontSize,
	)

	textEl := d2themes.NewThemableElement("text", inlineTheme)
	textEl.X = prefixTL.X
	textEl.Y = prefixTL.Y + fontSize*3/4
	textEl.Fill = shape.PrimaryAccentColor
	textEl.ClassName = "text-mono"
	textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "start", fontSize)
	writer.text(textEl, prefix)

	textEl.X = prefixTL.X + d2target.PrefixWidth
	textEl.Fill = shape.Fill
	textEl.ClassName = "text-mono"
	if underline {
		textEl.ClassName += " text-underline"
	}
	writer.text(textEl, nameText)

	textEl.X = typeTR.X
	textEl.Y = typeTR.Y + fontSize*3/4
	textEl.Fill = shape.SecondaryAccentColor
	textEl.ClassName = "text-mono"
	textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "end", fontSize)
	writer.text(textEl, typeText)

}

func drawClass(writer *objectWriter, diagramHash string, targetShape d2target.Shape, inlineTheme *d2themes.Theme) {
	el := d2themes.NewThemableElement("rect", inlineTheme)
	el.X = float64(targetShape.Pos.X)
	el.Y = float64(targetShape.Pos.Y)
	el.Width = float64(targetShape.Width)
	el.Height = float64(targetShape.Height)
	el.Fill, el.Stroke = d2themes.ShapeTheme(targetShape)
	el.FillPattern = targetShape.FillPattern
	el.FillPatternColor = targetShape.FillPatternColor
	el.Style = targetShape.CSSStyle()
	if targetShape.BorderRadius != 0 {
		el.Rx = float64(targetShape.BorderRadius)
		el.Ry = float64(targetShape.BorderRadius)
	}
	writer.element(el)

	box := geo.NewBox(
		geo.NewPoint(float64(targetShape.Pos.X), float64(targetShape.Pos.Y)),
		float64(targetShape.Width),
		float64(targetShape.Height),
	)
	rowHeight := box.Height / float64(2+len(targetShape.Class.Fields)+len(targetShape.Class.Methods))
	headerBox := geo.NewBox(box.TopLeft, box.Width, math.Max(2*rowHeight, float64(targetShape.LabelHeight)+2*label.PADDING))

	classHeader(writer, diagramHash, targetShape, headerBox, targetShape.Label, float64(targetShape.LabelWidth), float64(targetShape.LabelHeight), float64(targetShape.FontSize), inlineTheme)

	rowBox := geo.NewBox(box.TopLeft.Copy(), box.Width, rowHeight)
	rowBox.TopLeft.Y += headerBox.Height
	for _, f := range targetShape.Fields {
		classRow(writer, targetShape, rowBox, f.VisibilityToken(), f.Name, f.Type, float64(targetShape.FontSize), f.Underline, inlineTheme)
		rowBox.TopLeft.Y += rowHeight
	}

	lineEl := d2themes.NewThemableElement("line", inlineTheme)

	var inset float64
	if len(targetShape.Methods) == 0 {
		inset = d2themes.ClampBorderRadius(float64(targetShape.BorderRadius), box.Width, box.Height)
	}
	lineEl.X1, lineEl.Y1 = rowBox.TopLeft.X+inset, rowBox.TopLeft.Y
	lineEl.X2, lineEl.Y2 = rowBox.TopLeft.X+rowBox.Width-inset, rowBox.TopLeft.Y

	lineEl.Stroke = targetShape.Fill
	lineEl.Style = "stroke-width:1"
	writer.element(lineEl)

	for _, m := range targetShape.Methods {
		classRow(writer, targetShape, rowBox, m.VisibilityToken(), m.Name, m.Return, float64(targetShape.FontSize), m.Underline, inlineTheme)
		rowBox.TopLeft.Y += rowHeight
	}

	if targetShape.Icon != nil && targetShape.Type != d2target.ShapeImage {
		iconPosition := label.FromString(targetShape.IconPosition)
		iconSize := d2target.GetIconSize(box, targetShape.IconPosition)

		tl := iconPosition.GetPointOnBox(box, label.PADDING, float64(iconSize), float64(iconSize))

		fmt.Fprint(writer, svg.EmptyElement("image",
			svg.Attr("href", targetShape.Icon.String()),
			svg.FloatAttr("x", tl.X),
			svg.FloatAttr("y", tl.Y),
			svg.IntAttr("width", iconSize),
			svg.IntAttr("height", iconSize),
		))
	}
}
