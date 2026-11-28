package d2animate

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/d2lang/d2/d2renderers/d2svg"
)

// A local track uses the same clock and diagram class as the full film.
// The suffix isolates repeated shape indices across independent steps.
type motionSpan struct {
	first, startMS     int
	suffix             string
	accepted, animated map[string]bool
}

type stepArtwork struct {
	first, end          int
	startMS, durationMS int
	art, css            []byte
}

func nativeTimelineMotion(frames []Frame, svgs [][]byte, hash string, total int) (art, css []byte, needsClock bool) {
	if len(frames) != len(svgs) || len(frames) < 3 || !d2svg.UsesDefaultStylesheet() {
		return nativeMotion(frames, svgs, hash, total)
	}
	var steps []stepArtwork
	startMS := 0
	for i := 0; i < len(frames); {
		end := i
		for end < len(frames) && frames[end].TweenSample {
			end++
		}
		// Keep externally supplied, open-ended sample timelines on their
		// existing whole-film clock.
		if end == len(frames) {
			return nativeMotion(frames, svgs, hash, total)
		}
		end++ // Include the authored endpoint and its hold.
		if frames[i].TweenSample && (i > 0 || end < len(frames)) {
			steps = append(steps, stepArtwork{first: i, end: end, startMS: startMS})
		}
		for ; i < end; i++ {
			startMS += frames[i].DurationMS
		}
	}
	if len(steps) == 0 {
		return nativeMotion(frames, svgs, hash, total)
	}
	// Whole-film tracks remain preferable when the same drawing survives every
	// step. A mixed result can also keep a stable object across unrelated edits.
	global := motionSpan{animated: make(map[string]bool)}
	art, css, needsClock = nativeMotionSpan(frames, svgs, hash, total, global)
	if art != nil && !needsClock {
		return
	}
	localClock, gained := false, false
	for i := range steps {
		s := &steps[i]
		span := motionSpan{first: s.first, startMS: s.startMS, suffix: "-step" + strconv.Itoa(s.first), accepted: make(map[string]bool), animated: make(map[string]bool)}
		var clock bool
		s.art, s.css, clock = nativeMotionSpan(frames[s.first:s.end], svgs[s.first:s.end], hash, total, span)
		// Changing which object anchors the paint order must not take smooth
		// motion away from a shape that already had a whole-film track.
		if art != nil {
			for id := range global.animated {
				if !span.accepted[id] {
					return
				}
			}
			if s.art == nil && len(span.animated) > 0 {
				return
			} // The local resource order was unsafe.
		}
		if s.art == nil {
			continue
		}
		localClock = localClock || clock
		for id := range span.animated {
			gained = gained || !global.animated[id] || art == nil
		}
		for _, f := range frames[s.first:s.end] {
			s.durationMS += f.DurationMS
		}
	}
	if !gained {
		return
	}
	var body, styles bytes.Buffer
	at, elapsed := 0, 0
	raw := func(end int) bool {
		for ; at < end; at++ {
			// External styles, links and independently animated or nested artwork
			// can depend on the original full-document DOM. Keep that rendering.
			if _, ok := motionChildren(svgs[at], hash); !ok {
				return false
			}
			for _, shape := range frames[at].Picture().Shapes {
				if len(shape.Classes) != 0 {
					return false
				}
			}
			name := "d2Transition"
			if frames[at].TweenSample {
				localClock = true
			} else {
				// A standalone hold needs one visibility window, even if other steps
				// contain hundreds of native poses.
				name = "d2MotionBoard"
				var next frameStart
				if at+1 < len(frames) {
					next = frames[at+1].start
				}
				writeFrameKeyframes(&styles, name, hash, at, elapsed, frames[at].DurationMS, total, frames[at].start, next, "{opacity:0}", "{opacity:1}")
			}
			fmt.Fprintf(&body, `<g style="animation: %s-%s-%d %dms steps(1,end) infinite">`, name, hash, at, total)
			body.Write(svgs[at])
			body.WriteString("</g>")
			elapsed += frames[at].DurationMS
		}
		return true
	}
	for _, s := range steps {
		if s.art == nil {
			continue
		}
		if !raw(s.first) {
			return
		}
		var next frameStart
		if s.end < len(frames) {
			next = frames[s.end].start
		}
		styles.Write(s.css)
		writeFrameKeyframes(&styles, "d2MotionStep", hash, s.first, s.startMS, s.durationMS, total, frames[s.first].start, next, "{opacity:0}", "{opacity:1}")
		fmt.Fprintf(&body, `<g style="animation:d2MotionStep-%s-%d %dms steps(1,end) infinite">`, hash, s.first, total)
		body.Write(s.art)
		body.WriteString("</g>")
		at = s.end
		elapsed += s.durationMS
	}
	if !raw(len(frames)) {
		return
	}
	return body.Bytes(), styles.Bytes(), localClock
}
