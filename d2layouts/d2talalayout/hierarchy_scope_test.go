package d2talalayout

import (
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestHierarchyScopePreservesDirectedRanks(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		rightward    bool
	}{
		{"automatic split and rejoin", "direction: right; Start -> A -> Finish; Start -> B -> Finish; Start -> C -> Finish; Start -> D -> Finish", true},
		{"nested automatic split and rejoin", "group: {direction: up; Start -> A -> Finish; Start -> B -> Finish; Start -> C -> Finish; Start -> D -> Finish}", false},
		{"nested endpoints", "group: {shape: hierarchy; direction: up; A.a -> B.b -> C.c}", false},
		{"branch attached to a diamond", "shape: hierarchy; direction: right; a -> b -> d; a -> c -> d; b -> e; e -> f; e -> g", true},
		{"container and child as branch sources", "direction: up; src.child; src -> a -> dst; src.child -> b -> dst", false},
		{"container and child as branch sinks", "direction: up; dst.child; src -> a -> dst; src -> b -> dst.child", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, _, err := d2compiler.Compile("test.d2", strings.NewReader(tc.source), nil)
			if err != nil {
				t.Fatal(err)
			}
			ruler, err := textmeasure.NewRuler()
			if err != nil {
				t.Fatal(err)
			}
			if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
				t.Fatal(err)
			}
			if err := Layout(t.Context(), g, &Options{Seeds: []int64{1}}); err != nil {
				t.Fatal(err)
			}
			for _, e := range g.Edges {
				if tc.rightward {
					if e.Src.TopLeft.X+e.Src.Width >= e.Dst.TopLeft.X {
						t.Errorf("edge %s must flow right: source right %v, target left %v", e.AbsID(), e.Src.TopLeft.X+e.Src.Width, e.Dst.TopLeft.X)
					}
				} else if e.Dst.TopLeft.Y+e.Dst.Height >= e.Src.TopLeft.Y {
					t.Errorf("edge %s must flow up: target bottom %v, source top %v", e.AbsID(), e.Dst.TopLeft.Y+e.Dst.Height, e.Src.TopLeft.Y)
				}
			}
		})
	}
}
