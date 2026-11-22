package layoutgraph

import (
	"context"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/lib/geo"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
)

// chargeSnapshotFixture builds a graph that reaches every kind of runtime
// object the topology capture visits (containers, clusters, sequences, trees,
// hubs, common siblings, directions, abductions, herds, hierarchies, routed
// edges with spare point capacity), plus random extra structure.
func chargeSnapshotFixture(rng *rand.Rand) (*Graph, []*Node) {
	g := NewGraph()
	var nodes []*Node
	newNode := func() *Node {
		node := NewNode(EntityID(len(nodes)+1), 10+float64(rng.Intn(40)), 10+float64(rng.Intn(40)))
		node.TopLeft = geo.NewPoint(float64(rng.Intn(500)), float64(rng.Intn(500)))
		nodes = append(nodes, node)
		return node
	}
	count := 2 + rng.Intn(10)
	for range count {
		g.AddNodeUnchecked(newNode())
	}
	// Containers, some children only reachable through the map.
	for range rng.Intn(4) {
		container := nodes[rng.Intn(len(nodes))]
		child := newNode()
		child.Container = container
		g.Containers[container] = append(g.Containers[container], child)
	}
	for range rng.Intn(6) {
		from, to := nodes[rng.Intn(len(nodes))], nodes[rng.Intn(len(nodes))]
		edge := g.Connect(from, to)
		points := make([]*geo.Point, 1+rng.Intn(3), 4+rng.Intn(4))
		for i := range points {
			points[i] = geo.NewPoint(float64(i), float64(i))
		}
		edge.Points = points
		if rng.Intn(3) == 0 {
			abduction := &EdgeAbduction{Edge: edge, OriginallyFrom: from, OriginallyTo: to, CurrentFrom: from, CurrentTo: to}
			vessel := newNode()
			cluster := &Cluster{Vessel: vessel, Nodes: []*Node{from}, EdgeAbductions: []*EdgeAbduction{abduction}, Graph: g}
			g.Clusters[vessel] = cluster
			from.Cluster = cluster
		}
	}
	if rng.Intn(2) == 0 {
		vessel := newNode()
		member := nodes[rng.Intn(len(nodes))]
		sequence := &Sequence{Vessel: vessel, Nodes: []*Node{member, newNode()}, Graph: g}
		g.Sequences[vessel] = sequence
	}
	if rng.Intn(2) == 0 {
		leaf := newNode()
		child := &Tree{Node: leaf, SentinelEdge: NewEdge(nodes[0], leaf)}
		root := &Tree{Node: nodes[0], Children: []*Tree{child}}
		child.Parent = root
		g.Trees[nil] = append(g.Trees[nil], root)
		g.NodeToTree = map[*Node]*Tree{leaf: child}
	}
	if rng.Intn(2) == 0 {
		g.Hubs[nodes[0]] = []*Node{nodes[len(nodes)-1], newNode()}
	}
	if rng.Intn(2) == 0 {
		g.CommonUncleSiblings = map[*Node]Nodes{nodes[1]: {nodes[0], newNode()}}
	}
	if rng.Intn(2) == 0 {
		g.Directions = map[*Node]geo.Orientation{nodes[0]: geo.Left, newNode(): geo.Right}
	}
	if rng.Intn(2) == 0 {
		herd := &HerdAssignment{oppositeSidePaired: map[*Node]struct{}{newNode(): {}}, sameSidePaired: map[*Node]struct{}{nodes[0]: {}}}
		nodes[1].HerdAssignment = herd
	}
	if rng.Intn(2) == 0 {
		hierarchy := &Hierarchy{level: map[*Node]int{newNode(): 1, nodes[0]: 0}}
		nodes[0].Hierarchy = hierarchy
	}
	if rng.Intn(3) == 0 {
		nodes[0].AddNear(newNode())
	}
	return g, nodes
}

func TestChargeTopologySnapshotMatchesCapture(t *testing.T) {
	rng := rand.New(rand.NewSource(0x7A1A3C))
	for iteration := 0; iteration < 200; iteration++ {
		g, nodes := chargeSnapshotFixture(rng)
		probe := nodes[rng.Intn(len(nodes))]
		if rng.Intn(4) == 0 {
			probe = NewNode(9999, 1, 1)
		}
		// The full capture's units with an unlimited guard bound the limits
		// worth trying: every failure point and success.
		full, err := limits.NewWorkGuard(context.Background(), "PlaceTrees", 1<<40)
		if err != nil {
			t.Fatal(err)
		}
		state := NewGraphStateSnapshot(GraphStateSnapshotOptions{CaptureTopology: true, CaptureEdgeRoutes: true})
		if err := state.UpdateWithWorkGuard(g, full); err != nil {
			t.Fatal(err)
		}
		total := full.Used()
		for limit := int64(0); limit <= total+1; limit++ {
			want, _ := limits.NewWorkGuard(context.Background(), "PlaceTrees", limit)
			got, _ := limits.NewWorkGuard(context.Background(), "PlaceTrees", limit)
			state := NewGraphStateSnapshot(GraphStateSnapshotOptions{CaptureTopology: true, CaptureEdgeRoutes: true})
			wantErr := state.UpdateWithWorkGuard(g, want)
			tracked, gotErr := ChargeTopologySnapshot(g, got, probe)
			if (wantErr == nil) != (gotErr == nil) || (wantErr != nil && wantErr.Error() != gotErr.Error()) {
				t.Fatalf("iteration %d limit %d: error %v, want %v", iteration, limit, gotErr, wantErr)
			}
			if got.Used() != want.Used() {
				t.Fatalf("iteration %d limit %d: units %d, want %d", iteration, limit, got.Used(), want.Used())
			}
			if wantErr == nil && tracked != state.TracksNode(probe) {
				t.Fatalf("iteration %d: tracked %v, want %v", iteration, tracked, state.TracksNode(probe))
			}
		}
	}
}
