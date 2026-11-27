package d2animate

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/internal/svgframe"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/version"
)

// Match the existing keyframe precision and omit only insignificant zeros.
func percentage(v float64, precision int) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(v, 'f', precision, 64), "0"), ".") + "%"
}

func Wrap(rootDiagram *d2target.Diagram, svgs [][]byte, renderOpts d2svg.RenderOpts, intervalMS int) ([]byte, error) {
	if intervalMS <= 0 || len(svgs) > math.MaxInt/intervalMS {
		return nil, fmt.Errorf("SVG animation duration must be positive and fit in milliseconds")
	}
	durations := make([]int, len(svgs))
	for i := range durations {
		durations[i] = intervalMS
	}
	return WrapTimed(rootDiagram, svgs, renderOpts, durations)
}

// WrapTimed composes independently rendered SVG frames on a shared playback clock.
func WrapTimed(rootDiagram *d2target.Diagram, svgs [][]byte, renderOpts d2svg.RenderOpts, durationsMS []int) ([]byte, error) {
	if err := d2target.ValidateRenderTarget(rootDiagram); err != nil {
		return nil, err
	}
	if len(svgs) != len(durationsMS) {
		return nil, fmt.Errorf("animation requires one duration per SVG frame")
	}
	totalMS := 0
	uniform := true
	for _, ms := range durationsMS {
		if ms <= 0 || ms > math.MaxInt-totalMS {
			return nil, fmt.Errorf("animation duration must be positive and fit in milliseconds")
		}
		totalMS += ms
		uniform = uniform && ms == durationsMS[0]
	}
	buf := &bytes.Buffer{}

	pad := d2svg.DEFAULT_PADDING
	if renderOpts.Pad != nil {
		pad = int(*renderOpts.Pad)
	}
	tl, br := rootDiagram.NestedBoundingBox()
	viewport := image.Rect(tl.X-pad, tl.Y-pad, br.X+pad, br.Y+pad)
	// Rendered frames can extend beyond the diagram, for example when one
	// board has a forced appendix. All frames share the same viewport.
	for _, svg := range svgs {
		if frame, ok := svgframe.Read(svg); ok {
			viewport = viewport.Union(frame.ViewBox)
		}
	}
	left, top := viewport.Min.X, viewport.Min.Y
	width, height := viewport.Dx(), viewport.Dy()
	if width < 0 || height < 0 {
		return nil, fmt.Errorf("animation viewport dimensions exceed the supported range")
	}

	dimensions, err := svg.ScaledDimensions(width, height, renderOpts.Scale)
	if err != nil {
		return nil, err
	}

	if renderOpts.NoXMLTag == nil || !*renderOpts.NoXMLTag {
		buf.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	}
	buf.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"`)
	if renderOpts.OmitVersion == nil || !*renderOpts.OmitVersion {
		buf.WriteString(svg.RenderAttributes(svg.Attr("d2Version", version.Version)))
	}
	align := "xMinYMin"
	if renderOpts.Center != nil && *renderOpts.Center {
		align = "xMidYMid"
	}
	fmt.Fprintf(buf, ` preserveAspectRatio="%s meet" viewBox="0 0 %d %d"%s>`, align, width, height, dimensions)

	innerOpening := fmt.Sprintf(`<svg class="d2-svg" width="%d" height="%d" viewBox="%d %d %d %d">`,
		width, height, left, top, width, height)
	fmt.Fprint(buf, innerOpening)

	diagramHash, err := rootDiagram.HashID(renderOpts.Salt)
	if err != nil {
		return nil, err
	}

	defs, svgs := shareStaticShapes(rootDiagram, svgs, diagramHash)
	styleFrames := svgs
	graphics, svgs := shareFrameGraphics(svgs, diagramHash)
	var styleGraphics []byte
	if len(defs) != 0 {
		// The static shapes already introduce references, so font selection uses
		// the complete corpus. Scan the smaller shared artwork for theme classes.
		styleGraphics = graphics
		styleFrames = svgs
	}
	sourceSize := len(defs) + len(styleGraphics) + len(styleFrames) // One separator per frame.
	for _, frame := range styleFrames {
		sourceSize += len(frame)
	}
	var source strings.Builder
	source.Grow(sourceSize)
	source.Write(defs)
	source.Write(styleGraphics)
	for _, frame := range styleFrames {
		source.Write(frame)
		source.WriteByte(' ')
	}
	svgsStr := source.String()

	var transitions bytes.Buffer
	writeTransitions(&transitions, diagramHash, svgsStr, durationsMS, totalMS, uniform)
	if err := d2svg.WriteAnimationStylesWithTransitions(buf, diagramHash, svgsStr, rootDiagram, renderOpts, transitions.Bytes()); err != nil {
		return nil, err
	}

	stylePrefix := ` style="animation: d2Transition-` + diagramHash + "-"
	styleSuffix := " " + strconv.Itoa(totalMS) + `ms steps(1,end) infinite"`
	// Reserve room for the artwork and its timing attributes before copying
	// multi-megabyte frames into the final output buffer.
	attributeSize := len(stylePrefix) + len(strconv.Itoa(len(svgs))) + len(styleSuffix)
	artworkSize := len(defs) + len(graphics) + len("</svg></svg>") + len(svgs)*attributeSize
	for _, frame := range svgs {
		artworkSize += len(frame)
	}
	buf.Grow(artworkSize)
	buf.Write(defs)
	buf.Write(graphics)
	for i, frame := range svgs {
		before, after, found := bytes.Cut(frame, []byte("<g"))
		buf.Write(before)
		if found {
			buf.WriteString("<g")
			buf.WriteString(stylePrefix)
			buf.WriteString(strconv.Itoa(i))
			buf.WriteString(styleSuffix)
			buf.Write(after)
		}
	}
	buf.WriteString("</svg></svg>")

	return buf.Bytes(), nil
}

func writeTransitions(buf *bytes.Buffer, diagramHash, svgsStr string, durationsMS []int, totalMS int, uniform bool) {
	hidden, visible := "{opacity:0", "{opacity:1"
	if !uniform || len(durationsMS) > 100 {
		hidden, visible = "{clip-path:inset(50%)", "{clip-path:none"
	}
	if strings.Contains(svgsStr, "<a ") || strings.Contains(svgsStr, "<title>") {
		// Transparent frames can intercept the pointer; even clipped anchors
		// remain keyboard-focusable. Keep their paint servers available to
		// other frames by changing visibility only on the links themselves.
		fmt.Fprintf(buf, ".%s a{visibility:var(--d2-link-visibility,inherit)}", diagramHash)
		hidden += ";pointer-events:none;--d2-link-visibility:hidden"
		visible += ";pointer-events:inherit;--d2-link-visibility:initial"
	}
	hidden += "}"
	visible += "}"
	startMS := 0
	for i, duration := range durationsMS {
		// Switch whole frames on their authored boundary. Overlapping two
		// partially transparent frames makes even identical artwork flash.
		fmt.Fprintf(buf, "@keyframes d2Transition-%s-%d{", diagramHash, i)
		if startMS > 0 {
			buf.WriteString("0%" + hidden)
		}
		buf.WriteString(percentage(100*float64(startMS)/float64(totalMS), 9) + visible)
		if endMS := startMS + duration; endMS < totalMS {
			buf.WriteString(percentage(100*float64(endMS)/float64(totalMS), 9) + ",100%" + hidden)
		} else {
			buf.WriteString("100%" + visible)
		}
		buf.WriteString("}")
		startMS += duration
	}
}
