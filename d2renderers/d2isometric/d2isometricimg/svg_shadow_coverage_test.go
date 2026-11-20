package d2isometricimg

import (
	"context"
	"image"
	"image/color"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2renderers/d2scene"
)

// Check silhouette alpha before the outer blur. The native SVG importer does
// not implement filters, so omit the blur and RGB-only shadow recoloring here;
// neither changes the interior alpha whose double application caused the ring.
func rasterSVGShadowCoverage(t *testing.T, scene *nativeScene) *image.NRGBA {
	t.Helper()
	w := &nativeSVGWriter{ctx: context.Background()}
	writeSVGGroundShadow(w, scene)
	if w.err != nil {
		t.Fatal(w.err)
	}
	source := regexp.MustCompile(`<filter\b[^>]*>.*?</filter>`).ReplaceAllString(w.buf.String(), "")
	source = regexp.MustCompile(` filter="[^"]*"`).ReplaceAllString(source, "")
	return rasterVectorFragment(t, `<g xmlns:xlink="http://www.w3.org/1999/xlink" transform="scale(0.0025)">`+source+`</g>`, image.Rect(0, 0, 400, 400))
}

func TestSVGShadowOpaqueCapAndWallHaveOneStrength(t *testing.T) {
	ctx := context.Background()
	texture := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			texture.SetRGBA(x, y, color.RGBA{255, 255, 255, 255})
		}
	}
	root := d2scene.NewNode(d2scene.Rect{Box: d2scene.Box{Width: 1, Height: 1}, Fill: d2scene.SolidPaint{Color: color.NRGBA{255, 255, 255, 255}}})
	surface := &nativeVectorSurface{document: &d2scene.Document{Root: root, ViewBox: d2scene.Box{Width: 1, Height: 1}, LogicalWidth: 1, LogicalHeight: 1}}
	material := &Material{Color: color.NRGBA{255, 255, 255, 255}, Texture: texture, Vector: surface}
	b := &meshBuilder{ctx: ctx}
	// Both faces are elevated and cast only the far shadow. The wall alone
	// and cap alone must have the same alpha as that overlap, not a dark rim.
	a := Vertex{Position: nv(-1, 1.55, -.5), Normal: nv(0, 1, 0)}
	c := Vertex{Position: nv(1, 1.55, -.5), Normal: nv(0, 1, 0), U: 1}
	d := Vertex{Position: nv(1, 1.55, .5), Normal: nv(0, 1, 0), U: 1, V: 1}
	e := Vertex{Position: nv(-1, 1.55, .5), Normal: nv(0, 1, 0), V: 1}
	b.triangle(a, d, c, material, true)
	b.triangle(a, e, d, material, true)
	cap := append([]Triangle(nil), b.triangles...)
	b.flat(c.Position, nv(1, .9, -.5), nv(1, .9, .5), nativeMaterial("white", .5, 0, 1), true)
	b.flat(c.Position, nv(1, .9, .5), d.Position, nativeMaterial("white", .5, 0, 1), true)
	nativePhysicalShadows(b.triangles, false)
	nativePhysicalShadows(cap, false)
	camera := nativeCameraAxes()
	camera.scale, camera.width, camera.height = 100, 400, 400
	scene := &nativeScene{camera: camera, width: 400, height: 400, triangles: b.triangles}
	full := rasterSVGShadowCoverage(t, scene)
	scene.triangles = cap
	onlyCap := rasterSVGShadowCoverage(t, scene)
	want := uint8(math.Round(math.Round(groundShadowLayers[0].opacity*255) * 184 / 255))
	overlap, wallOnly := 0, 0
	for y := 0; y < 400; y++ {
		for x := 0; x < 400; x++ {
			got, withoutWall := full.NRGBAAt(x, y).A, onlyCap.NRGBAAt(x, y).A
			if got > want+1 {
				t.Fatalf("shadow darkens at overlapping opaque faces: alpha %d, want %d", got, want)
			}
			if withoutWall == want {
				overlap++
				if got != want {
					t.Fatal("adding a wall changed the filled cap's shadow strength")
				}
			}
			if withoutWall == 0 && got == want {
				wallOnly++
			}
		}
	}
	if overlap < 100 || wallOnly < 100 {
		t.Fatalf("fixture missed cap or wall shadow coverage: cap=%d wall=%d", overlap, wallOnly)
	}
}

func TestSVGShadowRetainsTransparentTextureHole(t *testing.T) {
	texture := image.NewRGBA(image.Rect(0, 0, 4, 1))
	root := d2scene.NewNode(nil)
	for _, x := range []int{0, 3} {
		texture.SetRGBA(x, 0, color.RGBA{255, 255, 255, 255})
		root.Children = append(root.Children, d2scene.NewNode(d2scene.Rect{Box: d2scene.Box{X: float64(x) / 4, Width: .25, Height: 1}, Fill: d2scene.SolidPaint{Color: color.NRGBA{255, 255, 255, 255}}}))
	}
	surface := &nativeVectorSurface{document: &d2scene.Document{Root: root, ViewBox: d2scene.Box{Width: 1, Height: 1}, LogicalWidth: 1, LogicalHeight: 1}}
	material := &Material{Color: color.NRGBA{255, 255, 255, 255}, Texture: texture, Vector: surface}
	b := &meshBuilder{ctx: context.Background()}
	vertex := func(x, z, u, v float64) Vertex {
		return Vertex{Position: nv(x, .8, z), Normal: nv(0, 1, 0), U: u, V: v}
	}
	a, c, d, e := vertex(-1, -.5, 0, 0), vertex(1, -.5, 1, 0), vertex(1, .5, 1, 1), vertex(-1, .5, 0, 1)
	b.triangle(a, d, c, material, true)
	b.triangle(a, e, d, material, true)
	camera := nativeCameraAxes()
	camera.scale, camera.width, camera.height = 100, 400, 400
	scene := &nativeScene{camera: camera, width: 400, height: 400, triangles: b.triangles}
	frame := rasterSVGShadowCoverage(t, scene)
	ground, light := rasterShadowGround(b.triangles), rasterShadowDirection()
	for _, x := range []float64{-.75, 0, .75} {
		p := nv(x, .8, 0)
		p = nsub(p, nmul(light, (p.Y-ground-rasterShadowNormalOffset)/light.Y))
		p.Y = ground
		q := camera.project(p)
		alpha := frame.NRGBAAt(int(q.x), int(q.y)).A
		if x == 0 && alpha != 0 || x != 0 && alpha < uint8(math.Round(255*groundShadowLayers[0].opacity))-1 {
			t.Fatalf("texture alpha changed at x=%g: %d", x, alpha)
		}
	}
}

func TestSVGShadowTightensTowardTheFoot(t *testing.T) {
	camera := nativeCameraAxes()
	camera.scale, camera.width, camera.height = 100, 400, 400
	b := &meshBuilder{ctx: context.Background()}
	material := nativeMaterial("white", .5, 0, 1)
	// A wall intersects the receiver. Moving along its cast samples steadily
	// higher points on the same wall without crossing any other geometry.
	b.flat(nv(0, 0, -1), nv(0, 1.4, -1), nv(0, 1.4, 1), material, true)
	b.flat(nv(0, 0, -1), nv(0, 1.4, 1), nv(0, 0, 1), material, true)
	scene := &nativeScene{camera: camera, width: 400, height: 400, triangles: b.triangles}
	frame := rasterSVGShadowCoverage(t, scene)
	ground, light := rasterShadowGround(b.triangles), rasterShadowDirection()
	last := uint8(255)
	for _, height := range []float64{.1, .4, 1.1} {
		p := nsub(nv(0, ground+rasterShadowNormalOffset+height, 0), nmul(light, height/light.Y))
		p.Y = ground
		q := camera.project(p)
		alpha := frame.NRGBAAt(int(q.x), int(q.y)).A
		if alpha == 0 || alpha >= last {
			t.Fatalf("wall shadow alpha at height %g = %d, previous %d", height, alpha, last)
		}
		last = alpha
	}
	w := &nativeSVGWriter{ctx: context.Background()}
	writeSVGGroundShadow(w, scene)
	if w.err != nil {
		t.Fatal(w.err)
	}
	radii := regexp.MustCompile(`<feGaussianBlur stdDeviation="([^ ]+) ([^"]+)"`).FindAllStringSubmatch(w.buf.String(), -1)
	if len(radii) != 3 {
		t.Fatalf("got %d shadow blur radii", len(radii))
	}
	for axis := 1; axis <= 2; axis++ {
		last := math.Inf(1)
		for _, r := range radii {
			sigma, err := strconv.ParseFloat(r[axis], 64)
			if err != nil || sigma <= 0 || sigma >= last {
				t.Fatalf("expected decreasing shadow blur on axis %d: %v", axis, radii)
			}
			last = sigma
		}
	}
}

func TestSVGShadowHorizontalCasterAtLayerCeiling(t *testing.T) {
	camera := nativeCameraAxes()
	camera.scale, camera.width, camera.height = 100, 400, 400
	// The unpainted vertex fixes ground at zero, independently of the cap.
	floor := Triangle{V: [3]Vertex{{Position: nv(0, -.08, 0)}}}
	for i, layer := range groundShadowLayers[1:] {
		ceiling := rasterShadowNormalOffset + layer.height
		for _, height := range []float64{ceiling, math.Nextafter(ceiling, math.Inf(1))} {
			cap := Triangle{CastShadow: true, V: [3]Vertex{
				{Position: nv(-1, height, 0)}, {Position: nv(1, height, 0)}, {Position: nv(0, height, 1)},
			}}
			scene := &nativeScene{camera: camera, width: 400, height: 400, triangles: []Triangle{floor, cap}}
			w := &nativeSVGWriter{ctx: context.Background()}
			writeSVGGroundShadow(w, scene)
			if w.err != nil {
				t.Fatal(w.err)
			}
			want := i + 2
			if height > ceiling {
				want--
			}
			if got := strings.Count(w.buf.String(), "<feGaussianBlur "); got != want {
				t.Errorf("height %.17g at ceiling %.17g: got %d layers, want %d", height, ceiling, got, want)
			}
		}
	}
}
