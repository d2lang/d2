package d2talalayout_test

import (
	"cmp"
	"strings"
	"testing"
)

func TestParallelReturnsKeepWorkflowDirection(t *testing.T) {
	for _, notation := range []string{"d -> a", "a <- d"} {
		source := "direction: right\na -> b -> c -> d\nb -> d\n" + strings.Repeat(notation+"\n", 4)
		shapes := hierarchyGeometry(t, source)
		for _, pair := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"b", "d"}} {
			a, b := shapes[pair[0]], shapes[pair[1]]
			if 2*a.Pos.X+a.Width >= 2*b.Pos.X+b.Width {
				t.Fatalf("return %q, forward edge %s -> %s points away from the requested direction", notation, pair[0], pair[1])
			}
		}
	}
}

func TestParallelReturnsRetainBetterCompletedDrawing(t *testing.T) {
	source := "direction: down\na -> b -> c -> d -> e\nb -> e\na -> c\n" + strings.Repeat("d -> a\n", 8)
	shapes := hierarchyGeometry(t, source)
	// The unguarded unique-pair trial reduces the finished drawing's net
	// agreement with the requested direction from three to zero.
	net := 0
	for _, pair := range [][2]string{{"a", "b"}, {"b", "c"}, {"c", "d"}, {"d", "e"}, {"b", "e"}, {"a", "c"}, {"d", "a"}} {
		a, b := shapes[pair[0]], shapes[pair[1]]
		net += cmp.Compare(2*b.Pos.Y+b.Height, 2*a.Pos.Y+a.Height)
	}
	if net < 3 {
		t.Fatalf("net downward relationship count = %d, want at least 3", net)
	}
}
