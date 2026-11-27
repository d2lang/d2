package d2scenebuild

import (
	"fmt"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	libcolor "github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

// buildAnnotations returns the board's marks, their notes and its caption.
// They are appended after every ordinary object so nothing covers them.
func (b *builder) buildAnnotations() ([]*d2scene.Node, error) {
	var nodes []*d2scene.Node
	for _, mark := range b.diagram.AnnotationMarks() {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
		if len(mark.Strokes) == 0 && (mark.LabelBox == nil || mark.LabelOpacity <= 0) {
			continue
		}
		object := fmt.Sprintf("annotation of %q", mark.ID)
		root := d2scene.NewNode(nil)
		root.ID = mark.ID + ":annotation"
		stroke, err := b.stroke(mark.Paint, mark.Width, 0, d2scene.CapRound, d2scene.JoinRound, object+" stroke")
		if err != nil {
			return nil, err
		}
		if stroke != nil {
			ink := d2scene.NewNode(nil)
			ink.ID = root.ID + ":ink"
			ink.Opacity = mark.Opacity
			for i, points := range mark.Strokes {
				commands := make([]d2scene.PathCommand, 0, len(points))
				for j, p := range points {
					if !finite(p.X) || !finite(p.Y) {
						return nil, invalidField(object, "stroke", nil, "must resolve to finite coordinates")
					}
					if j == 0 {
						commands = append(commands, d2scene.MoveTo(p.X, p.Y))
					} else {
						commands = append(commands, d2scene.LineTo(p.X, p.Y))
					}
				}
				path := d2scene.NewNode(d2scene.Path{Commands: commands, Stroke: stroke})
				path.ID = fmt.Sprintf("%s:%d", ink.ID, i)
				ink.Children = append(ink.Children, path)
			}
			root.Children = append(root.Children, ink)
		}
		if mark.LabelBox != nil && mark.LabelOpacity > 0 {
			note, err := b.buildAnnotationText(object+" note", root.ID+":note", mark.Label, mark.FontFamily, d2target.AnnotationFontSize, mark.LabelBox, mark.Paint)
			if err != nil {
				return nil, err
			}
			note.Opacity = mark.LabelOpacity
			root.Children = append(root.Children, note)
		}
		nodes = append(nodes, root)
	}

	// A sampled frame cross-fades the caption it is leaving into its own.
	inkTL, inkBR := b.diagram.InkBoundingBox()
	for i, caption := range []*d2target.Annotation{b.diagram.Root.LeavingAnnotation, b.diagram.Root.Annotation} {
		if caption == nil {
			continue
		}
		board := *b.diagram
		board.Root.Annotation = caption
		box := board.CaptionBox(inkTL, inkBR)
		if box == nil || caption.CaptionOpacity() <= 0 {
			continue
		}
		id := "root:caption"
		if i == 0 {
			id += ":leaving"
		}
		node, err := b.buildAnnotationText("board caption", id, caption.Label, "", d2target.CaptionFontSize, box, caption.CaptionPaint(libcolor.N1))
		if err != nil {
			return nil, err
		}
		node.Opacity = caption.CaptionOpacity()
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// buildAnnotationText lays the Markdown out with the same ruler the SVG
// renderer uses and centers it in the box reserved for it.
func (b *builder) buildAnnotationText(object, id, markdown, fontFamily string, fontSize int, box *geo.Box, paint string) (*d2scene.Node, error) {
	primary, mono := b.markdownFontFamilies(fontFamily)
	ruler, err := b.markdownLayoutRuler()
	if err != nil {
		return nil, fmt.Errorf("scene: %s Markdown ruler: %w", object, err)
	}
	layout, err := textmeasure.LayoutMarkdown(markdown, ruler, &primary, &mono, fontSize)
	if err != nil {
		return nil, fmt.Errorf("scene: %s Markdown layout: %w", object, err)
	}
	if len(layout.Primitives) > maxMarkdownPrimitives {
		return nil, fmt.Errorf("scene: %s Markdown primitive count %d exceeds limit %d", object, len(layout.Primitives), maxMarkdownPrimitives)
	}
	x := box.TopLeft.X + (box.Width-float64(layout.Width))/2
	y := box.TopLeft.Y + (box.Height-float64(layout.Height))/2
	if !finite(x) || !finite(y) || layout.Width <= 0 || layout.Height <= 0 {
		return nil, invalidField(object, "bounds", nil, "must resolve to finite positive dimensions")
	}
	return b.buildMarkdownLabel(
		object, id,
		d2target.Text{Label: markdown, FontFamily: fontFamily, FontSize: fontSize, LabelWidth: layout.Width, LabelHeight: layout.Height},
		paint, "", true,
		geo.NewPoint(x, y),
	)
}
