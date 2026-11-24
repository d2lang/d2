package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
)

// The first drawable board may live below a folder. Its native shape IDs
// distinguish diagram objects from unrelated <g> elements in the fragments.
func firstGridPlaybackFrame(root *d2target.Diagram) *d2target.Diagram {
	var first *d2target.Diagram
	var grid bool
	var visit func(*d2target.Diagram)
	visit = func(board *d2target.Diagram) {
		if first == nil && !board.IsFolderOnly {
			first = board
			if len(board.TweenFrames) != 0 {
				first = board.TweenFrames[0]
			}
		}
		grid = grid || board.GridMotion
		if first == nil || !grid {
			for _, children := range [][]*d2target.Diagram{board.Layers, board.Scenarios, board.Steps} {
				for _, child := range children {
					visit(child)
				}
			}
		}
	}
	visit(root)
	if !grid {
		return nil
	}
	return first
}

type staticShape struct {
	markup []byte
	frames int
	next   *staticShape // adjacent in every frame
	use    string
}

type shapeSpan struct {
	start, end int
	shape      *staticShape
}

// A use element creates a shadow tree. Only share plain geometry and text:
// links, tooltips, authored classes, animated elements, and frame-local paint
// servers and masks need their original DOM or resource scope.
func plainShapeElement(el xml.StartElement) bool {
	switch el.Name.Local {
	case "g", "rect", "ellipse", "path", "text", "tspan":
	default:
		return false
	}
	for _, a := range el.Attr {
		if a.Name.Space != "" || strings.Contains(strings.ToLower(a.Value), "url(") {
			return false
		}
		switch a.Name.Local {
		case "x", "y", "rx", "ry", "cx", "cy", "d", "width", "height", "dy",
			"fill", "stroke", "stroke-width":
		case "class":
			for _, class := range strings.Fields(a.Value) {
				if class != "shape" && class != "text" && !strings.HasPrefix(class, "text-") &&
					!strings.HasPrefix(class, "stroke-") && !strings.HasPrefix(class, "fill-") {
					return false
				}
			}
		case "style":
			for _, property := range strings.Split(a.Value, ";") {
				key, _, _ := strings.Cut(property, ":")
				switch key {
				case "", "stroke-width", "stroke-dasharray", "text-anchor", "font-size":
				default:
					return false
				}
			}
		default:
			return false
		}
	}
	return el.Name.Space == ""
}

func readPlainShape(d *xml.Decoder) (bool, error) {
	plain := true
	for depth := 1; depth > 0; {
		t, err := d.Token()
		if err != nil {
			return false, err
		}
		switch el := t.(type) {
		case xml.StartElement:
			depth++
			plain = plain && plainShapeElement(el)
		case xml.EndElement:
			depth--
		case xml.CharData:
		default:
			plain = false
		}
	}
	return plain, nil
}

func frameShapeSpans(frame []byte, hash string, index int, candidates map[string]*staticShape) ([]shapeSpan, bool) {
	d := xml.NewDecoder(bytes.NewReader(frame))
	t, err := d.Token()
	root, ok := t.(xml.StartElement)
	if err != nil || !ok || root.Name != (xml.Name{Local: "g"}) {
		return nil, false
	}
	match := false
	for _, a := range root.Attr {
		switch a.Name.Local {
		case "class":
			match = strings.TrimSpace(a.Value) == hash
		case "width", "height", "viewBox":
		default:
			return nil, false
		}
	}
	if !match {
		return nil, false
	}
	var spans []shapeSpan
	for start := int(d.InputOffset()); start < len(frame); {
		rest := frame[start:]
		if bytes.HasPrefix(rest, []byte("</g>")) {
			return spans, len(bytes.TrimSpace(rest[len("</g>"):])) == 0
		}

		// We are at a direct child boundary. A byte-identical subtree that
		// was parsed in the first frame needs no further XML decoding.
		if index > 0 {
			if body, ok := bytes.CutPrefix(rest, []byte(`<g class="`)); ok {
				if class, _, ok := bytes.Cut(body, []byte(`"`)); ok {
					shape := candidates[string(class)]
					if shape != nil && shape.frames == index && bytes.HasPrefix(rest, shape.markup) {
						end := start + len(shape.markup)
						spans = append(spans, shapeSpan{start, end, shape})
						shape.frames++
						start = end
						continue
					}
				}
			}
		}

		d = xml.NewDecoder(bytes.NewReader(rest))
		t, err := d.Token()
		if err != nil {
			return nil, false
		}
		if el, ok := t.(xml.StartElement); ok {
			var shape *staticShape
			if el.Name == (xml.Name{Local: "g"}) && len(el.Attr) == 1 && el.Attr[0].Name == (xml.Name{Local: "class"}) {
				shape = candidates[el.Attr[0].Value]
			}
			if shape != nil && index == 0 && shape.frames == 0 {
				var plain bool
				plain, err = readPlainShape(d)
				if plain {
					end := start + int(d.InputOffset())
					shape.markup = frame[start:end]
					shape.frames++
					spans = append(spans, shapeSpan{start, end, shape})
				}
			} else {
				if shape != nil {
					shape.frames = -1
				}
				err = d.Skip()
			}
			if err != nil {
				return nil, false
			}
		}
		start += int(d.InputOffset())
	}
	return nil, false
}

// shareStaticShapes keeps each run in its original position in the paint order.
// The caller's frame buffers stay intact. Unrecognized fragments pass through.
func shareStaticShapes(root *d2target.Diagram, frames [][]byte, hash string) (defs []byte, reduced [][]byte) {
	if len(frames) < 2 {
		return nil, frames
	}
	first := firstGridPlaybackFrame(root)
	if first == nil {
		return nil, frames
	}
	candidates := make(map[string]*staticShape)
	for _, s := range first.Shapes {
		if s.Icon == nil && s.Link == "" && s.Tooltip == "" && s.PrettyLink == "" && len(s.Classes) == 0 && !s.Animated && !s.Shadow {
			candidates[base64.URLEncoding.EncodeToString([]byte(svg.EscapeText(s.ID)))] = &staticShape{}
		}
	}
	if len(candidates) == 0 {
		return nil, frames
	}
	prefix := hash + "-grid-static-"
	spans := make([][]shapeSpan, len(frames))
	for i, frame := range frames {
		if bytes.Contains(frame, []byte(prefix)) {
			return nil, frames
		}
		var ok bool
		spans[i], ok = frameShapeSpans(frame, hash, i, candidates)
		if !ok || len(spans[i]) == 0 {
			return nil, frames
		}
	}

	// Form runs of shapes which are always adjacent in the same paint order.
	// A local swap can then reuse hundreds of stationary cells with one use.
	for i, row := range spans {
		for j, s := range row {
			var next *staticShape
			if j+1 < len(row) && s.end == row[j+1].start && row[j+1].shape.frames == len(frames) {
				next = row[j+1].shape
			}
			if i == 0 {
				s.shape.next = next
			} else if s.shape.next != next {
				s.shape.next = nil
			}
		}
	}
	var definitions bytes.Buffer
	for j := 0; j < len(spans[0]); {
		first := spans[0][j]
		last := first
		j++
		if first.shape.frames != len(frames) {
			continue
		}
		for last.shape.next != nil {
			last = spans[0][j]
			j++
		}
		markup := frames[0][first.start:last.end]
		id := fmt.Sprintf("%s%d", prefix, first.start)
		use := fmt.Sprintf(`<use href="#%s"/>`, id)
		open := fmt.Sprintf(`<g id="%s" class="%s">`, id, hash)
		// Include the definition and references only if they save bytes.
		if int64(len(frames)-1)*int64(len(markup)) <= int64(len(frames))*int64(len(use))+int64(len(open)+len("</g><defs></defs>")) {
			continue
		}
		if definitions.Len() == 0 {
			definitions.WriteString("<defs>")
		}
		definitions.WriteString(open)
		definitions.Write(markup)
		definitions.WriteString("</g>")
		first.shape.markup, first.shape.use = markup, use
	}
	if definitions.Len() == 0 {
		return nil, frames
	}
	definitions.WriteString("</defs>")
	reduced = make([][]byte, len(frames))
	for i, frame := range frames {
		var out bytes.Buffer
		start := 0
		for _, s := range spans[i] {
			if s.shape.use != "" {
				out.Write(frame[start:s.start])
				out.WriteString(s.shape.use)
				start = s.start + len(s.shape.markup)
			}
		}
		out.Write(frame[start:])
		reduced[i] = out.Bytes()
	}
	return definitions.Bytes(), reduced
}
