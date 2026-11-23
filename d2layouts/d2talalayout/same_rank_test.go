package d2talalayout

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
)

func TestLayoutSameRank(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		for _, example := range []string{"compound", "pipelines", "disconnected", "siblings"} {
			t.Run(example+"/"+direction, func(t *testing.T) {
				g := d2graph.NewGraph()
				g.Root.Direction.Value = direction
				newNode := func(parent *d2graph.Object, name string, ranked bool) *d2graph.Object {
					n := appendD2Object(parent, name)
					n.Width = float64(55 + 3*len(name))
					n.Height = float64(31 + 2*len(name))
					if ranked {
						n.Rank = &d2graph.Scalar{Value: "stage"}
					}
					return n
				}
				connect := func(from, to *d2graph.Object) {
					g.Edges = append(g.Edges, &d2graph.Edge{Src: from, Dst: to, DstArrow: true})
				}
				var expectedOrder [][2]*d2graph.Object
				switch example {
				case "siblings", "compound":
					container := g.Root
					if example == "compound" {
						container = newNode(g.Root, "Merchant", false)
					} else {
						g.Root.Shape.Value = d2target.ShapeHierarchy
					}
					checkout := newNode(container, "Checkout", false)
					commerce := newNode(container, "Commerce", true)
					channel := newNode(g.Root, "Channel", true)
					payments := newNode(g.Root, "Payments", true)
					connect(checkout, commerce)
					connect(commerce, channel)
					connect(channel, payments)
					expectedOrder = append(expectedOrder, [2]*d2graph.Object{checkout, commerce})
				case "pipelines", "disconnected":
					push := newNode(g.Root, "Push", false)
					lint := newNode(g.Root, "Lint", false)
					compile := newNode(g.Root, "Compile", false)
					test := newNode(g.Root, "Test", false)
					lintReport := newNode(g.Root, "LintReport", true)
					testReport := newNode(g.Root, "TestReport", true)
					connect(push, lint)
					connect(lint, lintReport)
					if example == "pipelines" {
						connect(push, compile)
					}
					connect(compile, test)
					connect(test, testReport)
					for _, e := range g.Edges {
						expectedOrder = append(expectedOrder, [2]*d2graph.Object{e.Src, e.Dst})
					}
				}
				if err := Layout(context.Background(), g, &Options{Seeds: []int64{1}}); err != nil {
					t.Fatal(err)
				}
				axis := func(n *d2graph.Object) float64 {
					if direction == "left" || direction == "right" {
						return n.Center().X
					}
					return n.Center().Y
				}
				group := g.SameRankGroups()[0]
				for _, n := range group[1:] {
					if math.Abs(axis(n)-axis(group[0])) > 1e-6 {
						t.Errorf("rank centers %s=%g, %s=%g", group[0].ID, axis(group[0]), n.ID, axis(n))
					}
				}
				for _, pair := range expectedOrder {
					if (axis(pair[1]) > axis(pair[0])) != (direction == "down" || direction == "right") || axis(pair[1]) == axis(pair[0]) {
						t.Errorf("unexpected flow from %s=%g to %s=%g", pair[0].ID, axis(pair[0]), pair[1].ID, axis(pair[1]))
					}
				}
				for _, n := range g.Objects {
					if p := n.Parent; p != g.Root {
						if n.TopLeft.X <= p.TopLeft.X || n.TopLeft.Y <= p.TopLeft.Y || n.TopLeft.X+n.Width >= p.TopLeft.X+p.Width || n.TopLeft.Y+n.Height >= p.TopLeft.Y+p.Height {
							t.Errorf("%s escapes %s", n.ID, p.ID)
						}
					}
				}
				for _, e := range g.Edges {
					if (example == "compound" || example == "siblings") && len(e.Route) != 2 {
						t.Errorf("straight path through three rank peers: %s -> %s has %d route points", e.Src.ID, e.Dst.ID, len(e.Route))
					}
					if len(e.Route) < 2 {
						t.Errorf("missing %s -> %s route", e.Src.ID, e.Dst.ID)
					}
					for i, p := range e.Route[1:] {
						prev := e.Route[i]
						if math.Abs(prev.X-p.X) > 1e-6 && math.Abs(prev.Y-p.Y) > 1e-6 {
							t.Errorf("diagonal %s -> %s: %v -> %v", e.Src.ID, e.Dst.ID, prev, p)
						}
						for _, n := range g.Objects {
							if len(n.ChildrenArray) > 0 || n == e.Src || n == e.Dst {
								continue
							}
							if math.Max(prev.X, p.X) > n.TopLeft.X && math.Min(prev.X, p.X) < n.TopLeft.X+n.Width && math.Max(prev.Y, p.Y) > n.TopLeft.Y && math.Min(prev.Y, p.Y) < n.TopLeft.Y+n.Height {
								t.Errorf("route %s -> %s crosses %s", e.Src.ID, e.Dst.ID, n.ID)
							}
						}
					}
				}
			})
		}
	}
}

// Multiple ranks crossing the same container require an elastic enclosure:
// translating Merchant as a single rectangle cannot generally satisfy both.
func TestSameRankMultipleGroupsAndFlatLabels(t *testing.T) {
	g := d2graph.NewGraph()
	g.Root.Direction.Value = "down"
	parent := appendD2Object(g.Root, "merchant")
	left1 := appendD2Object(parent, "authorize")
	left2 := appendD2Object(parent, "capture")
	right1 := appendD2Object(g.Root, "ledger")
	right2 := appendD2Object(g.Root, "receipt")
	middle := appendD2Object(g.Root, "settlement")
	for i, n := range g.Objects {
		n.Width, n.Height = 80+float64(i), 50+float64(i)
	}
	for _, n := range []*d2graph.Object{left1, right1} {
		n.Rank = &d2graph.Scalar{Value: "start"}
	}
	for _, n := range []*d2graph.Object{left2, right2} {
		n.Rank = &d2graph.Scalar{Value: "end"}
	}
	g.Edges = []*d2graph.Edge{
		{Src: left1, Dst: left2, DstArrow: true},
		{Src: right1, Dst: middle, DstArrow: true}, {Src: middle, Dst: right2, DstArrow: true},
		{Src: left1, Dst: right1, DstArrow: true}, {Src: left1, Dst: right1, DstArrow: true, Index: 1},
		{Src: right2, Dst: left2, DstArrow: true}, {Src: left2, Dst: left2, DstArrow: true},
	}
	for i, e := range g.Edges[3:5] {
		e.Label.Value = []string{"authorized", "validated"}[i]
		e.LabelDimensions = d2target.TextDimensions{Width: 80, Height: 18}
		e.Style.StrokeWidth = &d2graph.Scalar{Value: "3"}
	}
	if err := Layout(context.Background(), g, &Options{Seeds: []int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if left1.Center().Y != right1.Center().Y || left2.Center().Y != right2.Center().Y || !(right1.Center().Y < middle.Center().Y && middle.Center().Y < right2.Center().Y) {
		t.Fatal("rank groups or intervening settlement lost")
	}
	var labels []*geo.Box
	for _, e := range g.Edges[3:5] {
		if e.LabelPosition == nil {
			t.Fatal("flat edge label lost")
		}
		box := rerouteLabelBox(e)
		for _, n := range g.Objects {
			if n != parent && box.Overlaps(*n.Box) {
				t.Fatalf("%q label overlaps %s", e.Label.Value, n.ID)
			}
		}
		labels = append(labels, box)
	}
	if labels[0].Overlaps(*labels[1]) {
		t.Fatal("flat parallel-edge labels overlap")
	}
	if reflect.DeepEqual(g.Edges[3].Route, g.Edges[4].Route) {
		t.Fatal("parallel routes coincide")
	}
	if len(g.Edges[6].Route) < 4 {
		t.Fatal("self loop lost")
	}
}

func TestSameRankFixedCenters(t *testing.T) {
	for _, compatible := range []bool{false, true} {
		g := d2graph.NewGraph()
		a, b := appendD2Object(g.Root, "a"), appendD2Object(g.Root, "b")
		for i, n := range []*d2graph.Object{a, b} {
			n.Height = 20
			n.Width = 30
			n.Rank = &d2graph.Scalar{Value: "peers"}
			n.Top = &d2graph.Scalar{Value: "50"}
			n.Left = &d2graph.Scalar{Value: []string{"100", "300"}[i]}
		}
		if !compatible {
			b.Top.Value = "180"
		}
		err := Layout(context.Background(), g, &Options{Seeds: []int64{1}})
		if compatible {
			if err != nil || a.Center().Y != b.Center().Y || a.TopLeft.X != 100 || b.TopLeft.X != 300 || a.TopLeft.Y != 50 {
				t.Fatalf("compatible pins: %v, %v, %v", a.Box, b.Box, err)
			}
		} else if err == nil || !strings.Contains(err.Error(), "rank conflicts with the fixed top/left") {
			t.Fatalf("incompatible pins: %v", err)
		}
	}
}

func TestSameRankContainerEndpointAndNestedDirection(t *testing.T) {
	for _, localDirection := range []string{"up", "right"} {
		t.Run(localDirection, func(t *testing.T) {
			g := d2graph.NewGraph()
			g.Root.Direction.Value = "down"
			container := appendD2Object(g.Root, "processor")
			container.Direction.Value = localDirection
			step := appendD2Object(container, "step")
			done := appendD2Object(container, "done")
			peer := appendD2Object(g.Root, "peer")
			entry := appendD2Object(g.Root, "entry")
			exit := appendD2Object(g.Root, "exit")
			for _, n := range g.Objects {
				n.Width = 60
				n.Height = 40
			}
			for _, n := range []*d2graph.Object{done, peer} {
				n.Rank = &d2graph.Scalar{Value: "report"}
			}
			g.Edges = []*d2graph.Edge{
				{Src: step, Dst: done, DstArrow: true},
				{Src: entry, Dst: container, DstArrow: true}, {Src: container, Dst: exit, DstArrow: true},
				{Src: done, Dst: peer, DstArrow: true},
			}
			if err := Layout(context.Background(), g, &Options{Seeds: []int64{1}}); err != nil {
				t.Fatal(err)
			}
			if done.Center().Y != peer.Center().Y {
				t.Fatal("cross-container rank ignored")
			}
			if entry.Center().Y >= container.TopLeft.Y || exit.Center().Y <= container.TopLeft.Y+container.Height {
				t.Fatalf("external edge points against flow: entry %v, processor %v, exit %v", entry.Box, container.Box, exit.Box)
			}
			if localDirection == "up" && done.Center().Y >= step.Center().Y {
				t.Fatal("lost inner up direction")
			}
			if localDirection == "right" && done.Center().X <= step.Center().X {
				t.Fatal("lost inner right direction")
			}
		})
	}
}

func TestSameRankFeedbackAfterContraction(t *testing.T) {
	g := d2graph.NewGraph()
	a, b, c := appendD2Object(g.Root, "a"), appendD2Object(g.Root, "b"), appendD2Object(g.Root, "c")
	a.Rank, c.Rank = &d2graph.Scalar{Value: "peers"}, &d2graph.Scalar{Value: "peers"}
	g.Edges = []*d2graph.Edge{{Src: a, Dst: b, DstArrow: true}, {Src: b, Dst: c, DstArrow: true}, {Src: a, Dst: c, DstArrow: true}}
	if err := Layout(context.Background(), g, &Options{Seeds: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	if a.Center().Y != c.Center().Y || a.Center().Y == b.Center().Y {
		t.Fatalf("feedback after contraction has incorrect centers %v %v %v", a.Center(), b.Center(), c.Center())
	}
	for _, e := range g.Edges {
		if len(e.Route) < 2 {
			t.Fatal("missing feedback route")
		}
	}
}

func TestSameRankOpposingContainerEdges(t *testing.T) {
	g := d2graph.NewGraph()
	system := appendD2Object(g.Root, "system")
	inner := appendD2Object(system, "inner")
	component := appendD2Object(inner, "component")
	appendD2Object(inner, "helper")
	external := appendD2Object(g.Root, "external")
	other := appendD2Object(g.Root, "other")
	for _, n := range g.Objects {
		n.Width, n.Height = 80, 50
	}
	component.Rank, external.Rank = &d2graph.Scalar{Value: "stage"}, &d2graph.Scalar{Value: "stage"}
	g.Edges = []*d2graph.Edge{
		{Src: system, Dst: external, DstArrow: true}, {Src: external, Dst: system, DstArrow: true},
		{Src: component, Dst: external, DstArrow: true}, {Src: other, Dst: inner, DstArrow: true},
	}
	if err := Layout(context.Background(), g, &Options{Seeds: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	if component.Center().Y != external.Center().Y {
		t.Fatalf("rank centers differ %v %v", component.Center(), external.Center())
	}
	for _, e := range g.Edges {
		if len(e.Route) < 2 {
			t.Fatalf("no route for %s -> %s", e.Src.ID, e.Dst.ID)
		}
	}
}

func TestSameRankPlaybackTransitions(t *testing.T) {
	for _, direction := range []string{"down", "right", "up", "left"} {
		t.Run(direction, func(t *testing.T) {
			g := d2graph.NewGraph()
			g.Root.Direction.Value = direction
			g.Root.Shape.Value = d2target.ShapeHierarchy
			node := func(id string, w, h float64) *d2graph.Object {
				n := appendD2Object(g.Root, id)
				n.Width, n.Height = w, h
				return n
			}
			ready, playing := node("ready", 89, 66), node("playing", 123, 123)
			paused, finished := node("paused", 120, 120), node("finished", 104, 66)
			playing.Rank = &d2graph.Scalar{Value: "playback"}
			paused.Rank = &d2graph.Scalar{Value: "playback"}
			transition := func(from, to *d2graph.Object, name string, width int) *d2graph.Edge {
				e := &d2graph.Edge{Src: from, Dst: to, DstArrow: true}
				e.Label.Value = name
				e.LabelDimensions = d2target.TextDimensions{Width: width, Height: 21}
				g.Edges = append(g.Edges, e)
				return e
			}
			play := transition(ready, playing, "play", 31)
			pause := transition(playing, paused, "pause", 42)
			resume := transition(paused, playing, "resume", 49)
			finish := transition(playing, finished, "finish", 35)
			if err := Layout(t.Context(), g, &Options{Seeds: []int64{1}}); err != nil {
				t.Fatal(err)
			}
			for _, e := range []*d2graph.Edge{play, pause, resume, finish} {
				if len(e.Route) != 2 {
					t.Fatalf("%s transition should be straight, got %v", e.Label.Value, e.Route)
				}
			}
			if rerouteLabelBox(pause).Overlaps(*rerouteLabelBox(resume)) {
				t.Error("pause and resume labels overlap")
			}
			vertical := direction == "down" || direction == "up"
			if (vertical && pause.Route[0].Y == resume.Route[0].Y) || (!vertical && pause.Route[0].X == resume.Route[0].X) {
				t.Error("opposing transitions share a track")
			}
			// These two separate tracks can fit both labels in the same interval.
			gap := math.Abs(playing.Center().X-paused.Center().X) - (playing.Width+paused.Width)/2
			if !vertical {
				gap = math.Abs(playing.Center().Y-paused.Center().Y) - (playing.Height+paused.Height)/2
			}
			if gap > 110 {
				t.Errorf("reserved %g between peers for separate pause and resume tracks", gap)
			}
		})
	}
}

func TestTreeForkUsesAvailableRankGap(t *testing.T) {
	for _, direction := range []string{"down", "up", "right", "left"} {
		for _, ranked := range []bool{false, true} {
			mode := "automatic"
			if ranked {
				mode = "ranked"
			}
			t.Run(direction+"/"+mode, func(t *testing.T) {
				g := d2graph.NewGraph()
				g.Root.Direction.Value = direction
				g.Root.Shape.Value = d2target.ShapeHierarchy
				newNode := func(name string, width float64) *d2graph.Object {
					n := appendD2Object(g.Root, name)
					n.Width, n.Height = width, 66
					return n
				}
				push, lint, build := newNode("push", 79), newNode("lint", 72), newNode("build", 81)
				test := newNode("test", 76)
				lintReport, testReport := newNode("lintReport", 121), newNode("testReport", 124)
				if ranked {
					lintReport.Rank = &d2graph.Scalar{Value: "reports"}
					testReport.Rank = &d2graph.Scalar{Value: "reports"}
				}
				fork := []*d2graph.Edge{{Src: push, Dst: lint, DstArrow: true}, {Src: push, Dst: build, DstArrow: true}}
				g.Edges = []*d2graph.Edge{
					fork[0], {Src: lint, Dst: lintReport, DstArrow: true},
					fork[1], {Src: build, Dst: test, DstArrow: true}, {Src: test, Dst: testReport, DstArrow: true},
				}
				if err := DefaultLayout(context.Background(), g); err != nil {
					t.Fatal(err)
				}
				axis := func(p *geo.Point) float64 {
					switch direction {
					case "left":
						return -p.X
					case "right":
						return p.X
					case "up":
						return -p.Y
					default:
						return p.Y
					}
				}
				nearest := math.Inf(1)
				for _, edge := range fork {
					r := edge.Route
					if len(r) < 2 {
						t.Fatalf("missing fork route to %s", edge.Dst.ID)
					}
					nearest = min(nearest, axis(r[len(r)-1])-axis(r[0]))
				}
				if nearest <= 0 {
					t.Fatalf("children must be ahead of the parent, got %g px gap", nearest)
				}
				bends := 0
				for _, edge := range fork {
					r := edge.Route
					if !r[0].Equals(fork[0].Route[0]) {
						t.Fatal("siblings must leave the same centered tree port")
					}
					if len(r) == 2 {
						continue
					}
					bends++
					if len(r) != 4 || axis(r[1]) != axis(r[2]) {
						t.Fatalf("unexpected branch route to %s: %v", edge.Dst.ID, r)
					}
					if stem := axis(r[1]) - axis(r[0]); math.Abs(stem-nearest/2) > 0.5 {
						t.Errorf("branch to %s turns after %g px in the %g px gap", edge.Dst.ID, stem, nearest)
					}
				}
				wantBent := 1 // The unranked tree has a straight continuation to one child.
				if ranked {
					wantBent = 2
				}
				if bends != wantBent {
					t.Errorf("got %d bent branches, want %d", bends, wantBent)
				}
			})
		}
	}
}
