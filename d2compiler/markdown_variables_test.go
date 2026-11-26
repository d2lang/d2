package d2compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestMarkdownUsesRegularVariableLookup(t *testing.T) {
	for _, tc := range []struct {
		name, rootVars, localVars, label, want string
	}{
		{"nearest scope", "env: production", "env: staging", "${env}", "staging"},
		{"spaces inside reference", "env: production", "env: staging", "${ env }", "staging"},
		{"quoted closing brace", `"x}y": root`, `"x}y": local`, `${"x}y"}`, "local"},
		{"case insensitive", "env: production", "Env: staging", "${ENV}", "staging"},
		{"outer fallback", "region: us-west", "env: staging", "${region}", "us-west"},
		{"dotted paths", "db: {host: primary; port: 5432}", "db: {host: replica}", "${db.host} / ${db.port}", "replica / 5432"},
		{"quoted path segment", `"db.host": flat; db: {host: nested}`, "", `${"db.host"} / ${db.host}`, "flat / nested"},
		{"keyword spelling", "style: global", `"style": local`, `${style} / ${"style"}`, "global / local"},
		{"keyword fallback", "", `"height": tall`, "${HEIGHT}", "tall"},
		{"nested shorthand collision", "env: production", "meta: {env: metadata}", "${env}", "production"},
		{"scalar with children", "", "release: current {version: 2}", "${release} / ${release.version}", "current / 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := fmt.Sprintf("vars: {%s}\nservice: {\nvars: {%s}\nplain: \"%s\"\nmarkdown: |md\n%s\n|\n}", tc.rootVars, tc.localVars, tc.label, tc.label)
			g, _, err := d2compiler.Compile("scopes.d2", strings.NewReader(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			service := g.Root.Children["service"]
			for _, name := range []string{"plain", "markdown"} {
				if got := service.Children[name].Label.Value; got != tc.want {
					t.Errorf("%s label = %q, want %q", name, got, tc.want)
				}
			}
		})
	}
}

func TestMarkdownVariableLookupPreservesCodeAndUnknownNames(t *testing.T) {
	const source = "vars: {env: production}\nservice: {\nvars: {env: staging; meta: {region: test}}\n" +
		"label: |md\n${env} `${env}` ${region}\n|\n}"
	g, _, err := d2compiler.Compile("prose.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := g.Root.Children["service"].Label.Value, "staging `${env}` ${region}"; got != want {
		t.Errorf("Markdown label = %q, want %q", got, want)
	}
}

func TestMarkdownLinkUsesRegularVariableLookup(t *testing.T) {
	const source = `vars.url: https://example.com
service: {
  vars: {url: https://example.com/runbook; name: Runbook}
  plain: ${name} {link: ${url}}
  markdown: |md [${name}](${URL}) |
}`
	g, _, err := d2compiler.Compile("links.d2", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	service := g.Root.Children["service"]
	plain := service.Children["plain"]
	rendered, err := textmeasure.RenderMarkdown(service.Children["markdown"].Label.Value)
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf(`<a href="%s">%s</a>`, plain.Link.Value, plain.Label.Value)
	if !strings.Contains(rendered, want) {
		t.Errorf("Markdown rendered as %q, want %q", rendered, want)
	}
}

func TestMarkdownLocalVariableWithoutScalarShadowsOuterValue(t *testing.T) {
	for _, declaration := range []string{"env", "env: {region: us-west}"} {
		t.Run(declaration, func(t *testing.T) {
			source := fmt.Sprintf("vars: {env: production}\nservice: {vars: {%s}; label: |md ${env} |}", declaration)
			g, _, err := d2compiler.Compile("prose.d2", strings.NewReader(source), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := g.Root.Children["service"].Label.Value; got != "${env}" {
				t.Errorf("Markdown label = %q, want the literal ${env}", got)
			}
		})
	}
}
