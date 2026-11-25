package d2animate

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2renderers/d2svg"
)

// Only geometry is instanced. Text, frame groups, links, and clocks remain in
// their original order, including in SVG viewers which ignore animations.
type paintNode struct {
	open, close string
	children    []*paintNode
	geometry    bool
	candidate   *graphic
	path        *graphic
}

type graphic struct {
	markup string
	count  int
	id     string
}

type paintResources struct {
	ids    map[string]*paintResource
	bodies map[paintKey]string
	next   int
	prefix string
}

type paintKey struct {
	scope, markup string
}

type paintResource struct {
	markup, alias string
	seen, pooled  int
}

// SVG fragment links resolve to the first matching ID in document order.
// Leave that definition under its original ancestors, including tagged objects
// with externally supplied CSS. Only pool different IDs for self-contained
// native paints outside authored objects.
func (r *paintResources) take(markup, id, scope string) (*paintNode, bool) {
	attr := ` id="` + id + `"`
	i := strings.Index(markup, attr)
	if i < 0 {
		return nil, false
	}
	resource := r.ids[id]
	resource.pooled++
	if resource.alias != "" {
		return nil, true
	}
	var key paintKey
	if scope != "" {
		key = paintKey{scope: scope, markup: markup[:i] + markup[i+len(attr):]}
		if alias := r.bodies[key]; alias != "" {
			resource.alias = alias
			return nil, true
		}
	}
	alias := r.prefix + strconv.FormatInt(int64(r.next), 36)
	r.next++
	if scope != "" {
		r.bodies[key] = alias
	}
	resource.alias = alias
	return &paintNode{open: markup[:i] + ` id="` + alias + `"` + markup[i+len(attr):]}, true
}

func attrValue(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name && a.Name.Space == "" {
			return a.Value
		}
	}
	return ""
}

func safePaintElement(el xml.StartElement) bool {
	if el.Name.Space != "" && el.Name.Space != "http://www.w3.org/2000/svg" {
		return false
	}
	switch el.Name.Local {
	case "g", "svg", "path", "rect", "ellipse", "circle", "polygon", "polyline", "line", "text", "tspan", "use",
		"defs", "linearGradient", "radialGradient", "stop", "mask", "clipPath", "marker", "pattern":
	default:
		return false
	}
	for _, a := range el.Attr {
		switch a.Name.Local {
		case "id":
			// Keep author-supplied fragment targets and their DOM position.
			switch el.Name.Local {
			case "mask":
				if !strings.HasPrefix(a.Value, "d2-") || len(a.Value) == 3 || strings.Trim(a.Value[3:], "0123456789") != "" {
					return false
				}
			case "linearGradient", "radialGradient":
				if len(a.Value) != len("grad-")+40 || !strings.HasPrefix(a.Value, "grad-") || strings.Trim(a.Value[5:], "0123456789abcdef") != "" {
					return false
				}
			case "pattern", "path":
				if !strings.HasPrefix(a.Value, "pencil-d2-") {
					return false
				}
			case "marker":
				if !strings.HasPrefix(a.Value, "mk-d2-") || len(a.Value) == 6 || strings.Trim(a.Value[6:], "0123456789-") != "" {
					return false
				}
			default:
				return false
			}
		case "style":
			for _, decl := range strings.Split(a.Value, ";") {
				key, _, _ := strings.Cut(decl, ":")
				switch strings.TrimSpace(key) {
				case "", "stroke-width", "stroke-dasharray", "stroke-dashoffset", "stroke-linecap", "stroke-linejoin", "text-anchor", "font-size", "font-weight", "opacity", "user-select", "clip-path":
				default:
					return false
				}
			}
		case "class":
			if strings.Contains(a.Value, "animated") {
				return false
			}
		case "href":
			// Foreign documents have their own fragment ID namespace.
			if !strings.HasPrefix(a.Value, "#") || !validFragmentID(a.Value[1:]) {
				return false
			}
		case "aria-hidden", "d", "points", "x", "y", "dx", "dy", "x1", "x2", "y1", "y2", "r", "rx", "ry", "cx", "cy",
			"width", "height", "viewBox", "overflow", "transform", "fill", "fill-rule", "fill-opacity", "stroke",
			"stroke-width", "stroke-linejoin", "stroke-linecap", "stroke-opacity", "stroke-dasharray", "stroke-dashoffset",
			"offset", "stop-color", "stop-opacity", "gradientTransform", "gradientUnits", "fx", "fy",
			"mask", "maskUnits", "maskContentUnits", "clip-path", "clip-rule", "marker-end", "marker-start", "markerUnits",
			"markerWidth", "markerHeight", "refX", "refY", "orient", "patternUnits", "patternTransform",
			"font-size", "text-anchor", "space", "opacity", "lengthAdjust", "textLength", "preserveAspectRatio":
		default:
			return false
		}
	}

	return true
}

// D2 emits literal ASCII fragment IDs. Encoded IDs require XML, CSS or URL
// decoding before they can safely be compared or shortened.
func fragmentByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '-' || c == '_' || c == '.' || c == ':'
}

func validFragmentID(id string) bool {
	for i := 0; i < len(id); i++ {
		if !fragmentByte(id[i]) {
			return false
		}
	}
	return id != ""
}

func canonicalPaintURL(value string) bool {
	id, ok := strings.CutPrefix(value, "url(")
	if !ok || !strings.HasSuffix(id, ")") {
		return false
	}
	id = id[:len(id)-1]
	if len(id) > 1 && (id[0] == '\'' || id[0] == '"') && id[0] == id[len(id)-1] {
		id = id[1 : len(id)-1]
	}
	return strings.HasPrefix(id, "#") && validFragmentID(id[1:])
}

func nativeGraphicsClass(class string) bool {
	for _, c := range strings.Fields(class) {
		if c != "shape" && c != "connection" && !strings.HasPrefix(c, "fill-") && !strings.HasPrefix(c, "stroke-") && !strings.HasPrefix(c, "sketch-overlay-") {
			return false
		}
	}
	return true
}

// drawConnection names its group with the base64-encoded, XML-escaped
// connection ID, whose final component is "(source arrow target)[index]".
func nativeConnectionClass(class string) bool {
	id, err := base64.URLEncoding.DecodeString(class)
	if err != nil || !bytes.ContainsRune(id, '(') || !bytes.HasSuffix(id, []byte("]")) {
		return false
	}
	i := bytes.LastIndex(id, []byte(")["))
	return i >= 0 && i+2 < len(id)-1 && len(bytes.Trim(id[i+2:len(id)-1], "0123456789")) == 0
}

type frameParser struct {
	frame, object, class string // A possible native connection group and its frame.
	d                    *xml.Decoder
	src                  string
	res                  *paintResources
}

func (p *frameParser) node(el xml.StartElement, start, depth int, collect, protected bool) (*paintNode, bool) {
	if depth > 128 || !safePaintElement(el) {
		return nil, false
	}
	openEnd := int(p.d.InputOffset())
	open := p.src[start:openEnd]
	// Accept D2's canonical local URL spelling. Unusual XML entity or CSS
	// whitespace spellings may refer to the same ID without using those bytes.
	for _, a := range el.Attr {
		if strings.Contains(a.Value, `\`) {
			return nil, false
		}
		isURL := strings.Contains(a.Value, "(") && strings.Contains(strings.ToLower(a.Value), "url(")
		if isURL || a.Name.Local == "href" {
			if strings.Contains(open, "&") || (isURL && !canonicalPaintURL(a.Value)) {
				return nil, false
			}
		}
		if a.Name.Space != "" && a.Name.Space != "http://www.w3.org/XML/1998/namespace" && !((a.Name.Space == "http://www.w3.org/1999/xlink" || a.Name.Space == "xlink") && a.Name.Local == "href") {
			return nil, false
		}
	}
	if el.Name.Local == "g" && len(strings.Fields(attrValue(el, "class"))) > 1 {
		protected = true
	}
	if depth == 1 {
		p.object = ""
		if el.Name.Local == "g" && len(el.Attr) == 1 && !protected {
			p.object, p.class = open, attrValue(el, "class")
		}
	}
	n := &paintNode{open: p.src[start:openEnd]}
	switch el.Name.Local {
	case "rect", "path", "circle", "ellipse", "line", "polyline", "polygon":
		n.geometry = attrValue(el, "id") == "" && nativeGraphicsClass(attrValue(el, "class"))
	case "g":
		n.geometry = attrValue(el, "class") == "shape"
	}
	n.geometry = n.geometry && !protected
	group := el.Name.Local == "g" || el.Name.Local == "svg"
	// D2 emits each gradient inside its own defs. Its stops have explicit
	// colors; unrecognized resources stay in their own original subtree.
	gradientDefs := collect && el.Name.Local == "defs"
	id := attrValue(el, "id")
	mask := depth == 1 && el.Name.Local == "mask" && strings.HasPrefix(id, "d2-")
	var gradient, gradientID string
	marker := collect && el.Name.Local == "marker" && strings.HasPrefix(id, "mk-d2-")
	simpleMarker := marker && depth == 2 && p.object != "" && nativeConnectionClass(p.class)
	childScope := collect && group
	childCount := 0
	for {
		before := int(p.d.InputOffset())
		t, err := p.d.Token()
		if err != nil {
			return nil, false
		}
		switch x := t.(type) {
		case xml.StartElement:
			c, ok := p.node(x, before, depth+1, childScope, protected)
			if !ok {
				return nil, false
			}
			childCount++
			// D2 arrowheads carry an explicit fill and use the standard shape
			// classes. Their coordinates are relative to the referencing path.
			if marker && (c == nil || attrValue(x, "fill") == "" || attrValue(x, "style") != "") {
				marker = false
			}
			// Only share markers across IDs when every child is a plain drawable,
			// without its own ID, nested content, or an inherited paint server.
			simpleMarker = simpleMarker && c != nil && c.geometry && c.close == "" && !strings.Contains(attrValue(x, "fill"), "(")
			n.geometry = n.geometry && group && c != nil && c.geometry && c.close == ""
			if group && c != nil {
				n.children = append(n.children, c)
			}
			if gradientDefs && childCount == 1 && (x.Name.Local == "linearGradient" || x.Name.Local == "radialGradient") && strings.HasPrefix(attrValue(x, "id"), "grad-") {
				gradient, gradientID = p.src[before:p.d.InputOffset()], attrValue(x, "id")
			}
			// Only consolidate simple native rectangle masks that do not
			// depend on inherited paint, another resource, or a CSS class.
			if mask && (x.Name.Local != "rect" || attrValue(x, "class") != "" || attrValue(x, "style") != "" || (attrValue(x, "fill") != "white" && attrValue(x, "fill") != "black")) {
				mask = false
			}
		case xml.EndElement:
			raw := p.src[start:p.d.InputOffset()]
			if id != "" {
				resource := p.res.ids[id]
				if resource == nil {
					resource = &paintResource{markup: raw}
					p.res.ids[id] = resource
				} else if resource.markup != raw {
					return nil, false
				}
				resource.seen++
			}
			if gradientDefs && childCount == 1 && gradient != "" && len(el.Attr) == 0 {
				return p.res.take(raw, gradientID, "")
			}
			if (mask || marker) && childCount > 0 {
				scope := ""
				switch {
				case mask && !protected:
					scope = "mask"
				case marker && simpleMarker:
					scope = p.frame + p.object
				}
				return p.res.take(raw, id, scope)
			}
			if group {
				n.close = p.src[before:p.d.InputOffset()]
			} else {
				n.open = raw
			}
			return n, true
		case xml.CharData:
			if group && len(bytes.TrimSpace(x)) != 0 {
				return nil, false
			}
		default:
			return nil, false
		}
	}
}

func parsePaintFrame(src, hash string, res *paintResources) (*paintNode, bool) {
	if strings.Contains(src, "<![CDATA[") {
		return nil, false
	}
	p := frameParser{d: xml.NewDecoder(strings.NewReader(src)), src: src, res: res}
	t, err := p.d.Token()
	root, ok := t.(xml.StartElement)
	if err != nil || !ok || root.Name != (xml.Name{Local: "g"}) || strings.TrimSpace(attrValue(root, "class")) != hash {
		return nil, false
	}
	for _, a := range root.Attr {
		switch a.Name.Local {
		case "class", "width", "height", "viewBox":
		default:
			return nil, false
		}
	}
	p.frame = src[:p.d.InputOffset()]
	n, ok := p.node(root, 0, 0, true, false)
	if !ok {
		return nil, false
	}
	if t, err := p.d.Token(); err != io.EOF || t != nil {
		return nil, false
	}
	return n, true
}

// Rewrite fragment references only inside tag attributes. In particular a
// Markdown label that mentions a generated ID is still copied literally.
func (r *paintResources) refs(raw string) string {
	if !strings.Contains(raw, "#") {
		return raw
	}
	var b strings.Builder
	start := 0
	inTag := false
	valueStart := 0
	var quote byte
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if !inTag {
			inTag = c == '<'
			continue
		}
		if quote == 0 {
			switch c {
			case '>':
				inTag = false
			case '"', '\'':
				quote = c
				valueStart = i + 1
			}
			continue
		}
		if c == quote {
			quote = 0
			continue
		}
		// Only local fragment hrefs and url() paint references are internal.
		if c != '#' {
			continue
		}
		end := i + 1
		for end < len(raw) && fragmentByte(raw[end]) {
			end++
		}
		prefix := raw[valueStart:i]
		if resource := r.ids[raw[i+1:end]]; resource != nil && resource.alias != "" &&
			(prefix == "url(" || prefix == "url('" || prefix == `url("` || prefix == "" && hrefAttribute(raw[:valueStart-1])) {
			b.WriteString(raw[start : i+1])
			b.WriteString(resource.alias)
			start = end
		}
		i = end - 1
	}
	if start == 0 {
		return raw
	}
	b.WriteString(raw[start:])
	return b.String()
}

func hrefAttribute(beforeQuote string) bool {
	beforeQuote = strings.TrimRight(beforeQuote, " \t\r\n")
	name, ok := strings.CutSuffix(beforeQuote, "=")
	return ok && strings.HasSuffix(strings.TrimRight(name, " \t\r\n"), "href")
}

// A shared path may have different paint in each frame. Keep those attributes
// on its <use> and share only d, in the path's original coordinate system.
func pathData(markup string) (start, end int) {
	if !strings.HasPrefix(markup, "<path ") || !strings.HasSuffix(markup, "/>") {
		return -1, -1
	}
	i := strings.Index(markup, ` d="`)
	if i < 0 {
		return -1, -1
	}
	j := strings.IndexByte(markup[i+4:], '"')
	if j < 0 {
		return -1, -1
	}
	return i, i + 4 + j + 1
}

type graphics struct {
	index  map[string]*graphic
	prefix string
	defs   []string
}

func (g *graphics) count(key string) *graphic {
	item := g.index[key]
	if item == nil {
		item = &graphic{markup: key}
		g.index[key] = item
	}
	item.count++
	return item
}

func (g *graphics) collect(n *paintNode, r *paintResources) {
	n.open = r.refs(n.open)
	for _, c := range n.children {
		g.collect(c, r)
	}
	if !n.geometry {
		return
	}
	if n.close != "" {
		// A native shape group has no intervening text. Keep a single copy of
		// a frequently repeated drawing even if it has many component paths.
		var b strings.Builder
		b.WriteString("<g>")
		for _, c := range n.children {
			if c.candidate == nil {
				return
			}
			b.WriteString(c.candidate.markup)
		}
		b.WriteString("</g>")
		n.candidate = g.count(b.String())
		return
	}
	n.candidate = g.count(n.open)
	if i, j := pathData(n.open); i >= 0 && !strings.Contains(n.open, ` x=`) && !strings.Contains(n.open, ` y=`) {
		if data := "<path" + n.open[i:j] + "/>"; data != n.open {
			n.path = g.count(data)
		}
	}
}

// Include the definition, its ID and every use in the byte comparison. The
// final whole-document check also covers nested candidates that share callers.
func (g *graphics) reference(item *graphic, replaced int) string {
	if item == nil || item.count < 2 {
		return ""
	}
	if item.id != "" {
		return item.id
	}
	id := g.prefix + strconv.FormatInt(int64(len(g.defs)), 36)
	useLen := len(`<use href="#" xlink:href="#"/>`) + 2*len(id)
	if int64(item.count)*int64(replaced-useLen) <= int64(len(item.markup)+len(` id=""`)+len(id)) {
		return ""
	}
	item.id = id
	i := strings.IndexAny(item.markup, " />")
	g.defs = append(g.defs, item.markup[:i]+` id="`+id+`"`+item.markup[i:])
	return id
}

func (g *graphics) write(b *strings.Builder, n *paintNode) {
	if n.candidate != nil {
		if id := g.reference(n.candidate, len(n.candidate.markup)); id != "" {
			if n.close != "" {
				b.WriteString(n.open)
			}
			b.WriteString(`<use href="#` + id + `" xlink:href="#` + id + `"/>`)
			b.WriteString(n.close)
			return
		}
		if n.path != nil {
			i, j := pathData(n.open)
			if id := g.reference(n.path, j-i); id != "" {
				b.WriteString(`<use href="#` + id + `" xlink:href="#` + id + `"`)
				b.WriteString(n.open[len("<path"):i])
				b.WriteString(n.open[j:])
				return
			}
		}
	}
	b.WriteString(n.open)
	for _, c := range n.children {
		g.write(b, c)
	}
	b.WriteString(n.close)
}

func shareFrameGraphics(frames [][]byte, hash string) (defs []byte, reduced [][]byte) {
	if len(frames) < 2 || !d2svg.UsesDefaultStylesheet() {
		return nil, frames
	}
	// Use the same diagram namespace in a shorter radix for frequently used IDs.
	prefix := hash
	if h, err := strconv.ParseUint(strings.TrimPrefix(hash, "d2-"), 10, 32); err == nil {
		prefix = "d2x" + strconv.FormatUint(h, 36)
	}
	res := paintResources{prefix: prefix + "r", ids: map[string]*paintResource{}, bodies: map[paintKey]string{}}
	g := graphics{index: map[string]*graphic{}, prefix: prefix + "u"}
	roots := make([]*paintNode, len(frames))
	sourceSize := 0
	for i, frame := range frames {
		if bytes.Contains(frame, []byte(res.prefix)) || bytes.Contains(frame, []byte(g.prefix)) {
			return nil, frames
		}
		n, ok := parsePaintFrame(string(frame), hash, &res)
		if !ok {
			return nil, frames
		}
		roots[i] = n
		sourceSize += len(frame)
	}
	// A reference can resolve to an earlier ID in a retained subtree, where
	// paint inherits different properties. Keep its original document order.
	for _, resource := range res.ids {
		if resource.pooled != 0 && resource.pooled != resource.seen {
			return nil, frames
		}
	}
	for _, r := range roots {
		g.collect(r, &res)
	}
	reduced = make([][]byte, len(frames))
	outputSize := 0
	for i, root := range roots {
		var b strings.Builder
		g.write(&b, root)
		reduced[i] = []byte(b.String())
		outputSize += len(reduced[i])
	}
	var b strings.Builder
	if len(g.defs) > 0 {
		b.WriteString(`<defs><g class="` + hash + `">`)
		for _, d := range g.defs {
			b.WriteString(d)
		}
		b.WriteString("</g></defs>")
	}
	if b.Len()+outputSize >= sourceSize {
		return nil, frames
	}
	return []byte(b.String()), reduced
}
