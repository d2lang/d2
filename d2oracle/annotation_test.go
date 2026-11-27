package d2oracle_test

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2oracle"
	"github.com/d2lang/util-go/assert"
	"github.com/d2lang/util-go/go2"
)

func TestEditAnnotation(t *testing.T) {
	t.Parallel()

	g, _, err := d2compiler.Compile("test.d2", strings.NewReader("a -> b\n"), nil)
	assert.Success(t, err)

	// Set writes the note, then refines it with a mark.
	g, err = d2oracle.Set(g, nil, "a.annotation", nil, go2.Pointer("late"))
	assert.Success(t, err)
	g, err = d2oracle.Set(g, nil, "a.annotation.shape", nil, go2.Pointer("box"))
	assert.Success(t, err)
	g, err = d2oracle.Set(g, nil, "(a -> b)[0].annotation", nil, go2.Pointer("slow"))
	assert.Success(t, err)
	assert.String(t, "a -> b: {annotation: slow}\na.annotation: late\na.annotation.shape: box\n", d2format.Format(g.AST))
	assert.String(t, "late", g.Objects[0].Annotation.Label.Value)
	assert.String(t, "box", g.Objects[0].Annotation.Shape.Value)
	assert.String(t, "slow", g.Edges[0].Annotation.Label.Value)

	// Deleting one part leaves the rest of the annotation.
	g, err = d2oracle.Delete(g, nil, "a.annotation.shape")
	assert.Success(t, err)
	if a := g.Objects[0].Annotation; a == nil || a.Shape != nil || a.Label.Value != "late" {
		t.Fatalf("deleting the shape left %+v", a)
	}

	// Deleting the annotation leaves the shape it was on.
	g, err = d2oracle.Delete(g, nil, "a.annotation")
	assert.Success(t, err)
	g, err = d2oracle.Delete(g, nil, "(a -> b)[0].annotation")
	assert.Success(t, err)
	// A connection keeps its emptied map, as it does after losing a tooltip.
	assert.String(t, "a -> b: {}\n", d2format.Format(g.AST))
	if g.Objects[0].Annotation != nil || g.Edges[0].Annotation != nil {
		t.Fatal("annotations survived their deletion")
	}
}
