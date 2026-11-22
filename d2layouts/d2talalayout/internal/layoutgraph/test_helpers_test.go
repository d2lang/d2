package layoutgraph

import (
	"context"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// maps returns the relations as the former maps of maps (built once).
func (set *existingOverlapSet) maps() (near, exact map[*Node]map[*Node]struct{}) {
	if set == nil {
		return map[*Node]map[*Node]struct{}{}, map[*Node]map[*Node]struct{}{}
	}
	if set.nearMap == nil {
		build := func(relation [][]int32) map[*Node]map[*Node]struct{} {
			out := make(map[*Node]map[*Node]struct{})
			for i, neighbors := range relation {
				if len(neighbors) == 0 {
					continue
				}
				inner := make(map[*Node]struct{}, len(neighbors))
				for _, j := range neighbors {
					inner[set.nodes[j]] = struct{}{}
				}
				out[set.nodes[i]] = inner
			}
			return out
		}
		set.nearMap = build(set.near)
		set.exactMap = build(set.exact)
	}
	return set.nearMap, set.exactMap
}

// validateSharedEngineGraph is validateEngineGraph with the inventory in maps
// only; it writes nothing to the graph.
func validateSharedEngineGraph(ctx context.Context, location string, graph *Graph) error {
	return validateEngineGraphWith(ctx, location, graph, false)
}

func buildTransactionOverlaps(g *Graph, guard *limits.WorkGuard) (map[*Node]map[*Node]struct{}, map[*Node]map[*Node]struct{}, error) {
	return buildTransactionOverlapsWithReferenceLimit(g, guard, maxTransactionOverlapReferences)
}

// buildTransactionOverlapsWithReferenceLimit returns buildExistingOverlapSet's
// relations as maps of maps (near, exact).
func buildTransactionOverlapsWithReferenceLimit(g *Graph, guard *limits.WorkGuard, referenceLimit int64) (map[*Node]map[*Node]struct{}, map[*Node]map[*Node]struct{}, error) {
	set, err := buildExistingOverlapSet(g, guard, referenceLimit)
	if err != nil {
		return nil, nil, err
	}
	near, exact := set.maps()
	return near, exact, nil
}

func buildExistingOverlapSet(g *Graph, guard *limits.WorkGuard, referenceLimit int64) (*existingOverlapSet, error) {
	return g.buildExistingOverlapSetInto(&existingOverlapSet{}, guard, referenceLimit)
}
