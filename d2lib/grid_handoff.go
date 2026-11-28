package d2lib

import (
	"math"
	"reflect"
	"sort"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/animation"
	"github.com/d2lang/d2/lib/label"
)

const gridHandoffMS = 420

type gridHandoffs struct {
	pairs []*gridHandoff
	byID  map[string]*gridHandoff
}

type gridHandoff struct {
	from, to *d2target.Shape
	// Stable IDs for the prefix, outgoing text, and incoming text.
	textIDs [3]string
	prefix  int
}

func gridParentID(n *gridNode) string {
	if n.parent == nil {
		return "" // The board itself can be a grid.
	}
	return n.parent.shape.ID
}

// A fixed slot is meaningful only if the rest of the grid still identifies it.
// Changing the contents of a lone cell, or moving another member through the
// same position, uses the normal grid membership animation.
func detectGridHandoffs(old, next map[string]*gridNode) *gridHandoffs {
	type group struct {
		before, after   []*gridNode
		anchored, moved bool
	}
	groups := make(map[string]*group)
	add := func(nodes, other map[string]*gridNode, before bool) {
		for id, n := range nodes {
			if !n.gridCell {
				continue
			}
			parent := gridParentID(n)
			g := groups[parent]
			if g == nil {
				g = &group{}
				groups[parent] = g
			}
			if peer := other[id]; peer != nil {
				if !peer.gridCell || gridParentID(peer) != parent || !sameSlot(n.shape, peer.shape) {
					g.moved = true
				} else {
					g.anchored = true
				}
			} else if before {
				g.before = append(g.before, n)
			} else {
				g.after = append(g.after, n)
			}
		}
	}
	add(old, next, true)
	add(next, old, false)
	var pairs []*gridHandoff
	for _, g := range groups {
		if !g.anchored || g.moved || len(g.before) != 1 || len(g.after) != 1 {
			continue
		}
		a, b := g.before[0], g.after[0]
		if len(a.children) != 0 || len(b.children) != 0 || !handoffCompatible(a.shape, b.shape) {
			continue
		}
		pairs = append(pairs, &gridHandoff{from: a.shape, to: b.shape, prefix: handoffPrefix(a.shape, b.shape)})
	}
	if len(pairs) == 0 {
		return nil
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].to.ID < pairs[j].to.ID })
	h := &gridHandoffs{pairs: pairs, byID: make(map[string]*gridHandoff, 2*len(pairs))}
	used := make(map[string]bool)
	for _, p := range pairs {
		h.byID[p.from.ID], h.byID[p.to.ID] = p, p
		for i, suffix := range []string{"prefix", "out", "in"} {
			id := p.to.ID + ".__handoff_" + suffix
			for old[id] != nil || next[id] != nil || used[id] {
				id += "_"
			}
			p.textIDs[i], used[id] = id, true
		}
	}
	return h
}

func sameSlot(a, b *d2target.Shape) bool {
	return a.Pos == b.Pos && a.Width == b.Width && a.Height == b.Height
}

func handoffCompatible(from, to *d2target.Shape) bool {
	for _, s := range []*d2target.Shape{from, to} {
		if s.Type != d2target.ShapeRectangle || s.Icon != nil || s.Link != "" || s.Language != "" || s.Label == "" ||
			strings.ContainsAny(s.Label, "\r\n\t") || s.LabelFill != "" || s.LabelPosition != label.InsideMiddleCenter.String() ||
			s.LabelWidth <= 0 || s.LabelHeight <= 0 || s.Width < s.LabelWidth || s.Height < s.LabelHeight {
			return false
		}
	}
	// A differently styled replacement should retain its own entry and exit.
	a, b := *from, *to
	a.ID, a.Label, a.LabelWidth = b.ID, b.Label, b.LabelWidth
	return reflect.DeepEqual(a, b)
}

// A monospace identifier of the same length keeps the shared glyphs in exactly
// the same columns. For proportional fonts and non-ASCII text, move whole labels
// so shaping and grapheme clusters are preserved by the renderer.
func handoffPrefix(a, b *d2target.Shape) int {
	if a.FontFamily != "mono" || len(a.Label) != len(b.Label) || a.LabelWidth != b.LabelWidth {
		return 0
	}
	for i := range len(a.Label) {
		if a.Label[i] < '!' || a.Label[i] > '~' || b.Label[i] < '!' || b.Label[i] > '~' {
			return 0
		}
	}
	i := 0
	for i < len(a.Label) && a.Label[i] == b.Label[i] {
		i++
	}
	return i
}

func (h *gridHandoffs) apply(shapes []d2target.Shape, elapsedMS, durationMS int) []d2target.Shape {
	if h == nil {
		return shapes
	}
	t := gridPhase(float64(elapsedMS), 0, float64(min(gridHandoffMS, durationMS)))
	result := make([]d2target.Shape, 0, len(shapes)+3*len(h.pairs))
	seen := make(map[*gridHandoff]bool, len(h.pairs))
	for _, s := range shapes {
		p := h.byID[s.ID]
		if p == nil {
			result = append(result, s)
		} else if !seen[p] {
			result = p.sample(result, t)
			seen[p] = true
		}
	}
	for _, p := range h.pairs {
		// On mixed edits the old bubble can be gone before the new one starts.
		if !seen[p] {
			result = p.sample(result, t)
		}
	}
	return result
}

func (p *gridHandoff) sample(shapes []d2target.Shape, t float64) []d2target.Shape {
	if t <= 0 {
		return append(shapes, *p.from)
	}
	if t >= 1 {
		return append(shapes, *p.to)
	}
	shell := *p.to
	if t < .5 {
		shell = *p.from
	}
	if p.from.Label == p.to.Label {
		return append(shapes, shell)
	}
	shell.Label = ""
	shapes = append(shapes, shell)
	if p.prefix > 0 {
		shapes = append(shapes, handoffText(p.from, p.textIDs[0], 0, p.prefix, 0, 1))
	}
	// The suffixes pass one another vertically. Keep the travel within the
	// authored slot, including the glyphs' ascenders and descenders.
	travel := max(0, min(float64(shell.LabelHeight), float64(shell.Height-shell.LabelHeight)/2))
	position := animation.EaseInOut(t)
	out := 1 - animation.EaseInOut(gridPhase(t, .18, .70))
	in := animation.EaseInOut(gridPhase(t, .30, .82))
	if out > 0 {
		shapes = append(shapes, handoffText(p.from, p.textIDs[1], p.prefix, len(p.from.Label), -travel*position, out))
	}
	if in > 0 {
		shapes = append(shapes, handoffText(p.to, p.textIDs[2], p.prefix, len(p.to.Label), travel*(1-position), in))
	}
	return shapes
}

func handoffText(s *d2target.Shape, id string, start, end int, dy, opacity float64) d2target.Shape {
	// Use the original measured bounds for a whole label. For a fixed-width
	// font each glyph occupies an equal fraction of those bounds.
	advance := float64(s.LabelWidth) / float64(len(s.Label))
	width := int(math.Round(advance * float64(end-start)))
	// Target boxes use integer coordinates, but centered text can sit on a
	// half pixel. Pad the box by one when needed to preserve that baseline.
	centerX2 := 2*s.Pos.X + s.Width - s.LabelWidth + int(math.Round(float64(start+end)*advance))
	boxWidth := width + (centerX2-width)&1
	boxHeight := s.LabelHeight + (s.Height-s.LabelHeight)%2
	x := (centerX2 - boxWidth) / 2
	y := s.Pos.Y + (s.Height-boxHeight)/2 + int(math.Round(dy))
	text := s.Text
	text.Label, text.LabelWidth = s.Label[start:end], width
	return d2target.Shape{
		ID: id, Type: d2target.ShapeText, Pos: d2target.Point{X: x, Y: y},
		Width: boxWidth, Height: boxHeight, Text: text, LabelPosition: s.LabelPosition,
		Opacity: s.Opacity * opacity, Level: s.Level, ZIndex: s.ZIndex,
	}
}

// Both IDs identify this stationary box while incident connections crossfade.
// Supply both to the route sampler without adding an invisible shape to the SVG.
func (h *gridHandoffs) edgeShapes(shapes []d2target.Shape) []d2target.Shape {
	if h != nil {
		for _, p := range h.pairs {
			shapes = append(shapes, *p.from, *p.to)
		}
	}
	return shapes
}

// Keep each exported route attached to the painted slot. Both native renderers
// use the endpoint shape's stroke width when trimming the arrowhead.
func (h *gridHandoffs) bindEdges(edges []d2target.Connection, elapsedMS, durationMS int) []d2target.Connection {
	if h == nil || len(edges) == 0 {
		return edges
	}
	before := elapsedMS < (min(gridHandoffMS, durationMS)+1)/2
	rebind := func(id string) string {
		if p := h.byID[id]; p != nil {
			if before {
				return p.from.ID
			}
			return p.to.ID
		}
		return id
	}
	result := make([]d2target.Connection, 0, len(edges))
	for _, e := range edges {
		if e.Opacity == 0 && (h.byID[e.Src] != nil || h.byID[e.Dst] != nil) {
			continue
		}
		e.Src, e.Dst = rebind(e.Src), rebind(e.Dst)
		result = append(result, e)
	}
	return result
}
