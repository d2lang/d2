package packing

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

// orderingGraph builds random disconnected components, some in one or two
// containers, with outside labels (margins), icons, fixed nodes, overlapping
// starting positions, edges with minimum sizes and, sometimes, routes.
func orderingGraph(seed int64) (*layoutgraph.Graph, []*layoutgraph.Node, *layoutgraph.Node) {
	r := rand.New(rand.NewSource(seed))
	g := layoutgraph.NewGraph()
	var nodes []*layoutgraph.Node
	id := 0
	newNode := func(parent *layoutgraph.Node, x, y float64) *layoutgraph.Node {
		id++
		n := layoutgraph.NewNode(layoutgraph.EntityID(id), float64(20+10*r.Intn(10)), float64(20+10*r.Intn(10)))
		n.TopLeft = geo.NewPoint(x, y)
		g.AddNewNodeToContainer(parent, n)
		nodes = append(nodes, n)
		if r.Intn(4) == 0 {
			positions := []label.Position{label.OutsideTopCenter, label.OutsideLeftMiddle, label.OutsideBottomRight, label.InsideTopCenter, label.OutsideRightTop}
			n.Label = &layoutgraph.Label{Text: "l", Position: positions[r.Intn(len(positions))], Width: float64(10 + r.Intn(90)), Height: float64(10 + r.Intn(20))}
			n.UpdateSpacing()
		}
		if r.Intn(8) == 0 {
			n.Icon = &layoutgraph.Icon{Position: label.OutsideTopLeft}
		}
		return n
	}
	var containers []*layoutgraph.Node
	for c := r.Intn(3); c > 0; c-- {
		containers = append(containers, newNode(nil, 0, 0))
	}
	components := 2 + r.Intn(9)
	routed := r.Intn(3) == 0
	for c := 0; c < components; c++ {
		var parent *layoutgraph.Node
		if len(containers) > 0 && r.Intn(3) != 0 {
			parent = containers[r.Intn(len(containers))]
		}
		bx := float64(40 * r.Intn(40))
		by := float64(40 * r.Intn(30))
		size := 1 + r.Intn(5)
		var comp []*layoutgraph.Node
		for i := 0; i < size; i++ {
			comp = append(comp, newNode(parent, bx+float64(40*r.Intn(6)), by+float64(40*r.Intn(4))))
		}
		for i := 1; i < len(comp); i++ {
			e := g.Connect(comp[i-1], comp[i])
			if r.Intn(4) == 0 {
				e.MinWidth = 20 + r.Intn(60)
				e.MinHeight = 20 + r.Intn(60)
			}
			if routed {
				a, b := comp[i-1].Center(), comp[i].Center()
				e.Points = []*geo.Point{geo.NewPoint(a.X, a.Y), geo.NewPoint(b.X, a.Y), geo.NewPoint(b.X, b.Y)}
			}
		}
		if r.Intn(12) == 0 {
			comp[0].FixedTopLeft = geo.NewPoint(comp[0].TopLeft.X, comp[0].TopLeft.Y)
		}
	}
	for _, container := range containers {
		children := g.Containers[container]
		if len(children) == 0 {
			continue
		}
		minX, minY := math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, c := range children {
			minX = math.Min(minX, c.TopLeft.X)
			minY = math.Min(minY, c.TopLeft.Y)
			maxX = math.Max(maxX, c.TopLeft.X+c.Width)
			maxY = math.Max(maxY, c.TopLeft.Y+c.Height)
		}
		container.TopLeft = geo.NewPoint(minX-40, minY-40)
		container.Width = maxX - minX + 80
		container.Height = maxY - minY + 80
	}
	var root *layoutgraph.Node
	if len(containers) == 1 && len(g.Containers[nil]) == 1 && r.Intn(2) == 0 {
		root = containers[0]
	}
	return g, nodes, root
}

func orderingFingerprint(g *layoutgraph.Graph, nodes []*layoutgraph.Node) string {
	out := ""
	for _, n := range nodes {
		if n.TopLeft == nil {
			out += "nil;"
			continue
		}
		out += fmt.Sprintf("%x,%x,%x,%x;", math.Float64bits(n.TopLeft.X), math.Float64bits(n.TopLeft.Y), math.Float64bits(n.Width), math.Float64bits(n.Height))
	}
	for _, e := range g.Edges {
		for _, p := range e.Points {
			out += fmt.Sprintf("e%x,%x;", math.Float64bits(p.X), math.Float64bits(p.Y))
		}
	}
	return out
}

// Pack with the score-ordered, prefiltered candidate validation leaves the
// same geometry (or the same error) as the plain loop over every candidate.
func TestPackCandidateOrderingMatchesPlainLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := func(seed int64, disabled bool) string {
		candidateOrderingDisabled = disabled
		defer func() { candidateOrderingDisabled = false }()
		g, nodes, root := orderingGraph(seed)
		err := Pack(ctx, g, root)
		return fmt.Sprintf("err=%v geo=%s", err, orderingFingerprint(g, nodes))
	}
	moved := 0
	for seed := int64(1); seed <= 1500; seed++ {
		g, nodes, _ := orderingGraph(seed)
		before := orderingFingerprint(g, nodes)
		ordered := run(seed, false)
		plain := run(seed, true)
		if ordered != plain {
			t.Fatalf("seed %d: ordered and plain BinPack differ\nordered: %s\nplain:   %s", seed, ordered, plain)
		}
		if plain != "err=<nil> geo="+before {
			moved++
		}
	}
	if moved < 300 {
		t.Fatalf("only %d of 1500 graphs changed by BinPack", moved)
	}
	t.Logf("%d of 1500 graphs changed by BinPack", moved)
}
