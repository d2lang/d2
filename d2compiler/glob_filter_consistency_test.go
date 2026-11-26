package d2compiler_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/util-go/assert"
)

func TestFilteredEdgeDeletion(t *testing.T) {
	t.Parallel()
	const edges = "a -> b: drop\na -> b: keep\na -> b: drop\n"
	const filter = "(* -> *)[*]: null {&label: drop}\n"
	for name, src := range map[string]string{
		"after edges":  edges + filter,
		"before edges": filter + edges,
		"spread":       "vars.edges: {" + edges + "}\nbox: {...${edges}; " + filter + "}",
		"nested":       "a -> b: keep\nbox: {a -> b: drop}\n(** -> **)[*]: null {&label: drop}",
	} {
		t.Run(name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
			assert.Success(t, err)
			assert.Equal(t, 1, len(g.Edges))
			assert.String(t, "keep", g.Edges[0].Label.Value)
		})
	}
}

func TestFilteredAssignmentsUseOriginalLabel(t *testing.T) {
	t.Parallel()
	const src = `a: original
b: keep
a -> b: original
b -> a: keep
*: changed {&label: original; style.fill: green}
(* -> *)[*]: changed {&label: original; style.stroke: green}`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
	assert.Success(t, err)
	assert.String(t, "changed", g.Root.Children["a"].Label.Value)
	assert.String(t, "green", g.Root.Children["a"].Style.Fill.Value)
	assert.String(t, "keep", g.Root.Children["b"].Label.Value)
	assert.True(t, g.Root.Children["b"].Style.Fill == nil)
	assert.Equal(t, 2, len(g.Edges))
	assert.String(t, "changed", g.Edges[0].Label.Value)
	assert.String(t, "green", g.Edges[0].Style.Stroke.Value)
	assert.String(t, "keep", g.Edges[1].Label.Value)
	assert.True(t, g.Edges[1].Style.Stroke == nil)
}

func TestGlobEndpointProperties(t *testing.T) {
	t.Parallel()
	const src = `api: outside
db.shape: circle
prod: {
  api: old {label: ready}
  db: standby {label.near: top-center}
  api -> db: initial
  (* -> *)[*]: {&src.label: outside; label: wrong scope}
  (* -> *)[*]: {&src.label: old; style.stroke: red}
  (* -> *)[*]: {&dst.absent.shape: rectangle; style.fill: red}
  (* -> *)[*]: {&src.label: ready; &dst.label: standby; &dst.shape: rectangle; &src.style.opacity: 1; label: matched}
}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
	assert.Success(t, err)
	assert.Equal(t, 1, len(g.Edges))
	assert.String(t, "matched", g.Edges[0].Label.Value)
	assert.True(t, g.Edges[0].Style.Stroke == nil)
	assert.True(t, g.Edges[0].Style.Fill == nil)
}

func TestGlobLabelWildcardMatchesDisplayedLabel(t *testing.T) {
	t.Parallel()
	const src = `a: {leaf}
b: bee
c.label: sea
a -> b
*: {&label: *; style.fill: green}
(* -> *)[*]: {&src.label: *; &dst.label: *; &src.leaf: false; style.stroke: green}`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
	assert.Success(t, err)
	for _, name := range []string{"a", "b", "c"} {
		assert.String(t, "green", g.Root.Children[name].Style.Fill.Value)
	}
	assert.Equal(t, 1, len(g.Edges))
	assert.String(t, "green", g.Edges[0].Style.Stroke.Value)
}

func TestGlobClassArrayMatches(t *testing.T) {
	t.Parallel()
	const src = `vars: {role: worker; roles: [${role}; urgent]; alias: ${roles}; prefix: work}
box: {
  vars.role: local
  scalar.class: worker
  list.class: [urgent; worker]
  variable.class: ${alias}
  spread.class: [...${roles}]
  other.class: [urgent]
  *: {&class: ${prefix}*; style.fill: green}
  *: {!&class: urgent; tooltip: nonurgent}
}
`
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
	assert.Success(t, err)
	box := g.Root.Children["box"]
	for _, key := range []string{"scalar", "list", "variable", "spread"} {
		o := box.Children[key]
		if o.Style.Fill == nil || o.Style.Fill.Value != "green" {
			t.Errorf("%s did not match work*", key)
		}
		assert.Equal(t, key == "scalar", o.Tooltip != nil)
	}
	assert.True(t, box.Children["other"].Style.Fill == nil)
	assert.True(t, box.Children["other"].Tooltip == nil)
}

func TestGlobRootNamedContainer(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"root", "ROOT", "other"} {
		t.Run(name, func(t *testing.T) {
			src := strings.ReplaceAll(`box: {box -> target: original}
other-scope: {box -> target: hidden}
**: {&level: 1; style.fill: green}
**: suspend
(** -> **)[*]: suspend
(** -> **)[*]: unsuspend {&src: box.box; &dst: box.target}
`, "box", name)
			g, _, err := d2compiler.Compile("main.d2", strings.NewReader(src), nil)
			assert.Success(t, err)
			assert.Equal(t, 3, len(g.Objects))
			assert.Equal(t, 1, len(g.Edges))
			assert.String(t, "original", g.Edges[0].Label.Value)
			assert.True(t, g.Edges[0].Src.Parent.Style.Fill == nil)
			assert.String(t, "green", g.Edges[0].Src.Style.Fill.Value)
			assert.String(t, "green", g.Edges[0].Dst.Style.Fill.Value)
		})
	}
}

func TestImportFilterOperandErrorsFollowCandidates(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, operand, message string }{
		{"unresolved", "${missing}", `could not resolve variable "missing"`},
		{"array", "${roles}", "glob filters cannot be composites"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			module := "vars.roles: [worker]\ngood.node: ready\n" +
				"bad: {first: ready; second: ready}\nbad.*: {&label: " + tc.operand + "; style.fill: green}"
			opts := &d2compiler.CompileOptions{FS: fstest.MapFS{"lib.d2": {Data: []byte(module)}}}
			for _, path := range []string{"lib.good", "lib.good.node", "lib.bad.first", "lib.bad.second", "lib.bad", "lib"} {
				_, _, err := d2compiler.Compile("main.d2", strings.NewReader("out: @"+path), opts)
				if strings.HasPrefix(path, "lib.good") {
					if err != nil {
						t.Errorf("@%s: %v", path, err)
					}
					continue
				}
				if err == nil || strings.Count(err.Error(), tc.message) != 1 {
					t.Errorf("@%s: got %v, want one %q", path, err, tc.message)
				}
			}
		})
	}
}
