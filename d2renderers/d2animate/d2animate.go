package d2animate

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2sketch"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/internal/svgframe"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/version"
)

var transitionDurationMS = 1

func makeKeyframe(delayMS, durationMS, totalMS, identifier int, diagramHash string) string {
	percentageBefore := (math.Max(0, float64(delayMS-transitionDurationMS)) / float64(totalMS)) * 100.
	percentageStart := (float64(delayMS) / float64(totalMS)) * 100.
	percentageEnd := (float64(delayMS+durationMS-transitionDurationMS) / float64(totalMS)) * 100.
	if int(math.Ceil(percentageEnd)) == 100 {
		return fmt.Sprintf(`@keyframes d2Transition-%s-%d {
		0%%, %f%% {
				opacity: 0;
		}
		%f%%, %f%% {
				opacity: 1;
		}
}`, diagramHash, identifier, percentageBefore, percentageStart, math.Ceil(percentageEnd))
	}

	percentageAfter := (float64(delayMS+durationMS) / float64(totalMS)) * 100.
	return fmt.Sprintf(`@keyframes d2Transition-%s-%d {
		0%%, %f%% {
				opacity: 0;
		}
		%f%%, %f%% {
				opacity: 1;
		}
		%f%%, 100%% {
				opacity: 0;
		}
}`, diagramHash, identifier, percentageBefore, percentageStart, percentageEnd, percentageAfter)
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

	fitToScreenWrapperOpening := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?><svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" d2Version="%s" preserveAspectRatio="xMinYMin meet" viewBox="0 0 %d %d"%s>`,
		version.Version,
		width, height, dimensions,
	)
	fmt.Fprint(buf, fitToScreenWrapperOpening)

	innerOpening := fmt.Sprintf(`<svg class="d2-svg" width="%d" height="%d" viewBox="%d %d %d %d">`,
		width, height, left, top, width, height)
	fmt.Fprint(buf, innerOpening)

	diagramHash, err := rootDiagram.HashID(renderOpts.Salt)
	if err != nil {
		return nil, err
	}

	defs, svgs := shareStaticShapes(rootDiagram, svgs, diagramHash)
	var source strings.Builder
	source.Write(defs)
	for _, frame := range svgs {
		source.Write(frame)
		source.WriteByte(' ')
	}
	svgsStr := source.String()

	if err := d2svg.WriteAnimationStyles(buf, diagramHash, svgsStr, rootDiagram, &renderOpts); err != nil {
		return nil, err
	}

	if renderOpts.Sketch != nil && *renderOpts.Sketch {
		d2sketch.DefineFillPatterns(buf, diagramHash)
	}

	fmt.Fprint(buf, `<style type="text/css"><![CDATA[`)
	startMS := 0
	for i, duration := range durationsMS {
		if uniform && len(svgs) <= 100 {
			fmt.Fprint(buf, makeKeyframe(startMS, duration, totalMS, i, diagramHash))
		} else {
			// Use a discrete cut at exact boundaries. A 1 ms cross-fade or a
			// whole-percent rounding error can hide an entire short sample.
			fmt.Fprintf(buf, "@keyframes d2Transition-%s-%d {", diagramHash, i)
			if startMS > 0 {
				fmt.Fprint(buf, "0%{opacity:0}")
			}
			fmt.Fprintf(buf, "%.9f%%{opacity:1}", 100*float64(startMS)/float64(totalMS))
			if endMS := startMS + duration; endMS < totalMS {
				fmt.Fprintf(buf, "%.9f%%,100%%{opacity:0}", 100*float64(endMS)/float64(totalMS))
			} else {
				fmt.Fprint(buf, "100%{opacity:1}")
			}
			fmt.Fprint(buf, "}")
		}
		startMS += duration
	}
	fmt.Fprint(buf, `]]></style>`)

	buf.Write(defs)

	timing := ""
	if !uniform || len(svgs) > 100 {
		timing = " steps(1,end)"
	}
	for i, svg := range svgs {
		str := string(svg)
		str = strings.Replace(str, "<g", fmt.Sprintf(`<g style="animation: d2Transition-%s-%d %dms%s infinite"`, diagramHash, i, totalMS, timing), 1)
		buf.Write([]byte(str))
	}

	fmt.Fprint(buf, "</svg>")
	fmt.Fprint(buf, "</svg>")

	return buf.Bytes(), nil
}
