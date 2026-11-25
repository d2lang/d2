package d2svg

import (
	"fmt"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/util-go/go2"
)

// this func helps define a clipPath for shape class and sql_table to draw border-radius
func clipPathForBorderRadius(diagramHash string, shape d2target.Shape) string {
	box := geo.NewBox(
		geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)),
		float64(shape.Width),
		float64(shape.Height),
	)
	topX, topY := box.TopLeft.X+box.Width, box.TopLeft.Y

	id := borderRadiusClipPathID(diagramHash, shape.ID)
	out := svg.OpenElement("clipPath", svg.Attr("id", id.String()))
	out += fmt.Sprintf(`<path d="M %s %s L %s %s S %s %s %s %s `, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.BorderRadius)), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.BorderRadius)), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y), svg.FormatFloat(box.TopLeft.X+float64(shape.BorderRadius)), svg.FormatFloat(box.TopLeft.Y))
	out += fmt.Sprintf(`L %s %s L %s %s `, svg.FormatFloat(box.TopLeft.X+box.Width-float64(shape.BorderRadius)), svg.FormatFloat(box.TopLeft.Y), svg.FormatFloat(topX-float64(shape.BorderRadius)), svg.FormatFloat(topY))

	out += fmt.Sprintf(`S %s %s %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY), svg.FormatFloat(topX), svg.FormatFloat(topY+float64(shape.BorderRadius)))
	out += fmt.Sprintf(`L %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY+box.Height-float64(shape.BorderRadius)))

	if len(shape.Columns) != 0 {
		out += fmt.Sprintf(`L %s %s L %s %s`, svg.FormatFloat(topX), svg.FormatFloat(topY+box.Height), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+box.Height))
	} else {
		out += fmt.Sprintf(`S %s  %s %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY+box.Height), svg.FormatFloat(topX-float64(shape.BorderRadius)), svg.FormatFloat(topY+box.Height))
		out += fmt.Sprintf(`L %s %s `, svg.FormatFloat(box.TopLeft.X+float64(shape.BorderRadius)), svg.FormatFloat(box.TopLeft.Y+box.Height))
		out += fmt.Sprintf(`S %s %s %s %s`, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+box.Height), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+box.Height-float64(shape.BorderRadius)))
		out += fmt.Sprintf(`L %s %s`, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.BorderRadius)))
	}
	out += fmt.Sprintf(`Z %s %s" `, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y))
	return out + `fill="none" /> </clipPath>`
}

func borderRadiusClipPathID(diagramHash, shapeID string) svg.ID {
	return svg.LiteralID(fmt.Sprintf("%s-%s", diagramHash, svg.SVGID(shapeID)))
}

func tableHeader(writer *objectWriter, diagramHash string, shape d2target.Shape, box *geo.Box, text string, textWidth, textHeight, fontSize float64, inlineTheme *d2themes.Theme) {
	rectEl := d2themes.NewThemableElement("rect", inlineTheme)
	rectEl.X, rectEl.Y = box.TopLeft.X, box.TopLeft.Y
	rectEl.Width, rectEl.Height = box.Width, box.Height
	rectEl.Fill = shape.Fill
	rectEl.FillPattern = shape.FillPattern
	rectEl.ClassName = "class_header"
	if shape.BorderRadius != 0 {
		rectEl.SetClipPathID(borderRadiusClipPathID(diagramHash, shape.ID))
	}
	writer.element(rectEl)

	if text != "" {
		tl := label.InsideMiddleLeft.GetPointOnBox(
			box,
			float64(d2target.HeaderPadding),
			float64(shape.Width),
			textHeight,
		)

		textEl := d2themes.NewThemableElement("text", inlineTheme)
		textEl.X = tl.X
		textEl.Y = tl.Y + textHeight*3/4
		textEl.Fill = shape.GetFontColor()
		textEl.ClassName = "text"
		textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx",
			"start", 4+fontSize,
		)
		writer.text(textEl, text)
	}
}

func tableRow(writer *objectWriter, shape d2target.Shape, box *geo.Box, nameText, typeText, constraintText string, fontSize, longestNameWidth, longestTypeWidth float64, inlineTheme *d2themes.Theme) {
	// Row is made up of name, type, and constraint
	// e.g. | diagram   int   FK |
	nameTL := label.InsideMiddleLeft.GetPointOnBox(
		box,
		d2target.NamePadding,
		0,
		fontSize,
	)

	textEl := d2themes.NewThemableElement("text", inlineTheme)
	textEl.X = nameTL.X
	textEl.Y = nameTL.Y + fontSize*3/4
	textEl.Fill = shape.PrimaryAccentColor
	textEl.ClassName = "text"
	textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "start", fontSize)
	writer.text(textEl, nameText)

	textEl.X += longestNameWidth + d2target.TypePadding
	textEl.Fill = shape.NeutralAccentColor
	writer.text(textEl, typeText)

	textEl.X = box.TopLeft.X + (box.Width - d2target.NamePadding)
	textEl.Fill = shape.SecondaryAccentColor
	textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "end", fontSize)
	writer.text(textEl, constraintText)

}

func drawTable(writer *objectWriter, diagramHash string, targetShape d2target.Shape, inlineTheme *d2themes.Theme) {
	rectEl := d2themes.NewThemableElement("rect", inlineTheme)
	rectEl.X = float64(targetShape.Pos.X)
	rectEl.Y = float64(targetShape.Pos.Y)
	rectEl.Width = float64(targetShape.Width)
	rectEl.Height = float64(targetShape.Height)
	rectEl.Fill, rectEl.Stroke = d2themes.ShapeTheme(targetShape)
	rectEl.FillPattern = targetShape.FillPattern
	rectEl.ClassName = "shape"
	rectEl.Style = targetShape.CSSStyle()
	if targetShape.BorderRadius != 0 {
		rectEl.Rx = float64(targetShape.BorderRadius)
		rectEl.Ry = float64(targetShape.BorderRadius)
	}
	writer.element(rectEl)

	box := geo.NewBox(
		geo.NewPoint(float64(targetShape.Pos.X), float64(targetShape.Pos.Y)),
		float64(targetShape.Width),
		float64(targetShape.Height),
	)
	rowHeight := targetShape.SQLRowHeight()
	headerBox := geo.NewBox(box.TopLeft, box.Width, rowHeight)

	tableHeader(writer, diagramHash, targetShape, headerBox, targetShape.Label,
		float64(targetShape.LabelWidth), float64(targetShape.LabelHeight), float64(targetShape.FontSize), inlineTheme)

	var longestNameWidth int
	var longestTypeWidth int
	for _, f := range targetShape.Columns {
		longestNameWidth = go2.Max(longestNameWidth, f.Name.LabelWidth)
		longestTypeWidth = go2.Max(longestTypeWidth, f.Type.LabelWidth)
	}

	rowBox := geo.NewBox(box.TopLeft.Copy(), box.Width, rowHeight)
	rowBox.TopLeft.Y += headerBox.Height
	for idx, f := range targetShape.Columns {
		visible := targetShape.SQLRowVisibleHeight(rowBox.TopLeft.Y, rowHeight)
		if targetShape.RowReveal != nil && visible <= 0 {
			break
		}
		if visible < rowHeight {
			fmt.Fprint(writer.buf, svg.OpenClippedViewport(rowBox.TopLeft.X, rowBox.TopLeft.Y, rowBox.Width, visible, rowBox.TopLeft.Y+(rowHeight-visible)))
		}
		tableRow(writer, targetShape, rowBox, f.Name.Label, f.Type.Label, f.ConstraintAbbr(), float64(targetShape.FontSize), float64(longestNameWidth), float64(longestTypeWidth), inlineTheme)
		if visible < rowHeight {
			writer.buf.WriteString("</svg>")
		}
		y, inset, last := targetShape.SQLRowDivider(idx, rowBox.TopLeft.Y+rowHeight, float64(targetShape.BorderRadius))
		rowBox.TopLeft.Y = y

		lineEl := d2themes.NewThemableElement("line", inlineTheme)
		lineEl.X1, lineEl.Y1 = rowBox.TopLeft.X+inset, y
		lineEl.X2, lineEl.Y2 = rowBox.TopLeft.X+rowBox.Width-inset, y
		lineEl.Stroke = targetShape.Fill
		lineEl.Style = "stroke-width:2"
		writer.element(lineEl)
		if last {
			break
		}
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
