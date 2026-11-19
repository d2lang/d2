package d2isometricimg

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2parser"
	"github.com/d2lang/d2/d2renderers/d2isometric"
)

// A board link names a floor, not a shape on that floor. The leader starts
// on the linked source object, and its shaft terminates at a floor socket.
type stackConnector struct {
	from, to int
	source   Vec
	opacity  float64
}

type stackWire struct {
	captions            []labelSurface
	visible             bool
	anchor, left, right Vec
}

// Capture the ribbon after lane separation, bridging, endpoint clipping,
// dashing and caption knockouts. Only actual stroke triangles participate;
// the surrounding paper casing and unpainted caption gaps are not wire ink.
func (b *meshBuilder) captureStackWire(index int, edge d2isometric.Edge, ink *Material, first int, path []Vec, lengths []float64) {
	if b.options.stackWires == nil || len(path) < 2 || b.err != nil || !nativeRouteVisible(edge) {
		return
	}
	wire := &b.options.stackWires[index]
	axis := nativeCameraAxes().right
	lo, hi := math.Inf(1), math.Inf(-1)
	var mid Vec
	best := math.Inf(1)
	if edge.Link != "" {
		mid = pathPoint(path, lengths, .5)
	}
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
		wire.visible = true
		for k, vertex := range tri.V {
			p := vertex.Position
			x := ndot(axis, p)
			if x < lo {
				lo, wire.left = x, p
			}
			if x > hi {
				hi, wire.right = x, p
			}
			if edge.Link == "" {
				continue
			}
			// For a uniform ribbon the shared diagonal crosses the
			// centerline. Clipped or dashed ink still offers an actual
			// contact point on the nearest surviving painted triangle.
			delta := nsub(tri.V[(k+1)%3].Position, p)
			t := max(0., min(1., ndot(nsub(mid, p), delta)/max(1e-24, ndot(delta, delta))))
			q := nadd(p, nmul(delta, t))
			v := nsub(q, mid)
			if distance := ndot(v, v); distance < best {
				best, wire.anchor = distance, q
			}
		}
	}
}

func (b *meshBuilder) captureStackCaption(edge int, surface labelSurface) {
	if b.options.stackWires != nil {
		w := &b.options.stackWires[edge]
		w.captions = append(w.captions, surface)
	}
}

type stackObstacle struct {
	nativeLaneInterval
	floor         int
	upperEndpoint bool // encountered at an upper-floor port
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

func (b *meshBuilder) addStackConnectors(decks []explodedDeck) error {
	if len(decks) < 2 {
		return nil
	}
	index := indexStackFloors(decks)
	var links []stackConnector
	for i, deck := range decks {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		for _, n := range deck.scene.Nodes {
			if n.Opacity*255 < .5 || n.Link == "" || hierarchySpacer(n) {
				continue
			}
			to, ok := index.destination(n.Link, i)
			if !ok {
				continue
			}
			start := nadd(n.Position, deck.offset)
			start.Y = max(deck.surfaceY+.006, start.Y-n.Size.Y/2+.012)
			if n.Container {
				start.Z += n.Size.Z / 2
			}
			links = append(links, stackConnector{i, to, start, n.Opacity})
		}
		for j, wire := range deck.wires {
			e := deck.scene.Edges[j]
			if !wire.visible || e.Opacity*255 < .5 || e.Link == "" {
				continue
			}
			if to, ok := index.destination(e.Link, i); ok {
				links = append(links, stackConnector{i, to, nadd(wire.anchor, deck.offset), e.Opacity})
			}
		}
	}
	if len(links) == 0 {
		return nil
	}
	if err := b.reserveStackTransfers(links); err != nil {
		return err
	}
	unit := stackOpticalScale(decks[0].width)
	front := max(0, decks[0].depth/2-.18*unit)
	side := max(0, decks[0].width/2-.17*unit)
	right := nativeCameraAxes().right
	column := func(p Vec) float64 { return p.X + (p.Z-front)*right.Z/right.X }
	sort.SliceStable(links, func(i, j int) bool { return column(links[i].source) < column(links[j].source) })
	// In the fixed camera, every vertical shaft occupies one projected
	// column. Reserve the footprints on the lower and intervening floors;
	// the upper slab already hides the wire behind its own artwork.
	var obstacles []stackObstacle
	for i, deck := range decks {
		obstacles = appendStackObstacles(obstacles, deck, i, column)
	}
	sort.Slice(obstacles, func(i, j int) bool { return obstacles[i].lo < obstacles[j].lo })
	var blocked []nativeLaneInterval
	lastColumn := column(nv(side, 0, -front))
	spacing := min(.32*unit, (lastColumn+side)/float64(max(1, len(links)-1)))
	previous := -side - spacing
	for i, link := range links {
		if err := b.ctx.Err(); err != nil {
			return err
		}
		hi := lastColumn - float64(len(links)-1-i)*spacing
		lo := min(previous+spacing, hi)
		upper, lower := min(link.from, link.to), max(link.from, link.to)
		blocked = blocked[:0]
		for _, o := range obstacles {
			if o.floor < upper || o.floor > lower || o.floor == upper && !o.upperEndpoint {
				continue
			}
			last := len(blocked) - 1
			if last >= 0 && o.lo <= blocked[last].hi {
				blocked[last].hi = max(blocked[last].hi, o.hi)
			} else {
				blocked = append(blocked, o.nativeLaneInterval)
			}
		}
		port := stackGutterPort(link.source, side, front, lo, hi, blocked, column)
		previous = column(port)
		b.drawStackConnector(decks[link.from], decks[link.to], link, port, decks[upper+1:lower]...)
		if b.err != nil {
			return b.err
		}
	}
	return nil
}

// Prefer the shortest clear lead to a common front or side margin. Even on
// a crowded floor where every column is occupied, ports stay inside the slab.
func stackGutterPort(source Vec, side, front, low, high float64, blocked []nativeLaneInterval, column func(Vec) float64) Vec {
	best, cost, clear := Vec{}, math.Inf(1), false
	gutter := func(a, c Vec) {
		first, last := column(a), column(c)
		lo, hi := max(low, first), min(high, last)
		if lo > hi+1e-9 || last <= first {
			return
		}
		delta := nsub(c, a)
		fraction := max(0., min(1., ndot(nsub(source, a), delta)/ndot(delta, delta)))
		preferred := first + fraction*(last-first)
		placed, ok := clearStackColumn(max(lo, min(hi, preferred)), lo, hi, blocked)
		p := nadd(a, nmul(delta, (placed-first)/(last-first)))
		distance := math.Hypot(source.X-p.X, source.Z-p.Z)
		if ok && !clear || ok == clear && distance < cost {
			best, cost, clear = p, distance, ok
		}
	}
	gutter(nv(-side, 0, front), nv(side, 0, front))
	gutter(nv(-side, 0, front), nv(-side, 0, -front))
	gutter(nv(side, 0, front), nv(side, 0, -front))
	return best
}

func clearStackColumn(x, lo, hi float64, blocked []nativeLaneInterval) (float64, bool) {
	i := sort.Search(len(blocked), func(i int) bool { return blocked[i].hi > x })
	if i == len(blocked) || x <= blocked[i].lo {
		return x, true
	}
	left, right := blocked[i].lo, blocked[i].hi
	if left >= lo && (right > hi || x-left <= right-x) {
		return left, true
	}
	if right <= hi {
		return right, true
	}
	return x, false
}

func appendStackObstacles(out []stackObstacle, deck explodedDeck, floor int, column func(Vec) float64) []stackObstacle {
	// Express every obstacle on the common front row. A world point and
	// its X on that row share the same screen X through the whole stack.
	// Obstacles use equivalent X on the front row. Project the circular
	// receiving iris (the widest optical part), plus a small air gap.
	clearance := stackIrisRadius*stackOpticalScale(deck.width)/nativeCameraAxes().right.X + .025
	add := func(surface labelSurface) {
		if surface.width <= 0 || surface.depth <= 0 {
			return
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, p := range surfaceCorners(surface) {
			x := column(p)
			lo, hi = min(lo, x), max(hi, x)
		}
		out = append(out, stackObstacle{nativeLaneInterval: nativeLaneInterval{lo - clearance, hi + clearance}, floor: floor})
	}
	start := len(out)
	add(deck.title)
	if len(out) > start {
		out[start].upperEndpoint = true
	}
	for _, n := range deck.scene.Nodes {
		if n.Opacity <= 0 || hierarchySpacer(n) {
			continue
		}
		if !n.Container {
			add(labelSurface{center: nadd(n.Position, deck.offset), width: n.Size.X, depth: n.Size.Z})
		}
		s := n.Metadata.Original
		if n.Label != "" && s.Width > 0 && s.Height > 0 {
			caption := nativeNodeLabelSurface(n, s, 0)
			caption.center = nadd(caption.center, deck.offset)
			add(caption)
		}
	}
	for _, wire := range deck.wires {
		for _, caption := range wire.captions {
			caption.center = nadd(caption.center, deck.offset)
			add(caption)
		}
		if wire.visible {
			lo := column(nadd(wire.left, deck.offset))
			hi := column(nadd(wire.right, deck.offset))
			out = append(out, stackObstacle{nativeLaneInterval: nativeLaneInterval{lo - clearance, hi + clearance}, floor: floor})
		}
	}
	return out
}

// Optical transfers use a filled emitting lens and an open receiving iris.
// A crossed board only has a plain through-collar, with no new connection.
const (
	stackTracePaint = "#956038"
	stackLightPaint = "#e7b575"
	stackRingSides  = 20
	stackIrisRadius = .157
	stackIrisCut    = .04
	stackWaveStart  = .045
	stackWaveEnd    = .59
	stackBeamAlpha  = .61
	stackGlintAlpha = .90
)

// Reject stacks already over the common mesh limit before allocating any
// transfer triangles. Reserve once for the surviving stack; at most three
// wavefronts per link can be absent where they intersect another plate.
func (b *meshBuilder) reserveStackTransfers(links []stackConnector) error {
	minimum, maximum := 0, 0
	for _, link := range links {
		spans := max(link.from, link.to) - min(link.from, link.to)
		// Emitter bezel and lens (3 triangles/sector), split iris
		// (two arcs of nine sectors), receiver bore (3/sector), leader.
		faces := 6*stackRingSides + 4*stackArcSteps(stackIrisCut, .5-stackIrisCut) + 2
		faces += (spans - 1) * 2 * stackRingSides // plain collars
		for _, alpha := range [...]float64{stackBeamAlpha, stackGlintAlpha} {
			if link.opacity*255*alpha >= .5 {
				faces += 2 * spans
			}
		}
		minimum += faces
		maximum += faces + 3*2*stackArcSteps(stackWaveStart, stackWaveEnd)
	}
	room := maxNativeTriangles - len(b.triangles)
	if minimum > room {
		return fmt.Errorf("isometric exploded transfers exceed %d triangles", maxNativeTriangles)
	}
	b.triangles = slices.Grow(b.triangles, min(maximum, room))
	return nil
}

func stackOpticalScale(width float64) float64 {
	// Keep the apertures readable on wide plates without taking over
	// the reserved gutter on smaller ones.
	return max(1., min(2.5, width/8))
}

func (b *meshBuilder) drawStackConnector(from, to explodedDeck, link stackConnector, port Vec, crossed ...explodedDeck) {
	paint := func(color string, alpha float64) *Material {
		m := nativeMaterial(color, 1, 0, link.opacity*alpha)
		m.Unlit = true
		return m
	}
	copper := paint(stackTracePaint, 1)
	gold := paint("#dab072", 1)
	light := paint(stackLightPaint, stackBeamAlpha)
	gleam := paint("#ffebbc", stackGlintAlpha)
	wave := paint("#ac7447", .87)
	unit := stackOpticalScale(from.width)
	lens := paint("#fff0c3", 1)
	throat := paint("#38494d", 1)

	at := func(y float64) Vec { return nv(port.X, y, port.Z) }
	const lift = .008
	source := at(from.surfaceY + lift)
	receiver := at(to.surfaceY + lift)
	b.routeInk([]Vec{link.source, source}, .016*unit, copper)
	// A single polished lens emits. Disjoint concentric surfaces keep
	// authored translucency from accumulating in the glass and its bezel.
	b.stackRing(source, .052*unit, .087*unit, 0, 1, copper)
	b.stackRing(source, 0, .052*unit, 0, 1, lens)
	// The brass-edged dark bore is surrounded by a two-piece receiving
	// iris. The narrow breaks distinguish the outer rim at small sizes.
	b.stackRing(receiver, .120*unit, stackIrisRadius*unit, stackIrisCut, .5-stackIrisCut, copper)
	b.stackRing(receiver, .120*unit, stackIrisRadius*unit, .5+stackIrisCut, 1-stackIrisCut, copper)
	b.stackRing(receiver, .053*unit, .085*unit, 0, 1, gold)
	b.stackRing(receiver, 0, .053*unit, 0, 1, throat)

	emitY, receiveY := from.surfaceY-from.thickness, to.surfaceY+lift
	if from.surfaceY < to.surfaceY {
		emitY, receiveY = from.surfaceY+lift, to.surfaceY-to.thickness
	}
	radius := func(y float64) float64 {
		progress := max(0., min(1., (y-emitY)/(receiveY-emitY)))
		return (.026 + .092*progress) * unit
	}
	top, bottom := from, to
	if top.surfaceY < bottom.surfaceY {
		top, bottom = bottom, top
	}
	// Each span inherits the global opening angle. A crossed floor
	// interrupts the material without restarting the light's progression.
	next := top
	for _, d := range crossed {
		b.stackLightSpan(port, d.surfaceY+lift, next.surfaceY-next.thickness, radius, light, gleam)
		b.stackRing(at(d.surfaceY+lift), .068*unit, .101*unit, 0, 1, copper)
		next = d
	}
	b.stackLightSpan(port, bottom.surfaceY+lift, next.surfaceY-next.thickness, radius, light, gleam)

	// Short, increasingly broad wavefronts face the receiver. Omit a
	// pulse at a crossed plate so only the distinct through-collar remains.
	for _, t := range [...]float64{.33, .70, .87} {
		y := emitY + (receiveY-emitY)*t
		visible := true
		for _, d := range crossed {
			if y >= d.surfaceY-d.thickness-.08*unit && y <= d.surfaceY+.08*unit {
				visible = false
				break
			}
		}
		if visible {
			r := radius(y) * 1.30
			b.stackRing(at(y), r-.019*unit, r, stackWaveStart, stackWaveEnd, wave)
		}
	}
}

// The main face diverges from the emitter. A narrow facet at its edge
// catches the light. Both are unlit, so dark and light boards receive the
// same optical material; neither relies on a background-specific gradient.
func (b *meshBuilder) stackLightSpan(port Vec, low, high float64, radius func(float64) float64, light, gleam *Material) {
	if high <= low {
		return
	}
	right := nativeCameraAxes().right
	at := func(y, x float64) Vec { return nadd(nv(port.X, y, port.Z), nmul(right, x*radius(y))) }
	strip := func(left, right float64, material *Material) {
		a, c, d, e := at(low, left), at(low, right), at(high, right), at(high, left)
		b.flat(a, c, d, material, false)
		b.flat(a, d, e, material, false)
	}
	strip(-1, .68, light)
	strip(.68, 1, gleam)
}

// Distances are in the deck's physical plane; arc ends are fractions of a
// turn. Facing the camera from above also retains translucent rings in SVG.
func stackArcSteps(start, end float64) int { return int(math.Ceil(stackRingSides * (end - start))) }

func (b *meshBuilder) stackRing(center Vec, inner, outer, start, end float64, m *Material) {
	steps := stackArcSteps(start, end)
	point := func(i int, r float64) Vec {
		angle := 2 * math.Pi * (start + float64(i)/float64(steps)*(end-start))
		return nadd(center, nv(r*math.Cos(angle), 0, r*math.Sin(angle)))
	}
	for i := 0; i < steps; i++ {
		a, c := point(i, outer), point(i+1, outer)
		if inner == 0 {
			b.flat(c, a, center, m, false)
			continue
		}
		d, e := point(i+1, inner), point(i, inner)
		b.flat(c, a, d, m, false)
		b.flat(d, a, e, m, false)
	}
}
