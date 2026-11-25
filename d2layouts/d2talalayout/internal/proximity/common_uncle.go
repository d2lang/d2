package proximity

import (
	"cmp"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// CommonUncleSiblings finds siblings that connect to the same node at their
// parent's level. Placement uses the largest such set as a proximity hint.
func CommonUncleSiblings(graph *layoutgraph.Graph) map[*layoutgraph.Node]layoutgraph.Nodes {
	uncleToCousins := make(map[*layoutgraph.Node]layoutgraph.Nodes)
	common := make(map[*layoutgraph.Node]layoutgraph.Nodes)
	firstSeen := make(map[*layoutgraph.Node]int)
	var orderedUncles []*layoutgraph.Node
	for _, container := range graph.ContainerRDFSOrderUnbounded(nil) {
		orderedUncles = orderedUncles[:0]
		children := graph.Containers[container]
		for _, node := range children {
			for _, edge := range node.Edges {
				adjacent := node.Adjacent(edge)
				if adjacent.Container == container.Container {
					if _, ok := uncleToCousins[adjacent]; !ok {
						if _, seen := firstSeen[adjacent]; !seen {
							firstSeen[adjacent] = len(firstSeen)
						}
						orderedUncles = append(orderedUncles, adjacent)
						uncleToCousins[adjacent] = []*layoutgraph.Node{}
					}
					// Edges are visited together for each sibling, so a repeated
					// sibling is always the last member of this group.
					siblings := uncleToCousins[adjacent]
					if len(siblings) == 0 || siblings[len(siblings)-1] != node {
						uncleToCousins[adjacent] = append(uncleToCousins[adjacent], node)
					}
				}
			}
		}
		// Equal-sized groups retain the first destination discovered across
		// the graph. Only destinations present in this container can win.
		slices.SortFunc(orderedUncles, func(a, b *layoutgraph.Node) int {
			return cmp.Compare(firstSeen[a], firstSeen[b])
		})
		for _, uncle := range orderedUncles {
			siblings := uncleToCousins[uncle]
			if len(siblings) < 2 {
				continue
			}
			for _, sibling := range siblings {
				if existing, ok := common[sibling]; !ok || len(existing) < len(siblings) {
					common[sibling] = siblings
				}
			}
		}
		clear(uncleToCousins)
	}
	return common
}
