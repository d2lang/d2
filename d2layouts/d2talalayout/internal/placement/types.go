// Package placement positions nodes and refines their geometry before routing.
package placement

import (
	"context"
	"fmt"
	"math"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

const maxCompactionCandidateCount = limits.MaxGraphSize + 3

type pointerSnapshot[T any] struct {
	pointer *T
	value   T
}

func snapshotPointer[T any](pointer *T) pointerSnapshot[T] {
	if pointer == nil {
		return pointerSnapshot[T]{}
	}
	return pointerSnapshot[T]{pointer: pointer, value: *pointer}
}

func (snapshot pointerSnapshot[T]) restore() *T {
	if snapshot.pointer == nil {
		return nil
	}
	*snapshot.pointer = snapshot.value
	return snapshot.pointer
}

type nodePositionSnapshot struct {
	node    *layoutgraph.Node
	topLeft pointerSnapshot[geo.Point]
}

func restoreNodePositions(snapshots []nodePositionSnapshot) {
	for _, snapshot := range snapshots {
		snapshot.node.TopLeft = snapshot.topLeft.restore()
	}
}

func snapshotNodePositionsContext(ctx context.Context, location string, nodes []*layoutgraph.Node) ([]nodePositionSnapshot, error) {
	guard, err := limits.NewWorkGuard(ctx, location, limits.MaxEngineWorkUnits)
	if err != nil {
		return nil, err
	}
	// Nodes already recorded: found by scanning snapshots while there are
	// few, then in a set holding every recorded node.
	var seen map[*layoutgraph.Node]struct{}
	snapshots := make([]nodePositionSnapshot, 0, len(nodes))
	type pendingNode struct {
		node  *layoutgraph.Node
		graph *layoutgraph.Graph
	}
	queue := make([]pendingNode, 0, len(nodes))
	enqueue := func(node *layoutgraph.Node, graph *layoutgraph.Graph) error {
		if node == nil {
			return nil
		}
		if seen != nil {
			if _, ok := seen[node]; ok {
				return nil
			}
		} else {
			for i := range snapshots {
				if snapshots[i].node == node {
					return nil
				}
			}
		}
		if len(snapshots) >= limits.MaxEngineNodes {
			return fmt.Errorf("TALA %s unique node count exceeds limit %d", location, limits.MaxEngineNodes)
		}
		if err := guard.Step(); err != nil {
			return err
		}
		if seen == nil && len(snapshots) == smallNodeSetScan {
			seen = make(map[*layoutgraph.Node]struct{}, 2*smallNodeSetScan)
			for i := range snapshots {
				seen[snapshots[i].node] = struct{}{}
			}
		}
		if seen != nil {
			seen[node] = struct{}{}
		}
		snapshots = append(snapshots, nodePositionSnapshot{node: node, topLeft: snapshotPointer(node.TopLeft)})
		queue = append(queue, pendingNode{node: node, graph: graph})
		return nil
	}
	for _, node := range nodes {
		if node != nil {
			if err := enqueue(node, node.Graph); err != nil {
				return nil, err
			}
		}
	}
	for index := 0; index < len(queue); index++ {
		current := queue[index]
		if current.graph == nil {
			continue
		}
		node := current.node
		if node.IsContainer() {
			for _, child := range current.graph.Containers[node] {
				if err := enqueue(child, current.graph); err != nil {
					return nil, err
				}
			}
		}
		if node.IsClusterVessel() {
			if cluster := current.graph.Clusters[node]; cluster != nil {
				for _, child := range cluster.Nodes {
					if err := enqueue(child, current.graph); err != nil {
						return nil, err
					}
				}
			}
		}
		if sequence := current.graph.Sequences[node]; sequence != nil {
			for _, child := range sequence.Nodes {
				if err := enqueue(child, current.graph); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := guard.Finish(); err != nil {
		return nil, err
	}
	return snapshots, nil
}

// smallNodeSetScan is the size up to which node sets are kept as a list
// that is scanned instead of a map.
const smallNodeSetScan = 32

// sortSliceExact sorts s exactly as sort.Slice(s, func(i, j int) bool {
// return less(s[i], s[j]) }) does: slices.SortFunc runs the same pattern-
// defeating quicksort (both are generated from one template) and consults
// its comparison only as cmp(a, b) < 0, so every comparison, swap and the
// resulting order are the same; it just avoids sort.Slice's reflection.
// xsort.Func is slices.SortFunc in native builds and that sort.Slice call in
// WebAssembly builds, where one shared sorter is worth more than its speed.
func sortSliceExact[T any](s []T, less func(a, b T) bool) {
	xsort.Func(s, func(a, b T) int {
		if less(a, b) {
			return -1
		}
		return 1
	})
}

func nonNilEquals(first, second *geo.Point) bool {
	return first.X == second.X && first.Y == second.Y
}

func roundToPreviousCellSize(value, cellSize float64) float64 {
	return math.Floor(value/cellSize) * cellSize
}

func roundToNearestCellSize(value, cellSize float64) float64 {
	return math.Round(value/cellSize) * cellSize
}

func numPointsWithinManhattanDistance(distance float64) int {
	return int(math.Ceil(math.Pow(distance, 2) + math.Pow(distance+1, 2)))
}
