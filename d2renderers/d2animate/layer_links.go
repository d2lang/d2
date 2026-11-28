package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"image"
	"io"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

// A fragment identifies the source shape as well as the destination board.
type layerLink struct {
	from, to      int
	shapeID, href string
	legs          []frameLeg
	cameras       []string // One animation for each board visited by the click.
	duration      int
	inner         *d2target.Diagram
	id            string
}

func layerLinks(nav *LayerNavigation, frames []Frame, duration int) []layerLink {
	// A tween sample belongs to the next exact board on the timeline. Use its
	// currently drawn shape as the portal, and always arrive at an exact board.
	destinations := make(map[*d2target.Diagram]int)
	owners := make([]*d2target.Diagram, len(frames))
	var owner *d2target.Diagram
	for i := len(frames) - 1; i >= 0; i-- {
		if !frames[i].TweenSample {
			owner = frames[i].Diagram
			destinations[owner] = i
		}
		owners[i] = owner
	}
	var links []layerLink
	for i, frame := range frames {
		for j := range frame.Diagram.Shapes {
			shape := &frame.Diagram.Shapes[j]
			if shape.Link == "" {
				continue
			}
			board := nav.destination(shape.Link)
			to, ok := destinations[board]
			if !ok || board == nil {
				continue
			}
			legs := indexRoute(nav.Route(owners[i], board), destinations, i, to)
			if len(legs) == 1 && legs[0].zoom.Inward {
				// Enter through the clicked shape, even when several shapes open this layer.
				if shape.Width > 0 && shape.Height > 0 {
					legs[0].zoom.Focus = shape
				} else {
					legs = nil
				}
			}
			var inner *d2target.Diagram
			if len(legs) > 0 {
				inner = nav.boards[owners[i]].layer
				if legs[len(legs)-1].zoom.Inward {
					inner = nav.boards[board].layer
				}
			}
			links = append(links, layerLink{from: i, to: to, shapeID: shape.ID, href: shape.Link, duration: duration, legs: legs, inner: inner})
		}
	}
	return links
}

// Each board on a click route has at most two consecutive camera moves. An
// intermediate board arrives at its ordinary scale and then departs through
// the next portal. Reuse keyframes whenever the two moves have the same geometry.
func writeClickZooms(css *bytes.Buffer, links []layerLink, hash string, viewport image.Rectangle, offsets []image.Point) {
	type camera struct {
		x, y, width, height int
		inward              bool
	}
	geometry := func(z LayerZoom) camera {
		return camera{z.Focus.Pos.X + z.offset.X, z.Focus.Pos.Y + z.offset.Y, z.Focus.Width, z.Focus.Height, z.Inward}
	}
	type track struct {
		arrival, departure camera
		position, legs     int
		origin             int
	}
	keyframes := make(map[track]string)
	for i := range links {
		link := &links[i]
		legs := link.legs
		if len(legs) == 0 {
			continue
		}
		for j := range legs {
			leg := &legs[j]
			leg.zoom = placedZoom(leg.zoom, offsets, leg.from, leg.to)
		}
		link.cameras = make([]string, len(legs)+1)
		for pos := range link.cameras {
			key := track{position: pos, legs: len(legs)}
			if pos > 0 && pos < len(legs) {
				// Consecutive clicks can traverse the same waypoint. Its animation
				// must restart even if the two portal geometries are identical.
				key.origin = link.from
			}
			if pos > 0 {
				key.arrival = geometry(legs[pos-1].zoom)
			}
			if pos < len(legs) {
				key.departure = geometry(legs[pos].zoom)
			}
			name, ok := keyframes[key]
			if !ok {
				name = "d2-click-" + hash + "-" + strconv.Itoa(len(keyframes))
				keyframes[key] = name
				css.WriteString("@keyframes " + name + "{")
				if pos > 1 {
					css.WriteString("0%{opacity:0;visibility:hidden}")
				}
				if pos > 0 {
					z := legs[pos-1].zoom
					writeZoomSamples(css, z, viewport, !z.Inward, float64(pos-1), 1, float64(len(legs)), false)
				}
				if pos < len(legs) {
					z := legs[pos].zoom
					writeZoomSamples(css, z, viewport, z.Inward, float64(pos), 1, float64(len(legs)), false)
				}
				if pos+1 < len(legs) {
					css.WriteString("100%{opacity:0;visibility:hidden}")
				}
				css.WriteByte('}')
			}
			link.cameras[pos] = name
		}
	}
}

type shapeAnchor struct {
	open, class string // Exact D2 anchor markup and the enclosed shape's class.
}

type linkSpan struct {
	start, end, route int
}

// Visit group wrappers, skipping rich text, icon SVGs and other embedded trees
// as opaque XML. Only a canonical anchor enclosing its own shape is rewritten.
func findLayerLinks(frame []byte, targets map[shapeAnchor]int) []linkSpan {
	var matches []linkSpan
	var anchorStart, anchorEnd, anchorDepth int
	depth := 0
	d := xml.NewDecoder(bytes.NewReader(frame))
	for {
		start := int(d.InputOffset())
		token, err := d.Token()
		if err == io.EOF {
			return matches
		}
		if err != nil {
			return nil
		}
		switch el := token.(type) {
		case xml.StartElement:
			isSVG := el.Name.Space == "" || el.Name.Space == "http://www.w3.org/2000/svg"
			switch {
			case isSVG && el.Name.Local == "a" && anchorDepth == 0:
				anchorStart, anchorEnd, anchorDepth = start, int(d.InputOffset()), depth+1
			case isSVG && el.Name.Local == "g" && anchorDepth > 0 && depth == anchorDepth:
				if anchorEnd > 0 {
					class, _, _ := strings.Cut(attrValue(el, "class"), " ")
					if route, ok := targets[shapeAnchor{string(frame[anchorStart:anchorEnd]), class}]; ok {
						matches = append(matches, linkSpan{anchorStart, anchorEnd, route})
					}
					anchorEnd = 0
				}
				if err := d.Skip(); err != nil {
					return nil
				}
				continue
			case isSVG && el.Name.Local == "g", isSVG && depth == 0 && el.Name.Local == "svg":
				// A public caller can wrap native artwork in additional groups.
			default:
				if err := d.Skip(); err != nil {
					return nil
				}
				continue
			}
			depth++
		case xml.EndElement:
			if depth == anchorDepth {
				anchorDepth = 0
			}
			depth--
		}
	}
}

// Only create click routes for anchors present in the rendered fragment.
// layerLinks emits sources in frame order, so each frame needs one lookup.
func bindLayerLinks(frames [][]byte, links []layerLink, hash string) ([][]byte, []layerLink) {
	var updated [][]byte
	for i := 0; i < len(links); {
		frame := links[i].from
		targets := make(map[shapeAnchor]int)
		for ; i < len(links) && links[i].from == frame; i++ {
			link := &links[i]
			class := base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(link.shapeID)))
			targets[shapeAnchor{svg.OpenElement("a", svg.HrefAttributes(link.href)...), class}] = i
		}
		spans := findLayerLinks(frames[frame], targets)
		if len(spans) == 0 {
			continue
		}
		if updated == nil {
			updated = append([][]byte(nil), frames...)
		}
		var out bytes.Buffer
		pos := 0
		for _, span := range spans {
			link := &links[span.route]
			link.id = "d2-layer-link-" + hash + "-" + strconv.Itoa(span.route)
			out.Write(frames[frame][pos:span.start])
			out.WriteString(svg.OpenElement("a", svg.HrefAttributes("#"+link.id)...))
			pos = span.end
		}
		out.Write(frames[frame][pos:])
		updated[frame] = out.Bytes()
	}
	if updated == nil {
		return frames, nil
	}
	bound := links[:0]
	for _, link := range links {
		if link.id != "" {
			bound = append(bound, link)
		}
	}
	return updated, bound
}

// Fragment targets select a layer. Its private sequence starts at the linked
// destination once the camera arrives. Clearing the fragment resumes the
// global tour where the visitor left it.
func writeLayerNavigation(buf *bytes.Buffer, hash string, links []layerLink, slots []layerSlot) {
	marker := "d2-layer-link-" + hash
	allFrames := "." + marker + ":target~.d2-frame-" + hash
	hide := "d2-hide-" + hash
	buf.WriteString(`<style type="text/css"><![CDATA[`)
	writeCSS(buf, allFrames, "{animation-play-state:paused!important;opacity:0!important;transform:none!important;clip-path:none!important;pointer-events:none!important;--d2-link-visibility:hidden!important}")
	// Override the local step clock while the two clicked boards are in
	// motion. The overrides stop at arrival and the local clock takes over.
	const visible = "opacity:1;pointer-events:inherit;--d2-link-visibility:initial"
	const hidden = "opacity:0;pointer-events:none;--d2-link-visibility:hidden"
	writeCSS(buf, "@keyframes ", hide, "{from,to{", hidden, "}}")

	for _, link := range links {
		id := link.id
		selected := "#" + id + ":target~"
		frameSelector := selected + "#d2-frame-" + hash + "-"
		from := frameSelector + strconv.Itoa(link.from)
		to := frameSelector + strconv.Itoa(link.to)
		if len(link.legs) == 0 {
			// Intra-layer links and boards without a drawable portal jump to
			// the authored endpoint. Clearing the fragment resumes playback.
			writeCSS(buf, to, "{opacity:1!important;--d2-link-visibility:initial!important;pointer-events:inherit!important}")
			continue
		}
		// CSS keeps an animation's elapsed time when its name survives a
		// fragment change. Give each visit a fresh hold for its visible boards.
		hold := "d2-hold-" + id
		writeCSS(buf, "@keyframes ", hold, "{from,to{opacity:1;pointer-events:none;--d2-link-visibility:initial}}")
		group := selected + ".d2-layer-" + hash + "-" + strconv.Itoa(slots[link.to].id)
		duration := "var(--d2-click-time," + strconv.Itoa(link.duration) + "ms)"
		writeCSS(buf, from, ",", group, "{opacity:1!important;--d2-link-visibility:initial!important;pointer-events:inherit!important}")
		// Source shows only while the click camera is running.
		writeCSS(buf, from, ">g{", hidden, ";animation:", hold, " ", duration, " step-end}")
		if slot := slots[link.to]; slot.count > 1 {
			writeCSS(buf, group, ">g{animation:var(--d2-slot) calc(", duration, " - ", strconv.Itoa(slot.startMS), "ms) infinite,var(--d2-intro,", hide, ") ", duration, " step-end}")
			writeCSS(buf, to, ">g{--d2-intro:", hold, "}")
		} else {
			writeCSS(buf, to, ">g{", visible, ";animation:", hold, " ", duration, " step-end}")
		}
		for pos, camera := range link.cameras {
			frame := from
			if pos > 0 {
				frame = frameSelector + strconv.Itoa(link.legs[pos-1].to)
			}
			if pos > 0 && pos < len(link.legs) {
				// Paint this ancestor only during transit, without starting its step clock.
				writeCSS(buf, frame, "{opacity:1!important;--d2-link-visibility:initial!important}", frame, ">g{", hidden, ";animation:", hold, " ", duration, " step-end}")
			}
			writeCSS(buf, frame, ">g>g{animation:", camera, " ", duration, " linear both}")
		}
	}
	writeCSS(buf, "@media(prefers-reduced-motion:reduce){", allFrames, "{--d2-click-time:0s}}")
	buf.WriteString(`]]></style>`)
	for _, link := range links {
		writeCSS(buf, `<g id="`, link.id, `" class="`, marker, `"/>`)
	}
}

// Append CSS literals and selectors directly to the output in order.
func writeCSS(buf *bytes.Buffer, parts ...string) {
	for _, part := range parts {
		buf.WriteString(part)
	}
}
