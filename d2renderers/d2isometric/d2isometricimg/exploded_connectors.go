package d2isometricimg

import (
	"math"
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
	floor int
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
			if n.Opacity <= 0 || n.Link == "" || hierarchySpacer(n) {
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
			if !wire.visible || e.Link == "" {
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
	front := max(0, decks[0].depth/2-.18)
	side := max(0, decks[0].width/2-.17)
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
	spacing := min(.28, (lastColumn+side)/float64(max(1, len(links)-1)))
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
			if o.floor <= upper || o.floor > lower {
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
		b.drawStackConnector(decks[link.from], decks[link.to], link, port)
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
	add := func(surface labelSurface) {
		if surface.width <= 0 || surface.depth <= 0 {
			return
		}
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, p := range surfaceCorners(surface) {
			x := column(p)
			lo, hi = min(lo, x), max(hi, x)
		}
		out = append(out, stackObstacle{nativeLaneInterval{lo - .125, hi + .125}, floor})
	}
	add(deck.title)
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
			out = append(out, stackObstacle{nativeLaneInterval{lo - .125, hi + .125}, floor})
		}
	}
	return out
}

func (b *meshBuilder) drawStackConnector(from, to explodedDeck, link stackConnector, port Vec) {
	ink := nativeMaterial("#bd492b", 1, 0, link.opacity)
	ink.Unlit = true
	x, z := port.X, port.Z
	fromPort := nv(x, from.surfaceY+.008, z)
	toPort := nv(x, to.surfaceY+.008, z)
	// The short lead lies on the source floor. It disappears behind solids
	// in the same visibility pass that clips the inter-floor shaft.
	b.routeInk([]Vec{link.source, fromPort}, .020, ink)
	b.stackSocket(fromPort, ink)
	b.stackSocket(toPort, ink)

	// A world-Y line projects vertically. Use the camera's horizontal
	// axis for its width: the usual horizontal wire normal degenerates
	// when applied to a vertical shaft. Both faces still use real depth.
	sign := math.Copysign(1, toPort.Y-fromPort.Y)
	fromY, toY := fromPort.Y, toPort.Y
	if sign < 0 {
		fromY = from.surfaceY - from.thickness
	} else {
		toY = to.surfaceY - to.thickness
	}
	across := nativeCameraAxes().right
	at := func(y, width float64) Vec {
		return nadd(nv(x, y, z), nmul(across, width))
	}
	stem := func(start, end float64) {
		a, c := at(start, -.018), at(start, .018)
		d, e := at(end, .018), at(end, -.018)
		b.flat(a, c, d, ink, false)
		b.flat(a, d, e, ink, false)
	}
	// An upward tip at the underside is hidden by the lip. Put it just
	// outside the lip's projected silhouette and continue the stem to
	// the socket. Downward tips meet the destination socket directly.
	tip := toY
	if sign > 0 {
		view := nativeViewDirection()
		lip := min((to.depth/2-z)/view.Z, (to.width/2-x)/view.X) * view.Y
		tip -= min(math.Abs(toY-fromY)*.32, lip+.035)
	}
	head := min(.24, math.Abs(tip-fromY)*.4)
	stem(fromY, tip-sign*head*.76)
	if sign > 0 {
		stem(tip, toY)
	}
	left, right := at(tip-sign*head, -head*.39), at(tip-sign*head, head*.39)
	notch := at(tip-sign*head*.76, 0)
	b.flat(at(tip, 0), left, notch, ink, false)
	b.flat(at(tip, 0), notch, right, ink, false)
}

func (b *meshBuilder) stackSocket(center Vec, ink *Material) {
	// An open socket distinguishes a board destination from a node.
	const sides = 16
	point := func(i int, radius float64) Vec {
		angle := 2 * math.Pi * float64(i%sides) / sides
		return nadd(center, nv(radius*math.Cos(angle), 0, radius*math.Sin(angle)))
	}
	for i := 0; i < sides; i++ {
		a, c := point(i, .082), point(i+1, .082)
		d, e := point(i+1, .053), point(i, .053)
		b.flat(a, c, d, ink, false)
		b.flat(a, d, e, ink, false)
	}
}
