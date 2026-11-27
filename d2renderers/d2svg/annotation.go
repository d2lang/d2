package d2svg

import (
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

func polylineData(points []geo.Point) string {
	var d strings.Builder
	for i, p := range points {
		if i == 0 {
			d.WriteByte('M')
		} else {
			d.WriteByte('L')
		}
		d.WriteString(svg.FormatFloat(p.X))
		d.WriteByte(' ')
		d.WriteString(svg.FormatFloat(p.Y))
	}
	return d.String()
}

func opacityStyle(opacity float64) string {
	if opacity >= 1 {
		return ""
	}
	return ";opacity:" + svg.FormatFloat(float64(int(opacity*1000+0.5))/1000)
}

// drawAnnotations paints the board's marks, their notes and its caption above
// every object. They are written last so that nothing in the diagram covers
// them, and outside the shape groups so that an animation can still share
// the unchanged shapes between frames.
func drawAnnotations(writer *objectWriter, diagram *d2target.Diagram, inkTL, inkBR d2target.Point, inlineTheme *d2themes.Theme, markdown *markdownRenderer) error {
	for _, mark := range diagram.AnnotationMarks() {
		if len(mark.Strokes) == 0 && (mark.LabelBox == nil || mark.LabelOpacity <= 0) {
			continue
		}
		fmt.Fprint(writer, svg.OpenElement("g", svg.Attr("class", "annotation")))
		for _, stroke := range mark.Strokes {
			el := d2themes.NewThemableElement("path", inlineTheme)
			el.D = polylineData(stroke)
			el.Fill = color.None
			el.Stroke = mark.Paint
			el.Style = "stroke-width:" + svg.FormatFloat(mark.Width) + ";stroke-linecap:round;stroke-linejoin:round" + opacityStyle(mark.Opacity)
			fmt.Fprint(writer, el.Render())
		}
		if mark.LabelBox != nil && mark.LabelOpacity > 0 {
			if err := drawAnnotationText(writer, markdown, mark.Label, mark.FontFamily, d2target.AnnotationFontSize, mark.LabelBox, mark.Paint, mark.LabelOpacity); err != nil {
				return err
			}
		}
		fmt.Fprint(writer, svg.CloseElement("g"))
	}

	// A sampled frame cross-fades the caption it is leaving into its own.
	for _, caption := range []*d2target.Annotation{diagram.Root.LeavingAnnotation, diagram.Root.Annotation} {
		if caption == nil {
			continue
		}
		board := *diagram
		board.Root.Annotation = caption
		box := board.CaptionBox(inkTL, inkBR)
		if box == nil || caption.CaptionOpacity() <= 0 {
			continue
		}
		fmt.Fprint(writer, svg.OpenElement("g", svg.Attr("class", "annotation caption")))
		if err := drawAnnotationText(writer, markdown, caption.Label, "", d2target.CaptionFontSize, box, color.N1, caption.CaptionOpacity()); err != nil {
			return err
		}
		fmt.Fprint(writer, svg.CloseElement("g"))
	}
	return nil
}

func drawAnnotationText(writer *objectWriter, markdown *markdownRenderer, text, fontFamily string, fontSize int, box *geo.Box, paint string, opacity float64) error {
	layout, err := markdown.layout(text, fontFamily, fontSize, nil)
	if err != nil {
		return err
	}
	// Center the laid-out text in the box that was reserved for it.
	x := box.TopLeft.X + (box.Width-float64(layout.Width))/2
	y := box.TopLeft.Y + (box.Height-float64(layout.Height))/2
	rendered := markdown.render(layout, x, y, layout.Width, layout.Height, paint, "", true, false, nil)
	if opacity < 1 {
		fmt.Fprint(writer, svg.OpenElement("g", svg.Attr("opacity", svg.FormatFloat(float64(int(opacity*1000+0.5))/1000))))
		fmt.Fprint(writer, rendered)
		fmt.Fprint(writer, svg.CloseElement("g"))
		return nil
	}
	fmt.Fprint(writer, rendered)
	return nil
}
