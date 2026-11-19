package d2isometricimg

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2isometric"
)

func TestStackHatchSelection(t *testing.T) {
	floors := stackFloorIndex{"root": 0, "root.layers.next": 1}
	for _, tc := range []struct {
		role, target string
		want         bool
	}{
		{"", "root", false}, {"", "https://d2lang.com", false}, {"", "root.layers.missing", false},
		{"", "root.layers.next", true}, {"actor", "root.layers.next", true}, {"note", "root.layers.next", true},
		{"span", "root.layers.next", false}, {"group", "root.layers.next", false},
	} {
		n := fidelityNode("rectangle")
		n.SequenceRole, n.Link = tc.role, tc.target
		requests := requestedStackApertures(&d2isometric.Scene{Nodes: []d2isometric.Node{n}}, floors, 0)
		if _, ok := requests[n.ID]; ok != tc.want {
			t.Errorf("role %q target %q has hatch %v, want %v", tc.role, tc.target, ok, tc.want)
		}
	}
}

func TestStackHatchLidCaptionAndVisibleMouth(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, north := range []bool{false, true} {
			n := fidelityNode("rectangle")
			n.Position, n.Size, n.Opacity = nv(0, .2, 0), nv(2, .4, 1), .4
			captionZ := .8
			if north {
				captionZ, n.Metadata.Original.LabelPosition = -.8, "OUTSIDE_TOP_CENTER"
			}
			b := &meshBuilder{ctx: context.Background(), options: nativeSceneOptions{
				stackApertures: map[string]stackAperture{n.ID: {}}, vector: vector,
			}}
			uv := Vertex{Position: nv(0, .4, -.5), Normal: nv(0, 1, 0), U: .25, V: .1}
			b.triangle(uv, Vertex{Position: nv(-1, 0, .5)}, Vertex{Position: nv(1, .4, .5)}, nativeMaterial("white", 1, 0, 1), true)
			caption := nv(0, .41, captionZ)
			print := nativeMaterial("black", 1, 0, 1)
			print.outsideCaption = true
			b.flat(caption, nadd(caption, nv(.1, 0, 0)), nadd(caption, nv(0, 0, .1)), print, false)
			group := &nativeOpacityGroup{Opacity: .4}
			for i := range b.triangles {
				b.triangles[i].OpacityGroup = group
			}
			b.links = []nativeLink{{points: surfaceCorners(labelSurface{center: caption, width: .2, depth: .1})}}
			firstPoint := b.links[0].points[0]
			b.openStackHatch(n, 0, 0)
			raised := b.triangles[0].V[0]
			if raised.U != uv.U || raised.V != uv.V || math.Abs(raised.Position.Y-uv.Position.Y-hatchLidRise) > 1e-9 {
				t.Fatalf("artwork lost its mapping or lid rise: %+v", raised)
			}
			move := nv(0, 0, 0)
			if north {
				move = nv(0, hatchLidRise, .04)
			}
			if b.triangles[1].V[0].Position != nadd(caption, move) || b.links[0].points[0] != nadd(firstPoint, move) {
				t.Fatalf("north=%v vector=%v caption and hit region did not keep their corresponding clearances", north, vector)
			}
			h := b.options.stackApertures[n.ID]
			if h.opacity != n.Opacity || h.depth < n.Size.Z*.5 || h.width >= n.Size.X {
				t.Fatalf("invalid hatch geometry: %+v", h)
			}
			hitFaces := 0
			for _, tri := range b.triangles[2:] {
				if tri.OpacityGroup != group || tri.Material.Color.A != 255 {
					t.Fatal("rim lost or double-applied the object's opacity")
				}
				if tri.Material.svgHitOnly {
					hitFaces++
					if tri.CastShadow || !tri.NoDepthWrite {
						t.Fatal("invisible mouth changes physical visibility")
					}
				}
			}
			if (vector && hitFaces != 2) || (!vector && hitFaces != 0) {
				t.Fatalf("vector=%v got %d mouth hit triangles", vector, hitFaces)
			}
		}
	}
}

func TestStackHatchesPierceOnlyTheirMountedBoards(t *testing.T) {
	child := fidelityNode("rectangle")
	child.ID, child.BoardID = "processor", "inner"
	child.Position = nv(0, .4, 0)
	container := fidelityNode("rectangle")
	container.ID, container.Container = "container", true
	container.Size, container.Position = nv(8, .2, 6), nv(0, 0, 0)
	boards := []d2isometric.Board{
		{ID: "outer", Size: nv(9, .2, 7), Kind: "platform"},
		{ID: "inner", SourceID: "container", ParentID: "outer", Size: container.Size, Kind: "terrace"},
		{ID: "sibling", ParentID: "outer", Size: nv(3, .2, 2), Kind: "terrace"},
	}
	b := &meshBuilder{ctx: context.Background(), scale: .01, options: nativeSceneOptions{stackApertures: map[string]stackAperture{container.ID: {}, child.ID: {}}}}
	b.planStackHatches([]d2isometric.Node{container, child}, boards, newRouteCaptionPlacer())
	h := b.options.stackApertures[container.ID]
	if h.width <= 0 || h.depth <= 0 || (math.Abs(h.center.X-child.Position.X) < (h.width+child.Size.X)/2 && math.Abs(h.center.Z-child.Position.Z) < (h.depth+child.Size.Z)/2) {
		t.Fatalf("container did not find a clear opening around its mounted child: %+v", h)
	}
	for _, id := range []string{"outer", "inner"} {
		if len(b.stackBoardCuts[id]) != 2 {
			t.Fatalf("%s received %d of 2 apertures", id, len(b.stackBoardCuts[id]))
		}
	}
	if len(b.stackBoardCuts["sibling"]) != 0 {
		t.Fatal("unrelated sibling was cut")
	}
	paint := labelSurface{width: 8, depth: 6}
	b.surfaceTexture(image.NewRGBA(image.Rect(0, 0, 2, 2)), paint, .8)
	b.cutStackBoardFace(paint, b.stackBoardCuts["inner"], 0)
	area := 0.
	for _, tri := range b.triangles {
		a, c, d := tri.V[0], tri.V[1], tri.V[2]
		area += nlen(ncross(nsub(c.Position, a.Position), nsub(d.Position, a.Position))) / 2
		if tri.Material.Color.A != 204 || tri.DepthBias != .0005 {
			t.Fatal("support lost opacity or print offset")
		}
		for _, v := range tri.V {
			if math.Abs(v.U-(v.Position.X/8+.5)) > 1e-9 || math.Abs(v.V-(v.Position.Z/6+.5)) > 1e-9 {
				t.Fatal("original printed board UVs moved around a cutout")
			}
		}
	}
	childHole := leafStackOpening(child)
	expected := 48 - h.width*h.depth - childHole.width*childHole.depth
	if math.Abs(area-expected) > 1e-6 {
		t.Fatalf("printed support area %g, want %g", area, expected)
	}
}
