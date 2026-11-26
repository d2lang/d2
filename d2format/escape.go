package d2format

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/internal/d2string"
)

func escapeSingleQuotedValue(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteByte('\'')
		case '\n':
			// TODO: Unified string syntax.
			b.WriteByte('\\')
			b.WriteByte('n')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func escapeDoubledQuotedValue(s string, inKey bool) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
		case '\n':
			b.WriteByte('\\')
			b.WriteByte('n')
			continue
		}
		if !inKey && r == '$' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func escapeUnquotedValue(s string, inKey, wholeToken bool) string {
	if wholeToken && s == "" {
		return `""`
	}

	if wholeToken && strings.EqualFold(s, "null") {
		return `'null'`
	}

	var b strings.Builder
	for i, r := range s {
		switch r {
		case '\'', '"', '|':
			if i == 0 {
				b.WriteByte('\\')
			}
		case '\n':
			b.WriteByte('\\')
			b.WriteByte('n')
			continue
		default:
			if inKey {
				switch r {
				case '-':
					if i+1 < len(s) && s[i+1] == '-' {
						b.WriteByte('\\')
					}
				case '&':
					if i == 0 {
						b.WriteByte('\\')
					}
				default:
					if strings.ContainsRune(d2ast.UnquotedKeySpecials, r) {
						b.WriteByte('\\')
					}
				}
			} else if strings.ContainsRune(d2ast.UnquotedValueSpecials, r) {
				b.WriteByte('\\')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

// A caller can edit a parsed fragment's text without updating its raw spelling.
// Compare before reusing the raw escapes, including the distinction between
// a literal \* and a wildcard. The parser folds line continuations before
// storing the raw fragment and trims trailing whitespace from the final value.
func matchesUnquotedFragment(raw, text string, last bool) bool {
	if !strings.Contains(raw, `\`) {
		return raw == text
	}
	escaped := false
	for _, r := range raw {
		if escaped {
			r = d2string.DecodeEscape(r)
			escaped = false
		} else if r == '\\' {
			escaped = true
			continue
		}
		if text == "" && last && unicode.IsSpace(r) {
			continue
		}
		want, size := utf8.DecodeRuneInString(text)
		if size == 0 || r != want {
			return false
		}
		text = text[size:]
	}
	return !escaped && text == ""
}
