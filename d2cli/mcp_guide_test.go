//go:build !d2nomcp

package d2cli

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/localfile"
	"github.com/d2lang/d2/lib/version"
)

func TestMCPGuide(t *testing.T) {
	session := newTestMCP(t, mcpConfig{})
	for _, tc := range []struct {
		topic, title string
		contains     []string
	}{
		{"", "guide", []string{"## House style"}},
		{"reference", "reference", []string{"cylinder", "stroke-width", "top-center", "tala", "\n0 Neutral Default\n"}},
	} {
		t.Run(tc.title, func(t *testing.T) {
			args := map[string]any{}
			if tc.topic != "" {
				args["topic"] = tc.topic
			}
			result := callMCP(t, session, "guide", args)
			if result.IsError || result.StructuredContent != nil || len(result.Content) != 1 {
				t.Fatalf("unexpected guide result: %#v", result)
			}
			block, ok := result.Content[0].(*mcp.TextContent)
			prefix := fmt.Sprintf("# D2 %s (D2 %s)\n", tc.title, version.Version)
			if !ok || !strings.HasPrefix(block.Text, prefix) {
				t.Fatalf("expected text beginning with %q, got %#v", prefix, result.Content[0])
			}
			for _, want := range tc.contains {
				if !strings.Contains(block.Text, want) {
					t.Errorf("guide missing %q", want)
				}
			}
		})
	}
}

func TestMCPGuideReferenceCompleteness(t *testing.T) {
	for key := range d2ast.StyleKeywords {
		if mcpStyleDescriptions[key] == "" {
			t.Errorf("style keyword %q needs a description", key)
		}
	}
	scalarValues := map[string]string{
		"layout-engine": "tala", "theme-id": "0", "dark-theme-id": "200", "sketch": "true",
		"pad": "10", "center": "true", "scale": "1.2", "animate-interval": "100",
		"animate-duration": "2", "force-appendix": "true",
	}
	for _, key := range mcpConfigKeys {
		switch key {
		case "theme-overrides", "dark-theme-overrides", "data":
			continue // These keys take nested maps.
		}
		value, ok := scalarValues[key]
		if !ok {
			t.Errorf("missing test value for d2-config key %q", key)
			continue
		}
		source := fmt.Sprintf("vars: {d2-config: {%s: %s}}", key, value)
		if _, _, err := d2compiler.Compile("guide.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: localfile.Policy{}}); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}

func TestMCPGuideExamplesCompile(t *testing.T) {
	blocks := regexp.MustCompile("(?ms)^```d2\\n(.*?)^```$").FindAllStringSubmatch(mcpGuide, -1)
	if len(blocks) < 15 {
		t.Fatalf("found only %d D2 examples", len(blocks))
	}
	for i, block := range blocks {
		if _, _, err := d2compiler.Compile(fmt.Sprintf("guide-example-%d.d2", i+1), strings.NewReader(block[1]), &d2compiler.CompileOptions{FS: localfile.Policy{}}); err != nil {
			t.Errorf("%v\n%s", err, block[1])
		}
	}
}

// Run the snippets printed in the table. Several examples compile in both
// forms, so success alone cannot tell a nested name from a literal one.
func TestMCPGuideMistakes(t *testing.T) {
	_, section, ok := strings.Cut(mcpGuide, "## Mistakes to avoid\n")
	require.True(t, ok)
	_, table, ok := strings.Cut(section, "|---|---|---|\n")
	require.True(t, ok)
	table, _, _ = strings.Cut(table, "\n\n")

	codeSpan := regexp.MustCompile("`([^`]+)`")
	snippets := func(cell string) []string {
		var out []string
		for _, m := range codeSpan.FindAllStringSubmatch(cell, -1) {
			out = append(out, m[1])
		}
		return out
	}
	seen := make(map[string]bool)
	for _, row := range strings.Split(table, "\n") {
		cells := strings.Split(strings.Trim(row, "|"), "|")
		require.Len(t, cells, 3, "invalid mistakes table row: %s", row)
		before, note, after := snippets(cells[0]), snippets(cells[1]), snippets(cells[2])
		require.NotEmpty(t, before)
		require.NotEmpty(t, after)
		require.False(t, seen[before[0]], "duplicate row: %s", row)
		seen[before[0]] = true

		t.Run(before[0], func(t *testing.T) {
			compile := func(source string) *d2graph.Graph {
				t.Helper()
				g, _, err := d2compiler.Compile("guide.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: localfile.Policy{}})
				require.NoError(t, err, "source: %s", source)
				return g
			}
			reject := func(source, message string) {
				t.Helper()
				_, _, err := d2compiler.Compile("guide.d2", strings.NewReader(source), &d2compiler.CompileOptions{FS: localfile.Policy{}})
				require.ErrorContains(t, err, message, "source: %s", source)
			}
			objects := func(g *d2graph.Graph, ids ...string) {
				t.Helper()
				require.Len(t, g.Objects, len(ids))
				for i, id := range ids {
					require.Equal(t, id, g.Objects[i].AbsID())
				}
			}
			label := func(source string) string {
				t.Helper()
				g := compile(source)
				objects(g, "a")
				return g.Objects[0].Label.Value
			}
			bad, fix := before[0], after[0]
			switch bad {
			case "main.go -> utils.go":
				g := compile(bad)
				objects(g, "main", "main.go", "utils", "utils.go")
				require.Len(t, g.Edges, 1)
				require.Equal(t, "main.go", g.Edges[0].Src.AbsID())
				require.Equal(t, "utils.go", g.Edges[0].Dst.AbsID())
				g = compile(fix)
				objects(g, `"main.go"`, `"utils.go"`)
				require.Len(t, g.Edges, 1)
				require.Equal(t, "main.go", g.Edges[0].Src.IDVal)
				require.Equal(t, "utils.go", g.Edges[0].Dst.IDVal)
			case "api.example.com":
				require.Len(t, after, len(before))
				for i, name := range before {
					parts := strings.Split(name, ".")
					var nested []string
					for j := range parts {
						nested = append(nested, strings.Join(parts[:j+1], "."))
					}
					objects(compile(name), nested...)
					objects(compile(after[i]), strconv.Quote(name))
				}
			case "nginx:1.25 -> redis:7":
				g := compile(bad)
				objects(g, "nginx")
				require.Equal(t, "1.25 -> redis:7", g.Objects[0].Label.Value)
				require.Empty(t, g.Edges)
				g = compile(fix)
				require.Len(t, g.Edges, 1)
				require.Equal(t, "nginx:1.25", g.Edges[0].Src.IDVal)
				require.Equal(t, "redis:7", g.Edges[0].Dst.IDVal)
			case "a -> b: GET /users/{id}":
				reject(bad, "edge map keys must be reserved keywords")
				g := compile(fix)
				require.Len(t, g.Edges, 1)
				require.Equal(t, "GET /users/{id}", g.Edges[0].Label.Value)
			case "a: C# service":
				require.Equal(t, "C", label(bad))
				require.Equal(t, "#tag", label(note[len(note)-1]))
				require.Equal(t, "C# service", label(fix))
			case `a: "${price}"`:
				const vars = "vars: {price: 42}\n"
				require.Equal(t, "42", label(vars+bad))
				require.Equal(t, "costs $5", label("a: "+note[len(note)-1]))
				require.Equal(t, "${price}", label(vars+fix))
			case "a: read; write":
				g := compile(bad)
				objects(g, "a", "write")
				require.Equal(t, "read", g.Objects[0].Label.Value)
				require.Equal(t, "read; write", label(fix))
			case "Repo<Order>":
				g := compile(bad)
				require.Len(t, g.Edges, 1)
				require.Equal(t, "Repo", g.Edges[0].Src.IDVal)
				require.Equal(t, "Order>", g.Edges[0].Dst.IDVal)
				require.True(t, g.Edges[0].SrcArrow)
				g = compile(fix)
				require.Empty(t, g.Edges)
				objects(g, strconv.Quote(bad))
				for _, source := range note[1:] {
					_, want, ok := strings.Cut(source, "a: ")
					require.True(t, ok)
					require.Equal(t, want, label(source))
				}
			case "direction: LR":
				reject(bad, "direction must be one of")
				require.Equal(t, "right", compile(fix).Root.Direction.Value)
			case "shape: database":
				reject(bad, `unknown shape "database"`)
				require.Equal(t, "cylinder", compile(fix).Root.Shape.Value)
			case "fill: red":
				reject("a: {"+bad+"}", "fill must be style.fill")
				g := compile("a: {" + fix + "}")
				objects(g, "a")
				require.NotNil(t, g.Objects[0].Style.Fill)
				require.Equal(t, "red", g.Objects[0].Style.Fill.Value)
			case "color: white":
				g := compile("a: {" + bad + "}")
				objects(g, "a", "a.color")
				require.Nil(t, g.Objects[0].Style.FontColor)
				require.Equal(t, "white", g.Objects[1].Label.Value)
				g = compile("a: {" + fix + "}")
				objects(g, "a")
				require.NotNil(t, g.Objects[0].Style.FontColor)
				require.Equal(t, "white", g.Objects[0].Style.FontColor.Value)
			case "// note":
				g := compile(bad)
				require.Len(t, g.Objects, 1)
				require.Equal(t, "// note", g.Objects[0].IDVal)
				require.Empty(t, compile(fix).Objects)
			case "subgraph x {}":
				objects(compile(bad), "subgraph x")
				objects(compile(fix), "x")
			case "layers":
				for _, kind := range before {
					g := compile(kind + ": {detail: {n}}")
					objects(g)
					board := g.GetBoard("detail")
					require.NotNil(t, board, "%s should declare a board", kind)
					objects(board, "n")
					objects(compile("x: {"+kind+"}"), "x", "x."+kind)
					objects(compile(strings.ReplaceAll(fix, "layers", kind)), kind)
				}
			case "a -> b":
				g := compile(bad + "\n" + bad + ": {style.stroke: red}")
				require.Len(t, g.Edges, 2)
				require.Nil(t, g.Edges[0].Style.Stroke)
				require.NotNil(t, g.Edges[1].Style.Stroke)
				require.Equal(t, "red", g.Edges[1].Style.Stroke.Value)
				g = compile(bad + "\n" + fix)
				require.Len(t, g.Edges, 1)
				require.NotNil(t, g.Edges[0].Style.Stroke)
				require.Equal(t, "red", g.Edges[0].Style.Stroke.Value)
			case "x.class: servce":
				const classes = "classes: {service: {style.fill: red}}\n"
				g := compile(classes + bad)
				objects(g, "x")
				require.Nil(t, g.Objects[0].Style.Fill)
				g = compile(classes + fix)
				objects(g, "x")
				require.NotNil(t, g.Objects[0].Style.Fill)
				require.Equal(t, "red", g.Objects[0].Style.Fill.Value)
			default:
				t.Fatalf("add a compiler behavior check for the guide row %q", bad)
			}
		})
	}
	require.GreaterOrEqual(t, len(seen), 17, "the guide table was truncated")
}
