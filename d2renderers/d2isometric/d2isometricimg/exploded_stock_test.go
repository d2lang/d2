package d2isometricimg

import (
	"context"
	"image/color"
	"math"
	"testing"
)

func TestCutSlabJoinsOverlappingApertures(t *testing.T) {
	// Two 2×1 hatches share their middle 1×1 square.
	for _, tt := range []struct {
		name        string
		secondAlpha float64
		coverage    float64
		wallArea    float64
	}{
		{"open", 1, 21, (20 + 8) * .2},
		{"translucent", .5, 21 + 127./255, (20 + 6 + 2*128./255) * .2},
		{"invisible", 0, 22, (20 + 6) * .2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			deck := explodedDeck{width: 6, depth: 4, thickness: .2, surfaceY: 2, openings: map[string]stackAperture{
				"a": {center: nv(-.5, 2, 0), width: 2, depth: 1, opacity: 1},
				"b": {center: nv(.5, 2, 0), width: 2, depth: 1, opacity: tt.secondAlpha},
			}}
			b := &meshBuilder{ctx: context.Background()}
			b.cutSlab(&deck, &Material{Color: color.NRGBA{A: 255}})
			if b.err != nil {
				t.Fatal(b.err)
			}
			var top, bottom, walls float64
			for _, tri := range b.triangles {
				a, c, d := tri.V[0].Position, tri.V[1].Position, tri.V[2].Position
				cross := ncross(nsub(c, a), nsub(d, a))
				area := .5 * nlen(cross) * float64(tri.Material.Color.A) / 255
				switch {
				case cross.Y > 0:
					top += area
				case cross.Y < 0:
					bottom += area
				default:
					walls += area
				}
			}
			if math.Abs(top-tt.coverage) > 1e-6 || math.Abs(bottom-tt.coverage) > 1e-6 || math.Abs(walls-tt.wallArea) > 1e-6 {
				t.Fatalf("top, bottom, walls = %g, %g, %g; want %g, %g, %g", top, bottom, walls, tt.coverage, tt.coverage, tt.wallArea)
			}
		})
	}
}

func TestCutSlabPreservesWholeSheetGradient(t *testing.T) {
	deck := explodedDeck{width: 6, depth: 4, thickness: .2, surfaceY: 2}
	build := func() []Triangle {
		t.Helper()
		b := &meshBuilder{ctx: context.Background()}
		b.cutSlab(&deck, nativeMaterial("linear-gradient(to right, #828389, #eacfad)", 1, 0, 1))
		b.prepareGradientMaterials()
		if b.err != nil {
			t.Fatal(b.err)
		}
		return b.triangles
	}
	// The intact sheet places its vertices relative to its center. The
	// perforated sheet places them relative to its upper face.
	key := func(p Vec) [3]int {
		return [3]int{int(math.Round(p.X * 1000)), int(math.Round(p.Y * 1000)), int(math.Round(p.Z * 1000))}
	}
	vertices := map[[3]int]Vertex{}
	for _, tri := range build() {
		for _, v := range tri.V {
			vertices[key(v.Position)] = v
		}
	}
	deck.openings = map[string]stackAperture{"a": {width: 1, depth: 1, opacity: .5}}
	seen := map[[3]int]bool{}
	for _, tri := range build() {
		if tri.Material.Texture == nil {
			t.Fatal("cut stock lost its gradient")
		}
		for _, v := range tri.V {
			ref, ok := vertices[key(v.Position)]
			if ok {
				seen[key(v.Position)] = true
				if math.Abs(ref.U-v.U) > 1e-8 || math.Abs(ref.V-v.V) > 1e-8 {
					t.Fatalf("gradient at %v moved from %v,%v to %v,%v", v.Position, ref.U, ref.V, v.U, v.V)
				}
			}
		}
	}
	if len(seen) != 8 {
		t.Fatalf("found %d of the original sheet corners", len(seen))
	}
}
