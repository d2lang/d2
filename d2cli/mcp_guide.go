//go:build !d2nomcp

package d2cli

import (
	"context"
	_ "embed"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/d2themes"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/version"
)

//go:embed mcpassets/guide.md
var mcpGuide string

var mcpStyleDescriptions = map[string]string{
	"opacity":        "number from 0 to 1",
	"stroke":         "color: a CSS name, a hex code, or a gradient",
	"fill":           "color: a CSS name, a hex code, or a gradient",
	"fill-pattern":   "one of the fill patterns below",
	"stroke-width":   "integer from 0 to 15",
	"stroke-dash":    "integer from 0 to 10",
	"border-radius":  "integer, 0 or greater",
	"font":           "`default` or `mono`",
	"font-size":      "integer from 8 to 100",
	"font-color":     "color",
	"bold":           "`true` or `false`",
	"italic":         "`true` or `false`",
	"underline":      "`true` or `false`",
	"text-transform": "one of the text transforms below",
	"shadow":         "`true` or `false`, shapes only",
	"multiple":       "`true` or `false`, shapes only",
	"double-border":  "`true` or `false`, on squares, rectangles, circles, and ovals",
	"3d":             "`true` or `false`, on squares, rectangles, and hexagons",
	"animated":       "`true` or `false`. Animates shapes or connection dashes and plays messages in order on a sequence diagram",
	"filled":         "`true` or `false`, connections only",
}

// Source spellings, in the order shown by the reference. The JSON keys on
// d2target.Config describe the compiled diagram and use different names.
var mcpConfigKeys = []string{
	"layout-engine", "theme-id", "dark-theme-id", "sketch", "pad", "center", "scale",
	"animate-interval", "animate-duration", "force-appendix", "theme-overrides", "dark-theme-overrides", "data",
}

func (s *mcpServer) guide(_ context.Context, _ *mcp.CallToolRequest, in *guideInput) (*mcp.CallToolResult, any, error) {
	var guide string
	if in.Topic == "reference" {
		guide = mcpReference()
	} else {
		_, body, _ := strings.Cut(mcpGuide, "\n")
		guide = fmt.Sprintf("# D2 guide (D2 %s)\n%s", version.Version, body)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: guide}}}, nil, nil
}

func mcpReference() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# D2 reference (D2 %s)\n", version.Version)
	list := func(title string, items []string) {
		fmt.Fprintf(&b, "\n## %s\n%s\n", title, strings.Join(items, ", "))
	}
	list("Shapes", d2target.Shapes)
	list("Arrowhead shapes", slices.Sorted(maps.Keys(d2target.Arrowheads)))
	b.WriteString("\n## Style keywords\nWrite these under `style`, for example `style.fill: \"#fff\"`.\n")
	for _, key := range slices.Sorted(maps.Keys(d2ast.StyleKeywords)) {
		description := mcpStyleDescriptions[key]
		if description == "" {
			description = "see https://d2lang.com/tour/style"
		}
		fmt.Fprintf(&b, "%s: %s\n", key, description)
	}
	list("Fill patterns", d2ast.FillPatterns)
	list("Text transforms", d2ast.TextTransforms)
	list("Positions for `near` on a top-level shape", d2ast.NearConstantsArray)
	list("Positions for `label.near` and `icon.near`", d2ast.LabelPositionsArray)
	b.WriteString("\n## Themes\nSet with `vars.d2-config.theme-id` or the render tool's `theme`.\n")
	for _, catalog := range [][]d2themes.Theme{d2themescatalog.LightCatalog, d2themescatalog.DarkCatalog} {
		for _, theme := range catalog {
			fmt.Fprintf(&b, "%d %s\n", theme.ID, theme.Name)
		}
	}
	fmt.Fprintf(&b, "\n## Layout engines\n%s. Default: dagre.\n", strings.Join(builtinLayoutNames(), ", "))
	b.WriteString("\n## Reserved keywords\nQuote these words when using them as shape names. Inside a container, layers, scenarios, and steps may be unquoted.\n")
	b.WriteString(strings.Join(slices.Sorted(maps.Keys(d2ast.ReservedKeywords)), ", "))
	b.WriteString("\n\n## d2-config keys\nSet under `vars.d2-config`.\n")
	b.WriteString(strings.Join(mcpConfigKeys, ", "))
	b.WriteByte('\n')
	return b.String()
}
