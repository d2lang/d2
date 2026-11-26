package lspserver

import (
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2ir"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/internal/d2string"
)

// A near value is a scalar containing a second layer of D2 syntax, an
// absolute key path. Re-encode both layers so a literal dot stays in its name.
type nearReference struct {
	value    d2ast.Range
	key      *d2ast.KeyPath
	part     int
	computed bool
	noHit    bool
}

func (n *nearReference) rename(name d2ast.String) textEdit {
	key := n.key.Copy()
	key.Path[n.part] = d2ast.MakeValueBox(name).StringBox()
	value := d2ast.RawString(d2format.Format(key), false)
	return textEdit{Range: toLSPRange(n.value), NewText: d2format.Format(value)}
}

func (s *Server) nearOccurrences(doc document, near *d2ir.Field, value nearValue, lines map[string][]string) []occurrence {
	path, err := d2parser.ParseKey(value.resolved)
	if err != nil {
		return nil
	}
	board := d2ir.ParentBoard(near)
	if board == nil {
		return nil
	}
	m := board.Map()
	scalar := value.scalar
	r := value.source
	if name, ok := scalar.(d2ast.String); ok && !value.computed {
		r = s.fieldRange(doc, name, lines)
	}
	var spans []d2ast.Range
	if !value.computed {
		spans = s.nearPartRanges(doc, scalar, r, path, lines)
	}
	var out []occurrence
	for i, box := range path.Path {
		name := box.Unbox()
		if name.IsUnquoted() && name.ScalarString() == "_" {
			return nil
		}
		var field *d2ir.Field
		if m != nil {
			field = m.GetField(name)
		}
		// NearTarget accepts a trailing property (e.g. box.label) as its
		// container. Board words may also name real children below a shape.
		keyword := d2ast.Keyword(name)
		if i == len(path.Path)-1 && keyword != "" && keyword != "style" && (field == nil || d2ast.Keyword(field.Name) != "") {
			break
		}
		if field == nil || d2ast.Keyword(field.Name) != "" {
			// Keep track of literal placement constants for sharedNearTarget:
			// another board may instead have a shape named top-left. The
			// constant has no owner, so it is never a navigable occurrence.
			if _, constant := d2ast.NearConstants[path.Path[0].Unbox().ScalarString()]; constant {
				return []occurrence{{rng: r, near: &nearReference{value: r, key: path}}}
			}
			return nil
		}
		rng := r
		if len(spans) != 0 {
			rng = spans[i]
		}
		out = append(out, occurrence{
			rng: rng, field: field, kind: kindObject, name: name,
			near: &nearReference{value: r, key: path, part: i, computed: value.computed, noHit: len(spans) == 0 && len(path.Path) > 1},
		})
		m = field.Map()
	}
	return out
}

// ParseKey returns byte offsets in the decoded scalar. Stream through its
// source spelling to translate those boundaries past quotes and escapes.
func (s *Server) nearPartRanges(doc document, scalar d2ast.Scalar, r d2ast.Range, key *d2ast.KeyPath, lines map[string][]string) []d2ast.Range {
	if len(key.Path) == 1 {
		return []d2ast.Range{r}
	}
	start := r.Start
	var raw string
	var quote rune
	switch n := scalar.(type) {
	case *d2ast.UnquotedString:
		if len(n.Value) != 1 || n.Value[0].StringRaw == nil {
			return nil
		}
		raw = *n.Value[0].StringRaw
	case *d2ast.DoubleQuotedString:
		if len(n.Value) != 1 || n.Value[0].StringRaw == nil {
			return nil
		}
		quote = '"'
		raw = *n.Value[0].StringRaw
	case *d2ast.SingleQuotedString:
		quote = '\''
		raw = strings.ReplaceAll(n.Value, "'", "''")
	case *d2ast.BlockString:
		quote = '|'
	default:
		return nil
	}
	// A folded string loses line continuations in its AST. Block strings
	// also shed their fence and indentation. Recover their physical positions.
	if !r.OneLine() || quote == '|' {
		raw = literalSource(s.sourceLines(doc, r.Path, lines), r)
		if raw == "" {
			return nil
		}
		if quote == '|' {
			header := strings.IndexFunc(raw, unicode.IsSpace)
			if header < 0 {
				return nil
			}
			body := strings.TrimSuffix(raw[header:], scalar.(*d2ast.BlockString).Quote+"|")
			i := strings.Index(body, scalar.ScalarString())
			if i < 0 {
				return nil
			}
			start = start.AdvanceString(raw[:header+i], true)
			raw = scalar.ScalarString()
		} else if quote != 0 {
			raw = strings.TrimPrefix(raw, string(quote))
		}
	}
	if quote != 0 && quote != '|' {
		start = start.Advance(quote, true)
	}

	skipFold := func() bool {
		if quote == '|' || !strings.HasPrefix(raw, "\\\n") {
			return false
		}
		old := raw
		raw = raw[2:]
		if quote == 0 {
			raw = strings.TrimLeftFunc(raw, unicode.IsSpace)
		}
		start = start.AdvanceString(old[:len(old)-len(raw)], true)
		return true
	}
	offset := 0
	advanceTo := func(target int) d2ast.Position {
		for len(raw) != 0 && offset < target {
			if skipFold() {
				continue
			}
			ch, n := utf8.DecodeRuneInString(raw)
			raw = raw[n:]
			start = start.Advance(ch, true)
			if quote == '\'' && ch == '\'' && strings.HasPrefix(raw, "'") {
				raw = raw[1:]
				start = start.Advance(ch, true)
			} else if quote != '\'' && quote != '|' && ch == '\\' && len(raw) != 0 {
				next, n := utf8.DecodeRuneInString(raw)
				raw = raw[n:]
				start = start.Advance(next, true)
				ch = d2string.DecodeEscape(next)
			}
			offset += utf8.RuneLen(ch)
		}
		return start
	}
	ranges := make([]d2ast.Range, 0, len(key.Path))
	text := scalar.ScalarString()
	for _, box := range key.Path {
		name := box.Unbox()
		part := name.GetRange()
		// As in fieldRange, a final escape operand can fall just outside
		// an unquoted element's reported range.
		end := part.End.Byte
		if n, ok := name.(*d2ast.UnquotedString); ok && len(n.Value) == 1 && n.Value[0].StringRaw != nil {
			spelling := *n.Value[0].StringRaw
			if strings.HasPrefix(text[part.Start.Byte:], spelling) {
				end = max(end, part.Start.Byte+len(spelling))
			}
		}
		advanceTo(part.Start.Byte)
		for skipFold() {
		}
		begin := start
		ranges = append(ranges, d2ast.Range{Path: r.Path, Start: begin, End: advanceTo(end)})
	}
	return ranges
}

func literalSource(lines []string, r d2ast.Range) string {
	if r.Start.Line < 0 || r.End.Line >= len(lines) || r.Start.Line > r.End.Line {
		return ""
	}
	byteAt := func(line string, column int) int {
		for i, ch := range line {
			if column <= 0 {
				return i
			}
			column -= utf16.RuneLen(ch)
		}
		return len(line)
	}
	parts := append([]string(nil), lines[r.Start.Line:r.End.Line+1]...)
	last := len(parts) - 1
	parts[last] = parts[last][:byteAt(parts[last], r.End.Column)]
	parts[0] = parts[0][byteAt(parts[0], r.Start.Column):]
	return strings.Join(parts, "\n")
}

// An imported placement can bind to unrelated declarations in different
// boards. Its single physical value cannot be edited independently in each.
func (snap *snapshot) sharedNearTarget(sym *symbol) bool {
	type site struct {
		value d2ast.Range
		part  int
	}
	uses := make(map[site]bool)
	for _, o := range sym.occurrences {
		if o.near != nil {
			uses[site{o.near.value, o.near.part}] = true
		}
	}
	if len(uses) != 0 {
		for _, o := range snap.index {
			if o.near != nil && o.group != sym.hit.group && uses[site{o.near.value, o.near.part}] {
				return true
			}
		}
	}
	return false
}
