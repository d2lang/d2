package d2talalayout

import (
	"strconv"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
)

func TestResizeSwitchKeepsAVisibleAnchor(t *testing.T) {
	makeBoard := func(end bool) *d2graph.Graph {
		g := d2graph.NewGraph()
		server := appendD2Object(g.Root, "server")
		hooks := appendD2Object(g.Root, "hooks")
		worker := appendD2Object(hooks, "worker")
		db := appendD2Object(g.Root, "db")
		shard := appendD2Object(server, "shard")
		server.Box = geo.NewBox(geo.NewPoint(0, 200), 100, 100)
		hooks.Box = geo.NewBox(geo.NewPoint(0, 0), 130, 100)
		if end {
			server.TopLeft.Y, hooks.TopLeft.Y = 0, 200
			hooks.Width = 200
		}
		worker.Box = geo.NewBox(geo.NewPoint(10, hooks.TopLeft.Y+20), hooks.Width-20, 30)
		worker.WidthAttr = &d2graph.Scalar{Value: strconv.Itoa(int(worker.Width))}
		shard.Box = geo.NewBox(geo.NewPoint(10, server.TopLeft.Y+10), 30, 30)
		shard.Style.Opacity = &d2graph.Scalar{Value: "0"}
		server.Style.Opacity = &d2graph.Scalar{Value: "0.6"}
		db.Box = geo.NewBox(geo.NewPoint(400, 100), 60, 60)
		g.Edges = []*d2graph.Edge{{Src: server, Dst: db}, {Src: worker, Dst: db}}
		return g
	}
	start, end := makeBoard(false), makeBoard(true)
	first, last := resizeObjectIndex(start), resizeObjectIndex(end)
	staged := resizeCrossings(start, last, 20)
	if _, ok := staged["server"]; !ok || len(staged) != 1 {
		t.Fatalf("got switching subtrees %v; want only server", staged)
	}
	previousServerY := 200.
	previousAlpha := 0.6
	for i := 0; i < 20; i++ {
		f := makeBoard(false)
		n := f.Root.Children["server"]
		authored := n.Style.Opacity
		tick := float64(i) / 20
		boxes := resizeSamplePlan(f, first, last, staged, tick, i, 20)
		alpha, _ := strconv.ParseFloat(n.Style.Opacity.Value, 64)
		if alpha < 0 || alpha > 0.6 || authored.Value != "0.6" {
			t.Fatal("mutated or amplified authored opacity")
		}
		if boxes[n].TopLeft.Y != previousServerY && alpha != 0 && previousAlpha != 0 {
			t.Fatal("server switched positions while visible")
		}
		if f.Root.Children["hooks"].Style.Opacity != nil || f.Edges[1].Style.Opacity != nil {
			t.Fatal("the visible anchor or its connection faded")
		}
		if v := n.Children["shard"].Style.Opacity.Value; v != "0" {
			t.Fatalf("revealed an authored hidden object: opacity %s", v)
		}
		if value := f.Edges[0].Style.Opacity; value != nil {
			e, _ := strconv.ParseFloat(value.Value, 64)
			if e == 0 && alpha != 0 || e > 0 && alpha == 0 {
				t.Fatal("incident connection and subtree disagree on visibility")
			}
		}
		previousServerY, previousAlpha = boxes[n].TopLeft.Y, alpha
	}
}

func TestResizeTrackRollbackOnLateRouteFailure(t *testing.T) {
	scene := func(x, width int) *d2graph.Graph {
		g, e := newD2TransactionGraph(false)
		e.Src.Box = geo.NewBox(geo.NewPoint(float64(x), 20), float64(width), 60)
		e.Src.WidthAttr = &d2graph.Scalar{Value: strconv.Itoa(width)}
		e.Dst.TopLeft.X = float64(x) + 300
		e.Route = []*geo.Point{geo.NewPoint(float64(x+width), 50), geo.NewPoint(e.Dst.TopLeft.X, 50)}
		return g
	}
	start, end := scene(100, 100), scene(400, 180)
	first, second, bad := scene(15, 100), scene(20, 120), scene(0, 29900)
	end.TweenFrames = []*d2graph.Graph{first, second, bad}
	// The ordinary sample is within bounds. Its endpoint-guided placement
	// would extend past the coordinate limit only at the third sample.
	if !validBoardGeometry(bad, true) {
		t.Fatal("expected a valid input frame")
	}
	snapshots := map[*d2graph.Graph]d2GraphSnapshot{}
	for _, g := range []*d2graph.Graph{start, end, first, second, bad} {
		snapshots[g] = snapshotD2Graph(t, g)
	}
	if ok, err := TweenResize(t.Context(), start, end); err != nil || ok {
		t.Fatalf("late out-of-bounds layout returned %t, %v", ok, err)
	}
	for g, snapshot := range snapshots {
		snapshot.assertUnchanged(t, g)
	}
	for _, frames := range [][]*d2graph.Graph{{first, start}, {first, end}, {first, first}} {
		end.TweenFrames = frames
		if ok, err := TweenResize(t.Context(), start, end); err != nil || ok {
			t.Fatalf("aliased track returned %t, %v", ok, err)
		}
	}
}
