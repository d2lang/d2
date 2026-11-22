package layoutgraph

import (
	"fmt"
	"maps"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

type pointerSnapshot[T any] struct {
	pointer *T
	value   T
}

// exactSliceSnapshot retains both the original slice header and every element
// in its backing array. Restoring through the original header preserves
// pointer, length, capacity, and aliases even when a mutation appended into
// spare capacity before rollback.
type exactSliceSnapshot[S ~[]V, V any] struct {
	original S
	backing  []V
}

func captureExactSlice[S ~[]V, V any](values S) exactSliceSnapshot[S, V] {
	if values == nil {
		return exactSliceSnapshot[S, V]{}
	}
	full := values[:cap(values)]
	backing := make([]V, len(full))
	copy(backing, full)
	return exactSliceSnapshot[S, V]{original: values, backing: backing}
}

func (snapshot exactSliceSnapshot[S, V]) restore() S {
	if snapshot.original == nil {
		return nil
	}
	copy(snapshot.original[:cap(snapshot.original)], snapshot.backing)
	return snapshot.original
}

type exactSliceMapSnapshot[K comparable, S ~[]V, V any] struct {
	original map[K]S
	values   map[K]exactSliceSnapshot[S, V]
}

func captureExactSliceMap[K comparable, S ~[]V, V any](values map[K]S) exactSliceMapSnapshot[K, S, V] {
	snapshot := exactSliceMapSnapshot[K, S, V]{original: values}
	if values == nil {
		return snapshot
	}
	snapshot.values = make(map[K]exactSliceSnapshot[S, V], len(values))
	for key, items := range values {
		snapshot.values[key] = captureExactSlice(items)
	}
	return snapshot
}

func (snapshot exactSliceMapSnapshot[K, S, V]) restore() map[K]S {
	if snapshot.original == nil {
		return nil
	}
	clear(snapshot.original)
	for key, items := range snapshot.values {
		snapshot.original[key] = items.restore()
	}
	return snapshot.original
}

func snapshotPointer[T any](pointer *T) pointerSnapshot[T] {
	if pointer == nil {
		return pointerSnapshot[T]{}
	}
	return pointerSnapshot[T]{pointer: pointer, value: *pointer}
}

func (s pointerSnapshot[T]) restore() *T {
	if s.pointer == nil {
		return nil
	}
	*s.pointer = s.value
	return s.pointer
}

type nodeSnapshot struct {
	value Node

	d2ID          pointerSnapshot[string]
	topLeft       pointerSnapshot[geo.Point]
	fontSize      pointerSnapshot[int]
	fixedTopLeft  pointerSnapshot[geo.Point]
	desiredWidth  pointerSnapshot[float64]
	desiredHeight pointerSnapshot[float64]
	label         pointerSnapshot[Label]
	icon          pointerSnapshot[Icon]

	edges                            exactSliceSnapshot[[]*Edge, *Edge]
	nears                            map[*Node]struct{}
	loopOffsets                      map[geo.Orientation]float64
	longDistanceNeighborRequirements map[*Node]LongDistanceNeighborRequirements
}

type edgeSnapshot struct {
	value Edge

	d2ID                 pointerSnapshot[string]
	points               exactSliceSnapshot[[]*geo.Point, *geo.Point]
	pointValues          []pointerSnapshot[geo.Point]
	label                pointerSnapshot[Label]
	sourceArrowheadLabel pointerSnapshot[Label]
	targetArrowheadLabel pointerSnapshot[Label]
	fromTableColumnIndex pointerSnapshot[int]
	toTableColumnIndex   pointerSnapshot[int]
}

type clusterSnapshot struct {
	value          Cluster
	nodes          exactSliceSnapshot[[]*Node, *Node]
	edgeAbductions exactSliceSnapshot[[]*EdgeAbduction, *EdgeAbduction]
}

type sequenceSnapshot struct {
	value          Sequence
	nodes          exactSliceSnapshot[[]*Node, *Node]
	edgeAbductions exactSliceSnapshot[[]*EdgeAbduction, *EdgeAbduction]
}

type treeSnapshot struct {
	value    Tree
	children exactSliceSnapshot[[]*Tree, *Tree]
}

type herdSnapshot struct {
	value              HerdAssignment
	oppositeSidePaired map[*Node]struct{}
	sameSidePaired     map[*Node]struct{}
}

type hierarchySnapshot struct {
	value Hierarchy
	level map[*Node]int
}

type graphSnapshot struct {
	isRootHierarchy bool
	nodes           exactSliceSnapshot[[]*Node, *Node]
	edges           exactSliceSnapshot[[]*Edge, *Edge]
	cellSize        float64

	containers     exactSliceMapSnapshot[*Node, []*Node, *Node]
	clustersRef    map[*Node]*Cluster
	clusters       map[*Node]*Cluster
	trees          exactSliceMapSnapshot[*Node, []*Tree, *Tree]
	nodeToTreeRef  map[*Node]*Tree
	nodeToTree     map[*Node]*Tree
	hubs           exactSliceMapSnapshot[*Node, []*Node, *Node]
	sequencesRef   map[*Node]*Sequence
	sequences      map[*Node]*Sequence
	directionsRef  map[*Node]geo.Orientation
	directions     map[*Node]geo.Orientation
	commonSiblings exactSliceMapSnapshot[*Node, Nodes, *Node]
}

func cloneMapContents[K comparable, V any](values map[K]V) map[K]V {
	if len(values) == 0 {
		return nil
	}
	return maps.Clone(values)
}

func restoreMap[K comparable, V any](original, snapshot map[K]V) map[K]V {
	if original == nil {
		return nil
	}
	clear(original)
	maps.Copy(original, snapshot)
	return original
}

func captureNode(node *Node) nodeSnapshot {
	return nodeSnapshot{
		value:                            *node,
		d2ID:                             snapshotPointer(node.D2ID),
		topLeft:                          snapshotPointer(node.TopLeft),
		fontSize:                         snapshotPointer(node.FontSize),
		fixedTopLeft:                     snapshotPointer(node.FixedTopLeft),
		desiredWidth:                     snapshotPointer(node.DesiredWidth),
		desiredHeight:                    snapshotPointer(node.DesiredHeight),
		label:                            snapshotPointer(node.Label),
		icon:                             snapshotPointer(node.Icon),
		edges:                            captureExactSlice(node.Edges),
		nears:                            cloneMapContents(node.Nears),
		loopOffsets:                      cloneMapContents(node.LoopOffsets),
		longDistanceNeighborRequirements: cloneMapContents(node.LongDistanceNeighborRequirements),
	}
}

func (s nodeSnapshot) restore(node *Node) {
	s.d2ID.restore()
	s.topLeft.restore()
	s.fontSize.restore()
	s.fixedTopLeft.restore()
	s.desiredWidth.restore()
	s.desiredHeight.restore()
	s.label.restore()
	s.icon.restore()

	*node = s.value
	node.Edges = s.edges.restore()
	node.Nears = restoreMap(s.value.Nears, s.nears)
	node.LoopOffsets = restoreMap(s.value.LoopOffsets, s.loopOffsets)
	node.LongDistanceNeighborRequirements = restoreMap(s.value.LongDistanceNeighborRequirements, s.longDistanceNeighborRequirements)
}

func captureEdge(edge *Edge) edgeSnapshot {
	fullPoints := edge.Points[:cap(edge.Points)]
	pointValues := make([]pointerSnapshot[geo.Point], len(fullPoints))
	for i, point := range fullPoints {
		pointValues[i] = snapshotPointer(point)
	}
	return edgeSnapshot{
		value:                *edge,
		d2ID:                 snapshotPointer(edge.D2ID),
		points:               captureExactSlice(edge.Points),
		pointValues:          pointValues,
		label:                snapshotPointer(edge.Label),
		sourceArrowheadLabel: snapshotPointer(edge.SourceArrowheadLabel),
		targetArrowheadLabel: snapshotPointer(edge.TargetArrowheadLabel),
		fromTableColumnIndex: snapshotPointer(edge.FromTableColumnIndex),
		toTableColumnIndex:   snapshotPointer(edge.ToTableColumnIndex),
	}
}

func (s edgeSnapshot) restore(edge *Edge) {
	s.d2ID.restore()
	s.label.restore()
	s.sourceArrowheadLabel.restore()
	s.targetArrowheadLabel.restore()
	s.fromTableColumnIndex.restore()
	s.toTableColumnIndex.restore()
	// s.value.Points retains the original slice header. Repair that backing
	// array before reattaching it so rollback preserves aliases to both the
	// route slice and its point objects.
	for _, point := range s.pointValues {
		point.restore()
	}
	originalPoints := s.points.restore()

	*edge = s.value
	edge.Points = originalPoints
}

func captureGraph(graph *Graph) graphSnapshot {
	return graphSnapshot{
		isRootHierarchy: graph.IsRootHierarchy,
		nodes:           captureExactSlice(graph.Nodes),
		edges:           captureExactSlice(graph.Edges),
		cellSize:        graph.CellSize,
		containers:      captureExactSliceMap(graph.Containers),
		clustersRef:     graph.Clusters,
		clusters:        cloneMapContents(graph.Clusters),
		trees:           captureExactSliceMap(graph.Trees),
		nodeToTreeRef:   graph.NodeToTree,
		nodeToTree:      cloneMapContents(graph.NodeToTree),
		hubs:            captureExactSliceMap(graph.Hubs),
		sequencesRef:    graph.Sequences,
		sequences:       cloneMapContents(graph.Sequences),
		directionsRef:   graph.Directions,
		directions:      cloneMapContents(graph.Directions),
		commonSiblings:  captureExactSliceMap(graph.CommonUncleSiblings),
	}
}

// captureGeometryState records the fields changed by ordinary speculative
// placement transactions without walking and copying the graph's ownership
// maps. Topology-changing transactions additionally call captureRuntimeState.
func (gs *GraphState) captureGeometryStateContext(graph *Graph, guard *limits.WorkGuard) error {
	if gs.captureEdgeRoutes {
		edges := make(map[*Edge]struct{}, len(graph.Edges))
		for _, edge := range graph.Edges {
			if err := guard.Step(); err != nil {
				return err
			}
			if edge == nil {
				return fmt.Errorf("TALA geometry snapshot contains a nil edge")
			}
			edges[edge] = struct{}{}
		}
		for i := range gs.slots.entries {
			node := gs.slots.entries[i].node
			if err := guard.Step(); err != nil {
				return err
			}
			for _, edge := range node.Edges {
				if err := guard.Step(); err != nil {
					return err
				}
				if edge != nil {
					edges[edge] = struct{}{}
				}
			}
		}
		gs.edgeGeometry = make(map[*Edge]edgeSnapshot, len(edges))
		for edge := range edges {
			if err := guard.Step(); err != nil {
				return err
			}
			for range edge.Points[:cap(edge.Points)] {
				if err := guard.Step(); err != nil {
					return err
				}
			}
			gs.edgeGeometry[edge] = captureEdge(edge)
		}
	} else {
		gs.edgeGeometry = nil
	}

	clear(gs.treeOrientations)
	gs.treeOrientations = gs.treeOrientations[:0]
	for _, tree := range graph.NodeToTree {
		if err := guard.Step(); err != nil {
			return err
		}
		if tree != nil {
			gs.treeOrientations = append(gs.treeOrientations, treeOrientationSnapshot{tree: tree, orientation: tree.Orientation})
		}
	}
	return guard.Finish()
}

func (s graphSnapshot) restore(graph *Graph) {
	graph.IsRootHierarchy = s.isRootHierarchy
	graph.Nodes = s.nodes.restore()
	graph.Edges = s.edges.restore()
	graph.CellSize = s.cellSize
	graph.Containers = s.containers.restore()
	graph.Clusters = restoreMap(s.clustersRef, s.clusters)
	graph.Trees = s.trees.restore()
	graph.NodeToTree = restoreMap(s.nodeToTreeRef, s.nodeToTree)
	graph.Hubs = s.hubs.restore()
	graph.Sequences = restoreMap(s.sequencesRef, s.sequences)
	graph.Directions = restoreMap(s.directionsRef, s.directions)
	graph.CommonUncleSiblings = s.commonSiblings.restore()
}

type runtimeObjectCollection struct {
	nodes          map[*Node]struct{}
	nodeOrder      []*Node
	edges          map[*Edge]struct{}
	clusters       map[*Cluster]struct{}
	sequences      map[*Sequence]struct{}
	trees          map[*Tree]struct{}
	edgeAbductions map[*EdgeAbduction]struct{}
	herds          map[*HerdAssignment]struct{}
	hierarchies    map[*Hierarchy]struct{}
}

// collectRuntimeObjectsContext follows every reference that can keep mutable
// layout state alive outside Graph.Nodes. Both full transaction snapshots and
// narrower field snapshots use this one bounded traversal so their ownership
// coverage cannot drift apart.
func collectRuntimeObjectsContext(graph *Graph, guard *limits.WorkGuard, scope string) (runtimeObjectCollection, error) {
	c := runtimeCollector{guard: guard, scope: scope}
	c.nodes.seen = make(map[*Node]struct{}, len(graph.Nodes))
	addNode := func(node *Node) { collect(&c, &c.nodes, node, maxEngineNodes, "node") }
	addEdge := func(edge *Edge) { collect(&c, &c.edges, edge, maxEngineEdges, "edge") }
	addCluster := func(cluster *Cluster) { collect(&c, &c.clusters, cluster, maxEngineNodes, "cluster") }
	addSequence := func(sequence *Sequence) { collect(&c, &c.sequences, sequence, maxEngineNodes, "sequence") }
	addTree := func(tree *Tree) { collect(&c, &c.trees, tree, maxEngineNodes, "tree") }
	addEdgeAbduction := func(abduction *EdgeAbduction) {
		collect(&c, &c.edgeAbductions, abduction, maxEngineEdges, "edge-abduction")
	}
	addHerd := func(herd *HerdAssignment) { collect(&c, &c.herds, herd, maxEngineNodes, "herd") }
	addHierarchy := func(hierarchy *Hierarchy) { collect(&c, &c.hierarchies, hierarchy, maxEngineNodes, "hierarchy") }

	for _, node := range graph.Nodes {
		addNode(node)
	}
	for _, edge := range graph.Edges {
		addEdge(edge)
	}
	for container, children := range graph.Containers {
		addNode(container)
		for _, child := range children {
			addNode(child)
		}
	}
	for vessel, cluster := range graph.Clusters {
		addNode(vessel)
		addCluster(cluster)
	}
	for vessel, sequence := range graph.Sequences {
		addNode(vessel)
		addSequence(sequence)
	}
	for node, nodeTrees := range graph.Trees {
		addNode(node)
		for _, tree := range nodeTrees {
			addTree(tree)
		}
	}
	for node, tree := range graph.NodeToTree {
		addNode(node)
		addTree(tree)
	}
	for hub, hubNodes := range graph.Hubs {
		addNode(hub)
		for _, node := range hubNodes {
			addNode(node)
		}
	}
	for node, siblings := range graph.CommonUncleSiblings {
		addNode(node)
		for _, sibling := range siblings {
			addNode(sibling)
		}
	}
	for node := range graph.Directions {
		addNode(node)
	}
	if c.err != nil {
		return runtimeObjectCollection{}, c.err
	}

	var nodeIndex, edgeIndex, clusterIndex, sequenceIndex int
	var treeIndex, abductionIndex, herdIndex, hierarchyIndex int
	for nodeIndex < len(c.nodes.queue) || edgeIndex < len(c.edges.queue) ||
		clusterIndex < len(c.clusters.queue) || sequenceIndex < len(c.sequences.queue) ||
		treeIndex < len(c.trees.queue) || abductionIndex < len(c.edgeAbductions.queue) ||
		herdIndex < len(c.herds.queue) || hierarchyIndex < len(c.hierarchies.queue) {
		if c.err != nil {
			return runtimeObjectCollection{}, c.err
		}
		switch {
		case nodeIndex < len(c.nodes.queue):
			node := c.nodes.queue[nodeIndex]
			nodeIndex++
			addNode(node.Container)
			for near := range node.Nears {
				addNode(near)
			}
			for neighbor := range node.LongDistanceNeighborRequirements {
				addNode(neighbor)
			}
			for _, edge := range node.Edges {
				addEdge(edge)
			}
			addCluster(node.Cluster)
			addSequence(node.Sequence)
			addHerd(node.HerdAssignment)
			addHierarchy(node.Hierarchy)

		case edgeIndex < len(c.edges.queue):
			edge := c.edges.queue[edgeIndex]
			edgeIndex++
			addNode(edge.From)
			addNode(edge.To)

		case clusterIndex < len(c.clusters.queue):
			cluster := c.clusters.queue[clusterIndex]
			clusterIndex++
			addNode(cluster.Vessel)
			addNode(cluster.Container)
			for _, node := range cluster.Nodes {
				addNode(node)
			}
			for _, abduction := range cluster.EdgeAbductions {
				addEdgeAbduction(abduction)
			}

		case sequenceIndex < len(c.sequences.queue):
			sequence := c.sequences.queue[sequenceIndex]
			sequenceIndex++
			addNode(sequence.Vessel)
			addNode(sequence.Container)
			for _, node := range sequence.Nodes {
				addNode(node)
			}
			for _, abduction := range sequence.EdgeAbductions {
				addEdgeAbduction(abduction)
			}

		case treeIndex < len(c.trees.queue):
			tree := c.trees.queue[treeIndex]
			treeIndex++
			addNode(tree.Node)
			addEdge(tree.SentinelEdge)
			addTree(tree.Parent)
			for _, child := range tree.Children {
				addTree(child)
			}

		case abductionIndex < len(c.edgeAbductions.queue):
			abduction := c.edgeAbductions.queue[abductionIndex]
			abductionIndex++
			addEdge(abduction.Edge)
			addNode(abduction.OriginallyFrom)
			addNode(abduction.OriginallyTo)
			addNode(abduction.CurrentFrom)
			addNode(abduction.CurrentTo)

		case herdIndex < len(c.herds.queue):
			herd := c.herds.queue[herdIndex]
			herdIndex++
			for node := range herd.oppositeSidePaired {
				addNode(node)
			}
			for node := range herd.sameSidePaired {
				addNode(node)
			}

		case hierarchyIndex < len(c.hierarchies.queue):
			hierarchy := c.hierarchies.queue[hierarchyIndex]
			hierarchyIndex++
			for node := range hierarchy.level {
				addNode(node)
			}
		}
		if c.err != nil {
			return runtimeObjectCollection{}, c.err
		}
	}

	return runtimeObjectCollection{
		nodes:          c.nodes.seen,
		nodeOrder:      c.nodes.queue,
		edges:          c.edges.seen,
		clusters:       c.clusters.seen,
		sequences:      c.sequences.seen,
		trees:          c.trees.seen,
		edgeAbductions: c.edgeAbductions.seen,
		herds:          c.herds.seen,
		hierarchies:    c.hierarchies.seen,
	}, nil
}

// runtimeCollector is collectRuntimeObjectsContext's traversal state: a
// visited set and a queue per kind of object, and the first error, after
// which nothing more is charged or added.
type runtimeCollector struct {
	guard *limits.WorkGuard
	scope string
	err   error

	nodes          runtimeObjects[*Node]
	edges          runtimeObjects[*Edge]
	clusters       runtimeObjects[*Cluster]
	sequences      runtimeObjects[*Sequence]
	trees          runtimeObjects[*Tree]
	edgeAbductions runtimeObjects[*EdgeAbduction]
	herds          runtimeObjects[*HerdAssignment]
	hierarchies    runtimeObjects[*Hierarchy]
}

// runtimeObjects is the visited set and the queue of one kind of object, in
// the order they were found.
type runtimeObjects[T comparable] struct {
	seen  map[T]struct{}
	queue []T
}

// collect follows one reference to v. It charges one unit, then adds v to set
// unless v is nil or already there; adding an object to a set that holds
// limit objects fails instead. After the first error it does nothing.
func collect[T comparable](c *runtimeCollector, set *runtimeObjects[T], v T, limit int, kind string) {
	if c.err != nil {
		return
	}
	if c.err = c.guard.Step(); c.err != nil {
		return
	}
	var zero T
	if v == zero {
		return
	}
	if _, exists := set.seen[v]; exists {
		return
	}
	if len(set.seen) >= limit {
		c.err = c.limitError(kind, limit)
		return
	}
	if set.seen == nil {
		set.seen = make(map[T]struct{})
	}
	set.seen[v] = struct{}{}
	set.queue = append(set.queue, v)
}

// limitError reports a snapshot that would hold more than limit objects of
// kind.
func (c *runtimeCollector) limitError(kind string, limit int) error {
	return fmt.Errorf("TALA %s %s snapshot exceeds limit %d", c.scope, kind, limit)
}

// chargeRuntimeStateContext charges what captureRuntimeStateContext charges,
// without capturing: the same traversal (its units, limits and errors), then
// one unit for the graph, one per node, one per edge plus one per slot of its
// point capacity, and one per cluster, sequence, tree, abduction, herd and
// hierarchy, and the final Finish. Those capture loops can fail only through
// the guard, so charging their units at once fails exactly where one of
// their Steps would. The traversal's node set is kept for TracksNode.
func (gs *GraphState) chargeRuntimeStateContext(graph *Graph, guard *limits.WorkGuard) error {
	objects, err := collectRuntimeObjectsContext(graph, guard, "transaction runtime")
	if err != nil {
		return err
	}
	units := 1 + int64(len(objects.nodes))
	for edge := range objects.edges {
		units += 1 + int64(cap(edge.Points))
	}
	units += int64(len(objects.clusters)) + int64(len(objects.sequences)) + int64(len(objects.trees)) +
		int64(len(objects.edgeAbductions)) + int64(len(objects.herds)) + int64(len(objects.hierarchies))
	if err := guard.Add(units); err != nil {
		return err
	}
	gs.trackedNodes = objects.nodes
	return guard.Finish()
}

func (gs *GraphState) captureRuntimeStateContext(graph *Graph, guard *limits.WorkGuard) error {
	objects, err := collectRuntimeObjectsContext(graph, guard, "transaction runtime")
	if err != nil {
		return err
	}
	nodes := objects.nodes
	edges := objects.edges
	clusters := objects.clusters
	sequences := objects.sequences
	trees := objects.trees
	edgeAbductions := objects.edgeAbductions
	herds := objects.herds
	hierarchies := objects.hierarchies

	if err := guard.Step(); err != nil {
		return err
	}
	gs.graph = captureGraph(graph)
	gs.nodes = make(map[*Node]nodeSnapshot, len(nodes))
	for node := range nodes {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.nodes[node] = captureNode(node)
	}
	gs.edges = make(map[*Edge]edgeSnapshot, len(edges))
	for edge := range edges {
		if err := guard.Step(); err != nil {
			return err
		}
		for range edge.Points[:cap(edge.Points)] {
			if err := guard.Step(); err != nil {
				return err
			}
		}
		gs.edges[edge] = captureEdge(edge)
	}
	gs.clusters = make(map[*Cluster]clusterSnapshot, len(clusters))
	for cluster := range clusters {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.clusters[cluster] = clusterSnapshot{
			value:          *cluster,
			nodes:          captureExactSlice(cluster.Nodes),
			edgeAbductions: captureExactSlice(cluster.EdgeAbductions),
		}
	}
	gs.sequences = make(map[*Sequence]sequenceSnapshot, len(sequences))
	for sequence := range sequences {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.sequences[sequence] = sequenceSnapshot{
			value:          *sequence,
			nodes:          captureExactSlice(sequence.Nodes),
			edgeAbductions: captureExactSlice(sequence.EdgeAbductions),
		}
	}
	gs.trees = make(map[*Tree]treeSnapshot, len(trees))
	for tree := range trees {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.trees[tree] = treeSnapshot{value: *tree, children: captureExactSlice(tree.Children)}
	}
	gs.edgeAbductions = make(map[*EdgeAbduction]EdgeAbduction, len(edgeAbductions))
	for abduction := range edgeAbductions {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.edgeAbductions[abduction] = *abduction
	}
	gs.herds = make(map[*HerdAssignment]herdSnapshot, len(herds))
	for herd := range herds {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.herds[herd] = herdSnapshot{
			value:              *herd,
			oppositeSidePaired: cloneMapContents(herd.oppositeSidePaired),
			sameSidePaired:     cloneMapContents(herd.sameSidePaired),
		}
	}
	gs.hierarchies = make(map[*Hierarchy]hierarchySnapshot, len(hierarchies))
	for hierarchy := range hierarchies {
		if err := guard.Step(); err != nil {
			return err
		}
		gs.hierarchies[hierarchy] = hierarchySnapshot{value: *hierarchy, level: cloneMapContents(hierarchy.level)}
	}
	return guard.Finish()
}
