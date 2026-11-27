package d2svg

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/d2lang/d2/d2renderers/internal/svgstyle"
)

// compactCSS handles the grammar of D2's own rules. Caller-supplied CSS and
// font data URLs never pass through it.
func compactCSS(css string) string {
	var out strings.Builder
	out.Grow(len(css))
	var prev byte
	for i := 0; i < len(css); i++ {
		c := css[i]
		if themeRuleWhitespace(c) {
			for i+1 < len(css) && themeRuleWhitespace(css[i+1]) {
				i++
			}
			if prev != 0 && i+1 < len(css) && !strings.ContainsRune("{};,>:", rune(prev)) && !strings.ContainsRune("{};,>", rune(css[i+1])) {
				out.WriteByte(' ')
			}
			continue
		}
		if c == ';' {
			next := i + 1
			for next < len(css) && themeRuleWhitespace(css[next]) {
				next++
			}
			if next < len(css) && css[next] == '}' {
				continue
			}
		}
		out.WriteByte(c)
		// Apple ImageIO skips the rule immediately following a nested at-rule
		// unless whitespace separates them. The next rule can be appended by
		// another writer, as with fill patterns after a dark theme.
		if c == '}' && prev == '}' {
			out.WriteByte(' ')
		}
		prev = c
	}
	return out.String()
}

var compactBaseStylesheet = compactCSS(defaultBaseStylesheet)
var baseWithoutBlend = regexp.MustCompile(`\.blend\{[^{}]*\}`).ReplaceAllString(compactBaseStylesheet, "")

const legacyStyleOpen = `<style type="text/css"><![CDATA[`
const legacyStyleClose = `]]></style>`

func writeCDATA(buf *bytes.Buffer, css string) {
	buf.WriteString(strings.ReplaceAll(css, "]]>", "]]]]><![CDATA[>"))
}

// closeStyle writes plain XML text for ordinary CSS. A registered fallback
// font may contain XML delimiters, so protect those exceptional stylesheets.
func closeStyle(buf *bytes.Buffer, contentStart int) {
	css := buf.Bytes()[contentStart:]
	if bytes.ContainsAny(css, "<&") || bytes.Contains(css, []byte("]]>")) {
		text := string(css)
		buf.Truncate(contentStart)
		buf.WriteString(svgstyle.StyleText(text))
	}
	buf.WriteString("</style>")
}
