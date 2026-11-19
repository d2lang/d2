package d2isometricimg

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2isometric"
	"github.com/d2lang/d2/d2renderers/d2scene"
	"github.com/d2lang/d2/d2renderers/internal/scenepaint"
)

// The link targets the entire destination board. Its light originates at
// the linked artwork and expands toward the floor's full footprint.
type stackConnector struct {
	from, to int
	source   Vec
	contact  labelSurface
	opacity  float64
}

type stackWire struct {
	visible bool
	anchor  Vec
}

// Choose the closest surviving ink to the routed wire midpoint, after
// lane separation, bridging, endpoint clipping, dashing and label cutouts.
func (b *meshBuilder) captureStackWire(index int, edge d2isometric.Edge, ink *Material, first int, path []Vec, lengths []float64) {
	if b.options.stackWires == nil || edge.Link == "" || len(path) < 2 || b.err != nil || !nativeRouteVisible(edge) {
		return
	}
	wire := &b.options.stackWires[index]
	mid := pathPoint(path, lengths, .5)
	best := math.Inf(1)
	for i, tri := range b.triangles[first:] {
		if i%1024 == 0 {
			if err := b.ctx.Err(); err != nil {
				b.err = err
				return
			}
		}
		if tri.Material != ink {
			continue
		}
		for k, vertex := range tri.V {
			p := vertex.Position
			delta := nsub(tri.V[(k+1)%3].Position, p)
			t := max(0., min(1., ndot(nsub(mid, p), delta)/max(1e-24, ndot(delta, delta))))
			q := nadd(p, nmul(delta, t))
			d := nsub(q, mid)
			if sq := ndot(d, d); sq < best {
				best, wire.anchor, wire.visible = sq, q, true
			}
		}
	}
}

func (b *meshBuilder) captureStackTop(n d2isometric.Node, first int) {
	if b.options.stackTops == nil || n.Link == "" {
		return
	}
	top := math.Inf(-1)
	for i, t := range b.triangles[first:] {
		if i%1024 == 0 {
			if err := b.ctx.Err(); err != nil {
				b.err = err
				return
			}
		}
		if t.Material == nil || t.Material.outsideCaption || t.svgCoverageOnly {
			continue
		}
		a, c, d := t.V[0].Position, t.V[1].Position, t.V[2].Position
		v, w, p := nsub(c, a), nsub(d, a), nsub(n.Position, a)
		det := v.X*w.Z - w.X*v.Z
		if math.Abs(det) < 1e-12 {
			continue
		}
		u := (p.X*w.Z - w.X*p.Z) / det
		k := (v.X*p.Z - p.X*v.Z) / det
		if u >= -1e-9 && k >= -1e-9 && u+k <= 1+1e-9 {
			top = max(top, a.Y+u*v.Y+k*w.Y)
		}
	}
	if !math.IsInf(top, -1) {
		b.options.stackTops[n.ID] = top
	}
}

func stackPathKey(path []string) string {
	// A literal dot inside a quoted board name is not a path separator.
	return d2format.Format(d2ast.MakeKeyPath(path))
}

func stackLinkKey(link string) string {
	// Compilation has already resolved layers.foo and _.layers.foo
	// relative to the source board. Never interpret an external URL,
	// output filename or partial path as a connection between floors.
	if link != "root" && !strings.HasPrefix(link, "root.") {
		return ""
	}
	key, err := d2parser.ParseKey(link)
	if err != nil || key.Range.End.Byte != len(link) {
		return ""
	}
	return stackPathKey(key.StringIDA())
}

type stackFloorIndex map[string]int

func indexStackFloors(decks []explodedDeck) stackFloorIndex {
	index := make(stackFloorIndex, len(decks))
	for i, deck := range decks {
		index[stackPathKey(deck.path)] = i
	}
	return index
}

func (index stackFloorIndex) destination(link string, from int) (int, bool) {
	i, ok := index[stackLinkKey(link)]
	return i, ok && i != from
}

func (b *meshBuilder) stackLinks(decks []explodedDeck) ([]stackConnector, error) {
	if len(decks) < 2 {
		return nil, nil
	}
	index := indexStackFloors(decks)
	var links []stackConnector
	for i, d := range decks {
		if err := b.ctx.Err(); err != nil {
			return nil, err
		}
		for _, n := range d.scene.Nodes {
			if n.Opacity*255 < .5 || n.Link == "" || hierarchySpacer(n) {
				continue
			}
			to, ok := index.destination(n.Link, i)
			if !ok {
				continue
			}
			p := n.Position
			p.Y += n.Size.Y / 2
			if top, ok := d.tops[n.ID]; ok {
				p.Y = top
			}
			source := nadd(p, d.offset)
			contact := labelSurface{center: nv(source.X, d.surfaceY+.002, source.Z), width: n.Size.X * 1.6, depth: n.Size.Z * 1.7}
			links = append(links, stackConnector{i, to, source, contact, n.Opacity})
		}
		for j, w := range d.wires {
			e := d.scene.Edges[j]
			if !w.visible || e.Opacity*255 < .5 {
				continue
			}
			if to, ok := index.destination(e.Link, i); ok {
				source := nadd(w.anchor, d.offset)
				diameter := 10 * nativeRouteRadius(e)
				contact := labelSurface{center: nv(source.X, max(d.surfaceY+.002, source.Y-.002), source.Z), width: diameter, depth: diameter}
				links = append(links, stackConnector{i, to, source, contact, e.Opacity})
			}
		}
	}
	return links, nil
}

func (b *meshBuilder) addStackConnectors(decks []explodedDeck) error {
	links, err := b.stackLinks(decks)
	if err != nil {
		return err
	}
	if len(links) == 0 {
		return nil
	}
	// Preflight the actual axial slabs before allocating the common mesh.
	faces := 0
	for _, link := range links {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		bands := stackConeBands(decks, link)
		for _, band := range bands {
			faces += stackConeFaces(band)
		}
		if link.contact.width > 0 && link.contact.depth > 0 {
			faces += 2
		}
		if faces > maxNativeTriangles-len(b.triangles) {
			return fmt.Errorf("isometric exploded transfers exceed %d triangles", maxNativeTriangles)
		}
	}
	b.triangles = slices.Grow(b.triangles, faces)
	tex, err := b.stackConeTexture()
	if err != nil {
		return err
	}
	for _, link := range links {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		b.drawStackCone(decks, link, tex)
		if b.err != nil {
			return b.err
		}
	}
	return nil
}

const stackConeSides = 20
const stackConeFadeEnd = .90

// The transfer occupies only the air between decks. A skipped board slices
// the original cone; no additional source or destination is introduced.
type stackConeBand struct {
	start, end float64
	next       int
}

func stackConeHeights(decks []explodedDeck, l stackConnector) (float64, float64) {
	from, to := decks[l.from], decks[l.to]
	emit, receive := from.surfaceY-from.thickness, to.surfaceY
	if from.translucent || from.surfaceY < to.surfaceY {
		emit = max(from.surfaceY, l.source.Y)
	}
	if from.surfaceY < to.surfaceY {
		receive -= to.thickness
	}
	cam := nativeCameraAxes()
	// A virtual mouth in the air preserves straight cone generators.
	// Reserve depth proportional to the receiving plate's projected
	// relief, rather than accelerating the cone radius near its tip.
	view := cam.direction
	rear := min(to.width/2/view.X, to.depth/2/view.Z) * view.Y
	standOff := min(.40*math.Abs(receive-emit), .88*rear+.08)
	return emit, receive - math.Copysign(standOff, receive-emit)
}

// Collimate in the object's own X/Z column while the source plate hides
// its tip. Divergence begins exactly where the vertical axis clears the lip.
func stackConeReveal(d explodedDeck, source Vec, emit, receive float64) float64 {
	if d.translucent || emit <= receive {
		return 0
	}
	v := nativeCameraAxes().direction
	hidden := min((d.width/2-source.X)/v.X, (d.depth/2-source.Z)/v.Z) * v.Y
	return max(0., min(.80, hidden/(emit-receive)))
}

func stackConeBands(decks []explodedDeck, l stackConnector) []stackConeBand {
	emit, receive := stackConeHeights(decks, l)
	reveal := stackConeReveal(decks[l.from], l.source, emit, receive)
	step := 1
	if l.from > l.to {
		step = -1
	}
	var out []stackConeBand
	for i := l.from; i != l.to; i += step {
		a, z := decks[i].surfaceY-decks[i].thickness, decks[i+step].surfaceY
		if step < 0 {
			a, z = decks[i].surfaceY, decks[i+step].surfaceY-decks[i+step].thickness
		}
		if i == l.from {
			a = emit
		}
		start := max(0., (a-emit)/(receive-emit))
		end := min(reveal+(1-reveal)*stackConeFadeEnd, (z-emit)/(receive-emit))
		for _, progress := range [...]float64{0, .07, .18, .32, .50, .68, .80, stackConeFadeEnd} {
			k := reveal + (1-reveal)*progress
			if start < k && k < end {
				out = append(out, stackConeBand{start, k, i + step})
				start = k
			}
		}
		if start < end {
			out = append(out, stackConeBand{start, end, i + step})
		}
	}
	return out
}

func stackConeFaces(band stackConeBand) int {
	if band.start == 0 {
		return stackConeSides
	}
	return stackConeSides * 2
}

func (b *meshBuilder) stackConeTexture() (*image.RGBA, error) {
	glow, err := scenepaint.Gradient("linear-gradient(to bottom, rgba(255,235,188,0.90) 0%, rgba(251,217,159,0.73) 20%, rgba(240,196,133,0.57) 43%, rgba(231,181,117,0.30) 65%, rgba(231,181,117,0.07) 82%, rgba(231,181,117,0) 90%)")
	if err != nil {
		return nil, err
	}
	feather, err := scenepaint.Gradient("linear-gradient(to right, transparent 0%, rgba(255,255,255,0.33) 6%, rgba(255,255,255,0.80) 20%, white 50%, rgba(255,255,255,0.80) 80%, rgba(255,255,255,0.33) 94%, transparent 100%)")
	if err != nil {
		return nil, err
	}
	const size = 128
	box := d2scene.Box{Width: size, Height: size}
	root := d2scene.NewNode(d2scene.Rect{Box: box, Fill: glow})
	root.Mask = &d2scene.Mask{Type: d2scene.MaskAlpha, Transform: d2scene.Identity(), Root: d2scene.NewNode(d2scene.Rect{Box: box, Fill: feather})}
	return rasterNativeSurfaceDocument(b.ctx, d2scene.NewDocument(box, root), size, size)
}

type stackConeRay struct {
	end Vec
	u   float64
}

// Use the tangent generators of the projected ellipse to integrate the
// volume on its far shell. Each screen ray sees the feathered field once.
func stackConeSection(apex Vec, rx, rz, receive float64) (ring [stackConeSides + 1]stackConeRay) {
	cam := nativeCameraAxes()
	center := nv(0, receive, 0)
	rel := nsub(apex, center)
	x, y := ndot(rel, cam.right), ndot(rel, cam.up)
	xx, xy := rx*cam.right.X, rx*cam.up.X
	zx, zy := rz*cam.right.Z, rz*cam.up.Z
	det := xx*zy - zx*xy
	ux, uz := (x*zy-zx*y)/det, (xx*y-xy*x)/det
	angle := math.Atan2(uz, ux)
	distance := math.Hypot(ux, uz)
	if distance <= 1.000001 {
		// Looking through the oval mouth: no real tangent rays exist.
		// Integrate across its full projected diameter instead.
		phase := math.Atan2(rz*cam.right.Z, rx*cam.right.X)
		extent := nv(rx*math.Cos(phase), 0, rz*math.Sin(phase))
		for i := range ring {
			u := float64(i) / stackConeSides
			ring[i] = stackConeRay{nmul(extent, 2*u-1), u}
		}
		return ring
	}
	gamma := math.Acos(1 / distance)
	phi := func(p Vec) float64 {
		d := nsub(nadd(center, p), apex)
		return math.Atan2(ndot(d, cam.up), ndot(d, cam.right))
	}
	for i := range ring {
		k := float64(i) / stackConeSides
		a := angle + gamma + k*(2*math.Pi-2*gamma)
		ring[i].end = nv(rx*math.Cos(a), 0, rz*math.Sin(a))
	}
	firstRay := phi(ring[0].end)
	sweep := math.Remainder(phi(ring[stackConeSides].end)-firstRay, 2*math.Pi)
	for i := range ring {
		ring[i].u = float64(i) / stackConeSides
		if math.Abs(sweep) > 1e-8 {
			ring[i].u = max(0., min(1., math.Remainder(phi(ring[i].end)-firstRay, 2*math.Pi)/sweep))
		}
	}
	return ring
}

func (b *meshBuilder) drawStackCone(decks []explodedDeck, l stackConnector, tex *image.RGBA) {
	from, to := decks[l.from], decks[l.to]
	if s := l.contact; s.width > 0 && s.depth > 0 {
		halo := b.gradientTexture("radial-gradient(circle, rgba(255,235,188,0.35) 0%, rgba(255,208,143,0.60) 53%, rgba(242,189,115,0.41) 72%, rgba(231,181,117,0) 100%)")
		s.width = min(s.width, from.width-2*math.Abs(s.center.X))
		s.depth = min(s.depth, from.depth-2*math.Abs(s.center.Z))
		first := len(b.triangles)
		b.surfaceTexture(halo, s, l.opacity)
		for i := first; i < len(b.triangles); i++ {
			b.triangles[i].Material.svgPointerPassthrough = true
		}
	}
	emit, receive := stackConeHeights(decks, l)
	reveal := stackConeReveal(from, l.source, emit, receive)
	paint := nativeMaterial("white", 1, 0, l.opacity)
	paint.Unlit = true
	paint.svgPointerPassthrough = true
	paint.Texture, paint.Vector = tex, nativeVectorForTexture(b.ctx, tex)
	rx, rz := to.width/2, to.depth/2
	cam := nativeCameraAxes()
	apex := nv(l.source.X, emit+(receive-emit)*reveal, l.source.Z)
	ring := stackConeSection(apex, rx, rz, receive)

	// Screen-space distance to the approaching board's projected lip.
	// This also leaves clear air in front of a transparent receiver.
	clearance := func(p Vec, d explodedDeck) float64 {
		view := cam.direction
		if emit > receive {
			rear := min((p.X+rx)/view.X, (p.Z+rz)/view.Z) * view.Y
			return p.Y - d.surfaceY - rear
		}
		front := min((rx-p.X)/view.X, (rz-p.Z)/view.Z) * view.Y
		return d.surfaceY - d.thickness - front - p.Y
	}
	for _, band := range stackConeBands(decks, l) {
		first := len(b.triangles)
		approach := decks[band.next]
		air := max(.14, min(.5, approach.depth*.045))
		soft := max(.6, min(2.8, math.Abs(emit-receive)*.17))
		vertex := func(i int, t float64) Vertex {
			progress := max(0., (t-reveal)/(1-reveal))
			opening := progress
			if reveal > 0 {
				// The subpixel waist is narrow enough to stay inside the
				// object's column until it emerges from behind the plate.
				opening = .003*min(t/reveal, 1.) + .997*progress
			}
			p := nv(l.source.X+(ring[i].end.X-l.source.X)*opening, emit+(receive-emit)*t, l.source.Z+(ring[i].end.Z-l.source.Z)*opening)
			// Keep the mapping affine through the transparent edge. Clamping
			// here pulls lit texels across the projected lip of a nearby deck.
			v := max(progress, stackConeFadeEnd-(clearance(p, approach)-air)*.70/soft)
			return Vertex{Position: p, Normal: cam.direction, U: ring[i].u, V: v + t*1e-6}
		}
		for i := 0; i < stackConeSides; i++ {
			j := i + 1
			a, c, d, e := vertex(i, band.start), vertex(j, band.start), vertex(j, band.end), vertex(i, band.end)
			if band.start != 0 && min(a.V, c.V, d.V) < stackConeFadeEnd {
				b.triangle(a, c, d, paint, false)
			}
			if min(a.V, d.V, e.V) < stackConeFadeEnd {
				b.triangle(a, d, e, paint, false)
			}
		}
		for j := first; j < len(b.triangles); j++ {
			b.triangles[j].NoDepthWrite = true
			if l.from < l.to || band.next != l.from-1 {
				b.triangles[j].DepthBias = -min(rasterCoordinateLimit, from.width+from.depth+math.Abs(emit-receive))
			}
		}
	}
}
