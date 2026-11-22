package layoutgraph

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// TestContainerOrderMatchesSortSlice checks that repositionContainers'
// slices.SortFunc ordering equals the former sort.Slice ordering, including
// equal areas, equal IDs and NaN areas (inconsistent comparisons).
func TestContainerOrderMatchesSortSlice(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for trial := 0; trial < 3000; trial++ {
		count := rng.Intn(60)
		nodes := make([]*Node, count)
		for i := range nodes {
			width := float64(1 + rng.Intn(6))
			if rng.Intn(15) == 0 {
				width = math.NaN()
			}
			nodes[i] = NewNode(EntityID(rng.Intn(count+1)), width, float64(1+rng.Intn(3)))
		}
		legacy := slices.Clone(nodes)
		sort.Slice(legacy, func(i, j int) bool {
			if legacy[i].area() == legacy[j].area() {
				return legacy[i].ID < legacy[j].ID
			}
			return legacy[i].area() < legacy[j].area()
		})
		current := slices.Clone(nodes)
		slices.SortFunc(current, func(a, b *Node) int {
			less := a.area() < b.area()
			if a.area() == b.area() {
				less = a.ID < b.ID
			}
			if less {
				return -1
			}
			return 1
		})
		if !slices.Equal(legacy, current) {
			t.Fatalf("trial %d: orders differ", trial)
		}
	}
}

// TestIsDescendantOfMatchesRecursion compares the iterative ancestry walk
// with the former recursion over containers, clusters and sequences.
func TestIsDescendantOfMatchesRecursion(t *testing.T) {
	var recursive func(node, ancestor *Node) bool
	recursive = func(node, ancestor *Node) bool {
		if ancestor == node {
			return true
		}
		if node == nil {
			return false
		}
		if node.Container != nil {
			return recursive(node.Container, ancestor)
		}
		if node.Cluster != nil {
			return recursive(node.Cluster.Vessel, ancestor)
		}
		if node.Sequence != nil {
			return recursive(node.Sequence.Vessel, ancestor)
		}
		return ancestor == nil
	}
	rng := rand.New(rand.NewSource(5))
	for trial := 0; trial < 300; trial++ {
		var nodes []*Node
		for i := 0; i < 1+rng.Intn(30); i++ {
			node := NewNode(EntityID(i+1), 1, 1)
			if len(nodes) > 0 {
				parent := nodes[rng.Intn(len(nodes))]
				switch rng.Intn(4) {
				case 0:
					node.Container = parent
				case 1:
					node.Cluster = &Cluster{Vessel: parent}
				case 2:
					node.Sequence = &Sequence{Vessel: parent}
				}
			}
			nodes = append(nodes, node)
		}
		candidates := append([]*Node{nil}, nodes...)
		for _, node := range candidates {
			for _, ancestor := range candidates {
				if got, want := node.isDescendantOf(ancestor), recursive(node, ancestor); got != want {
					t.Fatalf("trial %d: isDescendantOf = %v, want %v", trial, got, want)
				}
			}
		}
	}
}

// TestTransactionFiniteBoxMatchesPredicates compares the subtraction form of
// transactionNodeHasFiniteBox with the per-value predicates it replaced.
func TestTransactionFiniteBoxMatchesPredicates(t *testing.T) {
	values := []float64{0, -0.0, 1, -1, 0.5, 1e308, -1e308, math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.Inf(1), math.Inf(-1), math.NaN()}
	legacy := func(node *Node) bool {
		return node != nil && node.TopLeft != nil &&
			transactionFinite(node.TopLeft.X) && transactionFinite(node.TopLeft.Y) &&
			transactionFinite(node.Width) && transactionFinite(node.Height) &&
			transactionFinite(node.TopLeft.X+node.Width) && transactionFinite(node.TopLeft.Y+node.Height) &&
			node.Width >= 0 && node.Height >= 0
	}
	for _, x := range values {
		for _, y := range values {
			for _, w := range values {
				for _, h := range values {
					node := NewNode(1, w, h)
					node.TopLeft = &geo.Point{X: x, Y: y}
					if got, want := transactionNodeHasFiniteBox(node), legacy(node); got != want {
						t.Fatalf("x=%v y=%v w=%v h=%v: %v, want %v", x, y, w, h, got, want)
					}
				}
			}
		}
	}
	if transactionNodeHasFiniteBox(nil) || transactionNodeHasFiniteBox(NewNode(1, 1, 1)) {
		t.Fatal("nil node or position reported finite")
	}
}
