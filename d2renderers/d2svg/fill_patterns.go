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
	needScan := strings.Contains(source, "&#")
	for _, p := range d2ast.FillPatterns {
		class := p + "-overlay"
		if !classes[class] && !strings.Contains(source, p) && !(custom && strings.Contains(BaseStylesheet, p)) {
			continue
		}
		ref := "#" + p + "-" + diagramHash
		hasRef := strings.Contains(source, ref)
		unknownClass := classes == nil && strings.Contains(source, class)
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
		if err == io.EOF {
			if opaque {
				maps.Copy(used, candidates)
			}
			return used
		}
		if err != nil {
			return candidates
		}
		el, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		// Opaque styles, scripts and animation can select a class indirectly.
		// Keep the legacy candidates in those cases, including any classes or
		// resource names mentioned by their source text.
		switch el.Name.Local {
		case "style", "script", "animate", "animateTransform", "animateMotion", "set", "foreignObject":
			opaque = true
		}
		for _, a := range el.Attr {
			if strings.HasPrefix(strings.ToLower(a.Name.Local), "on") {
				opaque = true
			}
			switch a.Name.Local {
			case "class":
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

// The renderer's resource IDs are ASCII identifiers. Accept local CSS URLs in
// both quoted and unquoted form; the hash suffix keeps a sibling board's
// reference from selecting this board's pattern.
var fillPatternURL = regexp.MustCompile(`(?i:url)\(\s*["']?#([^\s)"']+)["']?\s*\)`)

func markPatternID(used map[string]bool, id, diagramHash string) {
	if p, ok := strings.CutSuffix(id, "-"+diagramHash); ok && slices.Contains(d2ast.FillPatterns, p) {
		used[p] = true
	}
}
