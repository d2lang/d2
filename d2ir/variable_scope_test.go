package d2ir_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ir"
)

func TestMapVariableDeclarationScope(t *testing.T) {
	const dev = "dev: {vars: {env: dev}; box: ${template}}"
	const qa = "qa: {vars: {env: qa}; box: {...${template}}}"
	const vars = "vars: {env: root; template: {note: ${env}; nested: {vars: {env: nested}; note: ${env}}}}"
	for _, source := range []string{
		strings.Join([]string{dev, qa, vars}, "\n"),
		strings.Join([]string{qa, dev, vars}, "\n"),
		strings.Join([]string{vars, dev, qa}, "\n"),
	} {
		root, err := compileTween(t, source, d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, scope := range []string{"dev", "qa"} {
			if got := tweenText(t, root, scope, "box", "note"); got != "root" {
				t.Errorf("%s.box.note = %q, want root", scope, got)
			}
			if got := tweenText(t, root, scope, "box", "nested", "note"); got != "nested" {
				t.Errorf("%s.box.nested.note = %q, want nested", scope, got)
			}
		}
	}
}

func TestDottedVariableSelfReference(t *testing.T) {
	root, err := compileTween(t, `vars: {db: ${db.host} {host: production}; level: root}
base: ${db}
service: {
  vars: {db: {host: staging}; host: ${db.host}; level: ${LEVEL}-child}
  plain: ${host}
  markdown: |md ${host} |
  inherited: ${level}
}`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := tweenText(t, root, "base"); got != "production" {
		t.Errorf("base = %q, want production", got)
	}
	for label, want := range map[string]string{"plain": "staging", "markdown": "staging", "inherited": "root-child"} {
		if got := tweenText(t, root, "service", label); got != want {
			t.Errorf("%s = %q, want %q", label, got, want)
		}
	}
}

func TestForwardVariableDependencies(t *testing.T) {
	root, err := compileTween(t, `early: "Deploy ${endpoint}"
vars: {
  markdown: |md Deploy ${endpoint} |
  raw: |md ${key}tail} |
  "key}tail": "${region} coast"
  endpoint: api-${region}
  indirect: "${first}"
  first: ${second}
  second: ${region}
  from-template: ${template.name}
  template: {vars: {region: inner}; name: ${region}}
  region: west
}
late: "Deploy ${endpoint}"
note: ${markdown}
literal: ${raw}
alias: ${indirect}
nested: ${from-template}`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"early": "Deploy api-west", "late": "Deploy api-west", "note": "Deploy api-west", "alias": "west", "nested": "inner", "literal": "west coast"} {
		if got := tweenText(t, root, id); got != want {
			t.Errorf("%s = %q, want %q", id, got, want)
		}
	}
}

func TestForwardCompositeVariableDependencies(t *testing.T) {
	for _, proxy := range []string{`${source}`, `${source} {other: ok}`, `{...${source}}`} {
		source := `vars: {proxy: {host: outer}}
group: {
  vars: {result: ${proxy.host}; proxy: ` + proxy + `; source: {host: inner}}
  out: ${result}
}`
		root, err := compileTween(t, source, d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := tweenText(t, root, "group", "out"); got != "inner" {
			t.Errorf("proxy %q: result = %q, want inner", proxy, got)
		}
	}
}

func TestForwardTemplateInsideVariable(t *testing.T) {
	for _, tc := range []struct{ box, template string }{
		{`${alias}`, `{note: ${env}}`},
		{`{...${alias}}`, `{note: ${env}}`},
		{`${alias} {own: true}`, `{note: ${env}; ...${extras}}`},
	} {
		source := `vars: {
  env: root
  dev: {box: ` + tc.box + `; vars: {env: dev; alias: ${template}}}
  template: ` + tc.template + `
  extras: {extra: yes}
}
out: ${dev}`
		root, err := compileTween(t, source, d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := tweenText(t, root, "out", "box", "note"); got != "root" {
			t.Errorf("box %q: note = %q, want root", tc.box, got)
		}
	}
}

func TestScalarVariableDependencyCycle(t *testing.T) {
	_, err := compileTween(t, `vars: {a: "before ${b}"; b: ${a}}; out: ${a}`, d2ir.CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), `cyclic variable reference "a"`) {
		t.Fatalf("cycle diagnostic = %v", err)
	}
}
