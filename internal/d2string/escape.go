// Package d2string contains string syntax shared by the D2 parser and formatter.
package d2string

// DecodeEscape decodes the rune following a backslash in a D2 string literal.
func DecodeEscape(r rune) rune {
	switch r {
	case 'a':
		return '\a'
	case 'b':
		return '\b'
	case 'f':
		return '\f'
	case 'n':
		return '\n'
	case 'r':
		return '\r'
	case 't':
		return '\t'
	case 'v':
		return '\v'
	case '\\':
		return '\\'
	case '"':
		return '"'
	default:
		return r
	}
}
