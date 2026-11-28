package d2animate

import (
	"bytes"
	"fmt"
	"image"
	"strconv"

	"github.com/d2lang/d2/d2target"
)

type frameLeg struct {
	from, to int
	zoom     LayerZoom
}

// A caller can export a subset or use a tween sample as the click origin.
// Only intermediate waypoints must be looked up among exact rendered frames.
func indexRoute(route []LayerLeg, exact map[*d2target.Diagram]int, from, to int) []frameLeg {
	if len(route) == 0 {
		return nil
	}
	legs := make([]frameLeg, 0, len(route))
	for i, leg := range route {
		next := to
		if i < len(route)-1 {
			var ok bool
			next, ok = exact[leg.To]
			if !ok {
				return nil
			}
		}
		legs = append(legs, frameLeg{from, next, leg.Zoom})
		from = next
	}
	return legs
}

// Key by the outgoing frame, including the wrap to the first frame. A step's
// own tween already animates its entrance and keeps its original clock.
func playbackZooms(nav *LayerNavigation, frames []Frame) map[int][]frameLeg {
	exact := make(map[*d2target.Diagram]int)
	for i, frame := range frames {
		if !frame.TweenSample {
			exact[frame.Diagram] = i
		}
	}
	routes := make(map[int][]frameLeg)
	for i, from := range frames {
		j := (i + 1) % len(frames)
		to := frames[j]
		if from.TweenSample || to.TweenSample || len(to.Diagram.TweenFrames) > 0 {
			continue
		}
		route := nav.Route(from.Diagram, to.Diagram)
		if legs := indexRoute(route, exact, i, j); len(legs) > 0 {
			routes[i] = legs
		}
	}
	return routes
}

func writePlaybackZooms(buf *bytes.Buffer, hash string, durations []int, totalMS, zoomMS int, viewport image.Rectangle, offsets []image.Point, routes map[int][]frameLeg) {
	// Collect each board's visits in playback order. A board can appear in
	// its authored slot and briefly between branches elsewhere in the tour.
	type event struct {
		at, duration float64
		zoom         LayerZoom
		parent       bool
		visible      bool
	}
	events := make(map[int][]event)
	for _, legs := range routes {
		for _, leg := range legs {
			events[leg.from], events[leg.to] = nil, nil
		}
	}
	start := 0
	for i, duration := range durations {
		if _, ok := events[i]; ok {
			events[i] = append(events[i], event{at: float64(start), visible: true})
		}
		end := start + duration
		legs := routes[i]
		for j, leg := range legs {
			// Split the single configured travel window, even when it contains
			// more hops than whole milliseconds. There is no stop at a waypoint.
			at := float64(end-zoomMS) + float64(zoomMS)*float64(j)/float64(len(legs))
			length := float64(zoomMS) / float64(len(legs))
			zoom := placedZoom(leg.zoom, offsets, leg.from, leg.to)
			events[leg.from] = append(events[leg.from], event{at: at, duration: length, zoom: zoom, parent: zoom.Inward})
			events[leg.to] = append(events[leg.to], event{at: at, duration: length, zoom: zoom, parent: !zoom.Inward})
		}
		if _, ok := events[i]; ok && len(legs) == 0 && end < totalMS {
			events[i] = append(events[i], event{at: float64(end)})
		}
		start = end
	}

	// The ordinary step-end keyframes remain in effect for reduced motion.
	buf.WriteString("@media (prefers-reduced-motion:no-preference){")
	const hidden = "{opacity:0;pointer-events:none;--d2-link-visibility:hidden}"
	const visible = "{opacity:1;transform:none;pointer-events:inherit;--d2-link-visibility:initial}"
	lastVisible := len(durations) - 1
	if len(routes[lastVisible]) > 0 {
		lastVisible = 0
	}
	for i := range durations {
		track, ok := events[i]
		if !ok {
			continue
		}
		fmt.Fprintf(buf, "@keyframes d2Transition-%s-%d{0%%%s", hash, i, hidden)
		for _, e := range track {
			if e.zoom.Focus != nil {
				writeZoomSamples(buf, e.zoom, viewport, e.parent, e.at, e.duration, float64(totalMS), true)
				continue
			}
			state := hidden
			if e.visible {
				state = visible
			}
			buf.WriteString(percentage(100*e.at/float64(totalMS), 9) + state)
		}
		// Explicit endpoints prevent CSS from interpolating the last hidden
		// board back to its unanimated (visible) style as the clock wraps.
		ending := hidden
		if i == lastVisible {
			ending = visible
		}
		buf.WriteString("100%" + ending + "}")
	}
	buf.WriteString("}")
}

func writeZoomSamples(buf *bytes.Buffer, zoom LayerZoom, viewport image.Rectangle, parent bool, start, duration, total float64, playback bool) {
	// The camera moves along a line within each phase: the parent pans, then
	// the two boards scale and crossfade. Let the browser interpolate just
	// these three transforms rather than writing a copy per display frame.
	split := layerPanPhase
	firstEase, lastEase := "cubic-bezier(.333333,.666667,.666667,1)", layerInExpo
	if !zoom.Inward {
		split = 1 - layerPanPhase
		firstEase, lastEase = layerOutExpo, "cubic-bezier(.333333,0,.666667,.333333)"
		if !parent {
			split = layerShrinkPhase
		}
	}
	poseAt := func(t float64) LayerPose {
		p, c := zoom.Poses(viewport, t)
		if parent {
			return p
		}
		return c
	}
	at := func(t float64) string {
		return percentage(100*(start+duration*t)/total, 9)
	}

	for _, k := range []struct {
		t    float64
		ease string
	}{{0, firstEase}, {split, lastEase}, {1, ""}} {
		p := poseAt(k.t)
		scale := poseNumber(p.Scale)
		fmt.Fprintf(buf, "%s{transform:matrix(%s,0,0,%s,%s,%s);opacity:%s", at(k.t), scale, scale, poseNumber(p.DX), poseNumber(p.DY), poseNumber(p.Opacity))
		if k.ease != "" {
			buf.WriteString(";animation-timing-function:" + k.ease)
		}
		buf.WriteString("}")
	}

	if !playback {
		return // Click navigation handles interaction on its own clock.
	}

	// Link visibility also controls the linked shape's paint. Switch the
	// discrete interaction properties independently from the opacity curve.
	const steps = 30
	previous := false
	for i := 0; i <= steps; i++ {
		t := float64(i) / steps
		visible := poseAt(t).Opacity > 0
		next := i < steps && poseAt(float64(i+1)/steps).Opacity > 0
		if i == 0 || i == steps || visible != previous || visible != next {
			buf.WriteString(at(t))
			if visible {
				buf.WriteString("{pointer-events:inherit;--d2-link-visibility:initial}")
			} else {
				buf.WriteString("{pointer-events:none;--d2-link-visibility:hidden}")
			}
		}
		previous = visible
	}
}

func poseNumber(n float64) string {
	return strconv.FormatFloat(n, 'g', 8, 64)
}

var layerInExpo = layerExpoEasing(true)
var layerOutExpo = layerExpoEasing(false)

func layerExpoEasing(inward bool) string {
	// CSS linear() accepts evenly spaced stops. These are the same Expo
	// samples used by the GIF at its nominal 30fps for the 600ms zoom phase.
	var curve bytes.Buffer
	curve.WriteString("linear(")
	const stops = 18
	for i := 0; i <= stops; i++ {
		t := float64(i) / stops
		v := studioExpo(t)
		if !inward {
			v = 1 - studioExpo(1-t)
		}
		if i != 0 {
			curve.WriteByte(',')
		}
		curve.WriteString(strconv.FormatFloat(v, 'g', 5, 64))
	}
	curve.WriteByte(')')
	return curve.String()
}
