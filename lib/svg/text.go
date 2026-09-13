package svg

import (
	"bytes"
	"encoding/base32"
	"encoding/xml"
	"strings"
	"unicode/utf8"
)

func EscapeText(text string) string {
	buf := new(bytes.Buffer)
	_ = xml.EscapeText(buf, []byte(text))
	return buf.String()
}

// EscapeAttribute escapes a value for use in a double-quoted XML attribute.
// Single quotes are left unchanged because they cannot terminate that context.
// XML attribute whitespace is escaped so parsing preserves the original value.
func EscapeAttribute(attribute string) string {
	return strings.ReplaceAll(EscapeText(attribute), "&#39;", "'")
}

func validateXMLString(value string) {
	if validXMLString(value) {
		return
	}
	panic("invalid UTF-8 or character in XML value")
}

func validXMLString(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if validXMLRune(r) {
			continue
		}
		return false
	}
	return true
}

func validXMLRune(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' ||
		r >= 0x20 && r <= 0xD7FF ||
		r >= 0xE000 && r <= 0xFFFD ||
		r >= 0x10000 && r <= 0x10FFFF
}

func SVGID(text string) string {
	return strings.TrimRight(base32.StdEncoding.EncodeToString([]byte(text)), "=")
}
