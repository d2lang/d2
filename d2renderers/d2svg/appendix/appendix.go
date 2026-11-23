// Package appendix adds numbered footnotes to D2 SVGs and animation frames.
package appendix

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2renderers/d2fonts"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/internal/svgframe"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/lib/color"
	svglib "github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/util-go/go2"
)

//        ┌──────────────┐
//        │              │
//        │   DIAGRAM    │
//        │              │
// PAD_   │              │
// SIDES  │              │
//    │   │              │
//    │   └──────────────┘
//    ▼                   ◄──────  PAD_TOP
//
//    ─────────────────────────
//
//
//         1. asdfasdf
//
//                        ◄──── SPACER
//         2. qwerqwer
//
//

const (
	PAD_TOP   = 50
	PAD_SIDES = 40
	SPACER    = 20

	FONT_SIZE   = 16
	ICON_RADIUS = 16
)

var viewboxRegex = regexp.MustCompile(`viewBox=\"([0-9\- ]+)\"`)
var widthRegex = regexp.MustCompile(` width="[.0-9]+"`)
var heightRegex = regexp.MustCompile(` height="[.0-9]+"`)
var svgRegex = regexp.MustCompile(`<svg(.*?)>`)

func FindViewboxSlice(svg []byte) []string {
	viewboxMatches := viewboxRegex.FindAllStringSubmatch(string(svg), 2)
	viewboxMatch := viewboxMatches[1]
	viewboxRaw := viewboxMatch[1]
	return strings.Split(viewboxRaw, " ")
}

func Append(diagram *d2target.Diagram, renderOpts *d2svg.RenderOpts, ruler *textmeasure.Ruler, in []byte) []byte {
	svg := string(in)
	tl, br := diagram.BoundingBox()
	isFrame := strings.HasPrefix(svg, "<g ")
	var frame svgframe.Frame
	if isFrame {
		var ok bool
		frame, ok = svgframe.Read(in)
		if !ok {
			return in
		}
		// Keep the separator below rendered content, including a tall legend.
		br.Y = max(br.Y, frame.ViewBox.Max.Y-PAD_TOP)
	}

	appendix, w, h := generateAppendix(diagram, ruler, tl, br)

	if h == 0 {
		return in
	}

	separatorEl := d2themes.NewThemableElement("line", nil)
	separatorEl.X1 = float64(tl.X - PAD_SIDES)
	separatorEl.Y1 = float64(br.Y + PAD_TOP)
	separatorEl.X2 = float64(go2.IntMax(w, br.X) + PAD_SIDES)
	separatorEl.Y2 = float64(br.Y + PAD_TOP)
	separatorEl.Stroke = color.B2 // same as --color-border-muted in markdown
	appendix = separatorEl.Render() + appendix

	// Animated boards are group fragments. Their enclosing SVG supplies fonts
	// and uses the shared animation ID when drawing tooltip and link icons.
	var diagramHash string
	if isFrame {
		diagramHash = frame.MasterID
		var ok bool
		svg, ok = appendToFrame(svg, appendix, frame.ViewBox, tl, br, w, h)
		if !ok {
			return in
		}
	} else {
		var salt *string
		if renderOpts != nil {
			salt = renderOpts.Salt
		}
		var err error
		diagramHash, err = diagram.HashID(salt)
		if err != nil {
			return nil
		}
		scale := 1.0
		if renderOpts != nil && renderOpts.Scale != nil {
			scale = *renderOpts.Scale
		}
		svg = appendToStandaloneSVG(svg, appendix, w, h, scale)
	}
	return []byte(numberIcons(diagram, diagramHash, svg))
}

func appendToStandaloneSVG(svg, appendix string, w, h int, scale float64) string {
	// match 1st two viewboxes, 1st is outer fit-to-screen viewbox="0 0 innerWidth innerHeight"
	viewboxMatches := viewboxRegex.FindAllStringSubmatch(svg, 2)
	viewboxMatch := viewboxMatches[1]
	viewboxRaw := viewboxMatch[1]
	viewboxSlice := strings.Split(viewboxRaw, " ")
	viewboxPadLeft, _ := strconv.Atoi(viewboxSlice[0])
	viewboxWidth, _ := strconv.Atoi(viewboxSlice[2])
	viewboxHeight, _ := strconv.Atoi(viewboxSlice[3])

	w -= viewboxPadLeft
	w += PAD_SIDES * 2
	if viewboxWidth < w {
		viewboxWidth = w
	}

	viewboxHeight += h + PAD_TOP

	newOuterViewbox := fmt.Sprintf(`viewBox="0 0 %d %d"`, viewboxWidth, viewboxHeight)
	newViewbox := fmt.Sprintf(`viewBox="%s %s %s %s"`, viewboxSlice[0], viewboxSlice[1], strconv.Itoa(viewboxWidth), strconv.Itoa(viewboxHeight))

	// The outer SVG carries display dimensions. The inner SVG and its
	// background use unscaled diagram coordinates.
	outerEnd := svgRegex.FindStringIndex(svg)[1]
	outer, inner := svg[:outerEnd], svg[outerEnd:]
	outer = strings.Replace(outer, viewboxMatches[0][0], newOuterViewbox, 1)
	outer = widthRegex.ReplaceAllString(outer, svglib.RenderAttributes(svglib.FloatAttr("width", math.Ceil(scale*float64(viewboxWidth)))))
	outer = heightRegex.ReplaceAllString(outer, svglib.RenderAttributes(svglib.FloatAttr("height", math.Ceil(scale*float64(viewboxHeight)))))
	inner = strings.Replace(inner, viewboxMatch[0], newViewbox, 1)
	newWidth := svglib.RenderAttributes(svglib.IntAttr("width", viewboxWidth))
	newHeight := svglib.RenderAttributes(svglib.IntAttr("height", viewboxHeight))
	for _, match := range widthRegex.FindAllString(inner, 2) {
		inner = strings.Replace(inner, match, newWidth, 1)
	}
	for _, match := range heightRegex.FindAllString(inner, 2) {
		inner = strings.Replace(inner, match, newHeight, 1)
	}
	svg = outer + inner

	if !strings.Contains(svg, `font-family: "font-regular"`) {
		appendix += fmt.Sprintf(`<style type="text/css"><![CDATA[
.text {
	font-family: "font-regular";
}
@font-face {
	font-family: font-regular;
	src: url("%s");
}
]]></style>`, d2fonts.FontEncodings.Get(d2fonts.SourceSansPro.Font(0, d2fonts.FONT_STYLE_REGULAR)))
	}
	if !strings.Contains(svg, `font-family: "font-bold"`) {
		appendix += fmt.Sprintf(`<style type="text/css"><![CDATA[
.text-bold {
	font-family: "font-bold";
}
@font-face {
	font-family: font-bold;
	src: url("%s");
}
]]></style>`, d2fonts.FontEncodings.Get(d2fonts.SourceSansPro.Font(0, d2fonts.FONT_STYLE_BOLD)))
	}

	closingIndex := strings.LastIndex(svg, "</svg></svg>")
	return svg[:closingIndex] + appendix + svg[closingIndex:]
}

func numberIcons(diagram *d2target.Diagram, diagramHash, svg string) string {
	// icons are numbered according to diagram.Shapes which is based on their order of definition,
	// but they appear in the svg according to renderOrder so we have to replace in that order
	type appendixIcon struct {
		number    int
		isTooltip bool
		shape     d2target.Shape
	}
	var renderOrder []appendixIcon

	i := 1
	for _, s := range diagram.Shapes {
		if s.Tooltip != "" {
			renderOrder = append(renderOrder, appendixIcon{i, true, s})
			i++
		}
		if s.AppendixLinkText() != "" {
			renderOrder = append(renderOrder, appendixIcon{i, false, s})
			i++
		}
	}
	// sort to match render order
	sort.SliceStable(renderOrder, func(i, j int) bool {
		iZIndex := renderOrder[i].shape.GetZIndex()
		jZIndex := renderOrder[j].shape.GetZIndex()
		if iZIndex != jZIndex {
			return iZIndex < jZIndex
		}
		return renderOrder[i].shape.Level < renderOrder[j].shape.Level
	})

	// replace each rendered svg icon
	for _, icon := range renderOrder {
		// The clip-path has a unique ID, so this won't replace any user icons
		// In the existing SVG, the transform places it top-left, so we adjust
		var iconStr string
		if icon.isTooltip {
			iconStr = fmt.Sprintf(d2svg.TooltipIcon, diagramHash, svglib.SVGID(icon.shape.ID))
		} else {
			iconStr = fmt.Sprintf(d2svg.LinkIcon, diagramHash, svglib.SVGID(icon.shape.ID))
		}
		svg = strings.Replace(svg, iconStr, generateNumberedIcon(icon.number, 0, ICON_RADIUS), 1)
	}

	return svg
}

// D2 emits the frame group followed by its background rectangles (including
// double borders and pattern overlays). Resize only those opening elements.
var frameBackground = regexp.MustCompile(`^<g [^>]*>(?:<rect [^>]* />)*`)
var frameDimension = regexp.MustCompile(` (x|width|height)="(-?[0-9]+)"`)

func appendToFrame(svg, appendix string, box image.Rectangle, tl, br d2target.Point, w, h int) (string, bool) {
	left := min(box.Min.X, 0, tl.X-PAD_SIDES)
	right := max(box.Max.X, w+PAD_SIDES*2, br.X+PAD_SIDES)
	bottom := br.Y + 2*PAD_TOP + h
	width, height := right-left, bottom-box.Min.Y
	prefix := frameBackground.FindString(svg)
	end := strings.LastIndex(svg, "</g>")
	if len(prefix) == 0 || end < len(prefix) {
		return "", false
	}
	resized := frameDimension.ReplaceAllStringFunc(prefix, func(attr string) string {
		name, value, _ := strings.Cut(attr[1:], `="`)
		n, _ := strconv.Atoi(strings.TrimSuffix(value, `"`))
		switch name {
		case "x":
			n += left - box.Min.X
		case "width":
			n += width - box.Dx()
		case "height":
			n += height - box.Dy()
		}
		return svglib.RenderAttributes(svglib.IntAttr(name, n))
	})
	viewBox := svglib.Attr("viewBox", fmt.Sprintf("%d %d %d %d", left, box.Min.Y, width, height))
	resized = viewboxRegex.ReplaceAllString(resized, strings.TrimSpace(svglib.RenderAttributes(viewBox)))
	return resized + svg[len(prefix):end] + appendix + svg[end:], true
}

func generateAppendix(diagram *d2target.Diagram, ruler *textmeasure.Ruler, tl, br d2target.Point) (string, int, int) {
	maxWidth, totalHeight := 0, 0

	var lines []string
	i := 1

	for _, s := range diagram.Shapes {
		// Must list exactly the icons numbered in Append: one sequence, tooltip
		// before link, in diagram.Shapes order.
		for _, txt := range []string{s.Tooltip, s.AppendixLinkText()} {
			if txt != "" {
				line, w, h := generateLine(i, br.Y+(PAD_TOP*2)+totalHeight, txt, ruler)
				i++
				lines = append(lines, line)
				maxWidth = go2.IntMax(maxWidth, w)
				totalHeight += h + SPACER
			}
		}
	}
	if len(lines) == 0 {
		return "", 0, 0
	}
	totalHeight += SPACER

	return fmt.Sprintf(`<g class="appendix" x="%d" y="%d" width="%d" height="100%%">%s</g>
`, tl.X, br.Y, (br.X - tl.X), strings.Join(lines, "\n")), maxWidth, totalHeight
}

func generateNumberedIcon(i, x, y int) string {
	line := fmt.Sprintf(`<circle cx="%d" cy="%d" r="%d" fill="white" stroke="#DEE1EB" />`,
		x+ICON_RADIUS, y, ICON_RADIUS)

	line += fmt.Sprintf(`<text class="text-bold" x="%d" y="%d" style="font-size: %dpx;text-anchor:middle;">%d</text>`,
		x+ICON_RADIUS, y+5, FONT_SIZE, i)

	return line
}

func generateLine(i, y int, text string, ruler *textmeasure.Ruler) (string, int, int) {
	mtext := &d2target.MText{
		Text:     text,
		FontSize: FONT_SIZE,
	}

	dims := d2graph.GetTextDimensions(nil, ruler, mtext, nil)

	line := fmt.Sprintf(`<g transform="translate(%d %d)" class="appendix-icon">%s</g>`,
		0, y, generateNumberedIcon(i, 0, 0))

	line += fmt.Sprintf(`<text class="text" x="%d" y="%d" style="font-size: %dpx;">%s</text>`,
		ICON_RADIUS*3, y+5, FONT_SIZE, d2svg.RenderText(text, ICON_RADIUS*3, float64(dims.Height)))

	return line, dims.Width + ICON_RADIUS*3, go2.IntMax(dims.Height, ICON_RADIUS*2)
}
