package d2oracle_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2oracle"
)

func TestEditorRejectsPartialObjectKey(t *testing.T) {
	g, _, err := d2compiler.Compile("test.d2", strings.NewReader("a: first\nb: second"), nil)
	if err != nil {
		t.Fatal(err)
	}
	before := d2format.Format(g.AST)
	label := "changed"
	if _, err := d2oracle.Set(g, nil, "a; b", nil, &label); err == nil {
		t.Fatal("edited the first object in an invalid key")
	}
	if got := d2format.Format(g.AST); got != before {
		t.Errorf("invalid edit mutated diagram: %q", got)
	}
	for _, key := range []string{"a; b", "a -> b", "a: first"} {
		if got := d2oracle.GetObj(g, nil, key); got != nil {
			t.Errorf("%q looked up object %q", key, got.ID)
		}
		if _, err := d2oracle.GetChildrenIDs(g, nil, key); err == nil {
			t.Errorf("looked up children of invalid object ID %q", key)
		}
		if _, err := d2oracle.GetParentID(g, nil, key); err == nil {
			t.Errorf("looked up parent of invalid object ID %q", key)
		}
	}
	if got := d2oracle.GetObj(g, nil, "a"); got == nil || got.Label.Value != "first" {
		t.Fatal("lost original object")
	}
}
