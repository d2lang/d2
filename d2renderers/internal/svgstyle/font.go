// Package svgstyle writes font rules shared by SVG rendering and appendix generation.
package svgstyle

import "strings"

// FontClassRule selects a generated face for a text class. An empty scope
// selects the global fallback used by a standalone appendix.
func FontClassRule(scope, class, family string) string {
	selector := "." + class
	if scope != "" {
		selector = "." + scope + " " + selector
	}
	return selector + "{font-family:" + family + "}"
}

func FontFace(scope, class, family, uri string) string {
	// D2's generated faces are base64 data URLs. A caller can also register
	// a fallback encoding with characters that require a quoted CSS URL.
	if !strings.HasPrefix(uri, "data:application/font-woff;base64,") || strings.ContainsAny(uri, " \t\r\n\f()'\"\\") {
		uri = `"` + uri + `"`
	}
	return FontClassRule(scope, class, family) + "@font-face{font-family:" + family + ";src:url(" + uri + ")}"
}

// StyleText embeds CSS in XML without modifying the stylesheet text.
func StyleText(css string) string {
	if strings.ContainsAny(css, "<&") || strings.Contains(css, "]]>") {
		return "<![CDATA[" + strings.ReplaceAll(css, "]]>", "]]]]><![CDATA[>") + "]]>"
	}
	return css
}
