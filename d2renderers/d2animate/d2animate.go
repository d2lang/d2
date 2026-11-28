package d2animate

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strconv"
	"strings"
	"time"

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
	return wrapFrames(rootDiagram, nil, nil, svgs, renderOpts, durationsMS, 0)
}

// WrapTimeline plays every frame in order, zooming through linked layers.
// Activating a layer link pauses the tour to explore that layer and its steps.
// boardPath identifies the selected subtree relative to the document root.
func WrapTimeline(rootDiagram *d2target.Diagram, boardPath []string, frames []Frame, svgs [][]byte, renderOpts d2svg.RenderOpts, durationMS int) ([]byte, error) {
	if durationMS < 0 || durationMS > d2target.MaxTweenDurationMS {
		return nil, fmt.Errorf("invalid layer transition duration %dms", durationMS)
	}
	durations := make([]int, len(frames))
	for i, frame := range frames {
		if frame.Diagram == nil {
			return nil, fmt.Errorf("animation frame %d has no diagram", i)
		}
		durations[i] = frame.DurationMS
	}
	return wrapFrames(rootDiagram, boardPath, frames, svgs, renderOpts, durations, durationMS)
}

func wrapFrames(rootDiagram *d2target.Diagram, boardPath []string, frames []Frame, svgs [][]byte, renderOpts d2svg.RenderOpts, durationsMS []int, zoomMS int) ([]byte, error) {
	if err := d2target.ValidateRenderTarget(rootDiagram); err != nil {
		return nil, err
	}
	if len(svgs) != len(durationsMS) {
		return nil, fmt.Errorf("animation requires one duration per SVG frame")
	}
	diagramHash, err := rootDiagram.HashID(renderOpts.Salt)
	if err != nil {
		return nil, err
	}
	var nav *LayerNavigation
	var links []layerLink
	var zooms map[int][]frameLeg
	if len(frames) > 0 {
		nav = NewLayerNavigation(rootDiagram, boardPath)
		if zoomMS > 0 && len(frames) > 1 && len(nav.focus) > 0 {
			zooms = playbackZooms(nav, frames)
		}
		svgs, links = bindLayerLinks(svgs, layerLinks(nav, frames, zoomMS), diagramHash)
	}
	totalMS := 0
	uniform := true
	for i, ms := range durationsMS {
		if ms <= 0 {
			return nil, fmt.Errorf("animation duration must be positive and fit in milliseconds")
		}
		if _, ok := zooms[i]; ok {
			ms = max(ms, zoomMS)
			durationsMS[i] = ms
		}
		if ms > math.MaxInt-totalMS {
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
	var viewport image.Rectangle
	var offsets []image.Point
	var slots []layerSlot
	hasCamera := len(zooms) > 0
	for _, link := range links {
		if len(link.legs) > 0 {
			slots = layerSlots(nav, frames, links)
			hasCamera = true
			break
		}
	}
	if hasCamera {
		var err error
		viewport, offsets, err = layerViewport(nav, frames, svgs, pad)
		if err != nil {
			return nil, err
		}
	} else {
		if len(links) > 0 {
			// A plain board link only selects a frame. Preserve the ordinary
			// coordinate system and bounds with identity placements.
			offsets = make([]image.Point, len(frames))
		}
		tl, br := rootDiagram.NestedBoundingBox()
		viewport = image.Rect(tl.X-pad, tl.Y-pad, br.X+pad, br.Y+pad)
		// Rendered frames can extend beyond the diagram, for example when one
		// board has a forced appendix. All frames share the same viewport.
		for _, svg := range svgs {
			if frame, ok := svgframe.Read(svg); ok {
				viewport = viewport.Union(frame.ViewBox)
			}
		}
	}
	left, top := viewport.Min.X, viewport.Min.Y
	width, height := viewport.Dx(), viewport.Dy()
	if width < 0 || height < 0 {
		return nil, fmt.Errorf("animation viewport dimensions exceed the supported range")
	}

	artW, artH := width, height
	if renderOpts.Scale != nil {
		artW, artH, err = svg.ScaledSize(width, height, *renderOpts.Scale)
		if err != nil {
			return nil, err
		}
	}
	showProgress := totalMS > 0 && !viewport.Empty() && rootDiagram.Config.ResolveAnimateProgress(renderOpts.AnimateProgress, true)
	var footer image.Rectangle
	canvasW, canvasH := width, height
	if showProgress {
		extra := ProgressGap + ProgressLineHeight(artW, artH)
		if artH > math.MaxInt-extra {
			return nil, fmt.Errorf("animation progress footer exceeds the supported SVG height")
		}
		footer = image.Rect(0, artH, artW, artH+extra)
		canvasW, canvasH = artW, footer.Max.Y
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
	var dimensions, artAlign string
	innerW, innerH := width, height
	if renderOpts.Scale != nil {
		dimensions = svg.RenderAttributes(svg.IntAttr("width", artW), svg.IntAttr("height", artH+footer.Dy()))
		if showProgress {
			innerW, innerH = artW, artH
			artAlign = ` preserveAspectRatio="` + align + ` meet"`
		}
	}
	fmt.Fprintf(buf, ` preserveAspectRatio="%s meet" viewBox="0 0 %d %d"%s>`, align, canvasW, canvasH, dimensions)
	frameSibling := ""
	if showProgress && len(links) > 0 {
		// The fragment precedes the artwork and footer so their clocks can use
		// the same sibling selectors without requiring CSS ancestor selection.
		writeLayerTargets(buf, diagramHash, links)
		frameSibling = "*>"
	}
	// The nested SVG clips all camera and board paint to the original artwork viewport.
	fmt.Fprintf(buf, `<svg class="d2-svg" width="%d" height="%d" viewBox="%d %d %d %d"%s>`, innerW, innerH, left, top, width, height, artAlign)
	var transitions bytes.Buffer
	var progress, matte string
	var linkedMatte bool
	if showProgress {
		renderOpts.AnimateProgressColor = rootDiagram.Config.ResolveAnimateProgressColor(renderOpts.AnimateProgressColor)
		progress, err = svgProgress(&transitions, diagramHash, footer, totalMS, renderOpts, len(links) > 0)
		if err != nil {
			return nil, err
		}
		matte, linkedMatte = footerMatte(&transitions, diagramHash, footer, totalMS, renderOpts, rootDiagram, frames, len(links) > 0, slots)
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
	sourceSize := len(matte) + len(defs) + len(styleGraphics) + len(styleFrames) // One separator per frame.
	for _, frame := range styleFrames {
		sourceSize += len(frame)
	}
	var source strings.Builder
	source.Grow(sourceSize)
	source.WriteString(matte) // Keep footer theme tokens when pruning the responsive styles.
	source.Write(defs)
	source.Write(styleGraphics)
	for _, frame := range styleFrames {
		source.Write(frame)
		source.WriteByte(' ')
	}
	svgsStr := source.String()

	hidden, visible := "{opacity:0", "{opacity:1"
	if len(slots) == 0 && (!uniform || len(svgs) > 100) {
		hidden, visible = "{clip-path:inset(50%)", "{clip-path:none"
	}
	if strings.Contains(svgsStr, "<a ") || strings.Contains(svgsStr, "<title>") {
		// Transparent frames can intercept the pointer; even clipped anchors
		// remain keyboard-focusable. Keep their paint servers available to
		// other frames by changing visibility only on the links themselves.
		fmt.Fprintf(&transitions, ".%s a{visibility:var(--d2-link-visibility,inherit)}", diagramHash)
		hidden += ";pointer-events:none;--d2-link-visibility:hidden"
		visible += ";pointer-events:inherit;--d2-link-visibility:initial"
	}
	if len(offsets) > 0 {
		// Hide inactive layer content from the accessibility tree. A visible
		// board may still reference a mask or arrowhead inside a hidden board.
		fmt.Fprintf(&transitions, ".d2-frame-%[1]s>g>g>g{visibility:var(--d2-link-visibility,inherit)}.d2-frame-%[1]s :where(defs,mask,marker,clipPath,pattern){visibility:visible}", diagramHash)
	}
	hidden += "}"
	visible += "}"
	// Global playback runs until a fragment is selected. Each clickable
	// layer also has a local clock for exploring its own authored steps.
	startMS := 0
	var from frameStart
	for i, duration := range durationsMS {
		var to frameStart
		if i+1 < len(frames) {
			to = frames[i+1].start
		}
		writeFrameKeyframes(&transitions, "d2Transition", diagramHash, i, startMS, duration, totalMS, from, to, hidden, visible)
		startMS += duration
		if len(slots) > 0 && slots[i].count > 1 {
			slot := slots[i]
			writeFrameKeyframes(&transitions, "d2LayerStep", diagramHash, i, slot.startMS, frames[i].DurationMS, slot.totalMS, from, to, hidden, visible)
		}
		from = to
	}
	if len(zooms) > 0 {
		writePlaybackZooms(&transitions, diagramHash, durationsMS, totalMS, zoomMS, viewport, offsets, zooms)
	}
	if zoomMS > 0 && len(links) > 0 {
		writeClickZooms(&transitions, links, diagramHash, viewport, offsets)
	}
	if err := d2svg.WriteAnimationStylesWithTransitions(buf, diagramHash, svgsStr, rootDiagram, renderOpts, transitions.Bytes()); err != nil {
		return nil, err
	}
	if len(links) > 0 {
		writeLayerNavigation(buf, diagramHash, links, slots, frameSibling, linkedMatte)
		if frameSibling == "" {
			writeLayerTargets(buf, diagramHash, links)
		}
	}
	if strings.Contains(svgsStr, `class="d2-icon-tour-clock"`) {
		writeLayerIconClocks(buf, diagramHash, links, slots, frameSibling)
	}

	stylePrefix := ` style="animation: d2Transition-` + diagramHash + "-"
	styleSuffix := " " + strconv.Itoa(totalMS) + `ms steps(1,end) infinite"`
	// Reserve room for the artwork and its timing attributes before copying
	// multi-megabyte frames into the final output buffer.
	attributeSize := len(stylePrefix) + len(strconv.Itoa(len(svgs))) + len(styleSuffix)
	artworkSize := len(defs) + len(graphics) + len(matte) + len(progress) + len("</svg></svg>") + len(svgs)*attributeSize
	for _, frame := range svgs {
		artworkSize += len(frame)
	}
	buf.Grow(artworkSize)
	buf.Write(defs)
	buf.Write(graphics)
	for i, frame := range svgs {
		if len(offsets) > 0 {
			writeLayerFrame(buf, diagramHash, i, totalMS, slots, true, linkedMatte)
			fmt.Fprintf(buf, `<g transform="translate(%d %d)">`, offsets[i].X, offsets[i].Y)
			buf.Write(frame)
			buf.WriteString("</g></g></g></g>")
			continue
		}
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
	buf.WriteString("</svg>")
	buf.WriteString(matte)
	buf.WriteString(progress)
	buf.WriteString("</svg>")

	return buf.Bytes(), nil
}

func (s frameStart) percentage(ms, total int) string {
	at := float64(ms) + float64(s.fraction)/float64(time.Millisecond)
	percent := 100 * at / float64(total)
	if s.uniform {
		// Rounding upward can keep the previous pose visible on a display
		// tick exactly at a uniform tween's sample boundary.
		percent = math.Floor(percent*1e9) / 1e9
	}
	return percentage(percent, 9)
}

// The three groups own the global clock, the manually selected layer's steps,
// and its camera. A linked footer uses the same clocks without spatial transforms.
func writeLayerFrame(buf *bytes.Buffer, hash string, i, totalMS int, slots []layerSlot, artwork, linkedMatte bool) {
	buf.WriteString("<g")
	if artwork {
		fmt.Fprintf(buf, ` id="d2-frame-%s-%d"`, hash, i)
	}
	fmt.Fprintf(buf, ` class="d2-frame-%s`, hash)
	if linkedMatte {
		fmt.Fprintf(buf, " d2-frame-%s-%d", hash, i)
	}
	local := ""
	if len(slots) > 0 {
		slot := slots[i]
		fmt.Fprintf(buf, " d2-layer-%s-%d", hash, slot.id)
		if slot.count > 1 {
			local = fmt.Sprintf("--d2-slot:d2LayerStep-%s-%d %dms steps(1,end);", hash, i, slot.totalMS)
		}
	}
	fmt.Fprintf(buf, `" style="%sanimation: d2Transition-%s-%d %dms steps(1,end) infinite"><g><g>`, local, hash, i, totalMS)
}

// Switch whole frames on their authored boundary so shared artwork stays solid.
func writeFrameKeyframes(buf *bytes.Buffer, name, hash string, frame, start, duration, total int, from, to frameStart, hidden, visible string) {
	fmt.Fprintf(buf, "@keyframes %s-%s-%d{", name, hash, frame)
	if start > 0 {
		buf.WriteString("0%" + hidden)
	}
	buf.WriteString(from.percentage(start, total) + visible)
	if end := start + duration; end < total {
		buf.WriteString(to.percentage(end, total) + ",100%" + hidden)
	} else {
		buf.WriteString("100%" + visible)
	}
	buf.WriteString("}")
}
