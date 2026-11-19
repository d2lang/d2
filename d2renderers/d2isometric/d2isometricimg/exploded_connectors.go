package d2isometricimg

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2isometric"
)

// The four lines connect the source opening to the entire destination board.
type stackConnector struct {
	from, to int
	opening  stackAperture
	opacity  float64
}

type stackAperture struct {
	center       Vec
	width, depth float64
	opacity      float64
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
			opening, ok := d.openings[n.ID]
			if !ok {
				p := n.Position
				p.Y += n.Size.Y / 2
				if top, ok := d.tops[n.ID]; ok {
					p.Y = top
				}
				opening = stackAperture{center: nadd(p, d.offset), opacity: n.Opacity}
			}
			links = append(links, stackConnector{i, to, opening, n.Opacity})
		}
		for j, w := range d.wires {
			e := d.scene.Edges[j]
			if !w.visible || e.Opacity*255 < .5 {
				continue
			}
			if to, ok := index.destination(e.Link, i); ok {
				diameter := 2 * nativeRouteRadius(e)
				open := stackAperture{center: nadd(w.anchor, d.offset), width: diameter, depth: diameter, opacity: e.Opacity}
				links = append(links, stackConnector{i, to, open, e.Opacity})
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
	// Walk the same strokes for admission and painting. Dash lengths and
	// intervening boards may change the exact count for each transfer.
	// Coincident marks belong to the receiver. Several links to the same
	// board share one set of ticks at the strongest incoming opacity.
	ticks := make(map[int]int)
	for i, l := range links {
		prev, ok := ticks[l.to]
		if !ok || l.opacity > links[prev].opacity {
			ticks[l.to] = i
		}
	}
	faces := 0
	for i, l := range links {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		stackRayStrokes(decks, l, ticks[l.to] == i, func(_, _ Vec, _ float64, _ stackRayInk) { faces += 2 })
		if faces > maxNativeTriangles-len(b.triangles) {
			return fmt.Errorf("isometric exploded transfers exceed %d triangles", maxNativeTriangles)
		}
	}
	b.triangles = slices.Grow(b.triangles, faces)
	for i, l := range links {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		b.drawStackRays(decks, l, ticks[l.to] == i)
		if b.err != nil {
			return b.err
		}
	}
	return nil
}

type stackRayInk int

const (
	stackFrontRay stackRayInk = iota
	stackBackRay
	stackFrontTrace
	stackBackTrace
	stackCornerTick
)

// Entry parameter for a screen ray ending inside a board. Projecting onto
// the horizontal face lets us trim skipped boards and preserve their artwork.
func stackRayEntry(a, b Vec, floor explodedDeck, y float64) float64 {
	view := nativeCameraAxes().direction
	pa := nsub(a, nmul(view, (a.Y-y)/view.Y))
	pb := nsub(b, nmul(view, (b.Y-y)/view.Y))
	enter := 0.
	clip := func(start, end, half float64) {
		if start < -half && end > start {
			enter = max(enter, (-half-start)/(end-start))
		} else if start > half && end < start {
			enter = max(enter, (half-start)/(end-start))
		}
	}
	clip(pa.X, pb.X, floor.width/2)
	clip(pa.Z, pb.Z, floor.depth/2)
	return min(1., enter)
}

// Run a short plumb bundle through the hatch. Start the fan where the
// bundle clears the near silhouette of the source plate in this camera.
func stackRayShoulder(bundle stackAperture, from, next explodedDeck, descending bool, weight float64) float64 {
	gap := math.Abs(from.surfaceY - next.surfaceY)
	clearance := min(gap*.10, max(weight*8, min(from.width, from.depth)*.06))
	if !descending {
		room := max(0., next.surfaceY-next.thickness-bundle.center.Y)
		return bundle.center.Y + min(clearance, room*.25)
	}
	view := nativeCameraAxes().direction
	x := bundle.center.X - bundle.width/2
	z := bundle.center.Z - bundle.depth/2
	drop := max(0., min((from.width/2-x)/view.X, (from.depth/2-z)/view.Z)*view.Y)
	edge := from.surfaceY - from.thickness - drop
	// Use the room below the projected edge; a deep source near the back
	// of a tightly spaced deck may need almost the entire gap to emerge.
	room := max(gap*.02, edge-next.surfaceY)
	return min(bundle.center.Y, next.surfaceY+room-min(clearance, room*.25))
}

func stackRayStrokes(decks []explodedDeck, l stackConnector, ticks bool, line func(a, b Vec, width float64, ink stackRayInk)) {
	to := decks[l.to]
	step := 1
	landingY := to.surfaceY + .002
	if l.to < l.from {
		step = -1
		landingY = to.surfaceY - to.thickness - .001
	}
	edge := min(to.width, to.depth)
	inset := min(edge*.10, max(.075, min(.24, edge*.025)))
	x, z := to.width/2-inset, to.depth/2-inset
	weight := max(.018, min(.14, to.width*.0027))
	leg := min(edge*.06, max(.1, min(.32, edge*.045)))
	cam := nativeCameraAxes()
	// Four plumb strands close together still read as one source on a
	// long, narrow issuing object. A thin wire sets its own smaller limit.
	bundle := l.opening
	bundle.width = min(bundle.width*.64, weight*5.6)
	bundle.depth = min(bundle.depth*.64, weight*4.4)
	shoulder := stackRayShoulder(bundle, decks[l.from], decks[l.from+step], step > 0, weight)
	corners := [4][2]float64{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
	for _, c := range corners {
		sx, sz := c[0], c[1]
		origin := nadd(bundle.center, nv(sx*bundle.width/2, 0, sz*bundle.depth/2))
		elbow := nv(origin.X, shoulder, origin.Z)
		b := nv(sx*x, landingY, sz*z)
		span := b.Y - elbow.Y
		if math.Abs(span) < 1e-9 {
			continue
		}
		pointAt := func(y float64) Vec { return nlerp(elbow, b, max(0., min(1., (y-elbow.Y)/span))) }
		ink := stackFrontRay
		if sz < 0 {
			ink = stackBackRay
		}
		emit := func(p, q Vec, ink stackRayInk) {
			if nlen(nsub(q, p)) < 1e-9 {
				return
			}
			if ink == stackBackRay && step < 0 {
				// Short hidden-line dashes distinguish the return through a stack.
				delta := nsub(q, p)
				screenLength := math.Hypot(ndot(delta, cam.right), ndot(delta, cam.up))
				n := max(1, min(24, int(math.Ceil(screenLength/(weight*14)))))
				for i := 0; i < n; i++ {
					t := float64(i) / (float64(n) - .34)
					end := (float64(i) + .66) / (float64(n) - .34)
					line(nlerp(p, q, t), nlerp(p, q, end), weight*.82, ink)
				}
				return
			}
			if ink == stackBackRay || ink == stackBackTrace {
				line(p, q, weight*.82, ink)
			} else {
				line(p, q, weight, ink)
			}
		}
		if bundle.width != 0 || bundle.depth != 0 || (sx == 1 && sz == 1) {
			emit(origin, elbow, ink)
		}
		for i := l.from; i != l.to; i += step {
			leaving, next := decks[i], decks[i+step]
			exitY, entryY := leaving.surfaceY-leaving.thickness, next.surfaceY
			if step < 0 {
				exitY, entryY = leaving.surfaceY, next.surfaceY-next.thickness
			}
			p, q := pointAt(exitY), pointAt(entryY)
			if i == l.from {
				p = elbow
			}
			if i+step == l.to {
				q = b
			}
			if i != l.from {
				t := stackRayEntry(q, p, leaving, exitY)
				p = nlerp(q, p, t)
			}
			if i+step != l.to {
				q = nlerp(p, q, stackRayEntry(p, q, next, entryY))
				emit(p, q, ink)
			} else if step > 0 {
				// Register the visible tail on the receiving sheet underneath
				// its objects. Sliding along the camera ray keeps the line straight.
				e := nlerp(p, q, stackRayEntry(p, q, to, landingY))
				emit(p, e, ink)
				e = nsub(e, nmul(cam.direction, (e.Y-landingY)/cam.direction.Y))
				trace := stackFrontTrace
				if ink == stackBackRay {
					trace = stackBackTrace
				}
				emit(e, q, trace)
			} else {
				emit(p, q, ink)
			}
		}
		if ticks {
			corner := nv(b.X, to.surfaceY+.003, b.Z)
			line(corner, nadd(corner, nv(-sx*leg, 0, 0)), weight*1.22, stackCornerTick)
			line(corner, nadd(corner, nv(0, 0, -sz*leg)), weight*1.22, stackCornerTick)
		}
	}
}

func (b *meshBuilder) drawStackRays(decks []explodedDeck, l stackConnector, ticks bool) {
	inks := [...]string{
		stackFrontRay: "#52616b", stackBackRay: "#52616b",
		stackFrontTrace: "#62747b", stackBackTrace: "#62747b", stackCornerTick: "#4b5c64",
	}
	if readableSurfaceInk(b.palettePaper, 1) == "#f5f8fc" {
		inks[stackFrontRay], inks[stackBackRay] = "#becbd0", "#abbcc4"
	}
	if readableSurfaceInk(explodedDeckFill(decks[l.to].scene), 1) == "#f5f8fc" {
		inks[stackFrontTrace], inks[stackBackTrace], inks[stackCornerTick] = "#c3d1d4", "#c3d1d4", "#d1dbde"
	}
	var mats [5]*Material
	for i, k := range []float64{.83, .57, .57, .42, .85} {
		mats[i] = nativeMaterial(inks[i], 1, 0, k*l.opacity)
		mats[i].Unlit = true
		mats[i].svgPointerPassthrough = true
	}
	view := nativeCameraAxes().direction
	stackRayStrokes(decks, l, ticks, func(a, c Vec, width float64, ink stackRayInk) {
		side := ncross(view, nsub(c, a))
		if nlen(side) < 1e-10 {
			return
		}
		side = nmul(nunit(side), width/2)
		if math.Abs(a.Y-c.Y) < 1e-10 {
			// The receiving tail and corner tick lie flat against the stock;
			// both sides of the stroke remain below its printed captions.
			side = nsub(side, nmul(view, side.Y/view.Y))
		}
		v := func(p Vec) Vertex { return Vertex{Position: p, Normal: view} }
		first := len(b.triangles)
		b.triangle(v(nsub(a, side)), v(nsub(c, side)), v(nadd(c, side)), mats[ink], false)
		b.triangle(v(nsub(a, side)), v(nadd(c, side)), v(nadd(a, side)), mats[ink], false)
		for i := first; i < len(b.triangles); i++ {
			b.triangles[i].NoDepthWrite = true
		}
	})
}
