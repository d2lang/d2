package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"math"
	"reflect"
	"slices"

	"strings"

	"github.com/d2lang/d2/d2renderers/d2scene"

	"github.com/d2lang/d2/d2renderers/d2svgimport"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/svg"
)

// A wire keeps its original SVG path, decorations and sampled visibility. On
// engines with animatable CSS path data, the visible segment can also follow
// the two endpoints between samples. Its d attribute remains the fallback.
type wireLine struct {
	start, end geo.Point
	command    string
	path       []byte
	refs       []string
	appearance string
}

func readWireLine(raw []byte) (wireLine, bool) {
	var line wireLine
	d := xml.NewDecoder(bytes.NewReader(raw))
	root, err := d.Token()
	el, ok := root.(xml.StartElement)
	if err != nil || !ok || el.Name.Local != "g" || len(el.Attr) != 1 || el.Attr[0].Name.Local != "class" {
		return line, false
	}
	for {
		start := int(d.InputOffset())
		token, err := d.Token()
		if err != nil {
			return line, false
		}
		switch el := token.(type) {
		case xml.EndElement:
			return line, line.command != ""
		case xml.StartElement:
			switch el.Name.Local {
			case "marker": // remains under its original owner with the same resources
			case "path":
				if line.command != "" {
					return line, false
				}
				class := attrValue(el, "class")
				if !slices.Contains(strings.Fields(class), "connection") {
					return line, false
				}
				open := raw[start:d.InputOffset()]
				if !bytes.HasSuffix(open, []byte("/>")) {
					return line, false
				}
				line.path = open
				data := attrValue(el, "d")
				cmds, err := d2svgimport.ParsePath(nil, "wire", data, d2svgimport.PathLimits{MaxBytes: len(data), MaxCommands: 2})
				if err != nil || len(cmds) != 2 || cmds[0].Kind != d2scene.MoveCommand || cmds[1].Kind != d2scene.LineCommand {
					return line, false
				}
				a, b := cmds[0].P1, cmds[1].P1
				line.start = geo.Point{X: a.X, Y: a.Y}
				line.end = geo.Point{X: b.X, Y: b.Y}
				line.command = "M" + svg.FormatFloat(a.X) + " " + svg.FormatFloat(a.Y) + "L" + svg.FormatFloat(b.X) + " " + svg.FormatFloat(b.Y)
				var style strings.Builder
				for _, a := range el.Attr {
					switch a.Name.Local {
					case "d":
						continue
					case "mask", "marker-start", "marker-end":
						// D2 scopes resource IDs to each frame; their paint stays in that frame.
						if !canonicalPaintURL(a.Value) {
							return line, false
						}
						line.refs = append(line.refs, strings.TrimSuffix(strings.TrimPrefix(a.Value, "url(#"), ")"))
						a.Value = "local"
					default:
						// Other paint servers can depend on frame-local geometry.
						if strings.Contains(strings.ToLower(a.Value), "url(") {
							return line, false
						}
					}
					fmt.Fprintf(&style, "%q=%q ", a.Name.Local, a.Value)
				}
				line.appearance = style.String()
			default:
				return line, false
			}
			if d.Skip() != nil {
				return line, false
			}
		default:
			return line, false
		}
	}
}

// The compiler identifies a continuous translation with MotionOffset, including
// its zero value. Reject a reroute or a switch to another port even if it also
// happens to be a two-point line with an unchanged connection ID.
func endpointFollows(a, b *d2target.Shape, p, q geo.Point, nextSample bool) (moving, match bool) {
	if a == nil || b == nil || a.Type != b.Type || a.Width != b.Width || a.Height != b.Height {
		return false, false
	}
	start, end := a.RenderedPosition(), b.RenderedPosition()
	dx, dy := end.X-start.X, end.Y-start.Y
	moving = dx != 0 || dy != 0
	if moving && a.MotionOffset == nil && !(nextSample && b.MotionOffset != nil) {
		return moving, false
	}
	const precision = 0.000003 // the SVG stores six fractional digits per endpoint
	return moving, math.Abs((q.X-p.X)-dx) <= precision && math.Abs((q.Y-p.Y)-dy) <= precision
}

// Compare the resolved, self-contained mask and marker definitions. References
// may have different frame-local IDs for the same rendered paint.
func wireResources(frames [][]byte) map[string]string {
	out := map[string]string{}
	for _, frame := range frames {
		d := xml.NewDecoder(bytes.NewReader(frame))
		for {
			start := int(d.InputOffset())
			tok, err := d.Token()
			if err != nil {
				break
			}
			el, ok := tok.(xml.StartElement)
			if !ok || (el.Name.Local != "marker" && el.Name.Local != "mask") {
				continue
			}
			id := attrValue(el, "id")
			if id == "" || d.Skip() != nil {
				continue
			}
			raw := string(frame[start:d.InputOffset()])
			key := ` id="` + id + `"`
			simple := strings.Count(raw, ` id="`) == 1 && strings.Contains(raw, key) && !strings.Contains(raw, "url(")
			normalized := ""
			if simple {
				normalized = strings.Replace(raw, key, "", 1)
			}
			if prior, exists := out[id]; exists && prior != normalized {
				normalized = ""
			}
			out[id] = normalized
		}
	}
	return out
}

type wireTrack struct {
	lines   []wireLine
	smooth  []bool
	indices []int
}

func collectWireTracks(frames []Frame, svgs [][]byte, rows [][]motionChild, positions []map[string]int, shapes []map[string]*d2target.Shape) map[string]wireTrack {
	if len(frames[0].Picture().Connections) == 0 {
		return nil
	}
	out := map[string]wireTrack{}
	edges := make([]map[string]*d2target.Connection, len(frames))
	for i, f := range frames {
		edges[i] = map[string]*d2target.Connection{}
		for j := range f.Picture().Connections {
			c := &f.Picture().Connections[j]
			if len(c.Route) != 2 || c.IsCurve || c.Animated || c.Link != "" || c.Tooltip != "" || len(c.Classes) > 0 || c.Label != "" || c.Icon != nil || c.IconAnimation != nil || c.MotionTip != "" || c.Annotation != nil {
				continue
			}
			edges[i][c.ID] = c
		}
	}
	var resources map[string]string
	encoded := func(id string) string { return base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(id))) }
	for id, base := range edges[0] {
		key, source, destination := encoded(id), encoded(base.Src), encoded(base.Dst)
		track := wireTrack{make([]wireLine, len(frames)), make([]bool, len(frames)+1), make([]int, len(frames))}
		compatible, animated := true, false
		baseStyle := *base
		baseStyle.Route = nil
		for i, f := range frames {
			c := edges[i][id]
			k, ok := positions[i][key]
			if !ok || k < 0 || c == nil {
				compatible = false
				break
			}
			style := *c
			style.Route = nil
			if !reflect.DeepEqual(style, baseStyle) {
				compatible = false
				break
			}
			child := rows[i][k]
			line, ok := readWireLine(svgs[i][child.start:child.end])
			if !ok || (i > 0 && (line.appearance != track.lines[0].appearance || len(line.refs) != len(track.lines[0].refs))) {
				compatible = false
				break
			}
			if len(line.refs) > 0 && resources == nil {
				resources = wireResources(svgs)
			}
			for j, ref := range line.refs {
				if resources[ref] == "" || (i > 0 && resources[ref] != resources[track.lines[0].refs[j]]) {
					compatible = false
					break
				}
			}
			if !compatible {
				break
			}
			track.indices[i], track.lines[i] = k, line
			if i == 0 || !frames[i-1].TweenSample || line.command == track.lines[i-1].command {
				continue
			}
			prev := track.lines[i-1]
			sm, sok := endpointFollows(shapes[i-1][source], shapes[i][source], prev.start, line.start, f.TweenSample)
			dm, dok := endpointFollows(shapes[i-1][destination], shapes[i][destination], prev.end, line.end, f.TweenSample)
			if !sok || !dok || !(sm || dm) {
				compatible = false
				break
			}
			track.smooth[i-1] = true
			animated = true
		}
		if compatible && animated {
			out[key] = track
		}
	}
	return out
}

func (w wireTrack) write(body, css *bytes.Buffer, writeFragment func([]byte), frames [][]byte, rows [][]motionChild, clock []motionTick, name, hash string, first, total int) {
	// Keep the sampled paths and their original resources for engines without
	// CSS path interpolation. On capable engines only the persistent path runs.
	fmt.Fprintf(body, `<g class="%s">`, name)
	path := bytes.Replace(w.lines[0].path, []byte(`class="`), []byte(`class="d2-wire-ink `), 1)
	writeFragment(path)
	body.WriteString("<g>")
	for i, k := range w.indices {
		fmt.Fprintf(body, `<g style="animation: d2Transition-%s-%d %dms steps(1,end) infinite">`, hash, first+i, total)
		c := rows[i][k]
		writeFragment(frames[i][c.start:c.end])
		body.WriteString("</g>")
	}
	body.WriteString("</g></g>")
	fmt.Fprintf(css, `.%s>.d2-wire-ink{display:none}@supports(d:path("M0 0L1 1")){`, name)
	// Leave marker definitions available wherever their original IDs were used.
	fmt.Fprintf(css, `.%[1]s>g>g{animation:none!important}.%[1]s>g>g>g>:is(path,use){display:none}.%[1]s>.d2-wire-ink{display:inline;animation:%[1]s %[2]dms linear infinite}`, name, total)
	values := make([]motionValue, len(clock))
	for i, l := range w.lines {
		values[i] = motionValue{literal: `path("` + l.command + `")`}
	}
	values[len(w.lines)] = values[len(w.lines)-1]
	writeMotionProperty(css, name, "d", clock, values, w.smooth, false)
	writeMotionProperty(css, name+"-still", "d", clock, values, w.smooth, true)
	fmt.Fprintf(css, "@media(prefers-reduced-motion:reduce){.%s>.d2-wire-ink{animation:%s-still %dms steps(1,end) infinite}}}", name, name, total)
}
