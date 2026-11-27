package d2oracle_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2oracle"
)

func TestEditNestedBoardNames(t *testing.T) {
	for _, board := range [][]string{nil, {"detail"}} {
		source := "box: {other}"
		if board != nil {
			source = "box: root\nLaYeRs: {detail: {" + source + "}}"
		}
		g, _, err := d2compiler.Compile("test.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		g, key, err := d2oracle.Create(g, board, "box.Layers")
		if err != nil || key != "box.Layers" {
			t.Fatalf("create in %v: key %q, err %v", board, key, err)
		}
		g, _, err = d2oracle.Create(g, board, `box.Layers."with.dot"`)
		if err != nil {
			t.Fatal(err)
		}
		value := "blue"
		g, err = d2oracle.Set(g, board, "box.Layers.style.fill", nil, &value)
		if err != nil {
			t.Fatal(err)
		}
		obj := d2oracle.GetObj(g, board, "box.Layers")
		if obj == nil || obj.ID != "Layers" || obj.Style.Fill == nil || obj.Style.Fill.Value != value {
			t.Fatalf("missing Layers shape or fill in %v", board)
		}
		g, renamed, err := d2oracle.Rename(g, board, "box.Layers", "steps")
		if err != nil || renamed != "steps" {
			t.Fatalf("rename in %v: name %q, err %v", board, renamed, err)
		}
		g, err = d2oracle.Delete(g, board, "box.steps.style.fill")
		if err != nil {
			t.Fatal(err)
		}
		obj = d2oracle.GetObj(g, board, "box.steps")
		if obj == nil || obj.Style.Fill != nil {
			t.Fatalf("deleting fill in %v did not clear it", board)
		}
		g, err = d2oracle.Delete(g, board, "box.steps")
		if err != nil {
			t.Fatal(err)
		}
		if d2oracle.GetObj(g, board, "box.steps") != nil || d2oracle.GetObj(g, board, `box."with.dot"`) == nil {
			t.Fatalf("expected deletion in %v to hoist the remaining child", board)
		}
		if board != nil && d2oracle.GetObj(g, nil, "box").Label.Value != "root" {
			t.Fatal("board edit changed the root diagram")
		}
		if _, _, err := d2oracle.Rename(g, board, "box", "scenarios"); err == nil {
			t.Fatalf("renamed a root shape to a board holder in %v", board)
		}
	}
}

func TestDeleteQuotedPropertyName(t *testing.T) {
	for _, source := range []string{
		`box: {WIDTH: 130; "width": Port; marker}`,
		`box.WIDTH: 130; box."width": Port; box.marker`,
	} {
		for _, key := range []string{`box."width"`, "box.WIDTH"} {
			g, _, err := d2compiler.Compile("test.d2", strings.NewReader(source+`; box."width" -> external`), nil)
			if err != nil {
				t.Fatal(err)
			}
			g, err = d2oracle.Delete(g, nil, key)
			if err != nil {
				t.Fatal(err)
			}
			quoted := key == `box."width"`
			box := d2oracle.GetObj(g, nil, "box")
			port := d2oracle.GetObj(g, nil, `box."width"`)
			if box == nil || (box.WidthAttr != nil) != quoted || (port == nil) != quoted || (len(g.Edges) == 0) != quoted {
				t.Fatalf("deleting %s from %s changed the wrong object, property, or edge", key, source)
			}
		}
	}
}

func TestSetLiteralControlWords(t *testing.T) {
	for _, label := range []string{"SUSPEND", "sUsPeNd", "UnSuspend", "NULL", "suspend"} {
		g, _, err := d2compiler.Compile("test.d2", strings.NewReader("job -> worker"), nil)
		if err != nil {
			t.Fatal(err)
		}
		g, err = d2oracle.Set(g, nil, "job", nil, &label)
		if err != nil {
			t.Fatal(err)
		}
		job := d2oracle.GetObj(g, nil, "job")
		if job == nil || job.Label.Value != label || len(g.Edges) != 1 {
			t.Fatalf("setting %q lost the label or its connection", label)
		}
	}
}

func TestDeleteScopedPropertyPreservesChild(t *testing.T) {
	for _, src := range []string{
		"panel.steps.style.fill: blue; panel.steps.tip",
		"panel.steps: {style.fill: blue; tip}",
		"panel: {steps: {style.fill: blue; tip}}",
	} {
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader("panel.style.fill: red; "+src), nil)
		if err != nil {
			t.Fatal(err)
		}
		g, err = d2oracle.Delete(g, nil, "panel.style.fill")
		if err != nil {
			t.Fatal(err)
		}
		child := d2oracle.GetObj(g, nil, "panel.steps")
		if d2oracle.GetObj(g, nil, "panel").Style.Fill != nil || child == nil || child.Style.Fill == nil || child.Style.Fill.Value != "blue" {
			t.Fatalf("deleting the parent fill also changed %q", src)
		}
	}
}

func TestDeletingQuotedEdgeFieldKeepsEdge(t *testing.T) {
	g, _, err := d2compiler.Compile("main.d2", strings.NewReader("a -> b: works"), nil)
	if err != nil {
		t.Fatal(err)
	}
	g, err = d2oracle.Delete(g, nil, `(a -> b)[0]."width"`)
	if err != nil || len(g.Edges) != 1 || g.Edges[0].Label.Value != "works" {
		t.Fatalf("deleting an absent field affected its edge: %v", err)
	}
}

func TestDeleteInheritedQuotedEndpoint(t *testing.T) {
	for _, tc := range []struct{ key, edge string }{
		{`"width"`, `"width" -> db`},
		{`"WIDTH"`, `db -> "WIDTH"`},
		{`box."style"`, `box."style" -> db`},
		{`box."_"`, `box."_" -> db`},
	} {
		source := tc.key + ": Port; " + tc.edge + "; steps: {one: {k}}"
		g, _, err := d2compiler.Compile("main.d2", strings.NewReader(source), nil)
		if err != nil {
			t.Fatal(err)
		}
		board := []string{"one"}
		g, err = d2oracle.Delete(g, board, tc.key)
		if err != nil {
			t.Fatalf("deleting %s from step: %v", tc.key, err)
		}
		step := d2oracle.GetBoardGraph(g, board)
		if d2oracle.GetObj(g, nil, tc.key) == nil || len(g.Edges) != 1 ||
			step == nil || len(step.Edges) != 0 || d2oracle.GetObj(g, board, tc.key) != nil ||
			d2oracle.GetObj(g, board, "k") == nil || d2oracle.GetObj(g, board, "db") == nil {
			t.Fatalf("unexpected base or step after deleting %s", tc.key)
		}
	}
}
