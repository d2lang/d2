// Package htmlentity unescapes HTML character references exactly as the
// standard library's html.UnescapeString does, without linking its entity
// table. The standard library builds that table in code the first time it is
// used, which costs a WebAssembly module over 100 KB; golang.org/x/net/html,
// which D2 links to parse Markdown, already holds the same table as data.
package htmlentity

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Unescape returns s with its character references replaced, as
// html.UnescapeString does.
func Unescape(s string) string {
	i := strings.IndexByte(s, '&')
	if i < 0 {
		return s
	}
	b := make([]byte, 0, len(s))
	for i >= 0 {
		b = append(b, s[:i]...)
		s = s[i:]
		var n int
		if len(s) > 1 && s[1] == '#' {
			b, n = appendNumeric(b, s)
		} else {
			b, n = appendNamed(b, s)
		}
		s = s[n:]
		i = strings.IndexByte(s, '&')
	}
	return string(append(b, s...))
}

// appendNumeric appends the character that the numeric reference at the start
// of s stands for and returns how much of s it read. It follows the standard
// library, which reads a reference of one digit and no semicolon as text.
func appendNumeric(b []byte, s string) ([]byte, int) {
	if len(s) <= 3 {
		return append(b, '&'), 1
	}
	i := 2
	hex := s[i] == 'x' || s[i] == 'X'
	if hex {
		i++
	}
	var x rune
	for i < len(s) {
		c := s[i]
		i++
		switch {
		case '0' <= c && c <= '9' && hex:
			x = 16*x + rune(c) - '0'
		case '0' <= c && c <= '9':
			x = 10*x + rune(c) - '0'
		case 'a' <= c && c <= 'f' && hex:
			x = 16*x + rune(c) - 'a' + 10
		case 'A' <= c && c <= 'F' && hex:
			x = 16*x + rune(c) - 'A' + 10
		default:
			if c != ';' {
				i--
			}
			return appendCodePoint(b, s, x, i)
		}
	}
	return appendCodePoint(b, s, x, i)
}

func appendCodePoint(b []byte, s string, x rune, read int) ([]byte, int) {
	if read <= 3 {
		return append(b, '&'), 1
	}
	if 0x80 <= x && x <= 0x9F {
		// These are bytes of Windows-1252, which x/net/html maps the same way.
		return append(b, html.UnescapeString("&#"+strconv.Itoa(int(x))+";")...), read
	}
	if x == 0 || (0xD800 <= x && x <= 0xDFFF) || x > 0x10FFFF {
		x = utf8.RuneError
	}
	return utf8.AppendRune(b, x), read
}

// appendNamed appends the replacement of the named reference at the start of
// s and returns how much of s it read.
func appendNamed(b []byte, s string) ([]byte, int) {
	i := 1
	for i < len(s) {
		c := s[i]
		i++
		if 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || '0' <= c && c <= '9' {
			continue
		}
		if c != ';' {
			i--
		}
		break
	}
	reference := s[:i]
	// x/net/html knows the same names as the standard library and two more,
	// whose replacements are longer than they are.
	if reference == "&nGt;" || reference == "&nLt;" {
		return append(b, reference...), i
	}
	return append(b, html.UnescapeString(reference)...), i
}
