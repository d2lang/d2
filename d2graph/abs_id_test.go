package d2graph

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// legacyEdgeAbsID is Edge.AbsID as it was before it stopped using
// fmt.Sprintf and strings.Join.
func legacyEdgeAbsID(e *Edge) string {
	srcIDA := e.Src.AbsIDArray()
	dstIDA := e.Dst.AbsIDArray()

	var commonIDA []string
	for len(srcIDA) > 1 && len(dstIDA) > 1 {
		if !strings.EqualFold(srcIDA[0], dstIDA[0]) {
			break
		}
		commonIDA = append(commonIDA, srcIDA[0])
		srcIDA = srcIDA[1:]
		dstIDA = dstIDA[1:]
	}

	commonKey := ""
	if len(commonIDA) > 0 {
		commonKey = strings.Join(commonIDA, ".") + "."
	}

	return fmt.Sprintf("%s(%s %s %s)[%d]", commonKey, strings.Join(srcIDA, "."), e.ArrowString(), strings.Join(dstIDA, "."), e.Index)
}

func TestEdgeAbsIDMatchesLegacy(t *testing.T) {
	t.Parallel()
	ids := []string{"a", "A", "b", "B", "ä", "Ä", "ǅ", "ǆ", "", "x.y", "1", "\xff", "long name with spaces", "a"}
	r := rand.New(rand.NewSource(3))
	for c := 0; c < 300; c++ {
		root := &Object{}
		objects := []*Object{root}
		for i := 0; i < 1+r.Intn(60); i++ {
			parent := objects[r.Intn(len(objects))]
			if r.Intn(3) == 0 {
				// Grow deep chains (deeper than AbsID's stack buffers).
				parent = objects[len(objects)-1]
			}
			objects = append(objects, &Object{ID: ids[r.Intn(len(ids))], Parent: parent})
		}
		for i := 0; i < 50; i++ {
			e := &Edge{
				Src:      objects[r.Intn(len(objects))],
				Dst:      objects[r.Intn(len(objects))],
				SrcArrow: r.Intn(2) == 0,
				DstArrow: r.Intn(2) == 0,
				Index:    []int{0, 1, 9, 99, 100, 12345, -3}[r.Intn(7)],
			}
			if got, want := e.AbsID(), legacyEdgeAbsID(e); got != want {
				t.Fatalf("AbsID() = %q, want %q", got, want)
			}
		}
	}
}
