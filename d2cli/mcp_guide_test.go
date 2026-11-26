//go:build !d2nomcp

package d2cli

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2compiler"
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
