package d2oracle_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2oracle"
)

func TestEditFillTransition(t *testing.T) {
	const source = `fill-transition: Sample {
  fill-transition: Child {style.fill-transition: wipe-down}
  style: {fill: red; fill-transition: wipe-left}
}
fill-transition.sibling: {style: {fill-transition: wipe-down}}
registry: {shape: sql_table; fill-transition: int; style.fill-transition: wipe-right}
`
	g, _, err := d2compiler.Compile("", strings.NewReader(source), nil)
	if err != nil {
		t.Fatal(err)
	}
	obj := func(id ...string) *d2graph.Object {
		t.Helper()
		o, ok := g.Root.HasChild(id)
		if !ok {
			t.Fatalf("lost %v in %s", id, d2format.Format(g.AST))
		}
		return o
	}
	key := "fill-transition.style.fill-transition"
	value := "wipe-up"
	g, err = d2oracle.Set(g, nil, key, nil, &value)
	if err != nil || obj("fill-transition").Style.FillTransition.Value != value {
		t.Fatalf("setting %s: %v", key, err)
	}
	deltas, err := d2oracle.DeleteIDDeltas(g, nil, key)
	if err != nil || len(deltas) != 0 {
		t.Fatalf("unexpected delete deltas %v, %v", deltas, err)
	}
	for _, key := range []string{key, "registry.style.fill-transition"} {
		g, err = d2oracle.Delete(g, nil, key)
		if err != nil {
			t.Fatal(err)
		}
	}
	if obj("fill-transition").Style.FillTransition != nil || obj("registry").Style.FillTransition != nil ||
		obj("fill-transition").Style.Fill.Value != "red" || obj("fill-transition", "fill-transition").Style.FillTransition.Value != "wipe-down" ||
		obj("fill-transition", "sibling").Style.FillTransition.Value != "wipe-down" || !strings.Contains(d2format.Format(g.AST), "fill-transition: int") {
		t.Fatalf("unexpected source after style deletion:\n%s", d2format.Format(g.AST))
	}
	g, err = d2oracle.Delete(g, nil, "fill-transition.style.fill")
	if err != nil || obj("fill-transition").Style.Fill != nil {
		t.Fatalf("deleting ordinary style.fill: %v", err)
	}
}

func TestMoveFlatFillTransition(t *testing.T) {
	for _, src := range []string{"sample.style.fill-transition: wipe-right", "sample.style: {fill-transition: wipe-right}"} {
		t.Run(src, func(t *testing.T) {
			g, _, err := d2compiler.Compile("", strings.NewReader("group\n"+src), nil)
			if err != nil {
				t.Fatal(err)
			}
			g, err = d2oracle.Move(g, nil, "sample", "group.fill-transition", true)
			if err != nil {
				t.Fatal(err)
			}
			g, _, err = d2oracle.Rename(g, nil, "group.fill-transition", "moved")
			if err != nil {
				t.Fatal(err)
			}
			o, ok := g.Root.HasChild([]string{"group", "moved"})
			if !ok || o.Style.FillTransition == nil || o.Style.FillTransition.Value != "wipe-right" {
				t.Fatalf("move lost style: %s", d2format.Format(g.AST))
			}
			g, err = d2oracle.Delete(g, nil, "group.moved.style.fill-transition")
			if err != nil {
				t.Fatal(err)
			}
			o, ok = g.Root.HasChild([]string{"group", "moved"})
			if !ok || o.Style.FillTransition != nil {
				t.Fatalf("deleting only style lost object or left annotation: %s", d2format.Format(g.AST))
			}
		})
	}
}

func TestDeleteStepFillTransition(t *testing.T) {
	for _, tc := range []struct {
		name  string
		local string
	}{
		{"inherited", "dummy"},
		{"overridden", "a.style.fill-transition: wipe-left"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("", strings.NewReader("a.style.fill-transition: wipe-up\nsteps: {next: {"+tc.local+"}}"), nil)
			if err != nil {
				t.Fatal(err)
			}
			g, err = d2oracle.Delete(g, []string{"next"}, "a.style.fill-transition")
			if err != nil {
				t.Fatal(err)
			}
			base := d2oracle.GetObj(g, nil, "a")
			next := d2oracle.GetObj(g, []string{"next"}, "a")
			if base == nil || base.Style.FillTransition == nil || base.Style.FillTransition.Value != "wipe-up" || next == nil || next.Style.FillTransition != nil {
				t.Fatalf("unexpected source after deleting only in the step:\n%s", d2format.Format(g.AST))
			}
		})
	}
}
