package d2isometricimg

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2target"
)

func connectorTestDeck(y float64, path ...string) explodedDeck {
	return explodedDeck{path: path, width: 6, depth: 3, thickness: .1, surfaceY: y,
		offset: nv(0, y, 0), scene: &d2isometric.Scene{}}
}

func paintTestDeckWires(t *testing.T, deck *explodedDeck) {
	t.Helper()
	deck.wires = make([]stackWire, len(deck.scene.Edges))
	_, err := newNativeSceneWithOptions(context.Background(), deck.scene, 240, 240, nil, nil, nativeSceneOptions{deferRaster: true, stackWires: deck.wires})
	if err != nil {
		t.Fatal(err)
	}
}

// Slice each two-facet beam at a world height. The midpoint of its
// projected width is the underlying registered X/Z transfer axis.
func connectorTestCuts(ts []Triangle, height float64) []nativeLaneInterval {
	var cuts []nativeLaneInterval
	axis := nativeCameraAxes().right
	field := nativePaint(stackLightPaint, "")
	for i := 0; i+3 < len(ts); i++ {
		m := ts[i].Material
		if m.Color.R != field.R || m.Color.G != field.G || ts[i+1].Material != m {
			continue
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, tri := range ts[i : i+4] {
			for j, vertex := range tri.V {
				a, c := vertex.Position, tri.V[(j+1)%3].Position
				if (a.Y < height) == (c.Y < height) {
					continue
				}
				q := nlerp(a, c, (height-a.Y)/(c.Y-a.Y))
				x := ndot(axis, q)
				lo, hi = min(lo, x), max(hi, x)
			}
		}
		if hi >= lo {
			cuts = append(cuts, nativeLaneInterval{lo, hi})
		}
		i += 3
	}
	return cuts
}
func connectorTestColumns(ts []Triangle, height float64) []float64 {
	var columns []float64
	for _, cut := range connectorTestCuts(ts, height) {
		columns = append(columns, (cut.lo+cut.hi)/2)
	}
	return columns
}

func TestStackConnectorsResolveOnlyVisibleAuthoredBoardLinks(t *testing.T) {
	top := connectorTestDeck(6, "root", "layers", "studio")
	bottom := connectorTestDeck(0, "root", "layers", "studio", "layers", "press.room")
	top.scene.Nodes = []d2isometric.Node{{ID: "x", Opacity: 1}}
	bottom.scene.Nodes = []d2isometric.Node{{ID: "x", Opacity: 1}}
	const target = `root.layers."studio".layers."press.room"`
	for _, invalid := range []string{"", "https://example.com/root.layers.studio", "./studio.svg", "root.layers.studio", "root.layers.press.room", "root.layers.edition", target + "#x", target + ".x"} {
		top.scene.Nodes[0].Link = invalid
		b := &meshBuilder{ctx: context.Background()}
		if err := b.addStackConnectors([]explodedDeck{top, bottom}); err != nil || len(b.triangles) != 0 {
			t.Fatalf("%q produced %d triangles: %v", invalid, len(b.triangles), err)
		}
	}
	top.scene.Nodes[0].Link = target
	top.scene.Nodes = append(top.scene.Nodes, d2isometric.Node{ID: "hidden", Link: target})
	top.scene.Edges = []d2isometric.Edge{{Link: target, Opacity: 1, StrokeWidth: 2, Points: []Vec{nv(-1, 0, 0), nv(1, 0, 0)}}}
	paintTestDeckWires(t, &top)
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{top, bottom}); err != nil {
		t.Fatal(err)
	}
	columns := connectorTestColumns(b.triangles, 3)
	if len(columns) != 2 || math.Abs(columns[0]-columns[1]) < .26 {
		t.Fatalf("shape and edge board links need two distinct shafts: %v", columns)
	}
	for _, tri := range b.triangles {
		for _, v := range tri.V {
			if math.Abs(v.Position.X) > top.width/2 || math.Abs(v.Position.Z) > top.depth/2 {
				t.Fatalf("shaft geometry outside common footprint: %+v", v.Position)
			}
		}
	}
}

func TestStackConnectorClearsProjectedNodesAndFloorTitle(t *testing.T) {
	upper := connectorTestDeck(6, "root", "layers", "A")
	lower := connectorTestDeck(0, "root", "layers", "B")
	upper.scene.Nodes = []d2isometric.Node{{Link: "root.layers.B", Opacity: 1}}
	lower.scene.Nodes = []d2isometric.Node{{Position: nv(-.4, 0, 0), Size: nv(1.6, .4, .9), Opacity: 1}}
	lower.title = labelSurface{center: nv(1, 0, -1), width: 1.1, depth: .4}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{upper, lower}); err != nil {
		t.Fatal(err)
	}
	columns := connectorTestColumns(b.triangles, 3)
	if len(columns) != 1 {
		t.Fatalf("expected one shaft, got %v", columns)
	}
	shaftX := columns[0]
	for _, surface := range []labelSurface{lower.title, {center: lower.scene.Nodes[0].Position, width: 1.6, depth: .9}} {
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, p := range surfaceCorners(surface) {
			x := ndot(p, nativeCameraAxes().right)
			lo, hi = min(lo, x), max(hi, x)
		}
		if shaftX >= lo-.09 && shaftX <= hi+.09 {
			t.Fatalf("vertical shaft at %v intersects [%v,%v]", shaftX, lo, hi)
		}
	}
}

func TestStackConnectorUsesSideGutterToClearAnInterveningRoute(t *testing.T) {
	upper := connectorTestDeck(12, "root", "layers", "A")
	middle := connectorTestDeck(6, "root", "layers", "B")
	lower := connectorTestDeck(0, "root", "layers", "C")
	upper.scene.Nodes = []d2isometric.Node{{Link: "root.layers.C", Opacity: 1, Position: nv(1.8, 0, 0)}}
	middle.scene.Nodes = []d2isometric.Node{{Opacity: 1, Position: nv(1.8, 0, 0), Size: nv(1.5, .4, .9)}}
	middle.scene.Edges = []d2isometric.Edge{{Opacity: 1, StrokeWidth: 2, Points: []Vec{nv(-2.4, 0, 0), nv(1.1, 0, 0)}}}
	paintTestDeckWires(t, &middle)
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{upper, middle, lower}); err != nil {
		t.Fatal(err)
	}
	top, bottom := connectorTestColumns(b.triangles, 9), connectorTestColumns(b.triangles, 3)
	if len(top) != 1 || len(bottom) != 1 || math.Abs(top[0]-bottom[0]) > 1e-8 {
		t.Fatalf("shaft between three decks must occupy one column: %v, %v", top, bottom)
	}
	right := nativeCameraAxes().right
	outside := ndot(nv(1.8+1.5/2, 0, -.9/2), right)
	if top[0] < outside+.09 {
		t.Fatalf("shaft at %v should clear the route and land beside the node at %v", top[0], outside)
	}
}

func TestStackTransferEmitterReceiverAndTransitCollar(t *testing.T) {
	top := connectorTestDeck(9, "root")
	middle := connectorTestDeck(4.5, "root", "layers", "middle")
	low := connectorTestDeck(0, "root", "layers", "floor")
	port := nv(.2, 0, top.depth/2-.18)
	for _, test := range []struct {
		name     string
		from, to explodedDeck
	}{{"descending", top, low}, {"ascending", low, top}} {
		t.Run(test.name, func(t *testing.T) {
			b := &meshBuilder{ctx: context.Background()}
			b.drawStackConnector(test.from, test.to, stackConnector{source: nv(0, test.from.surfaceY+.012, 0), opacity: .6}, port, middle)
			// Identify just the coplanar endpoint hardware; the source
			// leader and vertical beam both have vertices off that plane.
			extent := func(floor explodedDeck) (inner, outer float64) {
				inner = math.Inf(1)
				for _, tri := range b.triangles {
					a, c, d := tri.V[0].Position, tri.V[1].Position, tri.V[2].Position
					if math.Abs(a.Y-floor.surfaceY-.008) > 1e-8 || a.Y != c.Y || c.Y != d.Y {
						continue
					}
					for _, v := range tri.V {
						r := math.Hypot(v.Position.X-port.X, v.Position.Z-port.Z)
						inner, outer = min(inner, r), max(outer, r)
					}
				}
				return inner, outer
			}
			fromInner, fromOuter := extent(test.from)
			throughInner, throughOuter := extent(middle)
			toInner, toOuter := extent(test.to)
			if fromInner > 1e-8 || toInner > 1e-8 || throughInner <= .05 ||
				!(fromOuter < throughOuter && throughOuter < toOuter && toOuter-fromOuter > .06) {
				t.Fatalf("emitting lens [%g,%g], hollow collar [%g,%g], receiving iris [%g,%g]", fromInner, fromOuter, throughInner, throughOuter, toInner, toOuter)
			}
			if len(connectorTestColumns(b.triangles, middle.surfaceY-middle.thickness/2)) != 0 || len(connectorTestColumns(b.triangles, 2)) != 1 || len(connectorTestColumns(b.triangles, 7)) != 1 {
				t.Fatal("the beam must be present on each side of the intermediate slab and absent inside it")
			}
			a, z := test.from.surfaceY, test.to.surfaceY
			nearSource := connectorTestCuts(b.triangles, a+.12*(z-a))
			nearReceiver := connectorTestCuts(b.triangles, a+.88*(z-a))
			if len(nearSource) != 1 || len(nearReceiver) != 1 || (nearReceiver[0].hi-nearReceiver[0].lo) < 2*(nearSource[0].hi-nearSource[0].lo) {
				t.Fatalf("beam must visibly diverge towards receiver: %v -> %v", nearSource, nearReceiver)
			}
			for _, tri := range b.triangles {
				if tri.Material.Color.A > 153 {
					t.Fatalf("optical material exceeded source opacity: %d", tri.Material.Color.A)
				}
			}
		})
	}
}

func TestExplodedPageCaptionlessEdgeOnSelectedRoot(t *testing.T) {
	root, child := captureDiagram(), captureDiagram()
	root.Name, child.Name = "upper", "lower"
	root.Layers = []*d2target.Diagram{child}
	edge := &root.Connections[0]
	edge.Label, edge.Link = "", "root.layers.upper.layers.lower"
	opts := &Options{Exploded: true, BoardPath: []string{"layers", "upper"}, Width: 160, Height: 100}
	page, err := RenderPage(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.PNG) == 0 || len(page.Links) != 0 {
		t.Fatalf("expected the rendered stack with no separate caption region, got %d bytes and %d links", len(page.PNG), len(page.Links))
	}
	for _, tc := range []struct {
		link, tooltip string
		exploded      bool
	}{
		{edge.Link, "", false},
		{"root.layers.upper", "", true},
		{"root.layers.omitted", "", true},
		{"https://example.org/", "", true},
		{"", "edge details", true},
	} {
		opts.Exploded = tc.exploded
		edge.Link, edge.Tooltip = tc.link, tc.tooltip
		if _, err := RenderPage(context.Background(), root, opts); err == nil || !strings.Contains(err.Error(), "needs a label or icon") {
			t.Errorf("exploded=%t link %q tooltip %q: expected caption validation, got %v", tc.exploded, tc.link, tc.tooltip, err)
		}
	}
}

func TestStackLinksStartOnTheirPaintedParallelWires(t *testing.T) {
	top := connectorTestDeck(6, "root", "layers", "A")
	bottom := connectorTestDeck(0, "root", "layers", "B")
	axis := nativeCameraAxes().right
	// Project the common compiled route onto a single screen column, so
	// the spread lane must also reserve obstacle space outside that column.
	v := nv(-axis.Z*1.8, 0, axis.X*1.8)
	source := nv(0, .08, 0)
	for _, id := range []string{"a", "b", "invisible"} {
		e := bridgeTestEdge(id, nsub(source, v), nadd(source, v))
		e.Link = "root.layers.B"
		top.scene.Edges = append(top.scene.Edges, e)
	}
	top.scene.Edges[2].StrokeExplicit = true
	top.scene.Edges[2].Stroke = "transparent"
	top.scene.Nodes = []d2isometric.Node{{ID: "spacer", Link: "root.layers.B", Fill: "transparent", Opacity: 1}}
	top.scene.PixelScale = .01
	paintTestDeckWires(t, &top)
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{top, bottom}); err != nil {
		t.Fatal(err)
	}

	// On each leader ribbon the first and third vertices straddle its
	// starting point. Each visible board link uses its own ink material.
	var leads []Vec
	seen := map[*Material]bool{}
	for _, tri := range b.triangles {
		if !seen[tri.Material] && tri.Material.Color == nativePaint(stackTracePaint, "") {
			leads = append(leads, nlerp(tri.V[0].Position, tri.V[2].Position, .5))
			seen[tri.Material] = true
		}
	}
	if len(leads) != 2 {
		t.Fatalf("painted leads = %d, want the two visible wires only", len(leads))
	}
	_, paths, err := nativeEdgeRoutes(context.Background(), top.scene.Edges, top.scene.Nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		p := pathPoint(paths[i], routeLengths(paths[i]), .5)
		if i == 1 && nlen(nsub(p, source)) < .09 {
			t.Fatalf("test edges did not form separate lanes: %v", p)
		}
		want := nadd(top.offset, p)
		if min(nlen(nsub(leads[0], want)), nlen(nsub(leads[1], want))) > .025 {
			t.Fatalf("no lead on rendered wire %d at %v; starts %v", i, want, leads)
		}
	}
	col := func(v Vec) float64 { return ndot(v, axis) }
	obstacles := appendStackObstacles(nil, top, 0, col)
	if len(obstacles) != 2 {
		t.Fatalf("obstacles for two painted wires = %d", len(obstacles))
	}
	for i := 0; i < 2; i++ {
		mid := nadd(top.offset, pathPoint(paths[i], routeLengths(paths[i]), .5))
		if x := col(mid); x < obstacles[i].lo+.12 || x > obstacles[i].hi-.12 {
			t.Fatalf("painted lane at %v is missing from obstacle %v", x, obstacles[i])
		}
	}
}

func TestStackObstaclesRetainPlacedEdgeCaption(t *testing.T) {
	deck := connectorTestDeck(0, "root")
	e := bridgeTestEdge("a", nv(-2, .08, 0), nv(2, .08, 0))
	e.Label = "a route caption"
	e.Metadata.Original.Label = e.Label
	e.Metadata.Original.FontSize = 16
	e.Metadata.Original.LabelWidth = 110
	e.Metadata.Original.LabelHeight = 22
	deck.scene.Edges = []d2isometric.Edge{e}
	deck.scene.PixelScale = .01
	paintTestDeckWires(t, &deck)
	if len(deck.wires[0].captions) != 1 {
		t.Fatal("missing placed edge caption")
	}
	col := func(v Vec) float64 { return ndot(v, nativeCameraAxes().right) }
	obstacles := appendStackObstacles(nil, deck, 0, col)
	if len(obstacles) != 2 {
		t.Fatalf("expected separate caption and visible route obstacles, got %v", obstacles)
	}
	for _, corner := range surfaceCorners(deck.wires[0].captions[0]) {
		x := col(nadd(corner, deck.offset))
		if x <= obstacles[0].lo || x >= obstacles[0].hi {
			t.Fatalf("caption corner %v outside %v", x, obstacles[0])
		}
	}
}

func TestStackWidePlateEndpointClearsUpperTitle(t *testing.T) {
	upper := connectorTestDeck(8, "root")
	lower := connectorTestDeck(0, "root", "layers", "lower")
	upper.width, lower.width = 20, 20
	u := stackOpticalScale(upper.width)
	front := upper.depth/2 - .18*u
	upper.scene.Nodes = []d2isometric.Node{{ID: "source", Link: "root.layers.lower", Opacity: 1, Position: nv(0, .2, front-.3), Size: nv(.2, .2, .2)}}
	upper.title = labelSurface{center: nv(0, upper.surfaceY, front), width: .7, depth: .35}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{upper, lower}); err != nil {
		t.Fatal(err)
	}
	columns := connectorTestColumns(b.triangles, 4)
	if len(columns) != 1 {
		t.Fatal(columns)
	}
	right := nativeCameraAxes().right
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range surfaceCorners(upper.title) {
		x := ndot(p, right)
		lo, hi = min(lo, x), max(hi, x)
	}
	if x := columns[0]; x > lo-stackIrisRadius*u && x < hi+stackIrisRadius*u {
		t.Fatalf("transfer column %g leaves insufficient aperture clearance at upper title [%g,%g]", x, lo, hi)
	}
}

func TestStackTransferBudgetBeforeGeometry(t *testing.T) {
	links := make([]stackConnector, MaxTimelineBoards*2)
	for i := range links {
		links[i] = stackConnector{from: 0, to: MaxTimelineBoards - 1, opacity: 1}
	}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.reserveStackTransfers(links); err == nil || cap(b.triangles) != 0 {
		t.Fatalf("oversized multi-floor transfers admitted or allocated triangles: %v, %d", err, cap(b.triangles))
	}
}
