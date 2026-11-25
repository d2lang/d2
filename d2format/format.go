package d2format

import (
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/d2lang/d2/d2ast"
)

// TODO: edges with shared path should be fmted as <rel>.(x -> y)
func Format(n d2ast.Node) string {
	var p printer
	p.node(n)
	return p.sb.String()
}

// Some map keys name user data even when they happen to spell a D2 keyword.
type keyScope int

const (
	diagramKeys     keyScope = iota
	definitionNames          // each child names a board or class
	variableKeys             // arbitrary variable names and data, including plugin configuration
)

func (scope keyScope) child(keyword string) keyScope {
	switch scope {
	case variableKeys:
		return variableKeys
	case definitionNames:
		return diagramKeys
	}
	switch keyword {
	case "vars":
		return variableKeys
	case "layers", "scenarios", "steps", "classes":
		return definitionNames
	default:
		return diagramKeys
	}
}

type printer struct {
	sb        strings.Builder
	indentStr string
	inKey     bool
	keyScope  keyScope
}

func (p *printer) indent() {
	p.indentStr += " " + " "
}

func (p *printer) deindent() {
	p.indentStr = p.indentStr[:len(p.indentStr)-2]
}

func (p *printer) newline() {
	p.sb.WriteByte('\n')
	p.sb.WriteString(p.indentStr)
}

func (p *printer) node(n d2ast.Node) {
	switch n := n.(type) {
	case *d2ast.Comment:
		p.comment(n)
	case *d2ast.BlockComment:
		p.blockComment(n)
	case *d2ast.Null:
		p.sb.WriteString("null")
	case *d2ast.Suspension:
		if n.Value {
			p.sb.WriteString("suspend")
		} else {
			p.sb.WriteString("unsuspend")
		}
	case *d2ast.Boolean:
		p.sb.WriteString(strconv.FormatBool(n.Value))
	case *d2ast.Number:
		p.sb.WriteString(n.Raw)
	case *d2ast.UnquotedString:
		p.interpolationBoxes(n.Value, false)
	case *d2ast.DoubleQuotedString:
		p.sb.WriteByte('"')
		p.interpolationBoxes(n.Value, true)
		p.sb.WriteByte('"')
	case *d2ast.SingleQuotedString:
		p.sb.WriteByte('\'')
		if n.Raw == "" {
			n.Raw = escapeSingleQuotedValue(n.Value)
		}
		p.sb.WriteString(escapeSingleQuotedValue(n.Value))
		p.sb.WriteByte('\'')
	case *d2ast.BlockString:
		p.blockString(n)
	case *d2ast.Substitution:
		p.substitution(n)
	case *d2ast.Import:
		p._import(n)
	case *d2ast.Array:
		p.array(n)
	case *d2ast.Map:
		p._map(n)
	case *d2ast.Key:
		p.mapKey(n)
	case *d2ast.KeyPath:
		p.key(n)
	case *d2ast.Edge:
		p.edge(n)
	case *d2ast.EdgeIndex:
		p.edgeIndex(n)
	}
}

func (p *printer) comment(c *d2ast.Comment) {
	lines := strings.Split(c.Value, "\n")
	for i, line := range lines {
		p.sb.WriteString("#")
		if line != "" {
			p.sb.WriteByte(' ')
		}
		p.sb.WriteString(line)
		if i < len(lines)-1 {
			p.newline()
		}
	}
}

func (p *printer) blockComment(bc *d2ast.BlockComment) {
	p.sb.WriteString(`"""`)
	if bc.Range.OneLine() {
		p.sb.WriteByte(' ')
	}

	p.blockLines(bc.Value, bc.Range.OneLine())

	if !bc.Range.OneLine() {
		p.newline()
	} else {
		p.sb.WriteByte(' ')
	}
	p.sb.WriteString(`"""`)
}

func (p *printer) blockLines(value string, oneLine bool) {
	// With no nonblank line, the parser keeps all indentation as literal text.
	indent := strings.TrimSpace(value) != ""
	for _, line := range strings.Split(value, "\n") {
		if !oneLine {
			if line != "" && indent {
				p.newline()
			} else {
				p.sb.WriteByte('\n')
			}
		}
		p.sb.WriteString(line)
	}
}

func (p *printer) interpolationBoxes(boxes []d2ast.InterpolationBox, isDoubleString bool) {
	wholeToken := len(boxes) == 1
	for i, b := range boxes {
		if b.Substitution != nil {
			p.substitution(b.Substitution)
			continue
		}

		var text string
		switch {
		case isDoubleString && (!wholeToken || b.StringRaw == nil):
			// In parsed double-quoted strings, the raw suffix can include text
			// from before a substitution. Each decoded fragment is independent.
			text = escapeDoubledQuotedValue(*b.String, p.inKey)
		case b.StringRaw != nil && (wholeToken || matchesUnquotedFragment(*b.StringRaw, *b.String, i == len(boxes)-1)):
			text = *b.StringRaw
		default:
			text = escapeUnquotedValue(*b.String, p.inKey, wholeToken)
		}
		if p.inKey && p.keyScope == diagramKeys && wholeToken && !isDoubleString {
			lower := strings.ToLower(text)
			if _, ok := d2ast.ReservedKeywords[lower]; ok {
				text = lower
			}
		}
		p.sb.WriteString(text)
	}
}

func (p *printer) blockString(bs *d2ast.BlockString) {
	quote := bs.Quote
	extend := '|'
	if quote != "" {
		extend, _ = utf8.DecodeLastRuneInString(quote)
	}
	for strings.Contains(bs.Value, "|"+quote) || strings.Contains(bs.Value, quote+"|") {
		quote += string(extend)
	}

	if bs.Range == (d2ast.Range{}) {
		if strings.IndexByte(bs.Value, '\n') > -1 {
			bs.Range = d2ast.MakeRange(",1:0:0-2:0:0")
		}
		bs.Value = strings.TrimSpace(bs.Value)
	}

	p.sb.WriteString("|" + quote)
	p.sb.WriteString(bs.Tag)
	if !bs.Range.OneLine() {
		p.indent()
	} else {
		p.sb.WriteByte(' ')
	}

	p.blockLines(bs.Value, bs.Range.OneLine())

	if !bs.Range.OneLine() {
		p.deindent()
		p.newline()
	} else if bs.Value != "" {
		p.sb.WriteByte(' ')
	}
	p.sb.WriteString(quote + "|")
}

func (p *printer) path(els []*d2ast.StringBox) {
	for i, s := range els {
		p.node(s.Unbox())
		if i < len(els)-1 {
			p.sb.WriteByte('.')
		}
	}
}

func (p *printer) substitution(s *d2ast.Substitution) {
	if s.Spread {
		p.sb.WriteString("...")
	}
	p.sb.WriteString("${")
	p.path(s.Path)
	p.sb.WriteByte('}')
}

func (p *printer) _import(i *d2ast.Import) {
	if i.Spread {
		p.sb.WriteString("...")
	}
	p.sb.WriteString("@")
	pre := path.Clean(i.Pre)
	if pre != "." {
		p.sb.WriteString(pre)
		p.sb.WriteRune('/')
	}
	if len(i.Path) == 0 {
		return
	}
	filename := path.Clean(i.Path[0].ScalarString())
	if len(i.Path) > 1 && i.Path[1].UnquotedString != nil && i.Path[1].ScalarString() == "d2" {
		// After a quoted filename, the parser reads .d2 as a field name.
		p.node(d2ast.FlatDoubleQuotedString(filename))
	} else {
		p.node(d2ast.RawString(filename, true))
	}
	if len(i.Path) > 1 {
		p.sb.WriteByte('.')
		p.path(i.Path[1:])
	}
}

func (p *printer) array(a *d2ast.Array) {
	p.sb.WriteByte('[')
	if !a.Range.OneLine() {
		p.indent()
	}

	prev := d2ast.Node(a)
	for i := 0; i < len(a.Nodes); i++ {
		nb := a.Nodes[i]
		n := nb.Unbox()

		// Handle inline comments.
		if i > 0 && (nb.Comment != nil || nb.BlockComment != nil) {
			if n.GetRange().Start.Line == prev.GetRange().End.Line && n.GetRange().OneLine() {
				p.sb.WriteByte(' ')
				p.node(n)
				continue
			}
		}

		if !a.Range.OneLine() {
			if prev != a {
				if n.GetRange().Start.Line-prev.GetRange().End.Line > 1 {
					p.sb.WriteByte('\n')
				}
			}
			p.newline()
		} else if i > 0 {
			p.sb.WriteString("; ")
		}

		p.node(n)
		prev = n
	}

	if !a.Range.OneLine() {
		p.deindent()
		p.newline()
	}
	p.sb.WriteByte(']')
}

func (p *printer) _map(m *d2ast.Map) {
	oneLine := !m.IsFileMap() && m.Range.OneLine()
	if !m.IsFileMap() {
		p.sb.WriteByte('{')
		if !oneLine {
			p.indent()
		}
	}

	boardNodes := []d2ast.MapNodeBox{}

	prev := d2ast.Node(m)
	for i := 0; i < len(m.Nodes); i++ {
		nb := m.Nodes[i]
		n := nb.Unbox()
		// extract out layer, scenario, and step nodes and skip
		if p.keyScope != variableKeys && nb.IsBoardNode() {
			if nb.MapKey.Value.Map != nil && len(nb.MapKey.Value.Map.Nodes) > 0 {
				boardNodes = append(boardNodes, nb)
			}
			prev = n
			continue
		}

		// A line comment can follow an entry without a separator.
		if i > 0 && nb.Comment != nil {
			if n.GetRange().Start.Line == prev.GetRange().End.Line && n.GetRange().OneLine() {
				p.sb.WriteByte(' ')
				p.node(n)
				continue
			}
		}

		if !oneLine {
			if prev != m {
				if n.GetRange().Start.Line-prev.GetRange().End.Line > 1 {
					p.sb.WriteByte('\n')
				}
			}
			if !m.IsFileMap() || i > 0 {
				p.newline()
			}
		} else if i > 0 {
			p.sb.WriteString("; ")
		}

		p.node(n)
		prev = n
	}

	// draw board nodes
	for i := 0; i < len(boardNodes); i++ {
		n := boardNodes[i].Unbox()
		// if this board is the very first line of the file, don't add an extra newline
		if n.GetRange().Start.Line != 0 {
			p.sb.WriteByte('\n')
		}
		// if scope only has boards, don't newline the first board
		if i != 0 || len(m.Nodes) > len(boardNodes) {
			p.sb.WriteByte('\n')
		}

		p.sb.WriteString(p.indentStr)
		p.node(n)
		prev = n
	}

	if !m.IsFileMap() {
		if !oneLine {
			p.deindent()
			p.newline()
		}
		p.sb.WriteByte('}')
	} else if len(m.Nodes) > 0 {
		// Always write a trailing newline for nonempty file maps.
		p.sb.WriteByte('\n')
	}
}

func (p *printer) mapKey(mk *d2ast.Key) {
	if mk.Ampersand {
		p.sb.WriteByte('&')
	} else if mk.NotAmpersand {
		p.sb.WriteByte('!')
		p.sb.WriteByte('&')
	}
	valueScope := p.keyScope
	if mk.Key != nil {
		valueScope = p.fieldPath(mk.Key)
	}

	if len(mk.Edges) > 0 {
		if mk.Key != nil {
			p.sb.WriteByte('.')
		}

		if mk.Key != nil || mk.EdgeIndex != nil || mk.EdgeKey != nil {
			p.sb.WriteByte('(')
		}
		if mk.Edges[0].Src != nil {
			p.key(mk.Edges[0].Src)
			p.sb.WriteByte(' ')
		}
		for i, e := range mk.Edges {
			p.edgeArrowAndDst(e)
			if i < len(mk.Edges)-1 {
				p.sb.WriteByte(' ')
			}
		}
		if mk.Key != nil || mk.EdgeIndex != nil || mk.EdgeKey != nil {
			p.sb.WriteByte(')')
		}

		if mk.EdgeIndex != nil {
			p.edgeIndex(mk.EdgeIndex)
		}
		if mk.EdgeKey != nil {
			p.sb.WriteByte('.')
			p.key(mk.EdgeKey)
		}
	}

	if mk.Primary.Unbox() != nil {
		p.sb.WriteString(": ")
		p.node(mk.Primary.Unbox())
	}
	if mk.Value.Map != nil && len(mk.Value.Map.Nodes) == 0 {
		return
	}
	if mk.Value.Unbox() != nil {
		if mk.Primary.Unbox() == nil {
			p.sb.WriteString(": ")
		} else {
			p.sb.WriteByte(' ')
		}
		parent := p.keyScope
		p.keyScope = valueScope
		p.node(mk.Value.Unbox())
		p.keyScope = parent
	}
}

// fieldPath follows scopes through dotted assignments just as through nested maps.
func (p *printer) fieldPath(k *d2ast.KeyPath) keyScope {
	parent := p.keyScope
	p.inKey = true
	for i, part := range k.Path {
		if i > 0 {
			p.sb.WriteByte('.')
		}
		s := part.Unbox()
		p.node(s)
		p.keyScope = p.keyScope.child(d2ast.Keyword(s))
	}
	p.inKey = false
	child := p.keyScope
	p.keyScope = parent
	return child
}

func (p *printer) key(k *d2ast.KeyPath) {
	p.inKey = true
	if k != nil {
		p.path(k.Path)
	}
	p.inKey = false
}

func (p *printer) edge(e *d2ast.Edge) {
	if e.Src != nil {
		p.key(e.Src)
		p.sb.WriteByte(' ')
	}
	p.edgeArrowAndDst(e)
}

func (p *printer) edgeArrowAndDst(e *d2ast.Edge) {
	if e.SrcArrow == "" {
		p.sb.WriteByte('-')
	} else {
		p.sb.WriteString(e.SrcArrow)
	}
	if e.DstArrow == "" {
		p.sb.WriteByte('-')
	} else {
		if e.SrcArrow != "" {
			p.sb.WriteByte('-')
		}
		p.sb.WriteString(e.DstArrow)
	}
	if e.Dst != nil {
		p.sb.WriteByte(' ')
		p.key(e.Dst)
	}
}

func (p *printer) edgeIndex(ei *d2ast.EdgeIndex) {
	p.sb.WriteByte('[')
	if ei.Glob {
		p.sb.WriteByte('*')
	} else {
		p.sb.WriteString(strconv.Itoa(*ei.Int))
	}
	p.sb.WriteByte(']')
}

func KeyPath(kp *d2ast.KeyPath) (ida []string) {
	for _, s := range kp.Path {
		// We format each string of the key to ensure the resulting strings can be parsed
		// correctly.
		n := &d2ast.KeyPath{
			Path: []*d2ast.StringBox{d2ast.MakeValueBox(d2ast.RawString(s.Unbox().ScalarString(), true)).StringBox()},
		}
		ida = append(ida, Format(n))
	}
	return ida
}
