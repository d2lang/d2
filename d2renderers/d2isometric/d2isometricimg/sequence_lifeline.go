package d2isometricimg

import (
	"cmp"
	"math"
	"slices"
	"sort"

	"github.com/d2lang/d2/d2renderers/d2isometric"
)

// A node's printed quad moves with the mesh when hierarchy relief is applied.
type sequenceNodePrint struct {
	triangle int
	opacity  float64
}

type sequenceTextArea struct {
	left, right, top, bottom, maxRight, opacity float64
}

func (b *meshBuilder) addSequenceText(s labelSurface, opacity float64) {
	if s.width <= 0 || s.depth <= 0 || opacity <= 0 {
		return
	}
	center := captionProjection(s.center)
	padding := 2 * b.scale
	b.sequenceText = append(b.sequenceText, sequenceTextArea{
		left: center.x - s.width/2 - padding, right: center.x + s.width/2 + padding,
		top: center.z - s.depth/2 - padding, bottom: center.z + s.depth/2 + padding, opacity: opacity,
	})
}

func (b *meshBuilder) indexSequenceText() {
	// Node relief and board placement have now transformed every printed quad.
	for _, print := range b.sequencePrints {
		v := b.triangles[print.triangle].V
		b.addSequenceText(labelSurface{
			center: nlerp(v[0].Position, v[2].Position, .5),
			width:  nlen(nsub(v[2].Position, v[1].Position)),
			depth:  nlen(nsub(v[1].Position, v[0].Position)),
		}, print.opacity)
	}
	slices.SortFunc(b.sequenceText, func(a, b sequenceTextArea) int { return cmp.Compare(a.left, b.left) })
	right := math.Inf(-1)
	for i := range b.sequenceText {
		right = max(right, b.sequenceText[i].right)
		b.sequenceText[i].maxRight = right
	}
}

type lifelineInkWindow struct {
	start, end, opacity float64
}

// Find the visible fractions of a v2 lifeline in the final camera projection.
// Captions lie at different heights above the wire. Account for that parallax
// before clipping its ink, then sample dashes against their original route.
func (b *meshBuilder) lifelineInkWindows(edge d2isometric.Edge, wire []Vec, radius float64) []lifelineInkWindow {
	if edge.Metadata.Original.SequenceRole != "lifeline" || len(wire) != 2 || wire[0].X != wire[1].X || wire[0].Z == wire[1].Z {
		return nil
	}
	first, last := captionProjection(wire[0]), captionProjection(wire[1])
	type boundary struct {
		at, transmission float64
		enter            bool
	}
	var boundaries []boundary
	i := sort.Search(len(b.sequenceText), func(i int) bool { return b.sequenceText[i].maxRight > first.x-radius })
	for _, text := range b.sequenceText[i:] {
		if text.left >= first.x+radius {
			break
		}
		if text.right <= first.x-radius {
			continue
		}
		a := (text.top - first.z) / (last.z - first.z)
		z := (text.bottom - first.z) / (last.z - first.z)
		lo, hi := max(0, min(a, z)), min(1, max(a, z))
		if lo < hi {
			transmission := max(0, 1-text.opacity)
			boundaries = append(boundaries, boundary{lo, transmission, true}, boundary{hi, transmission, false})
		}
	}
	if len(boundaries) == 0 {
		return nil
	}
	slices.SortStableFunc(boundaries, func(a, b boundary) int { return cmp.Compare(a.at, b.at) })
	windows := make([]lifelineInkWindow, 0, len(boundaries)+1)
	start, logAlpha, opaque := 0., 0., 0
	for _, boundary := range boundaries {
		if boundary.at > start && opaque == 0 {
			windows = append(windows, lifelineInkWindow{start, boundary.at, math.Exp(min(0, logAlpha))})
		}
		if boundary.transmission == 0 {
			if boundary.enter {
				opaque++
			} else {
				opaque--
			}
		} else if boundary.enter {
			logAlpha += math.Log(boundary.transmission)
		} else {
			logAlpha -= math.Log(boundary.transmission)
		}
		start = boundary.at
	}
	if start < 1 {
		windows = append(windows, lifelineInkWindow{start, 1, 1})
	}
	return windows
}

// Keep each original dash in place, drawing only its visible pieces. The
// cursor advances monotonically even when many captions cross the same actor.
func (b *meshBuilder) sequenceWirePainter(edge d2isometric.Edge, wire []Vec, lengths []float64, radius float64, ink *Material) func(float64, float64) {
	draw := func(start, end float64, material *Material) {
		if start == 0 && end == 1 {
			b.routeInk(wire, radius, material)
		} else {
			b.routeInk(nativeRouteSection(wire, lengths, start, end), radius, material)
		}
	}
	windows := b.lifelineInkWindows(edge, wire, radius)
	if windows == nil {
		return func(start, end float64) { draw(start, end, ink) }
	}
	return func(start, end float64) {
		for len(windows) > 0 && windows[0].end <= start {
			windows = windows[1:]
		}
		for _, w := range windows {
			if w.start >= end {
				break
			}
			material := ink
			if w.opacity < 1 {
				faded := *ink
				faded.Color.A = uint8(math.Round(float64(faded.Color.A) * w.opacity))
				material = &faded
			}
			draw(max(start, w.start), min(end, w.end), material)
		}
	}
}
