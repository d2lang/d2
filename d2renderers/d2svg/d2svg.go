// d2svg implements an SVG renderer for d2 diagrams.
// The input is d2exporter's output
package d2svg

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"hash/fnv"
	"html"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"math"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2latex"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2sketch"
	"github.com/d2lang/d2/d2renderers/internal/codestyle"
	"github.com/d2lang/d2/d2renderers/internal/patternassets"
	"github.com/d2lang/d2/d2renderers/internal/svgstyle"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/internal/htmlentity"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/d2/lib/version"
)

const (
	DEFAULT_PADDING = 100

	appendixIconRadius = 16

	// Legend constants
	LEGEND_PADDING        = 20
	LEGEND_ITEM_SPACING   = 15
	LEGEND_ICON_SIZE      = 24
	LEGEND_FONT_SIZE      = 14
	LEGEND_CORNER_PADDING = 10
)

var multipleOffset = geo.NewVector(d2target.MULTIPLE_OFFSET, -d2target.MULTIPLE_OFFSET)

//go:embed tooltip.svg
var TooltipIcon string

//go:embed link.svg
var LinkIcon string

//go:embed style.css
var defaultBaseStylesheet string

// BaseStylesheet supplies the shared CSS for rendered SVG diagrams.
var BaseStylesheet = defaultBaseStylesheet

// UsesDefaultStylesheet reports whether generated markup can be optimized
// without affecting selectors supplied by the caller.
func UsesDefaultStylesheet() bool { return BaseStylesheet == defaultBaseStylesheet }

// MarkdownCSS is retained for compatibility with callers that used the
// browser-era Markdown stylesheet directly.
//
// Deprecated: native Markdown SVG does not use or inject this HTML stylesheet.
//
//go:embed github-markdown.css
var MarkdownCSS string

//go:embed dots.txt
var dots string

//go:embed lines.txt
var lines string

type RenderOpts struct {
	Pad                *int64
	Sketch             *bool
	Center             *bool
	ThemeID            *int64
	DarkThemeID        *int64
	ThemeOverrides     *d2target.ThemeOverrides
	DarkThemeOverrides *d2target.ThemeOverrides
	// AnimateProgress draws an elapsed-time line on animated exports.
	AnimateProgress *bool
	// AnimateProgressColor overrides the color of an enabled progress line.
	AnimateProgressColor string
	Font                 string
	// the svg will be scaled by this factor, if unset the svg will fit to screen
	Scale *float64

	// MasterID is passed when the diagram should use something other than its own hash for unique targeting
	// Currently, that's when multi-boards are collapsed
	MasterID    string
	NoXMLTag    *bool
	Salt        *string
	OmitVersion *bool
}

func invalidPaddingError(pad int64) error {
	return fmt.Errorf("padding %d produces invalid SVG dimensions", pad)
}

func checkedIntAdd(a, b, minInt, maxInt int64) (int64, bool) {
	if b > 0 && a > maxInt-b || b < 0 && a < minInt-b {
		return 0, false
	}
	return a + b, true
}

func checkedIntSub(a, b, minInt, maxInt int64) (int64, bool) {
	if b > 0 && a < minInt+b || b < 0 && a > maxInt+b {
		return 0, false
	}
	return a - b, true
}

func validatePadding(tl, br d2target.Point, pad int64) (int, error) {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	if pad < minInt || pad > maxInt {
		return 0, invalidPaddingError(pad)
	}

	doublePad, ok := checkedIntAdd(pad, pad, minInt, maxInt)
	if !ok {
		return 0, invalidPaddingError(pad)
	}
	for _, bounds := range [][2]int64{
		{int64(tl.X), int64(br.X)},
		{int64(tl.Y), int64(br.Y)},
	} {
		if _, ok := checkedIntSub(bounds[0], pad, minInt, maxInt); !ok {
			return 0, invalidPaddingError(pad)
		}
		size, ok := checkedIntSub(bounds[1], bounds[0], minInt, maxInt)
		if !ok {
			return 0, invalidPaddingError(pad)
		}
		if _, ok := checkedIntAdd(size, doublePad, minInt, maxInt); !ok {
			return 0, invalidPaddingError(pad)
		}
	}
	return int(pad), nil
}

func expandDimensions(left, top, width, height, amount int) (int, int, int, int, bool) {
	maxInt := int64(^uint(0) >> 1)
	minInt := -maxInt - 1
	amount64 := int64(amount)
	doubleAmount, ok := checkedIntAdd(amount64, amount64, minInt, maxInt)
	if !ok {
		return 0, 0, 0, 0, false
	}
	left64, ok := checkedIntSub(int64(left), amount64, minInt, maxInt)
	if !ok {
		return 0, 0, 0, 0, false
	}
	top64, ok := checkedIntSub(int64(top), amount64, minInt, maxInt)
	if !ok {
		return 0, 0, 0, 0, false
	}
	width64, ok := checkedIntAdd(int64(width), doubleAmount, minInt, maxInt)
	if !ok {
		return 0, 0, 0, 0, false
	}
	height64, ok := checkedIntAdd(int64(height), doubleAmount, minInt, maxInt)
	if !ok {
		return 0, 0, 0, 0, false
	}
	return int(left64), int(top64), int(width64), int(height64), true
}

func dimensions(diagram *d2target.Diagram, pad int, tl, br d2target.Point) (left, top, width, height int) {
	left = tl.X - pad
	top = tl.Y - pad
	width = br.X - tl.X + pad*2
	height = br.Y - tl.Y + pad*2
	// Account for legend dimensions if present
	if hasLegend(diagram) {
		ruler, err := textmeasure.NewRuler()
		if err == nil && ruler != nil {
			legendHeight, maxLabelWidth, rows := measureLegend(diagram.Legend, ruler)
			left, top, width, height = fitLegend(left, top, width, height, pad, tl, br, legendBoxHeight(diagram.Legend, legendHeight, rows), maxLabelWidth)
		}
	}
	return left, top, width, height
}

func hasLegend(diagram *d2target.Diagram) bool {
	return diagram.Legend != nil && (len(diagram.Legend.Shapes) > 0 || len(diagram.Legend.Connections) > 0)
}

// legendLabelSize measures one legend label.
func legendLabelSize(ruler *textmeasure.Ruler, text, fontFamily string) *d2target.TextDimensions {
	mtext := &d2target.MText{
		Text:     text,
		FontSize: LEGEND_FONT_SIZE,
	}
	return d2graph.GetTextDimensions(nil, ruler, mtext, fontToFamily(fontFamily))
}

// measureLegend returns the height of a legend's title and labeled rows
// (without bottom padding), its widest label and its number of labeled rows.
func measureLegend(legend *d2target.Legend, ruler *textmeasure.Ruler) (height, maxLabelWidth, rows int) {
	height = LEGEND_PADDING + LEGEND_FONT_SIZE + LEGEND_ITEM_SPACING
	row := func(text, fontFamily string) {
		if text == "" {
			return
		}
		dims := legendLabelSize(ruler, text, fontFamily)
		maxLabelWidth = max(maxLabelWidth, dims.Width)
		height += max(dims.Height, LEGEND_ICON_SIZE) + LEGEND_ITEM_SPACING
		rows++
	}
	for _, s := range legend.Shapes {
		row(s.Label, s.FontFamily)
	}
	for _, c := range legend.Connections {
		row(c.Label, c.FontFamily)
	}
	if rows > 0 {
		height -= LEGEND_ITEM_SPACING / 2
	}
	return height, maxLabelWidth, rows
}

// legendBoxHeight adds the bottom padding RenderLegend draws below the rows.
func legendBoxHeight(legend *d2target.Legend, height, rows int) int {
	if rows > 0 && len(legend.Connections) > 0 {
		return height + LEGEND_PADDING*1.5
	}
	return height + LEGEND_PADDING*1.2
}

func legendWidth(maxLabelWidth int) int {
	return LEGEND_PADDING*2 + LEGEND_ICON_SIZE + LEGEND_PADDING + maxLabelWidth
}

// legendY is the top of a legend of the given height, bottom-aligned with the
// diagram but never above it.
func legendY(tl, br d2target.Point, legendHeight int) int {
	return max(br.Y-legendHeight, tl.Y)
}

// fitLegend grows the viewport to include a legend right of the diagram.
func fitLegend(left, top, width, height, pad int, tl, br d2target.Point, legendHeight, maxLabelWidth int) (int, int, int, int) {
	if legendHeight <= 0 || maxLabelWidth <= 0 {
		return left, top, width, height
	}
	y := legendY(tl, br, legendHeight)
	if legendRight := br.X + LEGEND_CORNER_PADDING + legendWidth(maxLabelWidth); left+width < legendRight {
		width = legendRight - left + pad/2
	}
	if y < top {
		height += top - y
		top = y
	}
	if legendBottom := y + legendHeight; top+height < legendBottom {
		height = legendBottom - top + pad/2
	}
	return left, top, width, height
}

func RenderLegend(buf *bytes.Buffer, diagram *d2target.Diagram, diagramHash string, theme *d2themes.Theme) error {
	if !hasLegend(diagram) {
		return nil
	}
	for _, targetShape := range diagram.Legend.Shapes {
		if textmeasure.IsDangerousLink(targetShape.Link) {
			return fmt.Errorf("legend shape %q uses an unsafe link URL scheme", targetShape.ID)
		}
	}

	tl, br := diagram.BoundingBox()

	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return err
	}

	totalHeight, maxLabelWidth, rows := measureLegend(diagram.Legend, ruler)
	totalHeight = legendBoxHeight(diagram.Legend, totalHeight, rows)
	legendWidth := legendWidth(maxLabelWidth)
	legendX := br.X + LEGEND_CORNER_PADDING
	legendY := legendY(tl, br, totalHeight)

	legendEl := d2themes.NewThemableElement("rect", theme)
	legendEl.Fill = "#F7F7FA"
	legendEl.Stroke = "#DEE1EB"
	legendEl.Style = "stroke-width: 1px; filter: drop-shadow(0px 2px 3px rgba(0, 0, 0, 0.1))"
	legendEl.X = float64(legendX)
	legendEl.Y = float64(legendY)
	legendEl.Width = float64(legendWidth)
	legendEl.Height = float64(totalHeight)
	legendEl.Rx = 4
	// The shadow is drawn under the same rect filled white.
	fmt.Fprint(buf, legendEl.Render())
	legendEl.Fill = "#ffffff"
	legendEl.Style = "stroke-width: 1px"
	fmt.Fprint(buf, legendEl.Render())

	legendLabel := "Legend"
	if diagram.Legend.Label != "" {
		legendLabel = diagram.Legend.Label
	}
	fmt.Fprintf(buf, `<text class="text-bold" x="%d" y="%d" style="font-size: %dpx;">%s</text>`,
		legendX+LEGEND_PADDING, legendY+LEGEND_PADDING+LEGEND_FONT_SIZE, LEGEND_FONT_SIZE+2, html.EscapeString(legendLabel))

	currentY := legendY + LEGEND_PADDING*2 + LEGEND_FONT_SIZE
	iconX := legendX + LEGEND_PADDING
	// row writes one labeled legend row; baseline places the text below the
	// row's middle by that fraction of the text height.
	row := func(text, fontFamily, icon string, baseline float64) {
		fmt.Fprint(buf, icon)
		dims := legendLabelSize(ruler, text, fontFamily)
		rowHeight := max(dims.Height, LEGEND_ICON_SIZE)
		textY := currentY + rowHeight/2 + int(float64(dims.Height)*baseline)
		fmt.Fprintf(buf, `<text class="text" x="%d" y="%d" style="font-size: %dpx;">%s</text>`,
			iconX+LEGEND_ICON_SIZE+LEGEND_PADDING, textY, LEGEND_FONT_SIZE,
			html.EscapeString(text))
		currentY += rowHeight + LEGEND_ITEM_SPACING
	}

	shapeCount := 0
	for _, s := range diagram.Legend.Shapes {
		if s.Label == "" {
			continue
		}
		shapeIcon, err := renderLegendShapeIcon(s, iconX, currentY, diagramHash, theme)
		if err != nil {
			return err
		}
		row(s.Label, s.FontFamily, shapeIcon, 0.3)
		shapeCount++
	}

	if shapeCount > 0 && len(diagram.Legend.Connections) > 0 {
		currentY += LEGEND_ITEM_SPACING / 2

		separatorEl := d2themes.NewThemableElement("line", theme)
		separatorEl.X1 = float64(legendX + LEGEND_PADDING)
		separatorEl.Y1 = float64(currentY)
		separatorEl.X2 = float64(legendX + legendWidth - LEGEND_PADDING)
		separatorEl.Y2 = float64(currentY)
		separatorEl.Stroke = "#DEE1EB"
		separatorEl.StrokeDashArray = "2,2"
		fmt.Fprint(buf, separatorEl.Render())

		currentY += LEGEND_ITEM_SPACING
	}

	for _, c := range diagram.Legend.Connections {
		if c.Label == "" {
			continue
		}
		connIcon, err := renderLegendConnectionIcon(c, iconX, currentY+LEGEND_ICON_SIZE/2, theme)
		if err != nil {
			return err
		}
		row(c.Label, c.FontFamily, connIcon, 0.2)
	}

	return nil
}

func renderLegendShapeIcon(s d2target.Shape, x, y int, diagramHash string, theme *d2themes.Theme) (string, error) {
	iconShape := s
	const sizeFactor = 5
	iconShape.Pos.X = 0
	iconShape.Pos.Y = 0
	iconShape.Width = LEGEND_ICON_SIZE * sizeFactor
	iconShape.Height = LEGEND_ICON_SIZE * sizeFactor
	iconShape.Label = ""
	buf := &bytes.Buffer{}
	appendixBuf := &bytes.Buffer{}
	finalBuf := &bytes.Buffer{}
	fmt.Fprintf(finalBuf, `<g transform="translate(%d, %d) scale(%s)">`,
		x, y, svg.FormatFloat(1.0/sizeFactor))
	_, err := drawShape(&objectWriter{buf: buf}, appendixBuf, diagramHash, diagramHash, iconShape, false, theme, newMarkdownRenderer(nil, nil, theme))
	if err != nil {
		return "", err
	}

	fmt.Fprint(finalBuf, buf.String())

	fmt.Fprint(finalBuf, `</g>`)

	return finalBuf.String(), nil
}

func renderLegendConnectionIcon(c d2target.Connection, x, y int, theme *d2themes.Theme) (string, error) {
	finalBuf := &bytes.Buffer{}

	buf := &bytes.Buffer{}

	const sizeFactor = 2

	legendConn := *d2target.BaseConnection()

	legendConn.ID = c.ID
	legendConn.SrcArrow = c.SrcArrow
	legendConn.DstArrow = c.DstArrow
	legendConn.StrokeDash = c.StrokeDash
	legendConn.StrokeWidth = c.StrokeWidth
	legendConn.StrokeWidthOffset = c.StrokeWidthOffset
	legendConn.Stroke = c.Stroke
	legendConn.Fill = c.Fill
	legendConn.BorderRadius = c.BorderRadius
	legendConn.Opacity = c.Opacity
	legendConn.Animated = c.Animated

	startX := 0.0
	midY := 0.0
	width := float64(LEGEND_ICON_SIZE * sizeFactor)

	legendConn.Route = []*geo.Point{
		{X: startX, Y: midY},
		{X: startX + width, Y: midY},
	}

	legendHash := fmt.Sprintf("legend-%s", hash(fmt.Sprintf("%s-%d-%d", c.ID, x, y), 10))

	defined := make(map[string]struct{})
	idToShape := make(map[string]d2target.Shape)

	fmt.Fprintf(finalBuf, `<g transform="translate(%d, %d) scale(%s)">`,
		x, y, svg.FormatFloat(1.0/sizeFactor))

	_, err := drawConnection(&objectWriter{buf: buf}, legendHash, connectionEffects{}, legendConn, defined, idToShape, false, theme, newMarkdownRenderer(nil, nil, theme))
	if err != nil {
		return "", err
	}

	fmt.Fprint(finalBuf, buf.String())

	fmt.Fprint(finalBuf, `</g>`)

	return finalBuf.String(), nil
}

func arrowheadMarkerID(diagramHash string, isTarget bool, connection d2target.Connection, placement string, compact bool) string {
	var arrowhead d2target.Arrowhead
	if isTarget {
		arrowhead = connection.DstArrow
	} else {
		arrowhead = connection.SrcArrow
	}

	// Keep every bit of both hashes and the board namespace. A trailing delimiter keeps whole-ID string replacement (e.g. Obsidian)
	// from matching a shorter paint hash inside another marker's ID.
	scope, base, end := diagramHash, 10, ""
	if compact {
		scope, base, end = strings.TrimPrefix(diagramHash, "d2-"), 36, "_"
	}
	paint := fmt.Sprintf("%s,%t,%s,%s", arrowhead, isTarget, strconv.FormatFloat(connection.RenderedStrokeWidth(), 'f', -1, 64), connection.Stroke)
	id := "mk-" + scope + "-" + hash(paint, base)
	if placement != "" {
		id += "-" + hash(placement, base)
	}
	return id + end
}

// markerPoints formats coordinate pairs as an SVG points list ("x,y x,y ...").
func markerPoints(xy ...float64) string {
	pairs := make([]string, 0, len(xy)/2)
	for i := 0; i+1 < len(xy); i += 2 {
		pairs = append(pairs, svg.FormatFloat(xy[i])+","+svg.FormatFloat(xy[i+1]))
	}
	return strings.Join(pairs, " ")
}

func arrowheadMarker(isTarget bool, id string, connection d2target.Connection, inlineTheme *d2themes.Theme, gradient *connectionGradient, worldToMarker d2scene.Matrix) string {
	arrowhead := connection.DstArrow
	if !isTarget {
		arrowhead = connection.SrcArrow
	}
	strokeWidth := connection.RenderedStrokeWidth()
	width, height := arrowhead.Dimensions(strokeWidth)

	markerWidth := width
	var refX float64
	refY := height / 2
	switch arrowhead {
	case d2target.DiamondArrowhead:
		if isTarget {
			refX = width - 0.6*strokeWidth
		} else {
			refX = width/8 + 0.6*strokeWidth
		}
		markerWidth *= 1.1
	default:
		if isTarget {
			refX = width - 1.5*strokeWidth
		} else {
			refX = 1.5 * strokeWidth
		}
	}

	if gradient != nil {
		worldToMarker = d2scene.Translate(refX, refY).Mul(worldToMarker)
		if !isTarget && (arrowhead == d2target.CfOne || arrowhead == d2target.CfOneRequired || arrowhead == d2target.CfMany || arrowhead == d2target.CfManyRequired) {
			// The source crow's foot is flipped inside the marker viewport.
			worldToMarker = d2scene.Translate(width, height).Mul(d2scene.Scale(-1, -1)).Mul(worldToMarker)
		}
		connection.Stroke = gradient.paint(worldToMarker)
	}

	// stroked creates an element drawn in the connection's stroke width. Solid
	// elements are filled with the stroke paint; outlined ones are stroked with
	// it over the background color.
	stroked := func(tag string, solid bool) *d2themes.ThemableElement {
		el := d2themes.NewThemableElement(tag, inlineTheme)
		el.ClassName = "connection"
		if solid {
			el.Fill = connection.Stroke
		} else {
			el.Fill = d2target.BG_COLOR
			el.Stroke = connection.Stroke
		}
		el.AddAttributes(svg.FloatAttr("stroke-width", strokeWidth))
		return el
	}

	var el *d2themes.ThemableElement
	switch arrowhead {
	case d2target.ArrowArrowhead:
		el = stroked("polygon", true)
		if isTarget {
			el.Points = markerPoints(0, 0, width, height/2, 0, height, width/4, height/2)
		} else {
			el.Points = markerPoints(0, height/2, width, 0, width*3/4, height/2, width, height)
		}
	case d2target.TriangleArrowhead, d2target.UnfilledTriangleArrowhead, d2target.LineArrowhead:
		// Outlined triangles and lines are inset by half the stroke to stay inside the marker.
		tag, inset := "polygon", 0.
		if arrowhead != d2target.TriangleArrowhead {
			inset = strokeWidth / 2
		}
		if arrowhead == d2target.LineArrowhead {
			tag = "polyline"
		}
		el = stroked(tag, arrowhead == d2target.TriangleArrowhead)
		if arrowhead == d2target.LineArrowhead {
			el.Fill = color.None
		}
		if isTarget {
			el.Points = markerPoints(inset, inset, width-inset, height/2, inset, height-inset)
		} else {
			el.Points = markerPoints(width-inset, inset, inset, height/2, width-inset, height-inset)
		}
	case d2target.FilledDiamondArrowhead:
		el = stroked("polygon", true)
		el.Points = markerPoints(0, height/2, width/2, 0, width, height/2, width/2, height)
	case d2target.DiamondArrowhead:
		el = stroked("polygon", false)
		if isTarget {
			el.Points = markerPoints(0, height/2, width/2, height/8, width, height/2, width/2, height*0.9)
		} else {
			el.Points = markerPoints(width/8, height/2, width*0.6, height/8, width*1.1, height/2, width*0.6, height*7/8)
		}
	case d2target.FilledCircleArrowhead, d2target.CircleArrowhead:
		radius := width / 2
		el = stroked("circle", arrowhead == d2target.FilledCircleArrowhead)
		el.Cy = radius
		el.R = radius - strokeWidth/2
		if arrowhead == d2target.CircleArrowhead {
			el.ClassName = ""
			el.R = radius - strokeWidth
		}
		if isTarget {
			el.Cx = radius + strokeWidth/2
		} else {
			el.Cx = radius - strokeWidth/2
		}
	case d2target.FilledBoxArrowhead, d2target.BoxArrowhead:
		inset := 0.
		el = stroked("polygon", arrowhead == d2target.FilledBoxArrowhead)
		if arrowhead == d2target.BoxArrowhead {
			el.Style = "stroke-linejoin:miter;"
			inset = strokeWidth / 2
		}
		el.Points = markerPoints(inset, inset, inset, height-inset, width-inset, height-inset, width-inset, inset)
	case d2target.CrossArrowhead:
		inset := strokeWidth / 8
		rotationAngle := math.Pi / 4
		origin := geo.NewPoint(width/2, height/2)
		newOrigin := geo.NewPoint(math.Cos(rotationAngle)*origin.X-math.Sin(rotationAngle)*origin.Y, math.Sin(rotationAngle)*origin.X+math.Cos(rotationAngle)*origin.Y)
		crossEl := d2themes.NewThemableElement("polygon", inlineTheme)
		// The comma after the fourth point is historical output.
		crossEl.Points = markerPoints(
			0, height/2+inset,
			width/2-inset, height/2+inset,
			width/2-inset, height,
			width/2+inset, height,
		) + ", " + markerPoints(
			width/2+inset, height/2+inset,
			width, height/2+inset,
			width, height/2-inset,
			width/2+inset, height/2-inset,
			width/2+inset, 0,
			width/2-inset, 0,
			width/2-inset, height/2-inset,
			0, height/2-inset,
		)
		crossEl.Transform = fmt.Sprintf("translate(%s, %s) rotate(45)", svg.FormatFloat(-newOrigin.X+width/2), svg.FormatFloat(-newOrigin.Y+height/2))
		if gradient != nil {
			crossEl.Stroke = gradient.paint(d2scene.RotateAround(-rotationAngle, width/2, height/2).Mul(worldToMarker))
		}
		tailX := 0.
		if isTarget {
			tailX = width
		}
		childPathEl := d2themes.NewThemableElement("path", inlineTheme)
		childPathEl.D = "M" + markerPoints(width/2, height/2, tailX, height/2)
		el = stroked("g", false)
		el.SetInnerSVG(svg.Join(svg.TrustedFragment(crossEl.Render()), svg.TrustedFragment(childPathEl.Render())))
	case d2target.CfOne, d2target.CfMany, d2target.CfOneRequired, d2target.CfManyRequired:
		offset := 3.0 + strokeWidth*1.8
		var modifierEl *d2themes.ThemableElement
		if arrowhead == d2target.CfOneRequired || arrowhead == d2target.CfManyRequired {
			modifierEl = stroked("path", false)
			modifierEl.D = "M" + markerPoints(offset, 0, offset, height)
		} else {
			modifierEl = stroked("circle", false)
			modifierEl.Cx = offset/2.0 + 2.0
			modifierEl.Cy = height / 2.0
			modifierEl.R = offset / 2.0
		}
		childPathEl := d2themes.NewThemableElement("path", inlineTheme)
		childPathEl.D = "M" + markerPoints(width-3.0, height/2.0, width+offset, height/2.0)
		if arrowhead == d2target.CfMany || arrowhead == d2target.CfManyRequired {
			childPathEl.D += " M" + markerPoints(offset+3.0, height/2.0, width+offset, 0) +
				" M" + markerPoints(offset+3.0, height/2.0, width+offset, height)
		} else {
			childPathEl.D += " M" + markerPoints(offset*2.0, 0, offset*2.0, height)
		}
		el = stroked("g", false)
		if !isTarget {
			el.Transform = fmt.Sprintf("scale(-1) translate(-%s, -%s)", svg.FormatFloat(width), svg.FormatFloat(height))
		}
		el.SetInnerSVG(svg.Join(svg.TrustedFragment(modifierEl.Render()), svg.TrustedFragment(childPathEl.Render())))
	default:
		return ""
	}
	widthText, heightText := svg.FormatFloat(markerWidth), svg.FormatFloat(height)
	return `<marker id="` + svg.EscapeAttribute(id) + `" markerWidth="` + widthText +
		`" markerHeight="` + heightText + `" refX="` + svg.FormatFloat(refX) + `" refY="` + svg.FormatFloat(refY) +
		`" viewBox="0 0 ` + widthText + " " + heightText + `" orient="auto" markerUnits="userSpaceOnUse">` +
		el.Render() + "</marker>"
}

// pathData serializes the same centerline used to find connection crossings.
func pathData(connection d2target.Connection, srcAdj, dstAdj *geo.Point) string {
	var path strings.Builder
	writePoint := func(p geo.Point) {
		path.WriteByte(' ')
		path.WriteString(svg.FormatFloat(p.X))
		path.WriteByte(' ')
		path.WriteString(svg.FormatFloat(p.Y))
	}
	for cmd := range connection.PathCommands(srcAdj, dstAdj) {
		if path.Len() > 0 {
			path.WriteByte(' ')
		}
		path.WriteByte(cmd.Kind)
		if cmd.Kind == 'C' {
			writePoint(cmd.Control1)
		}
		if cmd.Kind == 'C' || cmd.Kind == 'S' {
			writePoint(cmd.Control2)
		}
		writePoint(cmd.End)
	}
	return path.String()
}

func makeLabelMask(labelTL *geo.Point, width, height int, opacity float64) string {
	return makeLabelMaskWithTransform(labelTL, width, height, opacity, nil)
}

func makeLabelMaskWithTransform(labelTL *geo.Point, width, height int, opacity float64, shapePos *geo.Point) string {
	fill := "black"
	if opacity != 1 {
		fill = fmt.Sprintf("rgba(0,0,0,%.2f)", opacity)
	}

	x := labelTL.X - 2
	y := labelTL.Y

	// In sketch mode, shapes use transform="translate(shapePos.X, shapePos.Y)"
	// so we need to adjust mask coordinates to account for this transform
	if shapePos != nil {
		x -= shapePos.X
		y -= shapePos.Y
	}

	return svg.EmptyElement("rect", svg.FloatAttr("x", x), svg.FloatAttr("y", y),
		svg.IntAttr("width", width+4), svg.IntAttr("height", height), svg.Attr("fill", fill))
}

func hasBorderLabel(s d2target.Shape) bool {
	return s.Label != "" && label.FromString(s.LabelPosition).IsBorder()
}

// border label mask is minimal, intending to only cover the portion of the border that passes through the label
// otherwise, when the inside background and outside background colors don't match, there's a bigger discrepancy
func makeBorderLabelMask(labelPosition label.Position, labelTL *geo.Point, labelWidth, labelHeight int, shapeBox *geo.Box, strokeWidth, opacity float64, shapePos *geo.Point) string {
	fill := "black"
	if opacity != 1 {
		fill = fmt.Sprintf("rgba(0,0,0,%.2f)", opacity)
	}

	// In sketch mode, multiply stroke width by 3 for mask calculations to account for
	// the hand-drawn style that can be wider and more irregular
	effectiveStrokeWidth := strokeWidth
	if shapePos != nil {
		effectiveStrokeWidth *= 3
	}

	var maskX, maskY, maskWidth, maskHeight float64

	switch labelPosition {
	case label.BorderTopLeft, label.BorderTopCenter, label.BorderTopRight:
		maskX = labelTL.X - 2
		maskY = shapeBox.TopLeft.Y - effectiveStrokeWidth/2
		maskWidth = float64(labelWidth + 4)
		maskHeight = effectiveStrokeWidth

	case label.BorderBottomLeft, label.BorderBottomCenter, label.BorderBottomRight:
		maskX = labelTL.X - 2
		maskY = shapeBox.TopLeft.Y + shapeBox.Height - effectiveStrokeWidth/2
		maskWidth = float64(labelWidth + 4)
		maskHeight = effectiveStrokeWidth

	case label.BorderLeftTop, label.BorderLeftMiddle, label.BorderLeftBottom:
		maskX = shapeBox.TopLeft.X - effectiveStrokeWidth/2
		maskY = labelTL.Y - 2
		maskWidth = effectiveStrokeWidth
		maskHeight = float64(labelHeight + 4)

	case label.BorderRightTop, label.BorderRightMiddle, label.BorderRightBottom:
		maskX = shapeBox.TopLeft.X + shapeBox.Width - effectiveStrokeWidth/2
		maskY = labelTL.Y - 2
		maskWidth = effectiveStrokeWidth
		maskHeight = float64(labelHeight + 4)

	default:
		// never gets here
		return ""
	}

	// In sketch mode, adjust coordinates to account for shape transform
	if shapePos != nil {
		maskX -= shapePos.X
		maskY -= shapePos.Y
	}

	return svg.EmptyElement("rect", svg.FloatAttr("x", maskX), svg.FloatAttr("y", maskY),
		svg.FloatAttr("width", maskWidth), svg.FloatAttr("height", maskHeight), svg.Attr("fill", fill))
}

func openMotionViewport(writer io.Writer, c *d2target.MotionClip) {
	// Identical viewBox and viewport coordinates clip without translating
	// the child. This also avoids document-wide clip IDs in animated SVGs.
	fmt.Fprint(writer, svg.OpenElement("svg",
		svg.IntAttr("x", c.X), svg.IntAttr("y", c.Y), svg.IntAttr("width", c.Width), svg.IntAttr("height", c.Height),
		svg.Attr("viewBox", fmt.Sprintf("%d %d %d %d", c.X, c.Y, c.Width, c.Height)), svg.Attr("overflow", "hidden")))
}

type connectionEffects struct {
	labelID  string
	crossing crossingEffect
}

func drawConnection(writer *objectWriter, markerScope string, effects connectionEffects, connection d2target.Connection, defined map[string]struct{}, idToShape map[string]d2target.Shape, sketch bool, inlineTheme *d2themes.Theme, markdown *markdownRenderer) (labelMask string, _ error) {
	if c := connection.MotionClip; c != nil {
		openMotionViewport(writer, c)
		defer writer.buf.WriteString("</svg>")
	}
	classes := []string{base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(connection.ID)))}
	classes = append(classes, connection.Classes...)
	opacityStyle := ""
	if connection.Opacity != 1.0 {
		opacityStyle = fmt.Sprintf(" style='opacity:%s'", svg.FormatFloat(connection.Opacity))
	}
	writer.openGroup(classes, opacityStyle)
	srcAdj, dstAdj := connection.ArrowheadAdjustments(idToShape)
	var gradient *connectionGradient
	var srcMarker, dstMarker d2scene.Matrix
	if color.IsGradient(connection.Stroke) {
		var err error
		gradient, err = newConnectionGradient(writer, connection, defined)
		if err != nil {
			return "", err
		}
		connection.Stroke = gradient.paint(d2scene.Identity())
		if !sketch {
			srcMarker, dstMarker = markerEndpoints(connection, srcAdj, dstAdj)
		}
	}
	marker := func(arrow d2target.Arrowhead, target bool, placement d2scene.Matrix) []svg.Attribute {
		if arrow == d2target.NoArrowhead || (sketch && gradient != nil) {
			return nil
		}
		position := ""
		if gradient != nil {
			position = fmt.Sprint(placement)
		}
		id := arrowheadMarkerID(markerScope, target, connection, position, writer.compactMarkerIDs)
		if _, exists := defined[id]; !exists {
			markup := arrowheadMarker(target, id, connection, inlineTheme, gradient, placement)
			if markup == "" {
				panic(fmt.Sprintf("received empty arrow head marker for: %#v", connection))
			}
			fmt.Fprint(writer, markup)
			defined[id] = struct{}{}
		}
		attr := "marker-start"
		if target {
			attr = "marker-end"
		}
		return []svg.Attribute{svg.Attr(attr, svg.LocalIRI(svg.LiteralID(id)))}
	}
	markerStart := marker(connection.SrcArrow, false, srcMarker)
	markerEnd := marker(connection.DstArrow, true, dstMarker)

	if connection.Icon != nil {
		iconPos := connection.GetIconPosition()
		if iconPos != nil {
			attributes := []svg.Attribute{
				svg.Attr("href", connection.Icon.String()),
				svg.FloatAttr("x", iconPos.X),
				svg.FloatAttr("y", iconPos.Y),
				svg.IntAttr("width", d2target.DEFAULT_ICON_SIZE),
				svg.IntAttr("height", d2target.DEFAULT_ICON_SIZE),
			}
			if connection.IconBorderRadius != 0 {
				attributes = append(attributes, svg.Attr("clip-path", "inset(0 round "+svg.FormatFloat(connection.IconBorderRadius)+"px)"))
			}
			fmt.Fprint(writer, svg.EmptyElement("image", attributes...))
		}
	}

	var labelTL, maskTL *geo.Point
	maskWidth, maskHeight := d2target.DEFAULT_ICON_SIZE, d2target.DEFAULT_ICON_SIZE
	maskPosition := connection.IconPosition
	if connection.Label != "" {
		labelTL = connection.GetLabelTopLeft()
		labelTL.X = math.Round(labelTL.X)
		labelTL.Y = math.Round(labelTL.Y)

		maskTL = labelTL.Copy()
		maskWidth, maskHeight = connection.LabelWidth, connection.LabelHeight
		maskPosition = connection.LabelPosition
		if connection.Icon != nil {
			maskWidth += d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE
			maskTL.X -= float64(d2target.CONNECTION_ICON_LABEL_GAP + d2target.DEFAULT_ICON_SIZE)
		}
	} else if iconPos := connection.GetIconPosition(); iconPos != nil {
		maskTL = &geo.Point{X: iconPos.X, Y: iconPos.Y}
	}
	if maskTL != nil {
		opacity := 0.75
		if label.FromString(maskPosition).IsOnEdge() {
			opacity = 1
		}
		if p := connection.MotionLabel; p != nil && connection.Label != "" {
			opacity *= p.Opacity
		}
		labelMask = makeLabelMask(maskTL, maskWidth, maskHeight, opacity)
	}

	// An invisible caption or icon must not erase an unrelated stroke. A
	// visible caption keeps its usual contrast even on a translucent wire.
	if connection.Opacity == 0 {
		labelMask = ""
	}
	path := pathData(connection, srcAdj, dstAdj)
	crossing := effects.crossing
	var pathAttrs []svg.Attribute
	if effects.labelID != "" && !crossing.replacesEmptyMask {
		pathAttrs = []svg.Attribute{svg.Attr("mask", svg.LocalIRI(svg.LiteralID(effects.labelID)))}
	}
	wrapCrossing := crossing.id != "" && (sketch || (crossing.mask && !crossing.replacesEmptyMask))
	if wrapCrossing {
		fmt.Fprint(writer, svg.OpenElement("g", crossing.attribute()))
	} else if crossing.id != "" {
		pathAttrs = append(pathAttrs, crossing.attribute())
	}

	if sketch {
		out, err := d2sketch.ConnectionWithAttributes(connection, path, pathAttrs)
		if err != nil {
			return "", err
		}
		fmt.Fprint(writer, out)

		// render sketch arrowheads separately
		sketchPaint := func(arrow d2target.Arrowhead, target bool, adjustment *geo.Point) string {
			if gradient == nil || arrow == d2target.NoArrowhead {
				return connection.Stroke
			}
			p, degrees := d2sketch.ArrowheadPlacement(connection, target, adjustment)
			return gradient.paint(worldToEndpoint(p, degrees*math.Pi/180))
		}
		arrowPaths, err := d2sketch.ArrowheadsWithPaint(connection, srcAdj, dstAdj,
			sketchPaint(connection.SrcArrow, false, srcAdj), sketchPaint(connection.DstArrow, true, dstAdj))
		if err != nil {
			return "", err
		}
		fmt.Fprint(writer, arrowPaths)
	} else {
		className := "connection"
		if connection.Animated {
			className += " animated-connection"
		}
		stroke := func(d, extraStyle string, attrs ...svg.Attribute) {
			pathEl := d2themes.NewThemableElement("path", inlineTheme)
			pathEl.D = d
			pathEl.Fill = color.None
			pathEl.Stroke = connection.Stroke
			pathEl.ClassName = className
			pathEl.Style = connection.CSSStyle() + extraStyle
			pathEl.AddAttributes(append(attrs, pathAttrs...)...)
			writer.element(pathEl)
		}
		// If connection is animated and bidirectional
		if connection.Animated && (connection.DstArrow == d2target.NoArrowhead) == (connection.SrcArrow == d2target.NoArrowhead) {
			// There is no pure CSS way to animate bidirectional connections in two directions, so we split it up
			path1, path2, err := svg.SplitPath(path, 0.5)
			if err != nil {
				return "", err
			}
			stroke(path1, "animation-direction: reverse;", markerStart...)
			stroke(path2, "", markerEnd...)
		} else {
			stroke(path, "", append(markerStart, markerEnd...)...)
		}
	}

	if wrapCrossing {
		fmt.Fprint(writer, svg.CloseElement("g"))
	}

	if connection.Label != "" {
		if p := connection.MotionLabel; p != nil {
			fmt.Fprint(writer, svg.OpenElement("g", svg.FloatAttr("opacity", p.Opacity)))
		}
		richLabelLink := connection.Link != "" && connection.Language != ""
		if richLabelLink {
			fmt.Fprint(writer, svg.OpenElement("a", svg.HrefAttributes(connection.Link)...))
		}
		if connection.Language == "latex" {
			gEl, err := latexLabel(connection.Label, labelTL, connection.Color, inlineTheme)
			if err != nil {
				return labelMask, err
			}
			fmt.Fprint(writer, gEl.Render())
		} else if connection.Language == "markdown" {
			layout, err := markdown.layout(connection.Label, connection.FontFamily, connection.FontSize, connection.MarkdownStrike)
			if err != nil {
				return labelMask, err
			}
			fmt.Fprint(writer, markdown.render(
				layout,
				labelTL.X,
				labelTL.Y,
				connection.LabelWidth,
				connection.LabelHeight,
				connection.GetFontColor(),
				connection.Fill,
				connection.Link != "",
				connection.Underline, connection.UnderlineProgress,
			))
		} else if connection.Language != "" {
			if err := writeCode(writer, connection.Language, connection.Label, connection.FontSize, labelTL, nil, inlineTheme); err != nil {
				return labelMask, err
			}
		} else {
			fontClass := textClass(connection.Text)
			if connection.Fill != color.Empty {
				rectEl := d2themes.NewThemableElement("rect", inlineTheme)
				rectEl.Rx = 10
				rectEl.X, rectEl.Y = labelTL.X-4, labelTL.Y-3
				rectEl.Width, rectEl.Height = float64(connection.LabelWidth)+8, float64(connection.LabelHeight)+6
				rectEl.Fill = connection.Fill
				writer.element(rectEl)
			}

			textEl := d2themes.NewThemableElement("text", inlineTheme)
			textEl.X = labelTL.X + float64(connection.LabelWidth)/2
			textEl.Y = labelTL.Y + float64(connection.FontSize)
			textEl.ClassName = fontClass
			textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "middle", connection.FontSize)

			if connection.Link != "" {
				textEl.ClassName += " text-underline text-link"

				fmt.Fprint(writer, svg.OpenElement("a", svg.HrefAttributes(connection.Link)...))
			} else {
				textEl.Fill = connection.GetFontColor()
			}

			if connection.Link != "" {
				// Links have a permanent underline regardless of the authored style.
				writer.label(textEl, connection.Label, float64(connection.LabelHeight))
			} else {
				writer.animatedUnderline(textEl, connection.Label, float64(connection.LabelHeight), connection.UnderlineProgress)
			}
			writer.plainStrike(textEl, connection.Text, inlineTheme, connection.Link != "")

			if connection.Link != "" {
				fmt.Fprintf(writer, "</a>")
			}
		}
		if richLabelLink {
			fmt.Fprint(writer, "</a>")
		}
		if connection.MotionLabel != nil {
			fmt.Fprint(writer, "</g>")
		}
	}

	if connection.SrcLabel != nil && connection.SrcLabel.Label != "" {
		fmt.Fprint(writer, renderArrowheadLabel(connection, connection.SrcLabel.Label, false, inlineTheme))
	}
	if connection.DstLabel != nil && connection.DstLabel.Label != "" {
		fmt.Fprint(writer, renderArrowheadLabel(connection, connection.DstLabel.Label, true, inlineTheme))
	}
	writer.buf.WriteString(`</g>`)
	return
}

// textClass is the font class of a plain text label.
func textClass(t d2target.Text) string {
	class := "text"
	if t.FontFamily == "mono" {
		class = "text-mono"
	}
	if t.Bold {
		class += "-bold"
	} else if t.Italic {
		class += "-italic"
	}
	if t.Underline {
		class += " text-underline"
	}
	return class
}

// latexLabel renders a LaTeX label as a group translated to tl, without the
// XML declaration and DOCTYPE of the LaTeX renderer's SVG document.
func latexLabel(src string, tl *geo.Point, fontColor string, inlineTheme *d2themes.Theme) (*d2themes.ThemableElement, error) {
	render, err := d2latex.Render(src)
	if err != nil {
		return nil, err
	}
	render = strings.ReplaceAll(render, `<?xml version="1.0" encoding="UTF-8"?>`, "")
	render = strings.ReplaceAll(render, `<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.0//EN" "http://www.w3.org/TR/2001/REC-SVG-20010904/DTD/svg10.dtd">`, "")
	if UsesDefaultStylesheet() {
		render = compactMathJaxGlyphs(render)
	}
	gEl := d2themes.NewThemableElement("g", inlineTheme)
	gEl.SetTranslate(tl.X, tl.Y)
	gEl.Color = fontColor
	gEl.SetInnerSVG(svg.TrustedFragment(render))
	return gEl, nil
}

// writeCode writes syntax-highlighted code at tl, once for the light and once
// for the dark code theme. A code shape (frame) also gets a background rect
// and padding of half its font size.
func writeCode(writer *objectWriter, language, code string, fontSize int, tl *geo.Point, frame *d2target.Shape, inlineTheme *d2themes.Theme) error {
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	for _, isLight := range []bool{true, false} {
		theme := codestyle.Light
		if !isLight {
			theme = codestyle.Dark
		}
		style := codestyle.Get(theme)
		if style == nil {
			return errors.New(`code snippet style "github" not found`)
		}
		iterator, err := lexer.Tokenise(nil, code)
		if err != nil {
			return err
		}

		svgStyles := styleToSVG(style)
		class := "light-code"
		if !isLight {
			class = "dark-code"
		}
		var fontSizeStyle string
		if fontSize != d2fonts.FONT_SIZE_M {
			fontSizeStyle = fmt.Sprintf(` style="font-size:%v"`, fontSize)
		}
		fmt.Fprintf(writer, `<g transform="translate(%s %s)" class="%s"%s>`,
			svg.FormatFloat(tl.X), svg.FormatFloat(tl.Y), class, fontSizeStyle,
		)
		closing := "</g>"
		if frame != nil {
			rectEl := d2themes.NewThemableElement("rect", inlineTheme)
			rectEl.Width = float64(frame.Width)
			rectEl.Height = float64(frame.Height)
			rectEl.Stroke = frame.Stroke
			rectEl.ClassName = "shape"
			rectEl.Style = fmt.Sprintf(`fill:%s;stroke-width:%s;`,
				style.Get(chroma.Background).Background.String(),
				svg.FormatFloat(frame.RenderedStrokeWidth()),
			)
			writer.element(rectEl)
			// Padding = 0.5em
			padding := float64(frame.FontSize) / 2.
			fmt.Fprintf(writer, `<g transform="translate(%s %s)">`, svg.FormatFloat(padding), svg.FormatFloat(padding))
			closing = "</g></g>"
		}

		lineHeight := textmeasure.CODE_LINE_HEIGHT
		for index, tokens := range chroma.SplitTokensIntoLines(iterator.Tokens()) {
			fmt.Fprintf(writer, "<text class=\"text-mono\" x=\"0\" y=\"%sem\">", svg.FormatFloat(1+float64(index)*lineHeight))
			for _, token := range tokens {
				text := svgEscaper.Replace(token.String())
				attr := styleAttr(svgStyles, token.Type)
				if attr != "" {
					text = fmt.Sprintf("<tspan %s>%s</tspan>", attr, text)
				}
				fmt.Fprint(writer, text)
			}
			fmt.Fprint(writer, "</text>")
		}
		fmt.Fprint(writer, closing)
	}
	return nil
}

func renderArrowheadLabel(connection d2target.Connection, text string, isDst bool, inlineTheme *d2themes.Theme) string {
	var width, height float64
	if isDst {
		width = float64(connection.DstLabel.LabelWidth)
		height = float64(connection.DstLabel.LabelHeight)
	} else {
		width = float64(connection.SrcLabel.LabelWidth)
		height = float64(connection.SrcLabel.LabelHeight)
	}

	labelTL := connection.GetArrowheadLabelPosition(isDst)

	// svg text is positioned with the center of its baseline
	baselineCenter := geo.Point{
		X: labelTL.X + width/2.,
		Y: labelTL.Y + float64(connection.FontSize),
	}

	textEl := d2themes.NewThemableElement("text", inlineTheme)
	textEl.X = baselineCenter.X
	textEl.Y = baselineCenter.Y
	textEl.Fill = d2target.FG_COLOR
	if isDst {
		if connection.DstLabel.Color != "" {
			textEl.Fill = connection.DstLabel.Color
		}
	} else {
		if connection.SrcLabel.Color != "" {
			textEl.Fill = connection.SrcLabel.Color
		}
	}
	textEl.ClassName = "text-italic"
	textEl.Style = fmt.Sprintf("text-anchor:middle;font-size:%vpx", connection.FontSize)
	textEl.SetInnerSVG(svg.TrustedFragment(RenderText(text, textEl.X, height)))
	return textEl.Render()
}

func drawOval(writer *objectWriter, tl *geo.Point, width, height float64, fill, fillPattern, stroke, style string, inlineTheme *d2themes.Theme) {
	el := d2themes.NewThemableElement("ellipse", inlineTheme)
	el.Rx = width / 2
	el.Ry = height / 2
	el.Cx = tl.X + el.Rx
	el.Cy = tl.Y + el.Ry
	el.Fill, el.Stroke = fill, stroke
	el.FillPattern = fillPattern
	el.ClassName = "shape"
	el.Style = style
	writer.element(el)
}

func drawDoubleOval(writer *objectWriter, tl *geo.Point, width, height float64, fill, fillStroke, stroke, style string, inlineTheme *d2themes.Theme) {
	var innerTL *geo.Point = tl.AddVector(geo.NewVector(d2target.INNER_BORDER_OFFSET, d2target.INNER_BORDER_OFFSET))
	drawOval(writer, tl, width, height, fill, fillStroke, stroke, style, inlineTheme)
	drawOval(writer, innerTL, width-10, height-10, fill, "", stroke, style, inlineTheme)
}

func defineGradient(writer io.Writer, gradient color.Gradient, defined map[string]struct{}) {
	if _, ok := defined[gradient.ID]; ok {
		return
	}
	fmt.Fprint(writer, svg.OpenElement("defs"), color.GradientToSVG(gradient), svg.CloseElement("defs"))
	defined[gradient.ID] = struct{}{}
}

func defineShadowFilter(writer io.Writer) {
	fmt.Fprint(writer, `<defs>
	<filter id="shadow-filter" width="200%" height="200%" x="-50%" y="-50%">
		<feGaussianBlur stdDeviation="1.7 " in="SourceGraphic"></feGaussianBlur>
		<feFlood flood-color="#3d4574" flood-opacity="0.4" result="ShadowFeFlood" in="SourceGraphic"></feFlood>
		<feComposite in="ShadowFeFlood" in2="SourceAlpha" operator="in" result="ShadowFeComposite"></feComposite>
		<feOffset dx="3" dy="5" result="ShadowFeOffset" in="ShadowFeComposite"></feOffset>
		<feBlend in="SourceGraphic" in2="ShadowFeOffset" mode="normal" result="ShadowFeBlend"></feBlend>
	</filter>
</defs>`)
}

// pathPoints formats points relative to the shape at pos, each prefixed with cmd.
func pathPoints(cmd string, pos d2target.Point, points ...d2target.Point) []string {
	out := make([]string, len(points))
	for i, p := range points {
		out[i] = fmt.Sprintf("%s%d,%d", cmd, p.X+pos.X, p.Y+pos.Y)
	}
	return out
}

func render3DRect(diagramHash string, targetShape d2target.Shape, inlineTheme *d2themes.Theme) string {
	w, h, o := targetShape.Width, targetShape.Height, d2target.THREE_DEE_OFFSET
	pos := targetShape.Pos
	// draw border all in one path to prevent overlapping sections
	border := pathPoints("M", pos, d2target.Point{X: 0, Y: 0})
	border = append(border, pathPoints("L", pos,
		d2target.Point{X: o, Y: -o},
		d2target.Point{X: w + o, Y: -o},
		d2target.Point{X: w + o, Y: h - o},
		d2target.Point{X: w, Y: h},
		d2target.Point{X: 0, Y: h},
		d2target.Point{X: 0, Y: 0},
		d2target.Point{X: w, Y: 0},
		d2target.Point{X: w, Y: h},
	)...)
	// move to top right to draw last segment without overlapping
	border = append(border, pathPoints("M", pos, d2target.Point{X: w, Y: 0})...)
	border = append(border, pathPoints("L", pos, d2target.Point{X: w + o, Y: -o})...)

	// the main rectangle
	face := d2themes.NewThemableElement("rect", inlineTheme)
	face.X = float64(pos.X)
	face.Y = float64(pos.Y)
	face.Width = float64(w)
	face.Height = float64(h)

	return render3D(diagramHash, targetShape, inlineTheme, face, border, []d2target.Point{
		{X: 0, Y: 0},
		{X: o, Y: -o},
		{X: w + o, Y: -o},
		{X: w + o, Y: h - o},
		{X: w, Y: h},
		{X: w, Y: 0},
	})
}

func render3DHexagon(diagramHash string, targetShape d2target.Shape, inlineTheme *d2themes.Theme) string {
	scale := func(n int, f float64) int {
		return int(float64(n) * f)
	}
	halfYFactor := 43.6 / 87.3
	w, h, o := targetShape.Width, targetShape.Height, d2target.THREE_DEE_OFFSET
	yo := d2target.THREE_DEE_OFFSET / 2
	pos := targetShape.Pos
	left, right, midY := scale(w, 0.25), scale(w, 0.75), scale(h, halfYFactor)

	// draw border all in one path to prevent overlapping sections
	// start from the top-left
	border := pathPoints("M", pos, d2target.Point{X: left, Y: 0})
	// The following iterates through the sidepoints in clockwise order from top-left, then the main points in clockwise order from bottom-right
	border = append(border, pathPoints("L", pos,
		d2target.Point{X: left + o, Y: -yo},
		d2target.Point{X: right + o, Y: -yo},
		d2target.Point{X: w + o, Y: midY - yo},
		d2target.Point{X: right + o, Y: h - yo},
		d2target.Point{X: right, Y: h},
		d2target.Point{X: left, Y: h},
		d2target.Point{X: 0, Y: midY},
		d2target.Point{X: left, Y: 0},
		d2target.Point{X: right, Y: 0},
		d2target.Point{X: w, Y: midY},
		d2target.Point{X: right, Y: h},
	)...)
	for _, v := range []d2target.Point{{X: right, Y: 0}, {X: w, Y: midY}, {X: right, Y: h}} {
		border = append(border, pathPoints("M", pos, v)...)
		border = append(border, pathPoints("L", pos, d2target.Point{X: v.X + o, Y: v.Y - yo})...)
	}

	// the main hexagon
	face := d2themes.NewThemableElement("polygon", inlineTheme)
	face.X = float64(pos.X)
	face.Y = float64(pos.Y)
	face.Points = strings.Join(pathPoints("", pos,
		d2target.Point{X: left, Y: 0},
		d2target.Point{X: right, Y: 0},
		d2target.Point{X: w, Y: midY},
		d2target.Point{X: right, Y: h},
		d2target.Point{X: left, Y: h},
		d2target.Point{X: 0, Y: midY},
	), " ")

	return render3D(diagramHash, targetShape, inlineTheme, face, border, []d2target.Point{
		{X: left + o, Y: -yo},
		{X: right + o, Y: -yo},
		{X: w + o, Y: midY - yo},
		{X: right + o, Y: h - yo},
		{X: right, Y: h},
		{X: w, Y: midY},
		{X: right, Y: 0},
		{X: left, Y: 0},
	})
}

// render3D draws a 3D shape: its face and its sides in a darker color, both
// without stroke and cut by a mask of the border, which is drawn on top in
// one path. border holds path commands; sides holds polygon points relative
// to the shape.
func render3D(diagramHash string, targetShape d2target.Shape, inlineTheme *d2themes.Theme, face *d2themes.ThemableElement, border []string, sides []d2target.Point) string {
	fill, stroke := d2themes.ShapeTheme(targetShape)
	style := targetShape.CSSStyle()

	borderEl := d2themes.NewThemableElement("path", inlineTheme)
	borderEl.D = strings.Join(border, " ")
	borderEl.Fill = color.None
	borderEl.Stroke = stroke
	borderEl.Style = style

	// create mask from border stroke, to cut away from the shape fills
	maskID := svg.ScopedID("border-mask-"+diagramHash+"-", targetShape.ID, "")
	borderMask := strings.Join([]string{
		svg.OpenElement("defs") + svg.OpenElement("mask",
			svg.Attr("id", maskID.String()),
			svg.Attr("maskUnits", "userSpaceOnUse"),
			svg.IntAttr("x", targetShape.Pos.X),
			svg.IntAttr("y", targetShape.Pos.Y-d2target.THREE_DEE_OFFSET),
			svg.IntAttr("width", targetShape.Width+d2target.THREE_DEE_OFFSET),
			svg.IntAttr("height", targetShape.Height+d2target.THREE_DEE_OFFSET),
		),
		svg.OpenElement("rect",
			svg.IntAttr("x", targetShape.Pos.X),
			svg.IntAttr("y", targetShape.Pos.Y-d2target.THREE_DEE_OFFSET),
			svg.IntAttr("width", targetShape.Width+d2target.THREE_DEE_OFFSET),
			svg.IntAttr("height", targetShape.Height+d2target.THREE_DEE_OFFSET),
			svg.Attr("fill", "white"),
		) + svg.CloseElement("rect"),
		svg.EmptyElement("path",
			svg.Attr("d", strings.Join(border, "")),
			svg.Attr("style", style+";stroke:#000;fill:none;opacity:1;"),
		) + svg.CloseElement("mask") + svg.CloseElement("defs"),
	}, "\n")

	face.SetMaskID(maskID)
	face.Fill = fill
	face.FillPattern = targetShape.FillPattern
	face.Stroke = color.None
	face.Style = style

	// TODO make darker color part of the theme? or just keep this bypass
	darkerColor, err := color.Darken(targetShape.Fill)
	if err != nil {
		darkerColor = targetShape.Fill
	}
	sideEl := d2themes.NewThemableElement("polygon", inlineTheme)
	sideEl.Fill = darkerColor
	sideEl.Points = strings.Join(pathPoints("", targetShape.Pos, sides...), " ")
	sideEl.SetMaskID(maskID)
	sideEl.Style = style

	return borderMask + face.Render() + sideEl.Render() + borderEl.Render()
}

func drawShape(writer *objectWriter, appendixWriter io.Writer, diagramHash, resourceHash string, targetShape d2target.Shape, sketch bool, inlineTheme *d2themes.Theme, markdown *markdownRenderer) (labelMask string, err error) {
	closingTag := "</g>"
	if c := targetShape.MotionClip; c != nil {
		openMotionViewport(writer, c)
		defer writer.buf.WriteString("</svg>")
	}
	if targetShape.Link != "" {
		fmt.Fprint(writer, svg.OpenElement("a", svg.HrefAttributes(targetShape.Link)...))
		closingTag += "</a>"
	}

	// this clipPath must be defined outside `g` element
	if targetShape.BorderRadius != 0 && (targetShape.Type == d2target.ShapeClass || targetShape.Type == d2target.ShapeSQLTable) {
		fmt.Fprint(writer, clipPathForBorderRadius(resourceHash, targetShape))
	}
	var iconClipPathID svg.ID
	if targetShape.IconBorderRadius != 0 && (targetShape.Type == d2target.ShapeImage) {
		// Set the icon's border-radius to half of it's smaller dimension in case it exceeds that
		// https://www.w3.org/Style/CSS/Tracker/issues/29?changelog
		targetShape.IconBorderRadius = min(targetShape.IconBorderRadius, min(targetShape.Width, targetShape.Height)/2)
		iconClipPathID = svg.LiteralID(fmt.Sprintf("%v-%v-icon", resourceHash, svg.SVGID(targetShape.ID)))
		fmt.Fprint(writer, applyIconBorderRadius(iconClipPathID, targetShape))
	}
	classes := []string{base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(targetShape.ID)))}
	if targetShape.Animated {
		classes = append(classes, "animated-shape")
	}
	classes = append(classes, targetShape.Classes...)
	opacityStyle := ""
	if targetShape.Opacity != 1.0 {
		opacityStyle = fmt.Sprintf(" style='opacity:%s'", svg.FormatFloat(targetShape.Opacity))
	}
	writer.openGroup(classes, opacityStyle)
	tl := geo.NewPoint(float64(targetShape.Pos.X), float64(targetShape.Pos.Y))
	width := float64(targetShape.Width)
	height := float64(targetShape.Height)
	fill, stroke := d2themes.ShapeTheme(targetShape)
	style := targetShape.CSSStyle()
	s := targetShape.Geometry()

	writer.buf.WriteString(`<g class="shape`)
	if targetShape.Blend {
		writer.classes("blend")
		writer.buf.WriteString(" blend")
	}
	writer.buf.WriteByte('"')
	if targetShape.Shadow {
		switch targetShape.Type {
		case d2target.ShapeText, d2target.ShapeCode, d2target.ShapeClass, d2target.ShapeSQLTable:
		default:
			writer.buf.WriteString(` filter="url(#shadow-filter)"`)
		}
	}
	writer.buf.WriteByte('>')

	var multipleTL *geo.Point
	if targetShape.Multiple {
		multipleTL = tl.AddVector(multipleOffset)
	}

	borderLabelMask := hasBorderLabel(targetShape)
	switch targetShape.Type {
	case d2target.ShapeClass, d2target.ShapeSQLTable:
		isClass := targetShape.Type == d2target.ShapeClass
		if sketch {
			sketchTable := d2sketch.Table
			if isClass {
				sketchTable = d2sketch.Class
			}
			if err := writer.sketch(sketchTable(targetShape)); err != nil {
				return "", err
			}
		} else if isClass {
			drawClass(writer, resourceHash, targetShape, inlineTheme)
		} else {
			drawTable(writer, resourceHash, targetShape, inlineTheme)
		}
		err := addAppendixItems(appendixWriter, diagramHash, targetShape, s, markdown)
		if err != nil {
			return "", err
		}
		writer.buf.WriteString(`</g>`)
		writer.buf.WriteString(closingTag)
		return labelMask, nil
	case d2target.ShapeOval:
		draw, sketchOval := drawOval, d2sketch.Oval
		if targetShape.DoubleBorder {
			draw, sketchOval = drawDoubleOval, d2sketch.DoubleOval
		}
		if targetShape.Multiple {
			draw(writer, multipleTL, width, height, fill, "", stroke, style, inlineTheme)
		}
		if sketch {
			if err := writer.sketch(sketchOval(targetShape, resourceHash)); err != nil {
				return "", err
			}
		} else {
			draw(writer, tl, width, height, fill, targetShape.FillPattern, stroke, style, inlineTheme)
		}

	case d2target.ShapeImage:
		el := d2themes.NewThemableElement("image", inlineTheme)
		el.X = float64(targetShape.Pos.X)
		el.Y = float64(targetShape.Pos.Y)
		el.Width = float64(targetShape.Width)
		el.Height = float64(targetShape.Height)
		el.Href = targetShape.Icon.String()
		el.Fill = fill
		el.Stroke = stroke
		el.Style = style
		if targetShape.IconBorderRadius != 0 {
			el.SetClipPathID(iconClipPathID)
		}
		fmt.Fprint(writer, el.Render())

	// TODO should standardize "" to rectangle
	case d2target.ShapeRectangle, d2target.ShapeSequenceDiagram, d2target.ShapeHierarchy, "":
		if !sketch && targetShape.DrawingOutline() {
			body := d2themes.NewThemableElement("rect", inlineTheme)
			body.X, body.Y, body.Width, body.Height = tl.X, tl.Y, width, height
			body.Fill, body.Stroke = fill, "none"
			writer.element(body)
			points := targetShape.DrawnOutline()
			if len(points) > 0 {
				var d strings.Builder
				for i, p := range points {
					if i == 0 {
						d.WriteString("M ")
					} else {
						d.WriteString(" L ")
					}
					d.WriteString(svg.FormatFloat(p.X))
					d.WriteByte(' ')
					d.WriteString(svg.FormatFloat(p.Y))
				}
				line := d2themes.NewThemableElement("path", inlineTheme)
				line.D, line.Fill, line.Stroke, line.Style = d.String(), "none", stroke, style
				writer.element(line)
			}
			break
		}
		if targetShape.ThreeDee {
			fmt.Fprint(writer, render3DRect(resourceHash, targetShape, inlineTheme))
			break
		}
		borderRadius := math.MaxFloat64
		if targetShape.BorderRadius != 0 {
			borderRadius = float64(targetShape.BorderRadius)
		}
		// rect is the shape's rectangle moved by (dx, dy) and shrunk by inset on every side.
		rect := func(dx, dy, inset int, fill, fillPattern string) *d2themes.ThemableElement {
			el := d2themes.NewThemableElement("rect", inlineTheme)
			el.X = float64(targetShape.Pos.X + dx + inset)
			el.Y = float64(targetShape.Pos.Y + dy + inset)
			el.Width = float64(targetShape.Width - 2*inset)
			el.Height = float64(targetShape.Height - 2*inset)
			el.Fill, el.FillPattern, el.Stroke, el.Style, el.Rx = fill, fillPattern, stroke, style, borderRadius
			return el
		}
		double := targetShape.DoubleBorder
		if targetShape.Multiple {
			// Only a double-bordered copy repeats the fill pattern.
			pattern := ""
			if double {
				pattern = targetShape.FillPattern
			}
			writer.element(rect(10, -10, 0, fill, pattern))
			if double {
				writer.element(rect(10, -10, d2target.INNER_BORDER_OFFSET, fill, ""))
			}
		}
		if sketch {
			sketchRect := d2sketch.Rect
			if double {
				sketchRect = d2sketch.DoubleRect
			}
			if err := writer.sketch(sketchRect(targetShape, resourceHash)); err != nil {
				return "", err
			}
			break
		}
		el := rect(0, 0, 0, fill, targetShape.FillPattern)
		if !double && borderLabelMask {
			el.SetMaskID(svg.LiteralID(resourceHash))
		}
		writer.element(el)
		if double {
			writer.element(rect(0, 0, d2target.INNER_BORDER_OFFSET, "transparent", ""))
		}
	case d2target.ShapeText, d2target.ShapeCode:
	case d2target.ShapeHexagon:
		if targetShape.ThreeDee {
			fmt.Fprint(writer, render3DHexagon(resourceHash, targetShape, inlineTheme))
			break
		}
		// Hexagons do not mask border labels.
		borderLabelMask = false
		fallthrough
	default:
		el := d2themes.NewThemableElement("path", inlineTheme)
		el.Fill = fill
		el.Stroke = stroke
		el.Style = style
		if targetShape.Multiple {
			writer.paths(el, shape.NewShape(s.GetType(), geo.NewBox(multipleTL, width, height)).GetSVGPathData())
		}
		if sketch {
			if err := writer.sketch(d2sketch.Paths(targetShape, resourceHash, s.GetSVGPathData())); err != nil {
				return "", err
			}
			break
		}
		el.FillPattern = targetShape.FillPattern
		if borderLabelMask {
			el.SetMaskID(svg.LiteralID(resourceHash))
		}
		writer.paths(el, s.GetSVGPathData())
	}

	// // to examine shape's innerBox
	// innerBox := s.GetInnerBox()
	// el := d2themes.NewThemableElement("rect", inlineTheme)
	// el.X = float64(innerBox.TopLeft.X)
	// el.Y = float64(innerBox.TopLeft.Y)
	// el.Width = float64(innerBox.Width)
	// el.Height = float64(innerBox.Height)
	// el.Style = "fill:rgba(255,0,0,0.5);"
	// fmt.Fprint(writer, el.Render())

	// Closes the class=shape
	writer.buf.WriteString(`</g>`)

	if targetShape.Icon != nil && targetShape.Type != d2target.ShapeImage && targetShape.Opacity != 0 {
		icon := targetShape.IconBounds(s)
		tl := icon.TopLeft
		iconSize := int(icon.Width)

		attributes := []svg.Attribute{
			svg.Attr("href", targetShape.Icon.String()),
			svg.FloatAttr("x", tl.X),
			svg.FloatAttr("y", tl.Y),
			svg.IntAttr("width", iconSize),
			svg.IntAttr("height", iconSize),
		}
		if targetShape.IconBorderRadius != 0 {
			attributes = append(attributes, svg.Attr("clip-path", fmt.Sprintf("inset(0 round %dpx)", targetShape.IconBorderRadius)))
		}
		fmt.Fprint(writer, svg.EmptyElement("image", attributes...))
	}

	if targetShape.Label != "" && targetShape.Opacity != 0 {
		labelPosition := label.FromString(targetShape.LabelPosition)
		// The foreignObject Markdown renderer historically defaulted an unset
		// label position to the middle of the shape. Keep that behavior when the
		// native label viewport is smaller than an explicitly sized shape.
		if targetShape.Language == "markdown" && labelPosition == label.Unset {
			labelPosition = label.InsideMiddleCenter
		}
		var box *geo.Box
		if labelPosition.IsOutside() || labelPosition.IsBorder() {
			box = targetShape.OuterLabelBox(s.GetBox())
		} else {
			box = s.GetInnerBox()
		}
		labelTL := targetShape.LabelTopLeft(labelPosition, box)
		if labelPosition.IsBorder() {
			if sketch {
				labelMask = makeBorderLabelMask(labelPosition, labelTL, targetShape.LabelWidth, targetShape.LabelHeight, box, targetShape.RenderedStrokeWidth(), 1.0, tl)
			} else {
				labelMask = makeBorderLabelMask(labelPosition, labelTL, targetShape.LabelWidth, targetShape.LabelHeight, box, targetShape.RenderedStrokeWidth(), 1.0, nil)
			}
		}

		fontClass := textClass(targetShape.Text)

		if targetShape.Language == "latex" {
			// LaTeX labels default to the middle of the shape.
			labelPosition := label.FromString(targetShape.LabelPosition)
			if labelPosition == label.Unset {
				labelPosition = label.InsideMiddleCenter
			}
			var box *geo.Box
			if labelPosition.IsOutside() {
				box = s.GetBox()
			} else {
				box = s.GetInnerBox()
			}
			latexTL := targetShape.LabelTopLeft(labelPosition, box)
			gEl, err := latexLabel(targetShape.Label, latexTL, targetShape.GetFontColor(), inlineTheme)
			if err != nil {
				return labelMask, err
			}
			if targetShape.MotionScale != 0 {
				gEl.Transform += fmt.Sprintf(" scale(%s)", svg.FormatFloat(targetShape.MotionScale))
			}
			fmt.Fprint(writer, gEl.Render())
		} else if targetShape.Language == "markdown" {
			layout, err := markdown.layout(targetShape.Label, targetShape.FontFamily, targetShape.FontSize, targetShape.MarkdownStrike)
			if err != nil {
				return labelMask, err
			}
			fmt.Fprint(writer, markdown.render(
				layout,
				labelTL.X,
				labelTL.Y,
				targetShape.LabelWidth,
				targetShape.LabelHeight,
				targetShape.GetFontColor(),
				d2sketch.MarkdownBackground(targetShape, sketch),
				targetShape.Link != "",
				targetShape.Underline, targetShape.UnderlineProgress,
			))
		} else if targetShape.Language != "" {
			if err := writeCode(writer, targetShape.Language, targetShape.Label, targetShape.FontSize, box.TopLeft, &targetShape, inlineTheme); err != nil {
				return labelMask, err
			}
		} else {
			if targetShape.LabelFill != "" {
				rectEl := d2themes.NewThemableElement("rect", inlineTheme)
				rectEl.X = labelTL.X
				rectEl.Y = labelTL.Y
				rectEl.Width = float64(targetShape.LabelWidth)
				rectEl.Height = float64(targetShape.LabelHeight)
				rectEl.Fill = targetShape.LabelFill
				writer.element(rectEl)
			}
			textEl := d2themes.NewThemableElement("text", inlineTheme)
			textEl.X = labelTL.X + float64(targetShape.LabelWidth)/2
			// text is vertically positioned at its baseline which is at labelTL+FontSize
			textEl.Y = labelTL.Y + float64(targetShape.FontSize)
			textEl.Fill = targetShape.GetFontColor()
			textEl.ClassName = fontClass
			textEl.Style = fmt.Sprintf("text-anchor:%s;font-size:%vpx", "middle", targetShape.FontSize)
			writer.animatedUnderline(textEl, targetShape.Label, float64(targetShape.LabelHeight), targetShape.UnderlineProgress)
			writer.plainStrike(textEl, targetShape.Text, inlineTheme, false)
		}
	}
	if targetShape.Tooltip != "" && targetShape.TooltipPosition == "" {
		fmt.Fprintf(writer, `<title>%s</title>`,
			svg.EscapeText(targetShape.Tooltip),
		)
	}
	err = addAppendixItems(appendixWriter, diagramHash, targetShape, s, markdown)
	if err != nil {
		return "", err
	}

	writer.buf.WriteString(closingTag)
	return labelMask, nil
}

func applyIconBorderRadius(clipPathID svg.ID, shape d2target.Shape) string {
	box := geo.NewBox(
		geo.NewPoint(float64(shape.Pos.X), float64(shape.Pos.Y)),
		float64(shape.Width),
		float64(shape.Height),
	)
	topX, topY := box.TopLeft.X+box.Width, box.TopLeft.Y

	out := svg.OpenElement("clipPath", svg.Attr("id", clipPathID.String()))
	out += fmt.Sprintf(`<path d="M %s %s L %s %s S %s %s %s %s `, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.IconBorderRadius)), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.IconBorderRadius)), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y), svg.FormatFloat(box.TopLeft.X+float64(shape.IconBorderRadius)), svg.FormatFloat(box.TopLeft.Y))
	out += fmt.Sprintf(`L %s %s L %s %s `, svg.FormatFloat(box.TopLeft.X+box.Width-float64(shape.IconBorderRadius)), svg.FormatFloat(box.TopLeft.Y), svg.FormatFloat(topX-float64(shape.IconBorderRadius)), svg.FormatFloat(topY))

	out += fmt.Sprintf(`S %s %s %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY), svg.FormatFloat(topX), svg.FormatFloat(topY+float64(shape.IconBorderRadius)))
	out += fmt.Sprintf(`L %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY+box.Height-float64(shape.IconBorderRadius)))
	out += fmt.Sprintf(`S %s  %s %s %s `, svg.FormatFloat(topX), svg.FormatFloat(topY+box.Height), svg.FormatFloat(topX-float64(shape.IconBorderRadius)), svg.FormatFloat(topY+box.Height))
	out += fmt.Sprintf(`L %s %s `, svg.FormatFloat(box.TopLeft.X+float64(shape.IconBorderRadius)), svg.FormatFloat(box.TopLeft.Y+box.Height))
	out += fmt.Sprintf(`S %s %s %s %s`, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+box.Height), svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+box.Height-float64(shape.IconBorderRadius)))
	out += fmt.Sprintf(`L %s %s`, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y+float64(shape.IconBorderRadius)))
	out += fmt.Sprintf(`Z %s %s" `, svg.FormatFloat(box.TopLeft.X), svg.FormatFloat(box.TopLeft.Y))
	return out + `fill="none" /> </clipPath>`
}

func addAppendixItems(writer io.Writer, diagramHash string, targetShape d2target.Shape, s shape.Shape, markdown *markdownRenderer) error {
	var p1, p2 *geo.Point
	if targetShape.Tooltip != "" || targetShape.Link != "" {
		if c := targetShape.MotionClip; c != nil {
			openMotionViewport(writer, c)
			defer fmt.Fprint(writer, "</svg>")
		}
		if targetShape.Opacity != 1 {
			fmt.Fprint(writer, svg.OpenElement("g", svg.Attr("opacity", svg.FormatFloat(targetShape.Opacity))))
			defer fmt.Fprint(writer, "</g>")
		}
		bothIcons := targetShape.Tooltip != "" && targetShape.Link != ""
		corner := geo.NewPoint(float64(targetShape.Pos.X+targetShape.Width), float64(targetShape.Pos.Y))
		center := geo.NewPoint(
			float64(targetShape.Pos.X)+float64(targetShape.Width)/2.,
			float64(targetShape.Pos.Y)+float64(targetShape.Height)/2.,
		)
		offset := geo.Vector{-2 * appendixIconRadius, 0}
		var leftOnShape bool
		switch s.GetType() {
		case shape.STEP_TYPE, shape.HEXAGON_TYPE, shape.QUEUE_TYPE, shape.PAGE_TYPE:
			// trace straight left for these
			center.Y = float64(targetShape.Pos.Y)
		case shape.PACKAGE_TYPE:
			// trace straight down
			center.X = float64(targetShape.Pos.X + targetShape.Width)
		case shape.CIRCLE_TYPE, shape.OVAL_TYPE, shape.DIAMOND_TYPE,
			shape.PERSON_TYPE, shape.CLOUD_TYPE, shape.CYLINDER_TYPE:
			if bothIcons {
				leftOnShape = true
				corner = corner.AddVector(offset)
			}
		}
		v1 := center.VectorTo(corner)
		p1 = shape.TraceToShapeBorder(s, corner, corner.AddVector(v1))
		if bothIcons {
			if leftOnShape {
				// these shapes should have p1 on shape border
				p2 = p1.AddVector(offset.Reverse())
				p1, p2 = p2, p1
			} else {
				p2 = p1.AddVector(offset)
			}
		}
	}

	if targetShape.Tooltip != "" {
		if targetShape.TooltipPosition != "" {
			tt, err := renderPositionedTooltip(targetShape, targetShape.TooltipPosition, markdown)
			if err != nil {
				return err
			}
			fmt.Fprint(writer, tt)
		} else {
			x := int(math.Ceil(p1.X))
			y := int(math.Ceil(p1.Y))

			fmt.Fprintf(writer, `<g transform="translate(%d %d)" class="appendix-icon"><title>%s</title>%s</g>`,
				x-appendixIconRadius,
				y-appendixIconRadius,
				svg.EscapeText(targetShape.Tooltip),
				fmt.Sprintf(TooltipIcon, diagramHash, svg.SVGID(targetShape.ID)),
			)
		}
	}
	if targetShape.Link != "" {
		if p2 == nil {
			p2 = p1
		}
		x := int(math.Ceil(p2.X))
		y := int(math.Ceil(p2.Y))
		fmt.Fprintf(writer, `<g transform="translate(%d %d)" class="appendix-icon">%s</g>`,
			x-appendixIconRadius,
			y-appendixIconRadius,
			fmt.Sprintf(LinkIcon, diagramHash, svg.SVGID(targetShape.ID)),
		)
	}
	return nil
}

func calculateTooltipPosition(targetShape d2target.Shape, tooltipPosition string, tooltipWidth, tooltipHeight int) (x, y float64, tailDirection string, tailX, tailY float64) {
	shapeX := float64(targetShape.Pos.X)
	shapeY := float64(targetShape.Pos.Y)
	shapeWidth := float64(targetShape.Width)
	shapeHeight := float64(targetShape.Height)

	x, y = d2target.CalculateTooltipPosition(shapeX, shapeY, shapeWidth, shapeHeight, tooltipWidth, tooltipHeight, tooltipPosition)

	switch tooltipPosition {
	case "top-left":
		tailDirection = "bottom"
		tailX = 20
		tailY = float64(tooltipHeight)
	case "top-center":
		tailDirection = "bottom"
		tailX = float64(tooltipWidth) / 2
		tailY = float64(tooltipHeight)
	case "top-right":
		tailDirection = "bottom"
		tailX = float64(tooltipWidth) - 20
		tailY = float64(tooltipHeight)
	case "center-left":
		tailDirection = "right"
		tailX = float64(tooltipWidth)
		tailY = float64(tooltipHeight) / 2
	case "center-right":
		tailDirection = "left"
		tailX = 0
		tailY = float64(tooltipHeight) / 2
	case "bottom-left":
		tailDirection = "top"
		tailX = 20
		tailY = 0
	case "bottom-center":
		tailDirection = "top"
		tailX = float64(tooltipWidth) / 2
		tailY = 0
	case "bottom-right":
		tailDirection = "top"
		tailX = float64(tooltipWidth) - 20
		tailY = 0
	default:
		tailDirection = "bottom"
		tailX = float64(tooltipWidth) / 2
		tailY = float64(tooltipHeight)
	}

	return x, y, tailDirection, tailX, tailY
}

func renderTooltipTail(tailDirection string, tailX, tailY float64, inlineTheme *d2themes.Theme) string {
	tailSize := 8.0
	var path string

	switch tailDirection {
	case "top":
		path = fmt.Sprintf(`M %s %s L %s %s L %s %s Z`,
			svg.FormatFloat(tailX-tailSize/2), svg.FormatFloat(tailY),
			svg.FormatFloat(tailX+tailSize/2), svg.FormatFloat(tailY),
			svg.FormatFloat(tailX), svg.FormatFloat(tailY-tailSize))
	case "bottom":
		path = fmt.Sprintf(`M %s %s L %s %s L %s %s Z`,
			svg.FormatFloat(tailX-tailSize/2), svg.FormatFloat(tailY),
			svg.FormatFloat(tailX+tailSize/2), svg.FormatFloat(tailY),
			svg.FormatFloat(tailX), svg.FormatFloat(tailY+tailSize))
	case "left":
		path = fmt.Sprintf(`M %s %s L %s %s L %s %s Z`,
			svg.FormatFloat(tailX), svg.FormatFloat(tailY-tailSize/2),
			svg.FormatFloat(tailX), svg.FormatFloat(tailY+tailSize/2),
			svg.FormatFloat(tailX-tailSize), svg.FormatFloat(tailY))
	case "right":
		path = fmt.Sprintf(`M %s %s L %s %s L %s %s Z`,
			svg.FormatFloat(tailX), svg.FormatFloat(tailY-tailSize/2),
			svg.FormatFloat(tailX), svg.FormatFloat(tailY+tailSize/2),
			svg.FormatFloat(tailX+tailSize), svg.FormatFloat(tailY))
	default:
		return ""
	}
	tail := d2themes.NewThemableElement("path", inlineTheme)
	tail.D = path
	tail.Fill = color.N7
	tail.Stroke = color.N5
	tail.AddAttributes(svg.IntAttr("stroke-width", 1))
	return tail.Render()
}

func renderPositionedTooltip(targetShape d2target.Shape, tooltipPosition string, markdown *markdownRenderer) (string, error) {
	if targetShape.Tooltip == "" || tooltipPosition == "" {
		return "", nil
	}

	layout, err := markdown.layout(targetShape.Tooltip, targetShape.FontFamily, d2fonts.FONT_SIZE_M, nil)
	if err != nil {
		return "", err
	}
	tooltipWidth := layout.Width + 20
	tooltipHeight := layout.Height + 20
	x, y, tailDirection, tailX, tailY := calculateTooltipPosition(targetShape, tooltipPosition, tooltipWidth, tooltipHeight)

	tooltipContent := markdown.render(
		layout,
		x+10,
		y+10,
		layout.Width,
		layout.Height,
		color.N1,
		"",
		false,
		false, nil,
	)

	tooltipBox := d2themes.NewThemableElement("rect", markdown.inlineTheme)
	tooltipBox.X = x
	tooltipBox.Y = y
	tooltipBox.Width = float64(tooltipWidth)
	tooltipBox.Height = float64(tooltipHeight)
	tooltipBox.Rx = 4
	tooltipBox.Ry = 4
	tooltipBox.Fill = color.N7
	tooltipBox.Stroke = color.N5
	tooltipBox.AddAttributes(svg.IntAttr("stroke-width", 1))

	tail := renderTooltipTail(tailDirection, tailX+x, tailY+y, markdown.inlineTheme)

	return fmt.Sprintf(`<g class="positioned-tooltip">%s%s%s</g>`, tooltipBox.Render(), tail, tooltipContent), nil
}

func RenderText(text string, x, height float64) string {
	return renderText(text, x, height, nil)
}

func renderText(text string, x, height float64, visit func(string)) string {
	if !strings.Contains(text, "\n") {
		if visit != nil {
			visit(text)
		}
		return svg.EscapeText(text)
	}
	rendered := []string{}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		dy := height / float64(len(lines))
		if i == 0 {
			dy = 0
		}
		if line == "" {
			// if there are multiple newlines in a row we still need text for the tspan to render
			line = " "
		}
		if visit != nil {
			visit(line)
		}
		escaped := svg.EscapeText(line)
		rendered = append(rendered, fmt.Sprintf(`<tspan x="%s" dy="%s">%s</tspan>`, svg.FormatFloat(x), svg.FormatFloat(dy), escaped))
	}
	return strings.Join(rendered, "")
}

func EmbedFonts(buf *bytes.Buffer, diagramHash, source string, fontFamily *d2fonts.FontFamily, monoFontFamily *d2fonts.FontFamily, corpus string) {
	embedFonts(buf, diagramHash, source, fontFamily, monoFontFamily, corpus, nil)
}

func embedFonts(buf *bytes.Buffer, diagramHash, source string, fontFamily *d2fonts.FontFamily, monoFontFamily *d2fonts.FontFamily, corpus string, corpora fontCorpora) {
	start := buf.Len()
	buf.WriteString(legacyStyleOpen)
	contentStart := buf.Len()
	writeFontCSS(buf, diagramHash, source, fontFamily, monoFontFamily, corpus, corpora, false)
	if buf.Len() == contentStart {
		buf.Truncate(start)
		return
	}
	if css := buf.Bytes()[contentStart:]; bytes.Contains(css, []byte("]]>")) {
		text := string(css)
		buf.Truncate(contentStart)
		writeCDATA(buf, text)
	}
	buf.WriteString(legacyStyleClose)
}

func writeFontCSS(buf *bytes.Buffer, diagramHash, source string, fontFamily *d2fonts.FontFamily, monoFontFamily *d2fonts.FontFamily, corpus string, corpora fontCorpora, compact bool) {
	if fontFamily == nil || *fontFamily == "" {
		family := d2fonts.SourceSansPro
		fontFamily = &family
	}
	if monoFontFamily == nil || *monoFontFamily == "" {
		family := d2fonts.SourceCodePro
		monoFontFamily = &family
	}

	// Markdown generates text that may not exist literally in the D2 source,
	// such as list markers and decoded entities. Include those rendered runs in
	// every font subset, including multi-board animations where render-local
	// Markdown layout state is not available here.
	// The pattern can only match where the class name occurs, which most
	// diagrams never contain; skip scanning the whole document for those.
	if strings.Contains(source, "md-text") {
		for _, match := range nativeMarkdownTextPattern.FindAllStringSubmatch(source, -1) {
			corpus += htmlentity.Unescape(match[1])
		}
	}
	corpora = restrictFontCorpora(corpora, corpus)
	appendFace := func(class, face string, font *d2fonts.FontFamily, style d2fonts.FontStyle, triggers ...string) {
		appendFontOnTrigger(buf, source, triggers, corpora, class, corpus, func(corpus string) string {
			subsetFont := font.Font(0, style)
			family := diagramHash + "-font-" + face
			if compact {
				return svgstyle.FontFace(diagramHash, class, family, subsetFont.GetSVGEncodedSubset(corpus))
			}
			return fmt.Sprintf(`
.%s .%s {
	font-family: "%s";
}
@font-face {
	font-family: %s;
	src: url("%s");
}`, diagramHash, class, family, family, subsetFont.GetEncodedSubset(corpus))
		})
	}
	appendRule := func(trigger, css string) {
		if !strings.Contains(source, trigger) {
			return
		}
		if compact {
			css = compactCSS(css)
		}
		buf.WriteString(css)
	}
	appendFace("text", "regular", fontFamily, d2fonts.FONT_STYLE_REGULAR, `class="text"`, `class="text `, `class="md"`, `class="md `)

	appendFace("text-semibold", "semibold", fontFamily, d2fonts.FONT_STYLE_SEMIBOLD, `text-semibold`)

	appendRule("text-underline", `
.text-underline {
	text-decoration: underline;
}`)

	appendRule("text-link", `
.text-link {
	fill: blue;
}

.text-link:visited {
	fill: purple;
}`)

	appendRule("animated-connection", fmt.Sprintf(`
@keyframes dashdraw {
	from {
		stroke-dashoffset: 0;
	}
}
@media (prefers-reduced-motion: reduce) {
	.%s .animated-connection {
		animation: none !important;
	}
}
`, diagramHash))

	appendRule("animated-shape", `
@keyframes shapeappear {
    0%, 100% { transform: translateY(0); filter: drop-shadow(0px 0px 0px rgba(0,0,0,0)); }
    50% { transform: translateY(-4px); filter: drop-shadow(0px 12.6px 25.2px rgba(50,50,93,0.25)) drop-shadow(0px 7.56px 15.12px rgba(0,0,0,0.1)); }
}
.animated-shape {
	animation: shapeappear 1s linear infinite;
}
`)

	appendRule("appendix-icon", `
.appendix-icon {
	filter: drop-shadow(0px 0px 32px rgba(31, 36, 58, 0.1));
}`)

	appendFace("text-bold", "bold", fontFamily, d2fonts.FONT_STYLE_BOLD, `text-bold`, `<b>`, `<strong>`)

	appendFace("text-italic", "italic", fontFamily, d2fonts.FONT_STYLE_ITALIC, `text-italic`, `<em>`, `<dfn>`)

	appendFace("text-mono", "mono", monoFontFamily, d2fonts.FONT_STYLE_REGULAR, `text-mono`, `<pre>`, `<code>`, `<kbd>`, `<samp>`)

	appendFace("text-mono-semibold", "mono-semibold", monoFontFamily, d2fonts.FONT_STYLE_SEMIBOLD, `text-mono-semibold`)

	appendFace("text-mono-bold", "mono-bold", monoFontFamily, d2fonts.FONT_STYLE_BOLD, `text-mono-bold`)

	appendFace("text-mono-italic", "mono-italic", monoFontFamily, d2fonts.FONT_STYLE_ITALIC, `text-mono-italic`)

	appendRule("sketch-overlay-bright", fmt.Sprintf(`
.sketch-overlay-bright {
	fill: url(#streaks-bright-%s);
	mix-blend-mode: darken;
}`, diagramHash))

	appendRule("sketch-overlay-normal", fmt.Sprintf(`
.sketch-overlay-normal {
	fill: url(#streaks-normal-%s);
	mix-blend-mode: color-burn;
}`, diagramHash))

	appendRule("sketch-overlay-dark", fmt.Sprintf(`
.sketch-overlay-dark {
	fill: url(#streaks-dark-%s);
	mix-blend-mode: overlay;
}`, diagramHash))

	appendRule("sketch-overlay-darker", fmt.Sprintf(`
.sketch-overlay-darker {
	fill: url(#streaks-darker-%s);
	mix-blend-mode: lighten;
}`, diagramHash))

}

var nativeMarkdownTextPattern = regexp.MustCompile(`<text\b[^>]*\bclass="[^"]*\bmd-text\b[^"]*"[^>]*>([^<]*)</text>`)

// appendOnTriggerLazy avoids constructing a font subset unless its CSS is used.
func appendOnTriggerLazy(buf *bytes.Buffer, source string, triggers []string, newContent func() string) {
	for _, trigger := range triggers {
		if strings.Contains(source, trigger) {
			fmt.Fprint(buf, newContent())
			break
		}
	}
}

var DEFAULT_DARK_THEME *int64 = nil // no theme selected

func Render(diagram *d2target.Diagram, opts *RenderOpts) ([]byte, error) {
	return RenderContext(context.Background(), diagram, opts)
}

// RenderContext renders a single board and observes cancellation between
// render stages and individual objects.
func RenderContext(ctx context.Context, diagram *d2target.Diagram, opts *RenderOpts) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts != nil {
		if err := d2target.ValidateAnimateProgressColor(opts.AnimateProgressColor); err != nil {
			return nil, fmt.Errorf("animateProgressColor: %w", err)
		}
	}
	if diagram == nil {
		return nil, fmt.Errorf("render target is nil")
	}
	if err := validateRenderLinks(diagram); err != nil {
		return nil, err
	}
	if err := d2target.ValidateRenderTarget(diagram); err != nil {
		return nil, err
	}
	return renderValidated(ctx, diagram, opts)
}

func validateRenderLinks(diagram *d2target.Diagram) error {
	for _, targetShape := range diagram.Shapes {
		if textmeasure.IsDangerousLink(targetShape.Link) {
			return fmt.Errorf("shape %q uses an unsafe link URL scheme", targetShape.ID)
		}
	}
	for _, connection := range diagram.Connections {
		if textmeasure.IsDangerousLink(connection.Link) {
			return fmt.Errorf("connection %q uses an unsafe link URL scheme", connection.ID)
		}
	}
	return nil
}

// renderValidated renders one board after its complete target tree has been
// validated. Keeping this internal lets RenderMultiboard avoid repeatedly
// walking every descendant subtree.
func renderValidated(ctx context.Context, diagram *d2target.Diagram, opts *RenderOpts) ([]byte, error) {
	if err := validateRenderPaints(diagram, opts); err != nil {
		return nil, err
	}

	sketch := false
	pad := DEFAULT_PADDING
	tl, br := diagram.BoundingBox()
	themeID := d2themescatalog.NeutralDefault.ID
	darkThemeID := DEFAULT_DARK_THEME
	var scale *float64
	if opts != nil {
		if opts.Pad != nil {
			var err error
			pad, err = validatePadding(tl, br, *opts.Pad)
			if err != nil {
				return nil, err
			}
		}
		if opts.Sketch != nil && *opts.Sketch {
			sketch = true
		}
		if opts.ThemeID != nil {
			themeID = *opts.ThemeID
		}
		darkThemeID = opts.DarkThemeID
		scale = opts.Scale
	} else {
		opts = &RenderOpts{}
	}

	buf := &bytes.Buffer{}
	writer := &objectWriter{buf: buf, compactMarkerIDs: opts.MasterID == "" && UsesDefaultStylesheet()}
	if opts.MasterID == "" && !sketch && UsesDefaultStylesheet() {
		writer.usage = &svgUsage{classes: make(map[string]bool)}
	}

	// only define shadow filter if a shape uses it
	for _, s := range diagram.Shapes {
		if s.Shadow {
			defineShadowFilter(writer)
			break
		}
	}

	defined := make(map[string]struct{})
	definePaint := func(paint string) {
		if color.IsGradient(paint) {
			gradient, _ := color.ParseGradient(paint) // validated above
			defineGradient(writer, gradient, defined)
		}
	}
	definePaint(diagram.Root.Fill)
	definePaint(diagram.Root.Stroke)
	for _, shape := range diagram.Shapes {
		definePaint(shape.Fill)
		definePaint(shape.Stroke)
		definePaint(shape.Color)
	}
	for _, connection := range diagram.Connections {
		definePaint(connection.Fill)
	}

	writer.finish()

	// Apply hash on IDs for targeting, to be specific for this diagram
	diagramHash, err := diagram.HashID(opts.Salt)
	if err != nil {
		return nil, err
	}
	// Shape and connection geometry can change per frame. Keep its clips and
	// masks local while sharing theme, font and icon resources across boards.
	isolatedDiagramHash := diagramHash
	if opts != nil && opts.MasterID != "" {
		diagramHash = opts.MasterID
	}

	// SVG has no notion of z-index. The z-index is effectively the order it's drawn.
	// So draw from the least nested to most nested
	idToShape := make(map[string]d2target.Shape)
	allObjects := make([]DiagramObject, 0, len(diagram.Shapes)+len(diagram.Connections))
	for _, s := range diagram.Shapes {
		idToShape[s.ID] = s
		allObjects = append(allObjects, s)
	}
	for i := range diagram.Connections {
		allObjects = append(allObjects, indexedConnection{&diagram.Connections[i], i})
	}
	lightTheme := d2themescatalog.Find(themeID)
	lightTheme.ApplyOverrides(opts.ThemeOverrides)
	inlineTheme := &lightTheme
	strokeVisible := d2themes.PaintVisibleIn(lightTheme)
	if darkThemeID != nil {
		darkTheme := d2themescatalog.Find(*darkThemeID)
		darkTheme.ApplyOverrides(opts.DarkThemeOverrides)
		strokeVisible = d2themes.PaintVisibleIn(lightTheme, darkTheme)
		// Let the responsive stylesheet select the user's palette.
		inlineTheme = nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	crossings := d2target.ConnectionCrossings(diagram.Connections, idToShape, sketch, strokeVisible)

	left, top, w, h := dimensions(diagram, pad, tl, br)

	if hasLegend(diagram) {
		// The viewport also fits the legend without its bottom padding.
		ruler, _ := textmeasure.NewRuler()
		if ruler != nil {
			legendHeight, maxLabelWidth, _ := measureLegend(diagram.Legend, ruler)
			left, top, w, h = fitLegend(left, top, w, h, pad, tl, br, legendHeight+LEGEND_PADDING, maxLabelWidth)
		}
	}
	if w < 0 || h < 0 {
		return nil, invalidPaddingError(int64(pad))
	}

	viewport := crossingViewport{left, top, w, h}
	effects := viewport.planCrossings(isolatedDiagramHash, crossings, diagram, opts.MasterID != "")

	sortObjects(allObjects)

	appendixItemBuf := &bytes.Buffer{}

	var labelMasks []string
	markdown := newMarkdownRenderer(diagram.FontFamily, diagram.MonoFontFamily, inlineTheme)
	for _, obj := range allObjects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c, is := obj.(indexedConnection); is {
			paint := connectionEffects{labelID: isolatedDiagramHash, crossing: effects[c.index]}
			labelMask, err := drawConnection(writer, isolatedDiagramHash, paint, *c.Connection, defined, idToShape, sketch, inlineTheme, markdown)
			if err != nil {
				return nil, err
			}
			if labelMask != "" {
				labelMasks = append(labelMasks, labelMask)
			}
		} else if s, is := obj.(d2target.Shape); is {
			labelMask, err := drawShape(writer, appendixItemBuf, diagramHash, isolatedDiagramHash, s, sketch, inlineTheme, markdown)
			if err != nil {
				return nil, err
			} else if labelMask != "" {
				labelMasks = append(labelMasks, labelMask)
			}
		} else {
			return nil, fmt.Errorf("unknown object of type %T", obj)
		}
		writer.finish()
	}
	// add all appendix items afterwards so they are always on top
	fmt.Fprint(writer, appendixItemBuf)

	inkTL, inkBR := diagram.InkBoundingBox()
	if err := drawAnnotations(writer, diagram, inkTL, inkBR, inlineTheme, markdown); err != nil {
		return nil, err
	}
	writer.finish()

	if hasLegend(diagram) {
		legendBuf := &bytes.Buffer{}
		err := RenderLegend(legendBuf, diagram, diagramHash, inlineTheme)
		if err != nil {
			return nil, err
		}
		fmt.Fprint(writer, legendBuf)
	}
	writer.finish()

	// Label masks contain only fixed-color geometry and contribute no font or palette classes.
	// A connection can reference this mask even without a label. A shape or a
	// legend can also reference it without a connection on the board.
	if !UsesDefaultStylesheet() || len(diagram.Connections) != 0 || bytes.Contains(buf.Bytes(), []byte(svg.LocalIRI(svg.LiteralID(isolatedDiagramHash)))) {
		bounds := []svg.Attribute{svg.IntAttr("x", left), svg.IntAttr("y", top), svg.IntAttr("width", w), svg.IntAttr("height", h)}
		attrs := []svg.Attribute{svg.Attr("id", isolatedDiagramHash), svg.Attr("maskUnits", "userSpaceOnUse")}
		buf.WriteString(svg.OpenElement("mask", append(attrs, bounds...)...))
		buf.WriteString(svg.EmptyElement("rect", append(bounds, svg.Attr("fill", "white"))...))
		for _, label := range labelMasks {
			buf.WriteString(label)
		}
		buf.WriteString(svg.CloseElement("mask"))
	}

	viewport.writeCrossings(buf, effects, crossings)

	// Validate the theme before expanding the background, preserving error order.
	var themeStylesheet string
	if opts.MasterID == "" {
		var err error
		themeStylesheet, err = ThemeCSS(diagramHash, &themeID, darkThemeID, opts.ThemeOverrides, opts.DarkThemeOverrides)
		if err != nil {
			return nil, err
		}
	}

	// This shift is for background el to envelop the diagram
	strokeWidth := diagram.Root.RenderedStrokeWidth()
	strokePadding := int(math.Ceil(strokeWidth / 2.))
	left, top, w, h, ok := expandDimensions(left, top, w, h, strokePadding)
	if !ok {
		return nil, invalidPaddingError(int64(pad))
	}
	backgroundEl := d2themes.NewThemableElement("rect", inlineTheme)
	// We don't want to change the document viewbox, only the background el
	backgroundEl.X = float64(left)
	backgroundEl.Y = float64(top)
	backgroundEl.Width = float64(w)
	backgroundEl.Height = float64(h)
	backgroundEl.Fill = diagram.Root.Fill
	backgroundEl.Stroke = diagram.Root.Stroke
	backgroundEl.FillPattern = diagram.Root.FillPattern
	if diagram.Root.BorderRadius != 0 || !UsesDefaultStylesheet() {
		backgroundEl.Rx = float64(diagram.Root.BorderRadius)
	}
	if diagram.Root.StrokeDash != 0 {
		dashSize, gapSize := svg.GetStrokeDashAttributes(strokeWidth, diagram.Root.StrokeDash)
		backgroundEl.StrokeDashArray = fmt.Sprintf("%s, %s", svg.FormatFloat(dashSize), svg.FormatFloat(gapSize))
	}
	backgroundEl.AddAttributes(svg.FloatAttr("stroke-width", strokeWidth))

	// This shift is for viewbox to envelop the background el
	left, top, w, h, ok = expandDimensions(left, top, w, h, strokePadding)
	if !ok {
		return nil, invalidPaddingError(int64(pad))
	}

	doubleBorderElStr := ""
	if diagram.Root.DoubleBorder {
		offset := d2target.INNER_BORDER_OFFSET

		left, top, w, h, ok = expandDimensions(left, top, w, h, strokePadding+offset)
		if !ok {
			return nil, invalidPaddingError(int64(pad))
		}

		backgroundEl2 := backgroundEl.Copy()
		// No need to double-paint
		backgroundEl.Fill = "transparent"

		backgroundEl2.X = float64(left)
		backgroundEl2.Y = float64(top)
		backgroundEl2.Width = float64(w)
		backgroundEl2.Height = float64(h)
		doubleBorderElStr = backgroundEl2.Render()

		left, top, w, h, ok = expandDimensions(left, top, w, h, strokePadding)
		if !ok {
			return nil, invalidPaddingError(int64(pad))
		}
	}

	backgroundStr := backgroundEl.Render()

	// generate style elements that will be appended to the SVG tag
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bufStr := buf.String()
	upperBuf := &bytes.Buffer{}
	compact := UsesDefaultStylesheet()
	styleStart := 0
	var classes map[string]bool
	if opts.MasterID == "" {
		var corpora fontCorpora
		if sketch {
			corpora, classes, _ = collectFontCorporaAndClasses(bufStr)
		} else {
			corpora, classes = writer.metadata(bufStr)
		}
		// Appendix postprocessing can add footnotes and numbered icons after
		// Render. Preserve both the old face selection and its whole corpus
		// whenever the diagram can receive that additional text.
		for _, shape := range diagram.Shapes {
			if shape.Tooltip != "" || shape.Link != "" || shape.PrettyLink != "" {
				corpora = nil
				break
			}
		}

		if classes == nil {
			themeStylesheet = pruneThemeCSS(themeStylesheet, bufStr, backgroundStr, doubleBorderElStr)
		} else {
			themeStylesheet = pruneThemeCSSWithClasses(themeStylesheet, classes, backgroundStr, doubleBorderElStr)
		}
		styleStart = writeDiagramCSS(upperBuf, diagramHash, bufStr, diagram.FontFamily, diagram.MonoFontFamily,
			diagram.GetCorpus()+markdown.generatedCorpus(), corpora, classes, themeStylesheet, compact)

		if sketch && !compact {
			writeSketchPatterns(upperBuf, diagramHash)
		}
	}

	patternDefs := ""
	usedPatterns := usedFillPatterns(bufStr, diagramHash, classes)
	for _, pattern := range d2ast.FillPatterns {
		if usedPatterns[pattern] || strings.EqualFold(diagram.Root.FillPattern, pattern) {
			if patternDefs == "" {
				if !compact {
					upperBuf.WriteString(legacyStyleOpen)
				} else if opts.MasterID != "" {
					// A standalone frame supplies its own pattern rules.
					upperBuf.WriteString("<style>")
					styleStart = upperBuf.Len()
				}
			}
			switch pattern {
			case "dots":
				patternDefs += fmt.Sprintf(dots, diagramHash)
			case "lines":
				patternDefs += fmt.Sprintf(lines, diagramHash)
			case "grain":
				patternDefs += fmt.Sprintf(patternassets.GrainSVG(), diagramHash)
			case "paper":
				patternDefs += fmt.Sprintf(patternassets.PaperSVG(), diagramHash)
			}
			if compact {
				fmt.Fprintf(upperBuf, ".%s-overlay{fill:url(#%s-%s);mix-blend-mode:multiply}", pattern, pattern, diagramHash)
			} else {
				fmt.Fprintf(upperBuf, `
.%s-overlay {
	fill: url(#%s-%s);
	mix-blend-mode: multiply;
}`, pattern, pattern, diagramHash)
			}
		}
	}
	if compact && styleStart != 0 {
		closeStyle(upperBuf, styleStart)
	} else if patternDefs != "" {
		upperBuf.WriteString(legacyStyleClose)
	}
	if compact && sketch && opts.MasterID == "" {
		writeSketchPatterns(upperBuf, diagramHash)
	}
	if patternDefs != "" {
		fmt.Fprint(upperBuf, "<defs>")
		fmt.Fprint(upperBuf, patternDefs)
		fmt.Fprint(upperBuf, "</defs>")
	}

	dimensions, err := svg.ScaledDimensions(w, h, scale)
	if err != nil {
		return nil, err
	}

	alignment := "xMinYMin"
	if opts.Center != nil && *opts.Center {
		alignment = "xMidYMid"
	}
	fitToScreenWrapperOpening := ""
	xmlTag := ""
	fitToScreenWrapperClosing := ""
	idAttr := ""
	tag := "g"
	// Many things change when this is rendering for animation
	if opts.MasterID == "" {
		xlink := ""
		// Links and pattern definitions can each introduce an XLink attribute.
		// Animated frames get their namespace from the animation wrapper.
		if !UsesDefaultStylesheet() || strings.Contains(bufStr, "xlink:") || bytes.Contains(upperBuf.Bytes(), []byte("xlink:")) {
			xlink = ` xmlns:xlink="http://www.w3.org/1999/xlink"`
		}
		versionAttr := ""
		if opts.OmitVersion == nil || !*opts.OmitVersion {
			versionAttr = ` data-d2-version="` + svg.EscapeAttribute(version.Version) + `"`
		}
		fitToScreenWrapperOpening = `<svg xmlns="http://www.w3.org/2000/svg"` + xlink + versionAttr +
			` preserveAspectRatio="` + alignment + ` meet" viewBox="0 0 ` + strconv.Itoa(w) + " " + strconv.Itoa(h) + `"` + dimensions + ">"
		if opts.NoXMLTag == nil || !*opts.NoXMLTag {
			xmlTag = `<?xml version="1.0" encoding="utf-8"?>`
		}
		fitToScreenWrapperClosing = "</svg>"
		idAttr = `d2-svg`
		tag = "svg"
	}

	docRendered := fmt.Sprintf(`%s%s<%s class="%s" width="%d" height="%d" viewBox="%d %d %d %d">%s%s%s%s</%s>%s`,
		xmlTag,
		fitToScreenWrapperOpening,
		tag,
		strings.Join([]string{diagramHash, idAttr}, " "),
		w, h, left, top, w, h,
		doubleBorderElStr,
		backgroundStr,
		upperBuf.String(),
		bufStr,
		tag,
		fitToScreenWrapperClosing,
	)
	return []byte(docRendered), nil
}

// WriteAnimationStyles embeds the font faces and theme rules used by a complete
// animation. Source includes the shared definitions and all rendered frames.
func WriteAnimationStyles(buf *bytes.Buffer, diagramHash, source string, diagram *d2target.Diagram, opts RenderOpts) error {
	return WriteAnimationStylesWithTransitions(buf, diagramHash, source, diagram, opts, nil)
}

// WriteAnimationStylesWithTransitions adds the animation clock's CSS to the
// generated stylesheet. Custom base styles retain their own stylesheet so CSS
// imports and namespace declarations stay in their original positions.
func WriteAnimationStylesWithTransitions(buf *bytes.Buffer, diagramHash, source string, diagram *d2target.Diagram, opts RenderOpts, transitions []byte) error {
	stylesheet, err := ThemeCSS(diagramHash, opts.ThemeID, opts.DarkThemeID, opts.ThemeOverrides, opts.DarkThemeOverrides)
	if err != nil {
		return err
	}
	corpora, classes, _ := collectFontCorporaAndClasses(source)
	compact := UsesDefaultStylesheet()
	if compact {
		if classes == nil {
			stylesheet = pruneThemeCSS(stylesheet, source)
		} else {
			stylesheet = pruneThemeCSSWithClasses(stylesheet, classes)
		}
	}
	start := writeDiagramCSS(buf, diagramHash, source, diagram.FontFamily, diagram.MonoFontFamily, diagram.GetNestedCorpus(), corpora, classes, stylesheet, compact)
	if compact {
		buf.Write(transitions)
		closeStyle(buf, start)
	}
	if opts.Sketch != nil && *opts.Sketch {
		writeSketchPatterns(buf, diagramHash)
	}
	if !compact && transitions != nil {
		buf.WriteString(legacyStyleOpen)
		writeCDATA(buf, string(transitions))
		buf.WriteString(legacyStyleClose)
	}
	return nil
}

func writeSketchPatterns(buf *bytes.Buffer, diagramHash string) {
	if UsesDefaultStylesheet() {
		d2sketch.DefineSharedFillPatterns(buf, diagramHash)
	} else {
		d2sketch.DefineFillPatterns(buf, diagramHash)
	}
}

// writeDiagramCSS leaves the compact stylesheet open for pattern or animation
// rules. Custom styles keep the original font and theme elements and ordering.
func writeDiagramCSS(buf *bytes.Buffer, hash, source string, fontFamily, monoFamily *d2fonts.FontFamily, corpus string, corpora fontCorpora, classes map[string]bool, theme string, compact bool) int {
	if !compact {
		embedFonts(buf, hash, source, fontFamily, monoFamily, corpus, corpora)
		buf.WriteString(legacyStyleOpen)
		writeCDATA(buf, BaseStylesheet)
		writeCDATA(buf, theme)
		buf.WriteString(legacyStyleClose)
		return 0
	}

	buf.WriteString("<style>")
	start := buf.Len()
	writeFontCSS(buf, hash, source, fontFamily, monoFamily, corpus, corpora, true)
	// Unknown classes (e.g. scripted SVG or external references) can introduce
	// a blend at runtime, including a user-defined D2 class with that name.
	if classes == nil || classes["blend"] {
		buf.WriteString(compactBaseStylesheet)
	} else {
		buf.WriteString(baseWithoutBlend)
	}
	buf.WriteString(compactCSS(theme))
	return start
}

func ThemeCSS(diagramHash string, themeID *int64, darkThemeID *int64, overrides, darkOverrides *d2target.ThemeOverrides) (stylesheet string, err error) {
	if err := validateThemeOverrides(overrides, "theme override"); err != nil {
		return "", err
	}
	if err := validateThemeOverrides(darkOverrides, "dark theme override"); err != nil {
		return "", err
	}
	if themeID == nil {
		themeID = &d2themescatalog.NeutralDefault.ID
	}
	out, err := singleThemeRulesets(diagramHash, *themeID, overrides)
	if err != nil {
		return "", err
	}

	if darkThemeID != nil {
		darkOut, err := singleThemeRulesets(diagramHash, *darkThemeID, darkOverrides)
		if err != nil {
			return "", err
		}
		out += fmt.Sprintf("@media screen and (prefers-color-scheme:dark){%s}", darkOut)
	}

	return out, nil
}

func singleThemeRulesets(diagramHash string, themeID int64, overrides *d2target.ThemeOverrides) (rulesets string, err error) {
	out := ""
	theme := d2themescatalog.Find(themeID)
	theme.ApplyOverrides(overrides)

	// Global theme colors
	for _, property := range []string{"fill", "stroke", "background-color", "color"} {
		out += fmt.Sprintf(`
		.%s .%s-N1{%s:%s;}
		.%s .%s-N2{%s:%s;}
		.%s .%s-N3{%s:%s;}
		.%s .%s-N4{%s:%s;}
		.%s .%s-N5{%s:%s;}
		.%s .%s-N6{%s:%s;}
		.%s .%s-N7{%s:%s;}
		.%s .%s-B1{%s:%s;}
		.%s .%s-B2{%s:%s;}
		.%s .%s-B3{%s:%s;}
		.%s .%s-B4{%s:%s;}
		.%s .%s-B5{%s:%s;}
		.%s .%s-B6{%s:%s;}
		.%s .%s-AA2{%s:%s;}
		.%s .%s-AA4{%s:%s;}
		.%s .%s-AA5{%s:%s;}
		.%s .%s-AB4{%s:%s;}
		.%s .%s-AB5{%s:%s;}`,
			diagramHash,
			property, property, theme.Colors.Neutrals.N1,
			diagramHash,
			property, property, theme.Colors.Neutrals.N2,
			diagramHash,
			property, property, theme.Colors.Neutrals.N3,
			diagramHash,
			property, property, theme.Colors.Neutrals.N4,
			diagramHash,
			property, property, theme.Colors.Neutrals.N5,
			diagramHash,
			property, property, theme.Colors.Neutrals.N6,
			diagramHash,
			property, property, theme.Colors.Neutrals.N7,
			diagramHash,
			property, property, theme.Colors.B1,
			diagramHash,
			property, property, theme.Colors.B2,
			diagramHash,
			property, property, theme.Colors.B3,
			diagramHash,
			property, property, theme.Colors.B4,
			diagramHash,
			property, property, theme.Colors.B5,
			diagramHash,
			property, property, theme.Colors.B6,
			diagramHash,
			property, property, theme.Colors.AA2,
			diagramHash,
			property, property, theme.Colors.AA4,
			diagramHash,
			property, property, theme.Colors.AA5,
			diagramHash,
			property, property, theme.Colors.AB4,
			diagramHash,
			property, property, theme.Colors.AB5,
		)
	}

	// Appendix
	out += fmt.Sprintf(".appendix text.text{fill:%s}", theme.Colors.Neutrals.N1)

	// Retain the Markdown custom properties for output compatibility and for
	// consumers that style the native .md group. Native D2 primitives use the
	// corresponding fill/stroke theme classes directly.
	out += fmt.Sprintf(".md{--color-fg-default:%s;--color-fg-muted:%s;--color-fg-subtle:%s;--color-canvas-default:%s;--color-canvas-subtle:%s;--color-border-default:%s;--color-border-muted:%s;--color-neutral-muted:%s;--color-accent-fg:%s;--color-accent-emphasis:%s;--color-attention-subtle:%s;--color-danger-fg:%s;}",
		theme.Colors.Neutrals.N1, theme.Colors.Neutrals.N2, theme.Colors.Neutrals.N3,
		theme.Colors.Neutrals.N7, theme.Colors.Neutrals.N6,
		theme.Colors.B1, theme.Colors.B2,
		theme.Colors.Neutrals.N6,
		theme.Colors.B2, theme.Colors.B2,
		theme.Colors.Neutrals.N2,
		"red",
	)

	// Sketch style specific rulesets
	// B
	lc, err := color.LuminanceCategory(theme.Colors.B1)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B1, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.B2)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B2, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.B3)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B3, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.B4)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B4, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.B5)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B5, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.B6)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.B6, lc, diagramHash, blendMode(lc))

	// AA
	lc, err = color.LuminanceCategory(theme.Colors.AA2)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.AA2, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.AA4)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.AA4, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.AA5)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.AA5, lc, diagramHash, blendMode(lc))

	// AB
	lc, err = color.LuminanceCategory(theme.Colors.AB4)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.AB4, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.AB5)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.AB5, lc, diagramHash, blendMode(lc))

	// Neutrals
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N1)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N1, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N2)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N2, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N3)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N3, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N4)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N4, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N5)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N5, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N6)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N6, lc, diagramHash, blendMode(lc))
	lc, err = color.LuminanceCategory(theme.Colors.Neutrals.N7)
	if err != nil {
		return "", err
	}
	out += fmt.Sprintf(".sketch-overlay-%s{fill:url(#streaks-%s-%s);mix-blend-mode:%s}", color.N7, lc, diagramHash, blendMode(lc))

	if theme.IsDark() {
		out += ".light-code{display: none}"
		out += ".dark-code{display: block}"
	} else {
		out += ".light-code{display: block}"
		out += ".dark-code{display: none}"
	}

	return out, nil
}

func blendMode(lc string) string {
	switch lc {
	case "bright":
		return "darken"
	case "normal":
		return "color-burn"
	case "dark":
		return "overlay"
	case "darker":
		return "lighten"
	}
	panic("invalid luminance category")
}

type indexedConnection struct {
	*d2target.Connection
	index int
}

type DiagramObject interface {
	GetID() string
	GetZIndex() int
}

// sortObjects sorts all diagrams objects (shapes and connections) in the desired drawing order
// the sorting criteria is:
// 1. zIndex, lower comes first
// 2. two shapes with the same zIndex are sorted by their level (container nesting), containers come first
// 3. two shapes with the same zIndex and same level, are sorted in the order they were exported
// 4. shape and edge, shapes come first
func sortObjects(allObjects []DiagramObject) {
	sort.SliceStable(allObjects, func(i, j int) bool {
		// first sort by zIndex
		iZIndex := allObjects[i].GetZIndex()
		jZIndex := allObjects[j].GetZIndex()
		if iZIndex != jZIndex {
			return iZIndex < jZIndex
		}

		// then, if both are shapes, parents come before their children
		iShape, iIsShape := allObjects[i].(d2target.Shape)
		jShape, jIsShape := allObjects[j].(d2target.Shape)
		if iIsShape && jIsShape {
			return iShape.Level < jShape.Level
		}

		// then, shapes come before connections
		return iIsShape && !jIsShape
	})
}

func fontToFamily(fontFamily string) *d2fonts.FontFamily {
	ff, ok := d2fonts.D2_FONT_TO_FAMILY[fontFamily]
	if !ok {
		return nil
	}
	return &ff
}

func hash(s string, base int) string {
	const secret = "lalalas"
	h := fnv.New32a()
	h.Write([]byte(s))
	h.Write([]byte(secret))
	return strconv.FormatUint(uint64(h.Sum32()), base)
}

func RenderMultiboard(diagram *d2target.Diagram, opts *RenderOpts) ([][]byte, error) {
	if err := d2target.ValidateRenderTarget(diagram); err != nil {
		return nil, err
	}
	return renderMultiboard(diagram, opts)
}

func renderMultiboard(diagram *d2target.Diagram, opts *RenderOpts) ([][]byte, error) {
	var boards [][]byte
	for _, dl := range diagram.Layers {
		childrenBoards, err := renderMultiboard(dl, opts)
		if err != nil {
			return nil, err
		}
		boards = append(boards, childrenBoards...)
	}
	for _, dl := range diagram.Scenarios {
		childrenBoards, err := renderMultiboard(dl, opts)
		if err != nil {
			return nil, err
		}
		boards = append(boards, childrenBoards...)
	}
	for _, dl := range diagram.Steps {
		childrenBoards, err := renderMultiboard(dl, opts)
		if err != nil {
			return nil, err
		}
		boards = append(boards, childrenBoards...)
	}

	if !diagram.IsFolderOnly {
		if err := validateRenderLinks(diagram); err != nil {
			return boards, err
		}
		out, err := renderValidated(context.Background(), diagram, opts)
		if err != nil {
			return boards, err
		}
		boards = append([][]byte{out}, boards...)
		return boards, nil
	}
	return boards, nil
}
