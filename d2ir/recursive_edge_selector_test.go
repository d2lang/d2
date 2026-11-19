package d2ir

import (
	"slices"
	"testing"

	"github.com/d2lang/d2/d2parser"
)

func TestRecursiveEdgeSelectorOrder(t *testing.T) {
	t.Parallel()

	// Declare fields and edges in different orders, with parallel edges,
	// reversed spellings, a self-edge, and a separate board.
	base := compileIndexTestSource(t, `
z; a
box: { y; x; x -> y; y -> y; x -- y; x <-> y }
a -> z; z <- a; box.x -> a; z -> box.y; a -- z; z <-> a
layers.other: { p -> q; p -- q }
"folder*": { x -> y }
"folder*".a -> "folder*".b
"vars": { x -> y }
`)
	for _, glob := range []string{"**", "***"} {
		for _, arrow := range []string{"--", "->", "<-", "<->"} {
			t.Run(glob+arrow, func(t *testing.T) {
				m := base.Copy(nil).(*Map)
				key, err := d2parser.ParseMapKey("(" + glob + " " + arrow + " " + glob + ")[*]")
				if err != nil {
					t.Fatal(err)
				}
				ref := &RefContext{Key: key, Edge: key.Edges[0], ScopeMap: m}
				id := NewEdgeIDs(key)[0]
				newCompiler := func() *compiler {
					return &compiler{
						err:                 &d2parser.ParseError{},
						globContextStack:    [][]*globContext{{}},
						globRefContextStack: []*RefContext{ref},
					}
				}
				want := m.GetEdges(id, ref, newCompiler())
				c := newCompiler()
				got := m.getEdgesForCompile(id, ref, c)
				if len(want) == 0 || !slices.Equal(got, want) {
					t.Fatalf("matched edges %v, want %v", got, want)
				}
				if got := m.getEdgesForCompile(id, ref, c); len(got) != 0 {
					t.Fatalf("matched previously applied edges again: %v", got)
				}
			})
		}
	}
}
