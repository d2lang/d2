package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"image"
	"io"
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
			if shape.Link == "" || shape.Width <= 0 || shape.Height <= 0 {
				continue
			}
			board := nav.destination(shape.Link)
			to, ok := destinations[board]
			if !ok || board == nil || owners[i] == board {
				continue
			}
			legs := indexRoute(nav.Route(owners[i], board), destinations, i, to)
			if len(legs) == 0 {
				continue
			}
			if len(legs) == 1 && legs[0].zoom.Inward {
				// Enter through the clicked shape, even when several shapes open this layer.
				legs[0].zoom.Focus = shape
			}
			inner := nav.boards[owners[i]].layer
			if legs[len(legs)-1].zoom.Inward {
				inner = nav.boards[board].layer
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
	}
	keyframes := make(map[track]string)
	for i := range links {
		link := &links[i]
		legs := link.legs
		for j := range legs {
			leg := &legs[j]
			leg.zoom = placedZoom(leg.zoom, offsets, leg.from, leg.to)
		}
		link.cameras = make([]string, len(legs)+1)
		for pos := range link.cameras {
			key := track{position: pos, legs: len(legs)}
			if pos > 0 {
				key.arrival = geometry(legs[pos-1].zoom)
			}
			if pos < len(legs) {
				key.departure = geometry(legs[pos].zoom)
			}
			name, ok := keyframes[key]
			if !ok {
				name = fmt.Sprintf("d2-click-%s-%d", hash, len(keyframes))
				keyframes[key] = name
				fmt.Fprintf(css, "@keyframes %s{", name)
				if pos > 1 {
					css.WriteString("0%{opacity:0}")
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
					css.WriteString("100%{opacity:0}")
				}
				css.WriteByte('}')
			}
			link.cameras[pos] = name
		}
	}
}

type shapeAnchor struct {
	href, class string
}

type linkSpan struct {
	start, end, route int
}

// Match only the anchor enclosing this shape, preserving other anchors in
// rich text, icons and connections even if they share the same href.
func findLayerLinks(frame []byte, targets map[shapeAnchor]int) []linkSpan {
	type anchor struct {
		depth, start, end int
		href              string
	}
	var candidate anchor
	var matches []linkSpan
	depth := 0
	d := xml.NewDecoder(bytes.NewReader(frame))
	for {
		start := int(d.InputOffset())
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}
		switch t := t.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "a" && candidate.depth == 0 {
				candidate = anchor{depth, start, int(d.InputOffset()), attrValue(t, "href")}
			} else if candidate.depth != 0 && depth == candidate.depth+1 && t.Name.Local == "g" {
				class, _, _ := strings.Cut(attrValue(t, "class"), " ")
				route, ok := targets[shapeAnchor{candidate.href, class}]
				// Only D2's own canonical anchor has these exact attributes. Leave
				// arbitrary inline SVG and author-specified navigation alone.
				if ok && string(frame[candidate.start:candidate.end]) == svg.OpenElement("a", svg.HrefAttributes(candidate.href)...) {
					matches = append(matches, linkSpan{candidate.start, candidate.end, route})
				}
				candidate = anchor{}
			}
		case xml.EndElement:
			if candidate.depth == depth {
				candidate = anchor{}
			}
			depth--
		}
	}
	return matches
}

// Only create click routes for anchors present in the rendered fragment.
func bindLayerLinks(frames [][]byte, links []layerLink, hash string) ([][]byte, []layerLink) {
	targets := make(map[int]map[shapeAnchor]int)
	for i, link := range links {
		class := base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(link.shapeID)))
		if targets[link.from] == nil {
			targets[link.from] = make(map[shapeAnchor]int)
		}
		targets[link.from][shapeAnchor{link.href, class}] = i
	}
	var updated [][]byte
	for i, anchors := range targets {
		spans := findLayerLinks(frames[i], anchors)
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
			link.id = fmt.Sprintf("d2-layer-link-%s-%d", hash, span.route)
			out.Write(frames[i][pos:span.start])
			out.WriteString(svg.OpenElement("a", svg.HrefAttributes("#"+link.id)...))
			pos = span.end
		}
		out.Write(frames[i][pos:])
		updated[i] = out.Bytes()
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
	hold := "d2-hold-" + hash
	hide := "d2-hide-" + hash
	buf.WriteString(`<style type="text/css"><![CDATA[`)
	fmt.Fprintf(buf, "%s{animation-play-state:paused!important;opacity:0!important;transform:none!important;clip-path:none!important;pointer-events:none!important;--d2-link-visibility:hidden!important}", allFrames)
	// Override the local step clock while the two clicked boards are in
	// motion. The overrides stop at arrival and the local clock takes over.
	const visible = "opacity:1;pointer-events:inherit;--d2-link-visibility:initial"
	const hidden = "opacity:0;pointer-events:none;--d2-link-visibility:hidden"
	fmt.Fprintf(buf, "@keyframes %s{from,to{opacity:1;pointer-events:none;--d2-link-visibility:initial}}@keyframes %s{from,to{%s}}", hold, hide, hidden)

	for _, link := range links {
		id := link.id
		from := fmt.Sprintf("#%s:target~#d2-frame-%s-%d", id, hash, link.from)
		to := fmt.Sprintf("#%s:target~#d2-frame-%s-%d", id, hash, link.to)
		group := fmt.Sprintf("#%s:target~.d2-layer-%s-%d", id, hash, slots[link.to].id)
		duration := fmt.Sprintf("var(--d2-click-time,%dms)", link.duration)
		fmt.Fprintf(buf, "%s,%s{opacity:1!important;--d2-link-visibility:initial!important;pointer-events:inherit!important}", from, group)
		// Source shows only while the click camera is running.
		fmt.Fprintf(buf, "%s>g{%s;animation:%s %s step-end}", from, hidden, hold, duration)
		if slot := slots[link.to]; slot.count > 1 {
			fmt.Fprintf(buf, "%s>g{animation:var(--d2-slot) calc(%s - %dms) infinite,var(--d2-intro,%s) %s step-end}", group, duration, slot.startMS, hide, duration)
			fmt.Fprintf(buf, "%s>g{--d2-intro:%s}", to, hold)
		} else {
			fmt.Fprintf(buf, "%s>g{%s;animation:%s %s step-end}", to, visible, hold, duration)
		}
		for pos, camera := range link.cameras {
			frame := from
			if pos > 0 {
				frame = fmt.Sprintf("#%s:target~#d2-frame-%s-%d", id, hash, link.legs[pos-1].to)
			}
			if pos > 0 && pos < len(link.legs) {
				// Paint this ancestor only during transit, without starting its step clock.
				fmt.Fprintf(buf, "%s{opacity:1!important;--d2-link-visibility:initial!important}%s>g{%s;animation:%s %s step-end}", frame, frame, hidden, hold, duration)
			}
			fmt.Fprintf(buf, "%s>g>g{animation:%s %s linear both}", frame, camera, duration)
		}
	}
	fmt.Fprintf(buf, "@media(prefers-reduced-motion:reduce){%s{--d2-click-time:0s}}", allFrames)
	buf.WriteString(`]]></style>`)
	for _, link := range links {
		fmt.Fprintf(buf, `<g id="%s" class="%s"/>`, link.id, marker)
	}
}
