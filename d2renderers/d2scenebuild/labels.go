package d2scenebuild

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// buildConnectionMask creates one diagram-wide label mask. A label belonging
// to one object can cover an unrelated connection beneath it.
func (b *builder) buildConnectionMask(viewBox d2scene.Box) (*d2scene.Mask, error) {
	black := d2scene.SolidPaint{Color: color.NRGBA{A: 0xff}}
	white := d2scene.SolidPaint{Color: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}}
	holes := make([]*d2scene.Node, 0)

	for _, targetShape := range b.diagram.Shapes {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
		position := label.FromString(targetShape.LabelPosition)
		if targetShape.Label == "" || !position.IsBorder() {
			continue
		}
		// Class and table shapes do not participate in the ordinary border-label
		// mask stage.
		if targetShape.Type == d2target.ShapeClass || targetShape.Type == d2target.ShapeSQLTable {
			continue
		}
		box, topLeft := shapeLabelPlacement(targetShape)
		holeBox, ok := borderLabelMaskBox(position, topLeft, targetShape.LabelWidth, targetShape.LabelHeight, box, targetShape.RenderedStrokeWidth())
		if !ok || holeBox.Width <= 0 || holeBox.Height <= 0 {
			continue
		}
		hole := d2scene.NewNode(d2scene.Rect{Box: holeBox, Fill: black})
		hole.ID = targetShape.ID + ":border-label-mask-hole"
		holes = append(holes, hole)
	}

	paintVisible := d2themes.PaintVisibleIn(b.theme)
	for _, connection := range b.diagram.Connections {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
		maskCaption := connection.LabelMasksStroke(paintVisible)
		if connection.Opacity == 0 || !maskCaption && connection.Icon == nil {
			continue
		}
		var holeBox d2scene.Box
		position := connection.IconPosition
		if maskCaption {
			topLeft := connection.GetLabelTopLeft()
			if topLeft == nil {
				return nil, invalidField(fmt.Sprintf("connection %q", connection.ID), "labelPosition", connection.LabelPosition, "does not resolve on the route")
			}
			x := math.Round(topLeft.X) - 2
			width := connection.LabelWidth + 4
			if connection.Icon != nil {
				x -= d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE
				width += d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE
			}
			holeBox = d2scene.Box{
				X: x, Y: math.Round(topLeft.Y),
				Width: float64(width), Height: float64(connection.LabelHeight),
			}
			position = connection.LabelPosition
		} else {
			topLeft := connection.GetIconPosition()
			if topLeft == nil {
				return nil, invalidField(fmt.Sprintf("connection %q", connection.ID), "iconPosition", connection.IconPosition, "does not resolve on the route")
			}
			holeBox = d2scene.Box{
				X: topLeft.X - 2, Y: topLeft.Y,
				Width: d2target.DEFAULT_ICON_SIZE + 4, Height: d2target.DEFAULT_ICON_SIZE,
			}
		}
		hole := d2scene.NewNode(d2scene.Rect{
			Box:  holeBox,
			Fill: black,
		})
		hole.ID = connection.ID + ":label-mask-hole"
		if !label.FromString(position).IsOnEdge() {
			hole.Opacity *= .75
		}
		if p := connection.MotionLabel; p != nil {
			hole.Opacity *= p.Opacity
		}
		holes = append(holes, hole)
	}

	if len(holes) == 0 {
		return nil, nil
	}
	root := d2scene.NewNode(nil)
	root.ID = "connection-label-mask"
	base := d2scene.NewNode(d2scene.Rect{Box: viewBox, Fill: white})
	base.ID = "connection-label-mask:base"
	root.Children = append(root.Children, base)
	root.Children = append(root.Children, holes...)
	return &d2scene.Mask{Type: d2scene.MaskLuminance, Root: root, Transform: d2scene.Identity()}, nil
}

// These openings affect only the individual connection stroke.
func connectionStrokeMask(viewBox d2scene.Box, gaps []d2target.ConnectionGap, labelGaps []d2target.SequenceLabelGap) *d2scene.Mask {
	if len(gaps) == 0 && len(labelGaps) == 0 {
		return nil
	}
	root := d2scene.NewNode(nil)
	root.Children = append(root.Children, d2scene.NewNode(d2scene.Rect{
		Box: viewBox, Fill: d2scene.SolidPaint{Color: color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	}))
	for _, gap := range gaps {
		root.Children = append(root.Children, d2scene.NewNode(d2scene.Ellipse{
			Center:  d2scene.Point{X: gap.Center.X, Y: gap.Center.Y},
			RadiusX: gap.Radius, RadiusY: gap.Radius,
			Fill: d2scene.SolidPaint{Color: color.NRGBA{A: 255}},
		}))
	}
	for _, gap := range labelGaps {
		hole := d2scene.NewNode(d2scene.Rect{
			Box:  d2scene.Box{X: gap.TopLeft.X, Y: gap.TopLeft.Y, Width: gap.Width, Height: gap.Height},
			Fill: d2scene.SolidPaint{Color: color.NRGBA{A: 255}},
		})
		hole.Opacity = gap.Opacity
		root.Children = append(root.Children, hole)
	}
	return &d2scene.Mask{Type: d2scene.MaskLuminance, Root: root, Transform: d2scene.Identity()}
}

func borderLabelMaskBox(position label.Position, topLeft *geo.Point, labelWidth, labelHeight int, shapeBox *geo.Box, strokeWidth float64) (d2scene.Box, bool) {
	var box d2scene.Box
	switch position {
	case label.BorderTopLeft, label.BorderTopCenter, label.BorderTopRight:
		box = d2scene.Box{
			X: topLeft.X - 2, Y: shapeBox.TopLeft.Y - strokeWidth/2,
			Width: float64(labelWidth + 4), Height: strokeWidth,
		}
	case label.BorderBottomLeft, label.BorderBottomCenter, label.BorderBottomRight:
		box = d2scene.Box{
			X: topLeft.X - 2, Y: shapeBox.TopLeft.Y + shapeBox.Height - strokeWidth/2,
			Width: float64(labelWidth + 4), Height: strokeWidth,
		}
	case label.BorderLeftTop, label.BorderLeftMiddle, label.BorderLeftBottom:
		box = d2scene.Box{
			X: shapeBox.TopLeft.X - strokeWidth/2, Y: topLeft.Y - 2,
			Width: strokeWidth, Height: float64(labelHeight + 4),
		}
	case label.BorderRightTop, label.BorderRightMiddle, label.BorderRightBottom:
		box = d2scene.Box{
			X: shapeBox.TopLeft.X + shapeBox.Width - strokeWidth/2, Y: topLeft.Y - 2,
			Width: strokeWidth, Height: float64(labelHeight + 4),
		}
	default:
		return d2scene.Box{}, false
	}
	return box, true
}

func shapeLabelPlacement(targetShape d2target.Shape) (*geo.Box, *geo.Point) {
	position := label.FromString(targetShape.LabelPosition)
	box := targetShape.LabelBox(position, targetShape.Geometry())
	return box, targetShape.LabelTopLeft(position, box)
}

func (b *builder) buildConnectionLabels(connection d2target.Connection) ([]*d2scene.Node, error) {
	nodes := make([]*d2scene.Node, 0, 5)
	if connection.Label != "" {
		topLeft := connection.GetLabelTopLeft()
		if topLeft == nil {
			return nil, invalidField(fmt.Sprintf("connection %q", connection.ID), "labelPosition", connection.LabelPosition, "does not resolve on the route")
		}
		topLeft = geo.NewPoint(math.Round(topLeft.X), math.Round(topLeft.Y))
		if connection.Language == "latex" {
			latex, err := b.buildLatexLabelNode(
				fmt.Sprintf("connection %q", connection.ID), connection.ID+":label:0",
				connection.Label, connection.Color, topLeft, 1,
			)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, latex)
		} else if connection.Language == "markdown" {
			markdown, err := b.buildConnectionMarkdown(connection, topLeft)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, markdown)
		} else if connection.Language != "" {
			object := fmt.Sprintf("connection %q", connection.ID)
			codeStyle, err := b.activeCodeStyle(object)
			if err != nil {
				return nil, err
			}
			runs, err := b.buildCodeTextRuns(
				object, connection.ID+":code", connection.Text, codeStyle,
				d2scene.Point{X: topLeft.X, Y: topLeft.Y},
			)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, runs...)
		} else {
			if connection.Fill != "" {
				fill, err := b.paint(connection.Fill, fmt.Sprintf("connection %q label fill", connection.ID))
				if err != nil {
					return nil, err
				}
				background := d2scene.NewNode(d2scene.Rect{
					Box: d2scene.Box{
						X: topLeft.X - 4, Y: topLeft.Y - 3,
						Width: float64(connection.LabelWidth + 8), Height: float64(connection.LabelHeight + 6),
					},
					RadiusX: 10, RadiusY: 10, Fill: fill,
				})
				background.ID = connection.ID + ":label-fill"
				nodes = append(nodes, background)
			}
			font, err := b.font(connection.Text)
			if err != nil {
				return nil, fmt.Errorf("scene: connection %q label: %w", connection.ID, err)
			}
			fontColor := connection.GetFontColor()
			underline := connection.Underline
			progress := connection.UnderlineProgress
			if connection.Link != "" {
				// Linked connection labels are underlined and use unvisited-link blue.
				fontColor = "blue"
				underline = true
				progress = nil
			}
			fill, err := b.paint(fontColor, fmt.Sprintf("connection %q label color", connection.ID))
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, centeredTextRuns(
				connection.ID+":label", connection.Label, topLeft,
				connection.LabelWidth, connection.LabelHeight, connection.FontSize,
				font, fill, underline, progress,
			)...)
			nodes = appendPlainStrikes(nodes, connection.Text, topLeft, fill)
		}
		if p := connection.MotionLabel; p != nil {
			caption := d2scene.NewNode(nil)
			caption.Opacity, caption.Children = p.Opacity, nodes
			nodes = []*d2scene.Node{caption}
		}
	}

	for _, endpoint := range []struct {
		name  string
		text  *d2target.Text
		isDst bool
	}{
		{name: "src", text: connection.SrcLabel},
		{name: "dst", text: connection.DstLabel, isDst: true},
	} {
		if endpoint.text == nil || endpoint.text.Label == "" {
			continue
		}
		fontText := connection.Text
		fontText.Bold = false
		fontText.Italic = true
		fontText.Underline = false
		// Arrowhead labels use the primary italic face independently of the main
		// connection label's optional mono face.
		fontText.FontFamily = "default"
		font, err := b.font(fontText)
		if err != nil {
			return nil, fmt.Errorf("scene: connection %q %s arrowhead label: %w", connection.ID, endpoint.name, err)
		}
		colorValue := d2target.FG_COLOR
		if endpoint.text.Color != "" {
			colorValue = endpoint.text.Color
		}
		fill, err := b.paint(colorValue, fmt.Sprintf("connection %q %s arrowhead label color", connection.ID, endpoint.name))
		if err != nil {
			return nil, err
		}
		topLeft := connection.GetArrowheadLabelPosition(endpoint.isDst)
		nodes = append(nodes, centeredTextRuns(
			fmt.Sprintf("%s:%s-label", connection.ID, endpoint.name), endpoint.text.Label, topLeft,
			endpoint.text.LabelWidth, endpoint.text.LabelHeight, connection.FontSize,
			font, fill, false, nil,
		)...)
	}
	return nodes, nil
}

// The scene owns its text runs independently of the sampled render target.
func copyUnderlineProgress(progress *float64) *float64 {
	if progress == nil {
		return nil
	}
	visible := *progress
	return &visible
}

func appendPlainStrikes(nodes []*d2scene.Node, label d2target.Text, topLeft *geo.Point, fill d2scene.Paint) []*d2scene.Node {
	label.MarkdownStrike.PlainRules(topLeft.X, topLeft.Y+float64(label.FontSize), float64(label.LabelWidth), float64(label.FontSize), func(x, y, width, height float64) {
		nodes = append(nodes, d2scene.NewNode(d2scene.Rect{Box: d2scene.Box{X: x, Y: y, Width: width, Height: height}, Fill: fill}))
	})
	return nodes
}

func centeredTextRuns(idPrefix, text string, topLeft *geo.Point, width, height, fontSize int, font d2scene.Font, fill d2scene.Paint, underline bool, progress *float64) []*d2scene.Node {
	lines := strings.Split(text, "\n")
	lineAdvance := float64(height) / float64(len(lines))
	nodes := make([]*d2scene.Node, 0, len(lines))
	for index, line := range lines {
		lineTop := topLeft.Y + float64(index)*lineAdvance
		run := d2scene.TextRun{
			Text: line,
			Origin: d2scene.Point{
				X: topLeft.X + float64(width)/2,
				Y: topLeft.Y + float64(fontSize) + float64(index)*lineAdvance,
			},
			Anchor:            d2scene.AnchorMiddle,
			Font:              font,
			Fill:              fill,
			Underline:         underline,
			UnderlineProgress: copyUnderlineProgress(progress),
			Ink:               d2scene.NewBounds(topLeft.X, lineTop, topLeft.X+float64(width), lineTop+lineAdvance),
		}
		node := d2scene.NewNode(run)
		node.ID = fmt.Sprintf("%s:%d", idPrefix, index)
		nodes = append(nodes, node)
	}
	return nodes
}
