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
		if l.opening.width != 2*nativeRouteRadius(top.scene.Edges[0]) || l.opening.depth != l.opening.width {
			t.Fatalf("wire opening must match the painted stroke: %+v", l)
		}
	}
	for i := 0; i < 2; i++ {
		p := pathPoint(paths[i], routeLengths(paths[i]), .5)
		if i == 1 && nlen(nsub(p, source)) < .09 {
			t.Fatalf("test edges did not form separate lanes: %v", p)
		}
		want := nadd(top.offset, p)
		if min(nlen(nsub(links[0].opening.center, want)), nlen(nsub(links[1].opening.center, want))) > .025 {
			t.Fatalf("no lead on rendered wire %d at %v; starts %v", i, want, links)
		}
	}
}

func TestStackCornerRaysUseRealOpeningAndTargetCorners(t *testing.T) {
	decks := []explodedDeck{connectorTestDeck(16, "root"), connectorTestDeck(8, "root", "layers", "middle"), connectorTestDeck(0, "root", "layers", "bottom")}
	cam := nativeCameraAxes()
	for _, pair := range [][2]int{{0, 2}, {2, 0}} {
		from, to := decks[pair[0]], decks[pair[1]]
		opening := stackAperture{center: nv(.82, from.surfaceY+.31, -.24), width: .4, depth: .23, opacity: .55}
		link := stackConnector{from: pair[0], to: pair[1], opening: opening, opacity: .55}
		starts, ends := make(map[[2]int]bool), make(map[[2]int]bool)
		shoulders := make(map[[2]int]Vec)
		var fans [][2]Vec
		ticks := 0
		height := to.surfaceY + .002
		if pair[0] > pair[1] {
			height = to.surfaceY - to.thickness - .001
		}
		stackRayStrokes(decks, link, true, func(a, b Vec, width float64, ink stackRayInk) {
			if width <= 0 {
				t.Fatal("empty stroke")
			}
			if ink == stackCornerTick {
				ticks++
				if math.Abs(a.Y-to.surfaceY) > .01 || math.Abs(b.Y-to.surfaceY) > .01 {
					t.Fatal("spurious corner target on intervening deck")
				}
				return
			}
			if math.Abs(a.Y-opening.center.Y) < 1e-8 {
				d := nsub(a, opening.center)
				if math.Abs(d.X) > opening.width/2 || math.Abs(d.Z) > opening.depth/2 {
					t.Fatalf("wrong aperture point: %v", a)
				}
				starts[[2]int{int(math.Copysign(1, d.X)), int(math.Copysign(1, d.Z))}] = true
			}
			for _, p := range []Vec{a, b} {
				if math.Abs(p.X) > to.width/2+1e-8 || math.Abs(p.Z) > to.depth/2+1e-8 {
					t.Fatalf("ray outside shared footprint: %v", p)
				}
				if p.Y < decks[1].surfaceY-1e-8 && p.Y > decks[1].surfaceY-decks[1].thickness+1e-8 {
					t.Fatalf("ray enters intermediate slab: %v", p)
				}
			}
			if math.Abs(b.Y-height) < 1e-8 {
				if math.Abs(b.X) > .39*to.width && math.Abs(b.Z) > .39*to.depth {
					ends[[2]int{int(math.Copysign(1, b.X)), int(math.Copysign(1, b.Z))}] = true
				}
			}
			if math.Abs(a.X-b.X) < 1e-8 && math.Abs(a.Z-b.Z) < 1e-8 {
				key := [2]int{int(math.Copysign(1, a.X-opening.center.X)), int(math.Copysign(1, a.Z-opening.center.Z))}
				last, ok := shoulders[key]
				if !ok || math.Abs(b.Y-opening.center.Y) > math.Abs(last.Y-opening.center.Y) {
					shoulders[key] = b
				}
			} else {
				fans = append(fans, [2]Vec{a, b})
			}
		})
		if len(shoulders) != 4 {
			t.Fatalf("missing plumb starts: %v", shoulders)
		}
		for key, p := range shoulders {
			if pair[0] < pair[1] {
				// From below the plate, the ray toward the camera must miss
				// the stock before the four strokes start to spread.
				edge := nadd(p, nmul(cam.direction, (from.surfaceY-from.thickness-p.Y)/cam.direction.Y))
				if math.Abs(edge.X) <= from.width/2 && math.Abs(edge.Z) <= from.depth/2 {
					t.Fatalf("shoulder %v still hidden behind source stock", p)
				}
			}
			if !starts[key] {
				t.Fatal("plumb stroke does not originate inside the mouth")
			}
		}
		for _, segment := range fans {
			// The remainder of each line keeps aiming at the same corner,
			// including the lengths clipped against a printed sheet.
			straight := false
			for key, o := range shoulders {
				inset := .075
				last := nv(float64(key[0])*(to.width/2-inset), height, float64(key[1])*(to.depth/2-inset))
				v := nsub(last, o)
				offset := func(p Vec) float64 {
					vp := nsub(p, o)
					return math.Abs(ndot(v, cam.right)*ndot(vp, cam.up)-ndot(v, cam.up)*ndot(vp, cam.right)) / max(.01, math.Hypot(ndot(v, cam.right), ndot(v, cam.up)))
				}
				if offset(segment[0]) < 1e-7 && offset(segment[1]) < 1e-7 {
					straight = true
				}
			}
			if !straight {
				t.Fatalf("stroke %v misses its matching destination corner", segment)
			}
		}
		if len(starts) != 4 || len(ends) != 4 || ticks != 8 {
			t.Fatalf("missing corners, origins %v termini %v ticks %d", starts, ends, ticks)
		}
	}
}

func TestStackCornerRayUsesApertureBeforeRaisedCover(t *testing.T) {
	top, bottom := connectorTestDeck(6, "root"), connectorTestDeck(0, "root", "layers", "B")
	node := d2isometric.Node{ID: "cover", Position: nv(.82, .6, -.24), Size: nv(1.2, .7, 1.4), Link: "root.layers.B", Opacity: .6}
	top.scene.Nodes = []d2isometric.Node{node}
	top.tops = map[string]float64{"cover": 2}
	opening := stackAperture{center: nv(.82, 6.23, .48), width: .75, depth: .41, opacity: .6}
	top.openings = map[string]stackAperture{"cover": opening}
	b := &meshBuilder{ctx: context.Background()}
	links, err := b.stackLinks([]explodedDeck{top, bottom})
	if err != nil || len(links) != 1 || links[0].opening != opening {
		t.Fatalf("missing real opening: %+v (%v)", links, err)
	}
	b.drawStackRays([]explodedDeck{top, bottom}, links[0], true)
	if len(b.triangles) == 0 {
		t.Fatalf("missing geometry")
	}
	for _, tri := range b.triangles {
		if tri.Material.Texture != nil || !tri.Material.svgPointerPassthrough || !tri.NoDepthWrite || tri.Material.Color.A > uint8(255*node.Opacity) {
			t.Fatalf("transfer must use untextured, passthrough ink at source opacity")
		}
	}
}

func TestStackCornerMarksAreSharedByIncomingLinks(t *testing.T) {
	top, bottom := connectorTestDeck(8, "root"), connectorTestDeck(0, "root", "layers", "B")
	for i, alpha := range []float64{.3, .6, .45} {
		top.scene.Nodes = append(top.scene.Nodes, d2isometric.Node{ID: string(rune('a' + i)), Position: nv(float64(i-1), .1, 0), Size: nv(1, .2, 1), Link: "root.layers.B", Opacity: alpha})
	}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors([]explodedDeck{top, bottom}); err != nil {
		t.Fatal(err)
	}
	faces := 0
	for _, tri := range b.triangles {
		if math.Abs(tri.V[0].Position.Y-.003) > 1e-8 || math.Abs(tri.V[1].Position.Y-.003) > 1e-8 {
			continue
		}
		faces++
		if tri.Material.Color.A != 130 {
			t.Fatalf("expected strongest incoming opacity on shared marks, got %d", tri.Material.Color.A)
		}
	}
	if faces != 16 {
		t.Fatalf("four corners each have two strokes with two faces, got %d", faces)
	}
}

func TestStackCornerRayBudgetBeforeGeometry(t *testing.T) {
	decks := make([]explodedDeck, MaxTimelineBoards)
	for i := range decks {
		decks[i] = connectorTestDeck(float64(len(decks)-i)*6, "root", "layers", string(rune('a'+i)))
	}
	decks[len(decks)-1].path = []string{"root", "layers", "last"}
	for i := 0; i < 1800; i++ {
		decks[0].scene.Nodes = append(decks[0].scene.Nodes, d2isometric.Node{Opacity: 1, Link: "root.layers.last"})
	}
	b := &meshBuilder{ctx: context.Background()}
	if err := b.addStackConnectors(decks); err == nil || len(b.triangles) != 0 {
		t.Fatalf("oversized stack allocated geometry before rejecting the links: %v", err)
	}
}
