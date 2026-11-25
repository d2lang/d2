package layoutgraph

import (
	"math"

	"github.com/d2lang/d2/lib/geo"
)

// GeometryImage records everything a geometry transaction snapshots and
// restores, for exact before/after comparison: the box of every node it
// tracks (graph nodes and their container descendants, container keys and
// children, cluster and sequence vessels, containers and members; top-left
// pointer and the bits of every value), cluster arrangements and paddings,
// tree orientations, and the graph's node order.
//
// Nodes are kept in a slice in recording order with a slot hint on the node
// (Node.imageSlot, verified like snapshotSlots' hints), so recording and
// lookups need no map in the usual case.
type GeometryImage struct {
	order    []*Node
	entries  []geometryImageEntry
	index    map[*Node]int32
	stack    []*Node
	clusters []clusterImageEntry
	trees    map[*Tree]geo.Orientation
}

type geometryImageEntry struct {
	node    *Node
	topLeft *geo.Point
	x, y    uint64
	width   uint64
	height  uint64
}

type clusterImageEntry struct {
	cluster              *Cluster
	arrangement, desired ClusterArrangement
	padding              uint64
}

func (image *GeometryImage) recorded(node *Node) bool {
	slot := node.imageSlot
	return slot >= 0 && int(slot) < len(image.entries) && image.entries[slot].node == node
}

func (image *GeometryImage) record(root *Node, g *Graph) {
	image.stack = append(image.stack[:0], root)
	for len(image.stack) > 0 {
		node := image.stack[len(image.stack)-1]
		image.stack = image.stack[:len(image.stack)-1]
		if node == nil || image.recorded(node) {
			continue
		}
		entry := geometryImageEntry{
			node:    node,
			topLeft: node.TopLeft,
			width:   math.Float64bits(node.Width),
			height:  math.Float64bits(node.Height),
		}
		if node.TopLeft != nil {
			entry.x = math.Float64bits(node.TopLeft.X)
			entry.y = math.Float64bits(node.TopLeft.Y)
		}
		node.imageSlot = int32(len(image.entries))
		image.entries = append(image.entries, entry)
		image.stack = append(image.stack, g.Containers[node]...)
	}
}

// Capture records g's current geometry.
func (image *GeometryImage) Capture(g *Graph) {
	image.order = append(image.order[:0], g.Nodes...)
	clear(image.entries)
	image.entries = image.entries[:0]
	image.index = nil
	image.clusters = image.clusters[:0]
	clear(image.trees)
	for _, node := range g.Nodes {
		image.record(node, g)
	}
	for container, children := range g.Containers {
		image.record(container, g)
		for _, child := range children {
			image.record(child, g)
		}
	}
	for _, cluster := range g.Clusters {
		if cluster == nil {
			continue
		}
		image.clusters = append(image.clusters, clusterImageEntry{
			cluster:     cluster,
			arrangement: cluster.Arrangement,
			desired:     cluster.DesiredArrangement,
			padding:     math.Float64bits(cluster.Padding),
		})
		image.record(cluster.Vessel, g)
		image.record(cluster.Container, g)
		for _, node := range cluster.Nodes {
			image.record(node, g)
		}
	}
	for _, sequence := range g.Sequences {
		if sequence == nil {
			continue
		}
		image.record(sequence.Vessel, g)
		image.record(sequence.Container, g)
		for _, node := range sequence.Nodes {
			image.record(node, g)
		}
	}
	for _, tree := range g.NodeToTree {
		if tree != nil {
			if image.trees == nil {
				image.trees = make(map[*Tree]geo.Orientation)
			}
			image.trees[tree] = tree.Orientation
		}
	}
}

// lookup returns node's entry, or false when it was not recorded.
func (image *GeometryImage) lookup(node *Node) (geometryImageEntry, bool) {
	if image.recorded(node) {
		return image.entries[node.imageSlot], true
	}
	if image.index == nil {
		image.index = make(map[*Node]int32, len(image.entries))
		for i := range image.entries {
			image.index[image.entries[i].node] = int32(i)
		}
	}
	if slot, ok := image.index[node]; ok {
		return image.entries[slot], true
	}
	return geometryImageEntry{}, false
}

// Equal reports whether both images recorded the same node order, the same
// set of nodes with bit-identical geometry and top-left pointers, the same
// set of clusters with the same arrangements and paddings, and the same set
// of trees with the same orientations.
func (image *GeometryImage) Equal(other *GeometryImage) bool {
	if len(image.order) != len(other.order) || len(image.entries) != len(other.entries) ||
		len(image.trees) != len(other.trees) {
		return false
	}
	for i, node := range image.order {
		if other.order[i] != node {
			return false
		}
	}
	for i := range image.entries {
		entry := image.entries[i]
		otherEntry, ok := other.lookup(entry.node)
		if !ok || otherEntry != entry {
			return false
		}
	}
	// Clusters are few: compare them as sets (an entry repeats only when two
	// vessels map to one cluster, with equal values).
	if !clusterImagesCover(image.clusters, other.clusters) || !clusterImagesCover(other.clusters, image.clusters) {
		return false
	}
	for tree, orientation := range image.trees {
		if otherOrientation, ok := other.trees[tree]; !ok || otherOrientation != orientation {
			return false
		}
	}
	return true
}

// clusterImagesCover reports whether every cluster in entries appears in
// others with equal values.
func clusterImagesCover(entries, others []clusterImageEntry) bool {
	for _, entry := range entries {
		found := false
		for _, other := range others {
			if other.cluster == entry.cluster {
				if other != entry {
					return false
				}
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
