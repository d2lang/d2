package d2svg

import (
	"encoding/xml"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
)

// Most boards do not mention a pattern. Only inspect XML if the old markup
// trigger, a local resource reference, or an XML character reference occurs.
func usedFillPatterns(source, diagramHash string, classes map[string]bool) map[string]bool {
	var candidates map[string]bool
	custom := !UsesDefaultStylesheet()
	needScan := encodedPatternName(source)
	for _, p := range d2ast.FillPatterns {
		class := p + "-overlay"
		if !classes[class] && !strings.Contains(source, p) && !(custom && strings.Contains(BaseStylesheet, p)) {
			continue
		}
		ref := "#" + p + "-" + diagramHash
		hasRef := strings.Contains(source, ref)
		unknownClass := (custom || classes == nil) && strings.Contains(source, class)
		needScan = needScan || hasRef || unknownClass
		if classes[class] || unknownClass || hasRef || custom && strings.Contains(BaseStylesheet, ref) {
			if candidates == nil {
				candidates = make(map[string]bool)
			}
			candidates[p] = true
		}
	}
	if custom || !needScan {
		return candidates
	}

	used := make(map[string]bool)
	opaque := false
	decoder := xml.NewDecoder(strings.NewReader(source))
	for {
		token, err := decoder.Token()
		if err != nil {
			if err != io.EOF || opaque {
				maps.Copy(used, candidates)
			}
			return used
		}
		el, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		// Opaque styles, scripts and animation can select a class indirectly.
		// Keep the legacy candidates in those cases, including any classes or
		// resource names mentioned by their source text.
		switch el.Name.Local {
		case "style", "script":
			opaque = true
			var text string
			if err := decoder.DecodeElement(&text, &el); err != nil {
				return candidates
			}
			for _, p := range d2ast.FillPatterns {
				if strings.Contains(text, p+"-overlay") || strings.Contains(text, "#"+p+"-"+diagramHash) {
					used[p] = true
				}
			}
		case "animate", "animateTransform", "animateMotion", "set", "foreignObject":
			opaque = true
		}
		for _, a := range el.Attr {
			if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") {
				opaque = true
			}
			switch a.Name.Local {
			case "class":
				if a.Name.Space != "" {
					continue
				}
				for class := range strings.FieldsFuncSeq(a.Value, func(r rune) bool {
					return r < 128 && themeRuleWhitespace(byte(r))
				}) {
					if p, ok := strings.CutSuffix(class, "-overlay"); ok && slices.Contains(d2ast.FillPatterns, p) {
						used[p] = true
					}
				}
			case "href":
				if p, ok := strings.CutPrefix(strings.TrimSpace(a.Value), "#"); ok {
					markPatternID(used, p, diagramHash)
				}
			case "fill", "stroke", "style":
				if !strings.Contains(a.Value, "(") {
					continue
				}
				for _, ref := range fillPatternURL.FindAllStringSubmatch(a.Value, -1) {
					markPatternID(used, ref[1], diagramHash)
				}
			}
		}
	}
}

// The XML serializer escapes quotes and whitespace too. Those cannot encode
// any part of a pattern class or resource ID. Ignore them so ordinary quoted
// labels can use the writer's class metadata without a second XML pass.
func encodedPatternName(source string) bool {
	for {
		_, tail, found := strings.Cut(source, "&#")
		if !found {
			return false
		}
		entity, rest, _ := strings.Cut(tail, ";")
		switch entity {
		case "34", "39", "x9", "xA", "xD":
			source = rest
		default:
			return true
		}
	}
}

// The renderer's resource IDs are ASCII identifiers. Accept local CSS URLs in
// both quoted and unquoted form; the hash suffix keeps a sibling board's
// reference from selecting this board's pattern.
var fillPatternURL = regexp.MustCompile(`(?i:url)\(\s*["']?#([^\s)"']+)["']?\s*\)`)

func markPatternID(used map[string]bool, id, diagramHash string) {
	if p, ok := strings.CutSuffix(id, "-"+diagramHash); ok && slices.Contains(d2ast.FillPatterns, p) {
		used[p] = true
	}
}
