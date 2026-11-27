package d2svg

import (
	"bytes"
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/svg"
	"github.com/d2lang/d2/lib/xsort"
)

type crossingEffect struct {
	id                string
	mask              bool
	replacesEmptyMask bool
}

func (c crossingEffect) attribute() svg.Attribute {
	name := "clip-path"
	if c.mask {
		name = "mask"
	}
	return svg.Attr(name, svg.LocalIRI(svg.LiteralID(c.id)))
}

type crossingViewport struct {
	left, top, width, height int
}

// Circle masks compress well for a few stationary crossings because they
// reuse the board mask's markup. Switch to paths before the masks accumulate,
// or immediately if a masked shaft or its board will animate.
const staticCrossingMaskElements = 48

func (v crossingViewport) writeCrossings(buf *bytes.Buffer, boardID string, crossings [][]d2target.ConnectionGap, diagram *d2target.Diagram, frame bool) map[int]crossingEffect {
	elements := 0
	animated := frame
	for i, gaps := range crossings {
		if len(gaps) != 0 {
			elements += 2 + len(gaps) // mask, white rectangle, black circles
			animated = animated || diagram.Connections[i].Animated
		}
	}
	if elements == 0 {
		return nil
	}
	defaultStyles := UsesDefaultStylesheet()
	useClips := defaultStyles && (animated || elements > staticCrossingMaskElements)
	// Each crossing effect covers exactly the board. Keep a separate label
	// mask only if a caption, icon or border label could cut the shaft.
	emptyMask := defaultStyles && !slices.ContainsFunc(diagram.Shapes, hasBorderLabel) &&
		!slices.ContainsFunc(diagram.Connections, func(c d2target.Connection) bool { return c.Label != "" || c.Icon != nil })

	effects := make(map[int]crossingEffect)
	for i, gaps := range crossings {
		if len(gaps) == 0 {
			continue
		}
		id := boardID + "-cg" + strconv.Itoa(i)
		if !defaultStyles {
			// Custom stylesheets may select the original groups and IDs.
			id = "crossings-" + boardID + "-" + strconv.Itoa(i)
		}
		effect := v.writeCrossing(buf, id, gaps, useClips)
		effect.replacesEmptyMask = emptyMask
		effects[i] = effect
	}
	return effects
}

// The rectangle and the circular holes wind in opposite directions. Clipping
// the original shaft preserves its markers and its dash phase through each gap.
func (v crossingViewport) writeCrossing(buf *bytes.Buffer, id string, gaps []d2target.ConnectionGap, useClips bool) crossingEffect {
	effect := crossingEffect{id: id}
	if !useClips || !v.canClip(gaps) {
		effect.mask = true
		bounds := []svg.Attribute{svg.IntAttr("x", v.left), svg.IntAttr("y", v.top), svg.IntAttr("width", v.width), svg.IntAttr("height", v.height)}
		attrs := []svg.Attribute{svg.Attr("id", effect.id), svg.Attr("maskUnits", "userSpaceOnUse")}
		buf.WriteString(svg.OpenElement("mask", append(attrs, bounds...)...))
		buf.WriteString(svg.EmptyElement("rect", append(bounds, svg.Attr("fill", "white"))...))
		for _, g := range gaps {
			buf.WriteString(svg.EmptyElement("circle", svg.FloatAttr("cx", g.Center.X), svg.FloatAttr("cy", g.Center.Y), svg.FloatAttr("r", g.Radius), svg.Attr("fill", "black")))
		}
		buf.WriteString(svg.CloseElement("mask"))
		return effect
	}

	var path strings.Builder
	x := strconv.Itoa(v.left)
	path.WriteString("M" + x + " " + strconv.Itoa(v.top) + "h" + strconv.Itoa(v.width) + "v" + strconv.Itoa(v.height) + "H" + x + "Z")
	for _, g := range gaps {
		// SVG scales undersized circular arc radii to half the distance
		// between their endpoints. Only the diameter needs to vary per gap.
		r := svg.FormatFloat(min(1, g.Radius))
		d := svg.FormatFloat(2 * g.Radius)
		arc := r + " " + r + " 0 0 0 "
		path.WriteString("M" + svg.FormatFloat(g.Center.X-g.Radius) + " " + svg.FormatFloat(g.Center.Y) + "a" + arc + d + " 0 " + arc + "-" + d + " 0Z")
	}
	buf.WriteString(svg.OpenElement("clipPath", svg.Attr("id", effect.id)))
	buf.WriteString(svg.CompactEmptyElement("path", svg.Attr("d", path.String())))
	buf.WriteString(svg.CloseElement("clipPath"))
	return effect
}

func (v crossingViewport) canClip(gaps []d2target.ConnectionGap) bool {
	first := gaps[0].Center
	xmin, xmax, ymin, ymax := first.X, first.X, first.Y, first.Y
	for _, g := range gaps {
		x, y, r := g.Center.X, g.Center.Y, g.Radius
		// A hole outside the shell would add visible area beyond the viewport.
		if x-r < float64(v.left) || x+r > float64(v.left)+float64(v.width) ||
			y-r < float64(v.top) || y+r > float64(v.top)+float64(v.height) {
			return false
		}
		xmin, xmax, ymin, ymax = min(xmin, x), max(xmax, x), min(ymin, y), max(ymax, y)
	}
	if len(gaps) < 2 {
		return true
	}

	// Overlapping subpaths would reveal their intersection. Sweep along the
	// longer axis and keep the original mask for ambiguous or very dense sets.
	start := func(g d2target.ConnectionGap) float64 { return g.Center.X - g.Radius }
	if ymax-ymin > xmax-xmin {
		start = func(g d2target.ConnectionGap) float64 { return g.Center.Y - g.Radius }
	}
	sorted := slices.Clone(gaps)
	xsort.Func(sorted, func(a, b d2target.ConnectionGap) int { return cmp.Compare(start(a), start(b)) })
	budget := 16 * len(sorted)
	for i, a := range sorted {
		end := start(a) + 2*a.Radius
		for _, b := range sorted[i+1:] {
			if start(b) >= end {
				break
			}
			budget--
			if budget == 0 || math.Hypot(a.Center.X-b.Center.X, a.Center.Y-b.Center.Y) < a.Radius+b.Radius {
				return false
			}
		}
	}
	return true
}
