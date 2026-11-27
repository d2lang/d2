package d2ir_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ir"
)

func TestDottedBoardInheritsLikeMap(t *testing.T) {
	const before = "api: idle; db: database; api -> db\n*.style.fill: red\n(** -> **)[*]: {&label: current; label: styled}\n"
	for _, kind := range []string{"scenarios", "steps", "layers"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct{ dotted, maps string }{
				{"GROUP.one.api: live; GROUP.one.add: added", "GROUP.one: {api: live; add: added}"},
				{"GROUP.one.x; *.label: source; GROUP.one.y", "GROUP.one: {x}; *.label: source; GROUP.one: {y}"},
				{"GROUP.one.steps.next.x", "GROUP.one: {steps.next: {x}}"},
				{"GROUP.one.(api -> db): current", "GROUP.one: {api -> db: current}"},
				{"GROUP.one.a -> GROUP.one.b", "GROUP.one: {a -> b}"},
				{"GROUP.one.api: live; GROUP.two.next: next", "GROUP.one: {api: live}; GROUP.two: {next: next}"},
			} {
				compile := func(s string) *d2ir.Map {
					t.Helper()
					m, err := compileTween(t, before+strings.ReplaceAll(s, "GROUP", kind), d2ir.CompileOptions{})
					if err != nil {
						t.Fatal(err)
					}
					return tweenField(m, kind).Map()
				}
				dotted, explicit := compile(tc.dotted), compile(tc.maps)
				if got, want := dotted.String(), explicit.String(); got != want {
					t.Errorf("%s\ndotted:\n%s\nmap:\n%s", tc.dotted, got, want)
				}
			}
		})
	}
}

func TestDottedBoardReusesLocalGlobs(t *testing.T) {
	m, err := compileTween(t, `steps.one: {*.style.fill: green}
steps.one.worker`, d2ir.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := tweenText(t, m, "steps", "one", "worker", "style", "fill"); got != "green" {
		t.Errorf("worker fill = %q", got)
	}
}

func TestDottedBoardFilterSeesInheritedFieldsAsExisting(t *testing.T) {
	const base = `api: base
*: {&label: live; style.fill: green}
`
	compile := func(s string) string {
		t.Helper()
		m, err := compileTween(t, base+s, d2ir.CompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return tweenField(m, "steps", "one").Map().String()
	}
	dotted := compile("steps.one.api: live; steps.one.extra: live")
	explicit := compile("steps.one: {api: live; extra: live}")
	if dotted != explicit {
		t.Errorf("dotted:\n%s\nmap:\n%s", dotted, explicit)
	}
}

func TestDottedBoardRuleOrderAndScope(t *testing.T) {
	for _, tc := range []struct{ name, prefix, dotted, maps string }{
		{
			"inherited object and scoped edge", "x; steps.*.*.style.fill: orange",
			"steps.one.(a -> b); steps.two.y", "steps: {one: {a -> b}}; steps: {two: {y}}",
		},
		{
			"board rule after root rule", "*.style.fill: red; steps.*.*.style.fill: orange",
			"steps.one.n; steps.two.m", "steps: {one: {n}}; steps: {two: {m}}",
		},
		{
			"map on a dotted shape", "*.style.fill: red; scenarios.*: {p -> q}",
			"scenarios.one.k: {p -> q}", "scenarios: {one: {k: {p -> q}}}",
		},
		{
			"rule inside a glob value", "layers.*: {*.style.fill: blue}",
			"layers.one.n; layers.two.m", "layers: {one: {n}}; layers: {two: {m}}",
		},
		{
			"rule inside an intermediate layer", "layers.detail: {*.style.fill: red; steps.one: {seed}}",
			"layers.detail.steps.one.new: ready", "layers.detail.steps.one: {new: ready}",
		},
		{
			"edge rule inside an intermediate layer", "layers.detail: {(* -> *)[*]: {&label: ready; label: selected}; steps.one: {seed}}",
			"layers.detail.steps.one.(a -> b): ready", "layers.detail.steps.one: {a -> b: ready}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compile := func(s string) string {
				t.Helper()
				m, err := compileTween(t, tc.prefix+"\n"+s, d2ir.CompileOptions{})
				if err != nil {
					t.Fatal(err)
				}
				return m.String()
			}
			if got, want := compile(tc.dotted), compile(tc.maps); got != want {
				t.Errorf("dotted:\n%s\nmap:\n%s", got, want)
			}
		})
	}
}
