package d2isometricimg

import (
	"cmp"
	"iter"
	"maps"
	"math"
	"slices"
)

// Sweep through the ends of the rectangular openings. Each strip is solid
// stock, an open well, or a partially cut surface for a translucent link.
type stockCutEdge struct {
	x, back, front float64
	alpha, delta   int
}

type stockSpan struct {
	left, right                float64
	alpha, leftWall, rightWall int
}

func stockRow(edges []stockCutEdge, z, left, right float64, row []stockSpan) []stockSpan {
	row = row[:0]
	var active [256]int
	start, opening, count := left, 0, 0
	// Edges at the same x describe a single transition, even when two
	// openings meet. Overlapping cuts retain the most open of their sources.
	for i := 0; i < len(edges); {
		x := edges[i].x
		old := opening
		for i < len(edges) && edges[i].x == x {
			e := edges[i]
			if z >= e.back && z < e.front {
				count += e.delta
				active[e.alpha] += e.delta
				if e.delta > 0 {
					opening = max(opening, e.alpha)
				}
			}
			i++
		}
		if count == 0 {
			opening = 0
		} else {
			for active[opening] == 0 {
				opening--
			}
		}
		if opening != old {
			if start < x {
				row = append(row, stockSpan{left: start, right: x, alpha: 255 - old})
			}
			start = x
		}
	}
	if start < right {
		row = append(row, stockSpan{left: start, right: right, alpha: 255 - opening})
	}
	for i := range row {
		l, r := 0, 0
		if i > 0 {
			l = row[i-1].alpha
		}
		if i+1 < len(row) {
			r = row[i+1].alpha
		}
		row[i].leftWall = max(0, row[i].alpha-l)
		row[i].rightWall = max(0, row[i].alpha-r)
	}
	return row
}

func stockCutGrid(left, right, back, front float64, openings iter.Seq[stackAperture]) ([]float64, []stockCutEdge) {
	rows := []float64{back, front}
	var edges []stockCutEdge
	for h := range openings {
		l, r := max(left, h.center.X-h.width/2), min(right, h.center.X+h.width/2)
		z0, z1 := max(back, h.center.Z-h.depth/2), min(front, h.center.Z+h.depth/2)
		alpha := int(math.Round(255 * min(1., h.opacity)))
		if l >= r || z0 >= z1 || alpha <= 0 {
			continue
		}
		rows = append(rows, z0, z1)
		edges = append(edges, stockCutEdge{l, z0, z1, alpha, 1}, stockCutEdge{r, z0, z1, alpha, -1})
	}
	slices.Sort(rows)
	rows = slices.Compact(rows)
	slices.SortFunc(edges, func(a, c stockCutEdge) int { return cmp.Compare(a.x, c.x) })
	return rows, edges
}

func (b *meshBuilder) cutSlab(d *explodedDeck, stock *Material) {
	x, z := d.width/2, d.depth/2
	rows, edges := stockCutGrid(-x, x, -z, z, maps.Values(d.openings))
	if len(edges) == 0 {
		b.box(nv(0, d.surfaceY-d.thickness/2, 0), nv(d.width, d.thickness, d.depth), stock, 0)
		return
	}

	top, bottom := d.surfaceY, d.surfaceY-d.thickness
	// All pieces of the original stock share one gradient coordinate system.
	if stock.gradient != "" {
		texture := b.gradientTexture(stock.gradient)
		if b.err != nil {
			return
		}
		stock.Texture, stock.Vector = texture, nativeVectorForTexture(b.ctx, texture)
	}
	view := nativeCameraAxes().direction
	sx, sz := view.X/view.Y, view.Z/view.Y
	var materials [256]*Material
	materials[255] = stock
	quad := func(a, c, e, f Vec, alpha int) {
		if alpha == 0 {
			return
		}
		m := materials[alpha]
		if m == nil {
			v := *stock
			v.Color.A = uint8(math.Round(float64(stock.Color.A) * float64(alpha) / 255))
			m = &v
			materials[alpha] = m
		}
		normal := nunit(ncross(nsub(c, a), nsub(e, a)))
		vertex := func(p Vec) Vertex {
			return Vertex{Position: p, Normal: normal, U: (p.X + x + (top-p.Y)*sx) / (d.width + d.thickness*sx), V: (p.Z + z + (top-p.Y)*sz) / (d.depth + d.thickness*sz)}
		}
		va, vc, ve, vf := vertex(a), vertex(c), vertex(e), vertex(f)
		b.triangle(va, vc, ve, m, true)
		b.triangle(va, ve, vf, m, true)
	}
	wall := func(x0, z0, x1, z1 float64, alpha int) {
		quad(nv(x0, top, z0), nv(x1, top, z1), nv(x1, bottom, z1), nv(x0, bottom, z0), alpha)
	}
	caps := func(s stockSpan, back, front float64) {
		if s.alpha == 0 {
			return
		}
		l, r := s.left, s.right
		quad(nv(l, top, back), nv(l, top, front), nv(r, top, front), nv(r, top, back), s.alpha)
		quad(nv(l, bottom, back), nv(r, bottom, back), nv(r, bottom, front), nv(l, bottom, front), s.alpha)
		wall(l, front, l, back, s.leftWall)
		wall(r, back, r, front, s.rightWall)
	}
	b.stockStrips(-x, x, rows, edges, caps, wall)
}

// Use the same disjoint rectangles for the common stock and every printed
// supporting board between a nested hatch and that stock.
func (b *meshBuilder) stockStrips(left, right float64, rows []float64, edges []stockCutEdge, caps func(stockSpan, float64, float64), wall func(float64, float64, float64, float64, int)) {
	empty := stockSpan{left: left, right: right}
	prev := []stockSpan{empty}
	var next []stockSpan
	starts, continued := map[stockSpan]float64{}, map[stockSpan]float64{}
	for k, boundary := range rows {
		if err := b.ctx.Err(); err != nil {
			b.err = err
			return
		}
		if k+1 < len(rows) {
			next = stockRow(edges, boundary, left, right, next)
		} else {
			next = append(next[:0], empty)
		}
		// Keep unchanged strips as one rectangle across unrelated cuts.
		clear(continued)
		for _, s := range next {
			start, ok := starts[s]
			if !ok {
				start = boundary
			}
			continued[s] = start
		}
		for _, s := range prev {
			if _, ok := continued[s]; !ok {
				caps(s, starts[s], boundary)
			}
		}
		for i, j := 0, 0; wall != nil && i < len(prev) && j < len(next); {
			p, n := prev[i], next[j]
			l, r := max(p.left, n.left), min(p.right, n.right)
			if n.alpha > p.alpha {
				wall(l, boundary, r, boundary, n.alpha-p.alpha)
			}
			if p.alpha > n.alpha {
				wall(r, boundary, l, boundary, p.alpha-n.alpha)
			}
			if p.right <= n.right {
				i++
			}
			if n.right <= p.right {
				j++
			}
		}
		if b.err != nil {
			return
		}
		starts, continued = continued, starts
		prev, next = next, prev
	}
}
