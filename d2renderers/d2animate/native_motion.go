package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2renderers/d2svgimport"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/animation"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/svg"
)

type motionChild struct {
	start, end int
	id         string
	resources  []string
	tag        string
}

// Board groups only carry sizing metadata. A nested SVG, authored stylesheet,
// interactive object or local animation keeps its original DOM and clocks.
func motionChildren(frame []byte, hash string) ([]motionChild, bool) {
	d := xml.NewDecoder(bytes.NewReader(frame))
	t, err := d.Token()
	el, ok := t.(xml.StartElement)
	if err != nil || !ok || el.Name != (xml.Name{Local: "g"}) || strings.TrimSpace(attrValue(el, "class")) != hash {
		return nil, false
	}
	for _, a := range el.Attr {
		switch a.Name.Local {
		case "class", "width", "height", "viewBox":
		default:
			return nil, false
		}
	}
	var children []motionChild
	for {
		start := int(d.InputOffset())
		t, err := d.Token()
		if err != nil {
			return nil, false
		}
		switch el := t.(type) {
		case xml.StartElement:
			c := motionChild{start: start, tag: el.Name.Local}
			if el.Name == (xml.Name{Local: "g"}) {
				c.id = attrValue(el, "class")
			}
			// Read this direct child to its closing tag without revisiting it.
			depth := 0
			for {
				switch node := t.(type) {
				case xml.StartElement:
					if depth >= 128 || !safePaintElement(node) || node.Name.Local == "svg" || node.Name.Local == "use" {
						return nil, false
					}
					if id := attrValue(node, "id"); id != "" {
						c.resources = append(c.resources, id)
					}
					depth++
				case xml.EndElement:
					depth--
				case xml.CharData:
				default:
					return nil, false
				}
				if depth == 0 {
					break
				}
				t, err = d.Token()
				if err != nil {
					return nil, false
				}
			}
			c.end = int(d.InputOffset())
			children = append(children, c)
		case xml.EndElement:
			return children, len(bytes.TrimSpace(frame[d.InputOffset():])) == 0
		default:
			return nil, false
		}
	}
}

type motionPaint struct{ selector, property, value string }
type motionPose struct {
	x, y       float64
	continuous bool
	paint      []motionPaint
}

// Use the actual rendered tree as the test of compatibility. Normalize absolute
// coordinates to the shape's integer layout origin. All remaining geometry,
// labels, element order and theme classes must match exactly.
func readMotionShape(raw []byte, shape *d2target.Shape) (motionPose, string, bool) {
	x, y := float64(shape.Pos.X), float64(shape.Pos.Y)
	pose := motionPose{x: x, y: y, continuous: shape.MotionOffset != nil}
	d := xml.NewDecoder(bytes.NewReader(raw))
	var signature bytes.Buffer
	enc := xml.NewEncoder(&signature)
	var path []string
	var childCounts []int
	for {
		t, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return pose, "", false
		}
		switch el := t.(type) {
		case xml.StartElement:
			outer := len(path) == 0
			selector := ">:first-child"
			if !outer {
				i := len(path) - 1
				childCounts[i]++
				selector = path[i] + ">:nth-child(" + strconv.Itoa(childCounts[i]) + ")"
			}
			path = append(path, selector)
			childCounts = append(childCounts, 0)
			if outer {
				if el.Name != (xml.Name{Local: "g"}) {
					return pose, "", false
				}
				opacity := "1"
				for _, a := range el.Attr {
					switch a.Name.Local {
					case "class":
					case "style":
						value, ok := strings.CutPrefix(a.Value, "opacity:")
						if !ok {
							return pose, "", false
						}
						v, e := strconv.ParseFloat(value, 64)
						if e != nil || !(v >= 0 && v <= 1) {
							return pose, "", false
						}
						opacity = svg.FormatFloat(v)
					case "transform":
						args, ok := strings.CutPrefix(a.Value, "translate(")
						if !ok || !strings.HasSuffix(args, ")") {
							return pose, "", false
						}
						p := strings.Fields(strings.TrimSuffix(args, ")"))
						if len(p) != 2 {
							return pose, "", false
						}
						dx, e1 := strconv.ParseFloat(p[0], 64)
						dy, e2 := strconv.ParseFloat(p[1], 64)
						if e1 != nil || e2 != nil || math.IsNaN(dx+dy) || math.IsInf(dx+dy, 0) {
							return pose, "", false
						}
						pose.x += dx
						pose.y += dy
					default:
						return pose, "", false
					}
				}
				pose.paint = append(pose.paint, motionPaint{selector, "opacity", opacity})
				el.Attr = nil
			} else {
				if !plainShapeElement(el) {
					return pose, "", false
				}
				// A missing coordinate on a primitive defaults to zero, whereas a
				// tspan can intentionally inherit its current text position.
				switch el.Name.Local {
				case "rect", "text":
					if attrValue(el, "x") == "" || attrValue(el, "y") == "" {
						return pose, "", false
					}
				case "ellipse":
					if attrValue(el, "cx") == "" || attrValue(el, "cy") == "" {
						return pose, "", false
					}
				}
				class := attrValue(el, "class")
				for i, a := range el.Attr {
					switch a.Name.Local {
					case "x", "cx", "y", "cy":
						v, e := strconv.ParseFloat(a.Value, 64)
						if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
							return pose, "", false
						}
						if a.Name.Local == "x" || a.Name.Local == "cx" {
							v -= x
						} else {
							v -= y
						}
						el.Attr[i].Value = svg.FormatFloat(v)
					case "d":
						p, ok := relativeMotionPath(a.Value, x, y)
						if !ok {
							return pose, "", false
						}
						el.Attr[i].Value = p
					case "stroke-width":
						if v, ok := motionStrokeWidth(a.Value); ok {
							pose.paint = append(pose.paint, motionPaint{selector, "stroke-width", v})
							el.Attr[i].Value = "#"
						}
					case "style":
						// D2 writes ordinary shape strokes as inline style declarations.
						// Keep all other declarations in the compatibility signature.
						if strings.Contains(a.Value, "stroke-width:") && attrValue(el, "stroke-width") == "" {
							parts := strings.Split(a.Value, ";")
							for j, part := range parts {
								name, value, _ := strings.Cut(part, ":")
								if name == "stroke-width" {
									if v, ok := motionStrokeWidth(value); ok {
										pose.paint = append(pose.paint, motionPaint{selector, name, v})
										parts[j] = "stroke-width:#"
									}
								}
							}
							el.Attr[i].Value = strings.Join(parts, ";")
						}
					case "fill", "stroke":
						// Only literal paints independent of a theme rule can be
						// animated without overriding the light/dark stylesheet.
						if !strings.Contains(class, a.Name.Local+"-") {
							if c, ok := color.ParseRGBA(a.Value); ok {
								value := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
								if c.A != 255 {
									value += fmt.Sprintf("%02x", c.A)
								}
								pose.paint = append(pose.paint, motionPaint{selector, a.Name.Local, value})
								el.Attr[i].Value = "#"
							}
						}
					}
				}
			}
			t = el
		case xml.EndElement:
			path = path[:len(path)-1]
			childCounts = childCounts[:len(childCounts)-1]
		case xml.CharData:
		default:
			return pose, "", false
		}
		if enc.EncodeToken(t) != nil {
			return pose, "", false
		}
	}
	if enc.Flush() != nil {
		return pose, "", false
	}
	return pose, signature.String(), true
}

func motionStrokeWidth(value string) (string, bool) {
	v, err := strconv.ParseFloat(strings.TrimSuffix(value, "px"), 64)
	if err != nil || !(v >= 0) || math.IsInf(v, 0) {
		return "", false
	}
	return svg.FormatFloat(v) + "px", true
}

func relativeMotionPath(data string, x, y float64) (string, bool) {
	commands, err := d2svgimport.ParsePath(nil, "shape", data, d2svgimport.PathLimits{MaxBytes: len(data), MaxCommands: len(data)})
	if err != nil {
		return "", false
	}
	var out strings.Builder
	point := func(p d2scene.Point) {
		out.WriteByte(' ')
		out.WriteString(svg.FormatFloat(p.X - x))
		out.WriteByte(',')
		out.WriteString(svg.FormatFloat(p.Y - y))
	}
	for _, c := range commands {
		out.WriteByte(byte('A' + c.Kind))
		switch c.Kind {
		case d2scene.ArcCommand:
			fmt.Fprintf(&out, " %g %g %g %t %t", c.RadiusX, c.RadiusY, c.Rotation, c.LargeArc, c.Sweep)
			point(c.P1)
		case d2scene.CubicCommand:
			point(c.P1)
			point(c.P2)
			point(c.P3)
		case d2scene.QuadraticCommand:
			point(c.P1)
			point(c.P2)
		case d2scene.MoveCommand, d2scene.LineCommand:
			point(c.P1)
		}
	}
	return out.String(), true
}

type motionTick struct {
	at    float64
	pct   string
	tween bool
	// Only an authored arrival supplies the duration of its incoming motion.
	incomingMS int
}
type motionValue struct {
	x, y    float64
	literal string
}

func (v motionValue) css() string {
	if v.literal != "" {
		return v.literal
	}
	return "translate(" + svg.FormatFloat(v.x) + "px," + svg.FormatFloat(v.y) + "px)"
}

// cubicMotionMidpoint recognizes the compiler's rest-to-rest curve from its
// full sampled geometry. A midpoint alone cannot distinguish it from a line.
func cubicMotionMidpoint(count int, sample func(int) (fraction, x, y float64), tolerance float64) int {
	if count < 5 {
		return -1
	}
	t0, x0, y0 := sample(0)
	t1, x1, y1 := sample(count - 1)
	if t0 != 0 || t1 != 1 || math.Hypot(x1-x0, y1-y0) <= tolerance {
		return -1
	}
	mid := -1
	left, right := false, false
	prev := t0
	for i := 1; i < count-1; i++ {
		t, x, y := sample(i)
		if !(t > prev && t < 1) {
			return -1
		}
		u := animation.EaseInOut(t)
		if !(math.Abs(x-(x0+(x1-x0)*u)) <= tolerance && math.Abs(y-(y0+(y1-y0)*u)) <= tolerance) {
			return -1
		}
		switch {
		case math.Abs(t-.5) <= 1e-12:
			mid = i
		case t < .5:
			left = true
		default:
			right = true
		}
		prev = t
	}
	if !left || !right {
		return -1
	}
	return mid
}

// Preserve every authored boundary. A verified cubic needs a timing function
// on each half; linear and other sampled motions retain their own timings.
func writeMotionProperty(css *bytes.Buffer, name, property string, clock []motionTick, values []motionValue, smooth []bool, reduced bool) {
	fmt.Fprintf(css, "@keyframes %s{", name)
	writeKey := func(i int, timing string) {
		fmt.Fprintf(css, "%s{%s:%s", clock[i].pct, property, values[i].css())
		if timing != "" {
			css.WriteString(";animation-timing-function:" + timing)
		}
		css.WriteByte('}')
	}
	prev := 0
	for i := 0; i < len(values); i++ {
		v := values[i]
		if property == "transform" && !reduced && clock[i].tween && (i == 0 || !clock[i-1].tween) {
			end := i
			for end < len(clock) && smooth[end] {
				end++
			}
			// With no final hold the timeline intentionally shows the endpoint
			// before the last sample period is over. Preserve that exact cut.
			if end < len(clock) && clock[end].incomingMS > 0 && clock[end].at-clock[i].at == float64(clock[end].incomingMS) {
				mid := cubicMotionMidpoint(end-i+1, func(j int) (float64, float64, float64) {
					k := i + j
					return (clock[k].at - clock[i].at) / float64(clock[end].incomingMS), values[k].x, values[k].y
				}, 0.000002) // Rendered SVG coordinates have six fractional digits.
				if mid >= 0 {
					writeKey(i, "cubic-bezier(.3333333333,0,.6666666667,0)")
					writeKey(i+mid, "cubic-bezier(.3333333333,1,.6666666667,1)")
					prev = i + mid
					i = end - 1
					continue
				}
			}
		}
		if reduced && i > 0 && i+1 < len(values) && clock[i-1].tween && clock[i].tween {
			continue
		}
		if !reduced && i > 0 && i+1 < len(values) && smooth[i-1] && smooth[i] {
			p, n := values[prev], values[i+1]
			if p == v && v == n {
				continue
			}
			if v.literal == "" {
				ratio := (clock[i].at - clock[prev].at) / (clock[i+1].at - clock[prev].at)
				if math.Abs(v.x-(p.x+(n.x-p.x)*ratio)) < 0.000002 && math.Abs(v.y-(p.y+(n.y-p.y)*ratio)) < 0.000002 {
					continue
				}
			}
		}
		var timing string
		if !reduced && !smooth[i] && i+1 < len(values) {
			timing = "steps(1,end)"
		}
		writeKey(i, timing)
		prev = i
	}
	css.WriteByte('}')
}

func writeNativeShape(css *bytes.Buffer, prefix string, clock []motionTick, poses []motionPose, total int) bool {
	first := poses[0]
	var selectors []string
	rules := map[string][]string{}
	reduced := map[string][]string{}
	for prop := -1; prop < len(first.paint); prop++ {
		property := "transform"
		selector := ""
		values := make([]motionValue, len(clock))
		changed := false
		smooth := make([]bool, len(clock))
		for i, p := range poses {
			smooth[i] = clock[i].tween && (prop >= 0 || p.continuous || (i+1 < len(poses) && clock[i+1].tween && poses[i+1].continuous))
			if prop < 0 {
				values[i] = motionValue{x: p.x - first.x, y: p.y - first.y}
			} else {
				paint := p.paint[prop]
				property, selector = paint.property, paint.selector
				values[i] = motionValue{literal: paint.value}
			}
			if values[i] != values[0] {
				changed = true
			}
		}
		if !changed {
			continue
		}
		values[len(poses)] = values[len(poses)-1]
		name := prefix + "-" + strconv.Itoa(prop+1)
		duration := strconv.Itoa(total) + "ms"
		writeMotionProperty(css, name, property, clock, values, smooth, false)
		writeMotionProperty(css, name+"-still", property, clock, values, smooth, true)
		if _, ok := rules[selector]; !ok {
			selectors = append(selectors, selector)
		}
		rules[selector] = append(rules[selector], name+" "+duration+" linear infinite")
		reduced[selector] = append(reduced[selector], name+"-still "+duration+" steps(1,end) infinite")
	}
	for _, selector := range selectors {
		fmt.Fprintf(css, ".%s%s{animation:%s}@media(prefers-reduced-motion:reduce){.%s%s{animation:%s}}", prefix, selector, strings.Join(rules[selector], ","), prefix, selector, strings.Join(reduced[selector], ","))
	}
	return len(selectors) > 0
}

// Stable anchors partition the original paint order. Content between them can
// enter, disappear, change topology or reorder without invalidating their motion.
func nativeMotion(frames []Frame, svgs [][]byte, hash string, total int) (art, styles []byte, needsFrameClock bool) {
	return nativeMotionSpan(frames, svgs, hash, total, motionSpan{})
}

func nativeMotionSpan(frames []Frame, svgs [][]byte, hash string, total int, span motionSpan) (art, styles []byte, needsFrameClock bool) {
	if len(frames) < 2 || len(frames) != len(svgs) || !d2svg.UsesDefaultStylesheet() || !slices.ContainsFunc(frames, func(f Frame) bool { return f.TweenSample }) {
		return
	}
	rows := make([][]motionChild, len(frames))
	positions := make([]map[string]int, len(frames))
	shapes := make([]map[string]*d2target.Shape, len(frames))
	clock := make([]motionTick, len(frames)+1)
	ms := span.startMS
	for i, frame := range frames {
		raw := svgs[i]

		var ok bool
		rows[i], ok = motionChildren(raw, hash)
		if !ok {
			return nil, nil, false
		}
		positions[i] = map[string]int{}
		for j, c := range rows[i] {
			if c.id == "" {
				continue
			}
			if _, ok := positions[i][c.id]; ok {
				positions[i][c.id] = -1
			} else {
				positions[i][c.id] = j
			}
		}
		shapes[i] = map[string]*d2target.Shape{}
		for j := range frame.Picture().Shapes {
			s := &frame.Picture().Shapes[j]
			if len(s.Classes) != 0 {
				return nil, nil, false
			}
			if s.Link != "" || s.Tooltip != "" || s.Animated || s.Icon != nil {
				continue
			}
			shapes[i][base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(s.ID)))] = s
		}
		clock[i] = motionTick{at: float64(ms) + float64(frame.start.fraction)/float64(time.Millisecond), pct: frame.start.percentage(ms, total), tween: frame.TweenSample}
		if !frame.TweenSample {
			clock[i].incomingMS = frame.Diagram.TweenDurationMS
		}
		ms += frame.DurationMS
	}
	clock[len(frames)] = motionTick{at: float64(total), pct: "100%"}
	var body, css bytes.Buffer
	fmt.Fprintf(&body, `<g class="%s">`, hash)
	cursors := make([]int, len(frames))
	var animated bool
	var spans [][2]int
	writeGap := func(raw []byte) {
		if len(raw) > 0 {
			start := body.Len()
			body.Write(raw)
			spans = append(spans, [2]int{start, body.Len()})
		}
	}
	// A fragment ID resolves to the first definition in document order. Never
	// move competing definitions across each other when splitting frame artwork.
	resourceOwner := map[string]int{}
	orderSafe := true
	gapIndex := 0
	gap := func(ends []int) {
		gapIndex++
		chunks := make([][]byte, len(frames))
		defsOnly := true
		for i, row := range rows {
			for _, c := range row[cursors[i]:ends[i]] {
				if c.tag != "mask" && c.tag != "defs" && c.tag != "marker" {
					defsOnly = false
				}
				for _, id := range c.resources {
					if owner, ok := resourceOwner[id]; ok && owner != gapIndex {
						orderSafe = false
					}
					resourceOwner[id] = gapIndex
				}
			}
			if cursors[i] < ends[i] {
				chunks[i] = svgs[i][row[cursors[i]].start:row[ends[i]-1].end]
			}
		}
		if !slices.ContainsFunc(chunks, func(c []byte) bool { return !bytes.Equal(c, chunks[0]) }) {
			writeGap(chunks[0])
			return
		}
		if defsOnly {
			for _, c := range chunks {
				writeGap(c)
			}
			return
		}
		needsFrameClock = true
		for i, c := range chunks {
			if len(c) == 0 {
				continue
			}
			fmt.Fprintf(&body, `<g style="animation: d2Transition-%s-%d %dms steps(1,end) infinite">`, hash, i+span.first, total)
			writeGap(c)
			body.WriteString("</g>")
		}
	}
	wires := collectWireTracks(frames, svgs, rows, positions, shapes)
	for index, child := range rows[0] {
		id := child.id
		if wire, ok := wires[id]; ok {
			ordered := true
			for i, k := range wire.indices {
				if k < cursors[i] {
					ordered = false
					break
				}
			}
			if !ordered {
				continue
			}
			gap(wire.indices)
			name := "d2Wire-" + hash + span.suffix + "-" + strconv.Itoa(index)
			wire.write(&body, &css, writeGap, svgs, rows, clock, name, hash, span.first, total)
			gapIndex++
			for i, k := range wire.indices {
				for _, id := range rows[i][k].resources {
					if owner, ok := resourceOwner[id]; ok && owner != gapIndex {
						orderSafe = false
					}
					resourceOwner[id] = gapIndex
				}
				cursors[i] = k + 1
			}
			if span.accepted != nil {
				span.accepted[id] = true
			}
			if span.animated != nil {
				span.animated[id] = true
			}
			animated, needsFrameClock = true, true
			continue
		}
		if shapes[0][id] == nil {
			continue
		}
		indices := make([]int, len(frames))
		poses := make([]motionPose, len(frames))
		var signature string
		raw := svgs[0][child.start:child.end]
		compatible := true
		for i, row := range rows {
			k, ok := positions[i][id]
			s := shapes[i][id]
			if !ok || s == nil || k < cursors[i] {
				compatible = false
				break
			}
			indices[i] = k
			markup := svgs[i][row[k].start:row[k].end]
			if i > 0 && bytes.Equal(markup, raw) {
				poses[i] = poses[0]
				poses[i].continuous = s.MotionOffset != nil
				continue
			}
			p, sig, ok := readMotionShape(markup, s)
			if !ok || (i > 0 && sig != signature) {
				compatible = false
				break
			}
			if i == 0 {
				signature = sig
			}
			poses[i] = p
		}
		if !compatible {
			continue
		}
		gap(indices)
		prefix := "d2Motion-" + hash + span.suffix + "-" + strconv.Itoa(index)
		if span.accepted != nil {
			span.accepted[id] = true
		}
		if writeNativeShape(&css, prefix, clock, poses, total) {
			if span.animated != nil {
				span.animated[id] = true
			}
			fmt.Fprintf(&body, `<g class="%s">`, prefix)
			body.Write(raw)
			body.WriteString("</g>")
			animated = true
		} else {
			body.Write(raw)
		}
		for i := range cursors {
			cursors[i] = indices[i] + 1
		}
	}
	if !animated {
		return nil, nil, false
	}
	ends := make([]int, len(frames))
	for i := range ends {
		ends[i] = len(rows[i])
	}
	gap(ends)
	if !orderSafe {
		return nil, nil, false
	}
	body.WriteString("</g>")
	out := body.Bytes()
	// A resource in a local span may be referenced by a later board. Retain
	// its original ID and definition when only part of the film is visible here.
	if needsFrameClock && span.suffix == "" {
		out = compactMotionGaps(out, spans, hash)
	}
	return out, css.Bytes(), needsFrameClock
}

// Compact all fallback regions in paint order, so SVG's first-ID lookup and
// pooling of shared masks and arrowheads work exactly as in whole-frame output.
func compactMotionGaps(art []byte, spans [][2]int, hash string) []byte {
	open := []byte(`<g class="` + hash + `">`)
	close := []byte("</g>")
	fragments := make([][]byte, len(spans))
	for i, s := range spans {
		f := make([]byte, 0, len(open)+s[1]-s[0]+len(close))
		f = append(f, open...)
		f = append(f, art[s[0]:s[1]]...)
		fragments[i] = append(f, close...)
	}
	defs, reduced := shareFrameGraphics(fragments, hash)
	if len(reduced) == 0 || (len(defs) == 0 && slices.EqualFunc(reduced, fragments, bytes.Equal)) {
		return art
	}
	var out bytes.Buffer
	out.Write(defs)
	at := 0
	for i, s := range spans {
		out.Write(art[at:s[0]])
		f := reduced[i]
		out.Write(f[len(open) : len(f)-len(close)])
		at = s[1]
	}
	out.Write(art[at:])
	return out.Bytes()
}

// Snap mixed frame regions and the footer to the same boards as the native
// objects when the viewer requests reduced motion. Keep the first sample in
// each incoming run because an authored board can also change at its start.
func writeReducedMotionFrames(css *bytes.Buffer, hash string, frames []Frame, total int, hidden, visible string) {
	css.WriteString("@media(prefers-reduced-motion:reduce){")
	start := 0
	for i := 0; i < len(frames); {
		j := i + 1
		duration := frames[i].DurationMS
		if frames[i].TweenSample {
			for j < len(frames) && frames[j].TweenSample {
				fmt.Fprintf(css, "@keyframes d2Transition-%s-%d{0%%,100%%%s}", hash, j, hidden)
				duration += frames[j].DurationMS
				j++
			}
		}
		var end frameStart
		if j < len(frames) {
			end = frames[j].start
		}
		writeFrameKeyframes(css, "d2Transition", hash, i, start, duration, total, frames[i].start, end, hidden, visible)
		start += duration
		i = j
	}
	css.WriteByte('}')
}
