package placement

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
)

// sortSliceExact must leave elements in exactly sort.Slice's order, also
// for comparisons that are not a strict weak order (NaN keys) and for ties.
func TestSortSliceExactMatchesSortSlice(t *testing.T) {
	type item struct {
		key float64
		id  int
	}
	random := rand.New(rand.NewSource(5))
	for trial := range 2000 {
		n := random.Intn(80)
		items := make([]item, n)
		for i := range items {
			switch random.Intn(10) {
			case 0:
				items[i].key = math.NaN()
			case 1:
				items[i].key = math.Copysign(0, -1)
			default:
				items[i].key = float64(random.Intn(12))
			}
			items[i].id = i
		}
		lessKeyOnly := trial%2 == 0
		less := func(a, b item) bool {
			if !lessKeyOnly && a.key == b.key {
				return a.id < b.id
			}
			return a.key < b.key
		}
		want := append([]item(nil), items...)
		sort.Slice(want, func(i, j int) bool { return less(want[i], want[j]) })
		got := append([]item(nil), items...)
		sortSliceExact(got, less)
		for i := range want {
			if want[i].id != got[i].id {
				t.Fatalf("trial %d: position %d holds %d, sort.Slice has %d", trial, i, got[i].id, want[i].id)
			}
		}
	}
}

// referenceSnapshotNodePositions is snapshotNodePositionsContext before the
// list/set change, kept verbatim as the reference.
func referenceSnapshotNodePositions(nodes []*layoutgraph.Node) []nodePositionSnapshot {
	seen := make(map[*layoutgraph.Node]struct{})
	snapshots := make([]nodePositionSnapshot, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		if _, ok := seen[node]; ok {
			continue
		}
		seen[node] = struct{}{}
		snapshots = append(snapshots, nodePositionSnapshot{node: node, topLeft: snapshotPointer(node.TopLeft)})
	}
	return snapshots
}

func TestSnapshotNodePositionsDedup(t *testing.T) {
	random := rand.New(rand.NewSource(9))
	for trial := range 300 {
		pool := make([]*layoutgraph.Node, 1+random.Intn(90))
		for i := range pool {
			pool[i] = layoutgraph.NewNode(layoutgraph.EntityID(i+1), 4, 4)
			if random.Intn(5) != 0 {
				pool[i].TopLeft = geo.NewPoint(float64(random.Intn(50)), float64(random.Intn(50)))
			}
		}
		var nodes []*layoutgraph.Node
		for range random.Intn(150) {
			if random.Intn(12) == 0 {
				nodes = append(nodes, nil)
				continue
			}
			nodes = append(nodes, pool[random.Intn(len(pool))])
		}
		got, err := snapshotNodePositionsContext(context.Background(), "Test", nodes)
		if err != nil {
			t.Fatal(err)
		}
		want := referenceSnapshotNodePositions(nodes)
		if len(got) != len(want) {
			t.Fatalf("trial %d: %d snapshots, want %d", trial, len(got), len(want))
		}
		for i := range want {
			if got[i].node != want[i].node || got[i].topLeft != want[i].topLeft {
				t.Fatalf("trial %d: snapshot %d differs", trial, i)
			}
		}
		holds := snapshotHolds(got)
		for _, node := range pool {
			_, inWant := func() (struct{}, bool) {
				for _, s := range want {
					if s.node == node {
						return struct{}{}, true
					}
				}
				return struct{}{}, false
			}()
			if holds(node) != inWant {
				t.Fatalf("trial %d: snapshotHolds(%d) = %v", trial, node.ID, holds(node))
			}
		}
	}
}
