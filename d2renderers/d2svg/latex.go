package d2svg

import (
	"strconv"
	"strings"
)

// MathJax emits the same outline at multiple positions in a formula. Only
// change the d attribute on its generated glyph paths, so CSS, transforms
// and character metadata still apply to the original elements.
func compactMathJaxGlyphs(markup string) string {
	const prefix = `<path data-c="`
	const pathAttr = `" d="`
	var out strings.Builder
	cache := make(map[string]string)
	copied := 0
	for scan := 0; ; {
		start := strings.Index(markup[scan:], prefix)
		if start < 0 {
			break
		}
		scan += start + len(prefix)
		endCode := strings.IndexByte(markup[scan:], '"')
		if endCode < 0 {
			break
		}
		scan += endCode
		if !strings.HasPrefix(markup[scan:], pathAttr) {
			continue
		}
		start = scan + len(pathAttr)
		end := strings.IndexByte(markup[start:], '"')
		if end < 0 {
			break
		}
		scan = start + end
		d := markup[start:scan]
		// The cache lives for one label. Skip unusually large outlines, and
		// bound the number of different glyphs retained from long formulas.
		if len(d) > 16<<10 {
			continue
		}
		short, ok := cache[d]
		if !ok {
			short = compactGlyphPath(d)
			if len(cache) < 128 {
				cache[d] = short
			}
		}
		if len(short) < len(d) {
			if copied == 0 {
				out.Grow(len(markup))
			}
			out.WriteString(markup[copied:start])
			out.WriteString(short)
			copied = scan
		}
	}
	if copied == 0 {
		return markup
	}
	out.WriteString(markup[copied:])
	return out.String()
}

// The bundled glyph font uses explicit uppercase commands with small integer
// coordinates. Keep other path grammars verbatim. All differences in this
// range are exactly representable even by a single-precision renderer.
func compactGlyphPath(d string) string {
	if len(d) == 0 || d[0] != 'M' {
		return d
	}
	var out strings.Builder
	out.Grow(len(d))
	var x, y, moveX, moveY int64
	var previous byte
	for i := 0; i < len(d); {
		op := d[i]
		i++
		n := 0
		switch op {
		case 'Z':
			out.WriteByte('Z')
			x, y = moveX, moveY
			previous = 'Z'
			continue
		case 'H', 'V':
			n = 1
		case 'M', 'L', 'T':
			n = 2
		case 'Q':
			n = 4
		default:
			return d
		}
		var abs, relative [4]int64
		origin := [4]int64{x, y, x, y}
		if op == 'V' {
			origin[0] = y
		}
		for k := 0; k < n; k++ {
			for i < len(d) && d[i] == ' ' {
				i++
			}
			negative := i < len(d) && d[i] == '-'
			if negative {
				i++
			}
			digits := i
			var v int64
			for i < len(d) && d[i] >= '0' && d[i] <= '9' {
				v = v*10 + int64(d[i]-'0')
				if v > 1<<20 {
					return d
				}
				i++
			}
			if digits == i {
				return d
			}
			if negative {
				v = -v
			}
			abs[k], relative[k] = v, v-origin[k]
		}
		var a, r [40]byte
		absolute := glyphCommand(a[:0], op, previous, abs[:n])
		delta := glyphCommand(r[:0], op+('a'-'A'), previous, relative[:n])
		if len(delta) < len(absolute) {
			out.Write(delta)
			previous = op + ('a' - 'A')
		} else {
			out.Write(absolute)
			previous = op
		}
		switch op {
		case 'H':
			x = abs[0]
		case 'V':
			y = abs[0]
		default:
			x, y = abs[n-2], abs[n-1]
		}
		if op == 'M' {
			moveX, moveY = x, y
		}
	}
	return out.String()
}

func glyphCommand(dst []byte, op, previous byte, values []int64) []byte {
	// A negative sign delimits a number. An immediately repeated drawing
	// command can therefore disappear before a negative first operand.
	if op != previous || op == 'M' || op == 'm' || values[0] >= 0 {
		dst = append(dst, op)
	}
	for i, v := range values {
		if i > 0 && v >= 0 {
			dst = append(dst, ' ')
		}
		dst = strconv.AppendInt(dst, v, 10)
	}
	return dst
}
