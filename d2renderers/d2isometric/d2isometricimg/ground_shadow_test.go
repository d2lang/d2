package d2isometricimg

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"math"
	"testing"
)

func TestGroundShadowFallsOffFromCasterFoot(t *testing.T) {
	// A straight wall isolates the penumbra from the shape of the mesh.
	// The light projects its top toward negative Z on the ground.
	build := func(height float64) rasterGroundShadow {
		t.Helper()
		b := &meshBuilder{ctx: context.Background()}
		material := nativeMaterial("#ffffff", .5, 0, 1)
		a, c := nv(-1, 0, 0), nv(1, 0, 0)
		d, e := nv(1, height, 0), nv(-1, height, 0)
		b.flat(a, c, d, material, true)
		b.flat(a, d, e, material, true)
		w := &rasterWork{ctx: b.ctx, remaining: rasterMaxWork}
		shadow, err := buildGroundShadow(w, b.triangles, 0, rasterFit(b.triangles, rasterShadowDirection(), 2048, 2048, 1.08))
		if err != nil {
			t.Fatal(err)
		}
		return shadow
	}
	tall := build(1.3)
	near, middle, end := tall.coverage(nv(0, 0, -.06)), tall.coverage(nv(0, 0, -.22)), tall.coverage(nv(0, 0, -.52))
	if near < .21 || middle < .12 || end < .06 || near-middle < .05 || middle-end < .04 {
		t.Fatalf("wall shadow must lose strength away from contact: near %.3f, middle %.3f, end %.3f", near, middle, end)
	}
	// Just beyond the geometric endpoint, some soft cast is still visible.
	tail := tall.coverage(nv(0, 0, -.78))
	if tail < .005 || tail >= end/2 {
		t.Fatalf("expected a faint penumbra beyond the wall: end %.3f, tail %.3f", end, tail)
	}
	low := build(.2)
	if low.coverage(nv(0, 0, -.05)) < .15 || low.coverage(nv(0, 0, -.30)) > .01 {
		t.Fatal("a low caster must keep a short shadow close to its foot")
	}
}

func TestGroundShadowKeepsTextureAlphaAcrossBands(t *testing.T) {
	texture := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	for x, alpha := range []uint8{255, 64, 0} {
		texture.SetNRGBA(x, 0, color.NRGBA{A: alpha})
	}
	material := &Material{Color: color.NRGBA{A: 255}, Texture: texture}
	light := rasterShadowDirection()
	vertex := func(x, z, u, v float64) Vertex {
		// Align the projected ends of the strip at +/-3 on the receiver.
		p := nadd(nv(x, rasterShadowNormalOffset, z), nmul(light, .16/light.Y))
		return Vertex{Position: p, Normal: nv(0, 1, 0), U: u, V: v}
	}
	a, b, c, d := vertex(-3, -1, 0, 0), vertex(3, -1, 1, 0), vertex(3, 1, 1, 1), vertex(-3, 1, 0, 1)
	triangles := []Triangle{{V: [3]Vertex{a, b, c}, Material: material, CastShadow: true}, {V: [3]Vertex{a, c, d}, Material: material, CastShadow: true}}
	work := rasterWork{ctx: context.Background(), remaining: rasterMaxWork}
	shadow, err := buildGroundShadow(&work, triangles, 0, rasterFit(triangles, light, 2048, 2048, 1.08))
	if err != nil {
		t.Fatal(err)
	}
	for i, alpha := range []float64{1, 64. / 255, 0} {
		transmission := 1.
		for _, layer := range groundShadowLayers {
			transmission *= 1 - layer.opacity*alpha
		}
		if got := shadow.coverage(nv(-2+float64(i)*2, 0, 0)); math.Abs(got-(1-transmission)) > .01 {
			t.Fatalf("ground shadow at texture band %d = %.3f, want %.3f", i, got, 1-transmission)
		}
	}
}

func TestGroundShadowIncludesLevelCapsOnBandCeilings(t *testing.T) {
	for _, layer := range groundShadowLayers[1:] {
		height := rasterShadowNormalOffset + layer.height
		triangles := []Triangle{{V: [3]Vertex{
			{Position: nv(-2.1, height, 3.4)}, {Position: nv(-1.4, height, -.7)}, {Position: nv(2.8, height, -.3)},
		}, CastShadow: true}}
		light := rasterFit(triangles, rasterShadowDirection(), 128, 128, 1.08)
		work := rasterWork{ctx: context.Background(), remaining: rasterMaxWork}
		full, err := rasterizeShadow(&work, triangles, light, 0, math.Inf(1))
		if err != nil {
			t.Fatal(err)
		}
		capped, err := rasterizeShadow(&work, triangles, light, 0, height)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(full.opacity, []byte{255}) || !bytes.Equal(full.opacity, capped.opacity) {
			t.Fatalf("cap exactly on the %g ceiling lost coverage", layer.height)
		}
	}
}
