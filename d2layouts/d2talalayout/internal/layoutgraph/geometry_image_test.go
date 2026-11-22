package layoutgraph

import (
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"
)

// referenceGeometryMap is the former map-based fingerprint of a graph's
// tracked geometry (placement's graphGeometryFingerprint).
type referenceGeometryMap struct {
	order []*Node
	nodes map[*Node][5]uint64
	tops  map[*Node]*geo.Point
}

func captureReferenceGeometry(g *Graph) referenceGeometryMap {
	ref := referenceGeometryMap{order: append([]*Node(nil), g.Nodes...), nodes: map[*Node][5]uint64{}, tops: map[*Node]*geo.Point{}}
	var record func(*Node)
	record = func(node *Node) {
		if node == nil {
			return
		}
		if _, ok := ref.nodes[node]; ok {
			return
		}
		value := [5]uint64{1, 0, 0, math.Float64bits(node.Width), math.Float64bits(node.Height)}
		if node.TopLeft != nil {
			value[1] = math.Float64bits(node.TopLeft.X)
			value[2] = math.Float64bits(node.TopLeft.Y)
		}
		ref.nodes[node] = value
		ref.tops[node] = node.TopLeft
		for _, child := range g.Containers[node] {
			record(child)
		}
	}
	for _, node := range g.Nodes {
		record(node)
	}
	for container, children := range g.Containers {
		record(container)
		for _, child := range children {
			record(child)
		}
	}
	return ref
}

func (ref referenceGeometryMap) equal(other referenceGeometryMap) bool {
	if len(ref.order) != len(other.order) || len(ref.nodes) != len(other.nodes) {
		return false
	}
	for i := range ref.order {
		if ref.order[i] != other.order[i] {
			return false
		}
	}
	for node, value := range ref.nodes {
		if otherValue, ok := other.nodes[node]; !ok || otherValue != value || other.tops[node] != ref.tops[node] {
			return false
		}
	}
	return true
}

// TestGeometryImageEqualMatchesMapFingerprint compares GeometryImage's
// verdict with the map-based fingerprint under random mutations: moves,
// signed zeros, NaN, replaced top-left pointers, reordered nodes.
func TestGeometryImageEqualMatchesMapFingerprint(t *testing.T) {
	agree, differ := 0, 0
	for seed := int64(0); seed < 400; seed++ {
		rng := rand.New(rand.NewSource(seed))
		g := randomContainerGraph(rng)
		var before, after GeometryImage
		before.Capture(g)
		ref := captureReferenceGeometry(g)
		if rng.Intn(2) == 0 {
			node := g.Nodes[rng.Intn(len(g.Nodes))]
			switch rng.Intn(6) {
			case 0:
				node.TopLeft.X += 1
			case 1:
				node.TopLeft = &geo.Point{X: node.TopLeft.X, Y: node.TopLeft.Y}
			case 2:
				node.Width = math.Copysign(0, -1)
			case 3:
				node.Height = math.NaN()
			case 4:
				if len(g.Nodes) > 1 {
					g.Nodes[0], g.Nodes[len(g.Nodes)-1] = g.Nodes[len(g.Nodes)-1], g.Nodes[0]
				}
			case 5:
				node.TopLeft.Y = node.TopLeft.Y + 0
			}
		}
		after.Capture(g)
		want := ref.equal(captureReferenceGeometry(g))
		if got := before.Equal(&after); got != want {
			t.Fatalf("seed %d: Equal = %v, reference %v", seed, got, want)
		}
		if want {
			agree++
		} else {
			differ++
		}
	}
	if agree == 0 || differ == 0 {
		t.Fatalf("fixtures one-sided: %d equal, %d different", agree, differ)
	}
}
