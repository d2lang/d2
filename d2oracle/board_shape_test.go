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
