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

func TestStackLinksResolveOnlyPaintedSourcesAndSelectedLayers(t *testing.T) {
	top := connectorTestDeck(6, "root", "layers", "studio")
	bottom := connectorTestDeck(0, "root", "layers", "studio", "layers", "press.room")
	const target = `root.layers."studio".layers."press.room"`
	top.scene.Nodes = []d2isometric.Node{{ID: "x", Opacity: 1}}
	b := &meshBuilder{ctx: context.Background()}
	for _, invalid := range []string{"", "https://d2lang.com/root.layers.studio", "./studio.svg", "root.layers.studio", "root.layers.press.room", target + "#x", target + ".x"} {
		top.scene.Nodes[0].Link = invalid
		links, err := b.stackLinks([]explodedDeck{top, bottom})
		if err != nil || len(links) != 0 {
			t.Fatalf("%s produced %v: %v", invalid, links, err)
		}
	}
	top.scene.Nodes[0].Link = target
	top.scene.Nodes = append(top.scene.Nodes, d2isometric.Node{Link: target, Opacity: 0}, d2isometric.Node{Link: target, Opacity: 1, Fill: "transparent"})
	links, err := b.stackLinks([]explodedDeck{top, bottom})
	if err != nil || len(links) != 1 || links[0].to != 1 {
		t.Fatalf("wrong board link %v: %v", links, err)
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
	// Coincident source routes must emit from their separate painted lanes.
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
	links, err := b.stackLinks([]explodedDeck{top, bottom})
	if err != nil || len(links) != 2 {
		t.Fatalf("expected two visible wires, got %v: %v", links, err)
	}
	_, paths, err := nativeEdgeRoutes(context.Background(), top.scene.Edges, top.scene.Nodes, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range links {
		if l.contact.width <= 0 || l.contact.width > .75 || l.contact.center.X != l.source.X || l.contact.center.Z != l.source.Z || math.Abs(l.contact.center.Y-l.source.Y) > .003 {
			t.Fatalf("wire contact must follow the painted ink, got %+v", l)
		}
	}
	for i := 0; i < 2; i++ {
		p := pathPoint(paths[i], routeLengths(paths[i]), .5)
		if i == 1 && nlen(nsub(p, source)) < .09 {
			t.Fatalf("test edges did not form separate lanes: %v", p)
		}
		want := nadd(top.offset, p)
		if min(nlen(nsub(links[0].source, want)), nlen(nsub(links[1].source, want))) > .025 {
			t.Fatalf("no lead on rendered wire %d at %v; starts %v", i, want, links)
		}
	}
}

func TestStackConeGeometryAndAirGaps(t *testing.T) {
	decks := []explodedDeck{connectorTestDeck(16, "root"), connectorTestDeck(7.5, "root", "layers", "middle"), connectorTestDeck(0, "root", "layers", "bottom")}
	for _, pair := range [][2]int{{0, 2}, {2, 0}} {
		b := &meshBuilder{ctx: context.Background()}
		tex, err := b.stackConeTexture()
		if err != nil {
			t.Fatal(err)
		}
		from, to := decks[pair[0]], decks[pair[1]]
		source := nv(.82, from.surfaceY+.31, -.24)
		link := stackConnector{from: pair[0], to: pair[1], source: source, opacity: .55}
		b.drawStackCone(decks, link, tex)
		if b.err != nil || len(b.triangles) == 0 {
			t.Fatalf("missing cone mesh: %v", b.err)
		}
		emit, receive := stackConeHeights(decks, link)
		reveal := stackConeReveal(from, source, emit, receive)
		start := false
		before, after := false, false
		minX, maxX := math.Inf(1), math.Inf(-1)
		visibleApex := false
		for _, tri := range b.triangles {
			if tri.Material.Color.A != 140 || !tri.NoDepthWrite || !tri.Material.svgPointerPassthrough {
				t.Fatal("cone ignored link opacity or wrote opaque depth")
			}
			lo, hi := math.Inf(1), math.Inf(-1)
			for _, v := range tri.V {
				p := v.Position
				if math.Abs(p.X-source.X)+math.Abs(p.Z-source.Z)+math.Abs(p.Y-emit) < 1e-7 {
					start = true
				}
				if math.Abs(p.Y-(emit+(receive-emit)*reveal)) < 1e-8 {
					if math.Hypot(p.X-source.X, p.Z-source.Z) > .01*to.width {
						t.Fatalf("the visible apex diverged before clearing the source lip: %v", p)
					}
					visibleApex = true
				}
				if math.Abs(p.X) > from.width/2+1e-8 || math.Abs(p.Z) > from.depth/2+1e-8 {
					t.Fatalf("cone outside plate footprint %v", p)
				}
				if f := (p.Y - emit) / (receive - emit); f < -1e-8 || f > reveal+(1-reveal)*stackConeFadeEnd+1e-8 {
					t.Fatalf("cone reached receiving surface %v", p)
				}
				minX, maxX = min(minX, p.X), max(maxX, p.X)
				lo, hi = min(lo, p.Y), max(hi, p.Y)
			}
			if lo < decks[1].surfaceY-1e-8 && hi > decks[1].surfaceY-decks[1].thickness+1e-8 {
				t.Fatal("cone cuts through middle plate")
			}
			before = before || lo >= decks[1].surfaceY-1e-8
			after = after || hi <= decks[1].surfaceY-decks[1].thickness+1e-8
		}
		if !start || !visibleApex || !before || !after || maxX-minX < .62*to.width {
			t.Fatalf("narrow or detached cone: bounds %g..%g; source and spans %t %t %t", minX, maxX, start, before, after)
		}
	}
}

func TestStackConeSourceOnTransparentPlate(t *testing.T) {
	top, bottom := connectorTestDeck(14, "root"), connectorTestDeck(0, "root", "layers", "B")
	top.translucent = true
	// A sloped painted cap and a high external caption overlap this
	// shape's center. Only the cap gives the real emission height.
	node := d2isometric.Node{ID: "object", Size: nv(2, 8, 2), Link: "root.layers.B", Opacity: 1}
	top.scene.Nodes = []d2isometric.Node{node}
	top.tops = make(map[string]float64)
	b := &meshBuilder{ctx: context.Background(), options: nativeSceneOptions{stackTops: top.tops}}
	b.flat(nv(-1, .2, -1), nv(1, .6, -1), nv(0, .4, 1), nativeMaterial("white", 1, 0, 1), false)
	caption := nativeMaterial("white", 1, 0, 1)
	caption.outsideCaption = true
	b.flat(nv(-1, 3, -1), nv(1, 3, -1), nv(0, 3, 1), caption, false)
	b.captureStackTop(node, 0)
	decks := []explodedDeck{top, bottom}
	links, err := b.stackLinks(decks)
	if err != nil || len(links) != 1 {
		t.Fatalf("missing transfer: %+v, %v", links, err)
	}
	l := links[0]
	emit, receive := stackConeHeights(decks, l)
	bands := stackConeBands(decks, l)
	if math.Abs(emit-(top.surfaceY+.4)) > 1e-8 || stackConeReveal(top, l.source, emit, receive) != 0 || len(bands) == 0 || bands[0].start != 0 {
		t.Fatalf("light must emerge from the painted object on transparent stock: emit %g, bands %v", emit, bands)
	}
	b.triangles = nil
	tex, err := b.stackConeTexture()
	if err != nil {
		t.Fatal(err)
	}
	b.drawStackCone(decks, l, tex)
	full := b.triangles
	l.opacity = .45
	b.triangles = nil
	b.drawStackCone(decks, l, tex)
	if b.err != nil || len(full) < 3 || len(full) != len(b.triangles) {
		t.Fatalf("partial opacity changed the cone mesh: %v", b.err)
	}
	for i, tri := range full {
		partial := b.triangles[i]
		if tri.V != partial.V || !tri.Material.svgPointerPassthrough || !partial.Material.svgPointerPassthrough ||
			tri.Material.Color.A != 255 || partial.Material.Color.A != 115 {
			t.Fatalf("full and partial opacity must share passthrough geometry, facet %d", i)
		}
	}
}

func TestStackConeMouthAroundProjectedApex(t *testing.T) {
	// Short, wide boards can put the projected apex inside the target
	// ellipse. A diameter still gives every SVG facet an invertible UV.
	apex := nv(.12, .02, .05)
	ring := stackConeSection(apex, 3, 2, 0)
	for i := 1; i < len(ring); i++ {
		if ring[i].u <= ring[i-1].u || nlen(nsub(ring[i].end, ring[i-1].end)) == 0 {
			t.Fatalf("degenerate optical section: %+v", ring)
		}
	}
}

func TestStackConeFadesAheadOfTransparentPortraitReceiver(t *testing.T) {
	top, bottom := connectorTestDeck(9.23, "root"), connectorTestDeck(0, "root", "layers", "last")
	for _, d := range []*explodedDeck{&top, &bottom} {
		d.width, d.depth, d.thickness = 4.2, 5.86, .055
	}
	bottom.translucent = true
	l := stackConnector{from: 0, to: 1, source: nv(-1.165, 9.99, -2.067), opacity: 1}
	b := &meshBuilder{ctx: context.Background()}
	tex, err := b.stackConeTexture()
	if err != nil {
		t.Fatal(err)
	}
	b.drawStackCone([]explodedDeck{top, bottom}, l, tex)
	view := nativeCameraAxes().direction
	samples := 0
	for _, tri := range b.triangles {
		a, c, d := tri.V[0], tri.V[1], tri.V[2]
		for i := 0; i <= 16; i++ {
			for j := 0; i+j <= 16; j++ {
				w1, w2 := float64(i)/16, float64(j)/16
				w0 := 1 - w1 - w2
				p := nadd(nadd(nmul(a.Position, w0), nmul(c.Position, w1)), nmul(d.Position, w2))
				hit := nsub(p, nmul(view, p.Y/view.Y))
				if math.Abs(hit.X) > bottom.width/2-.03 || math.Abs(hit.Z) > bottom.depth/2-.03 {
					continue
				}
				samples++
				u, v := w0*a.U+w1*c.U+w2*d.U, w0*a.V+w1*c.V+w2*d.V
				alpha := tex.RGBAAt(int(math.Round(127*u)), int(math.Round(127*min(1., v)))).A
				if alpha > 2 {
					t.Fatalf("cone visibly paints the receiving plate at %v with alpha %d/255", hit, alpha)
				}
			}
		}
	}
	if samples == 0 {
		t.Fatal("fixture did not sample the projected receiving plate")
	}
}

func TestStackConeSoftTexture(t *testing.T) {
	b := &meshBuilder{ctx: context.Background()}
	tex, err := b.stackConeTexture()
	if err != nil {
		t.Fatal(err)
	}
	alpha := func(u, v float64) uint8 { return tex.RGBAAt(int(127*u), int(127*v)).A }
	if !(alpha(.5, .12) > alpha(.5, .50) && alpha(.5, .50) > alpha(.5, .80) && alpha(.5, .97) == 0 && alpha(.5, .42) > 5*alpha(.01, .42)) {
		t.Fatalf("cone must soften across its section and dissipate near the target")
	}
}

func TestStackConeBudgetBeforeGeometry(t *testing.T) {
	decks := make([]explodedDeck, MaxTimelineBoards)
	for i := range decks {
		decks[i] = connectorTestDeck(float64(len(decks)-i)*6, "root", "layers", string(rune('a'+i)))
	}
	decks[len(decks)-1].path = []string{"root", "layers", "last"}
	for i := 0; i < 400; i++ {
		decks[0].scene.Nodes = append(decks[0].scene.Nodes, d2isometric.Node{Opacity: 1, Link: "root.layers.last"})
	}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors(decks); err == nil || len(b.triangles) != 0 {
		t.Fatalf("oversized stack allocated geometry before rejecting the links: %v", err)
	}
}
