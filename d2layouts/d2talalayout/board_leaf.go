package d2talalayout

import (
	"context"
	"image"
	"math"
	"slices"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/limits"
	"github.com/d2lang/d2/lib/geo"
)

// placeBoardLeaf tries a single straight connection from a free face of host.
// The caller has matched and checked the existing graph, including decorations.
// The boxes and routes are the preceding board's complete, read-only geometry.
func placeBoardLeaf(ctx context.Context, host, leaf *geo.Box, boxes []*geo.Box, routes [][]*geo.Point, preferred geo.Orientation, leafIsSource bool) (*geo.Box, []*geo.Point) {
	var hostRect, envelope image.Rectangle
	others := make([]image.Rectangle, 0, len(boxes))
	for _, box := range boxes {
		// The exporter truncates shape coordinates and dimensions independently.
		body := decorationPixels(box)
		if box == host {
			hostRect = body
		} else {
			others = append(others, body)
		}
		envelope = envelope.Union(body)
	}
	var segments []geo.Segment
	for _, route := range routes {
		for i, p := range route {
			envelope.Min.X = min(envelope.Min.X, int(math.Floor(p.X)))
			envelope.Min.Y = min(envelope.Min.Y, int(math.Floor(p.Y)))
			envelope.Max.X = max(envelope.Max.X, int(math.Ceil(p.X)))
			envelope.Max.Y = max(envelope.Max.Y, int(math.Ceil(p.Y)))
			if i > 0 {
				segments = append(segments, geo.Segment{Start: route[i-1], End: p})
			}
		}
	}
	if leafIsSource {
		preferred = preferred.GetOpposite()
	}
	faces := []geo.Orientation{geo.Bottom, geo.Right, geo.Top, geo.Left}
	if i := slices.Index(faces, preferred); i >= 0 {
		faces[0], faces[i] = faces[i], faces[0]
	}
	var best *geo.Box
	var route []*geo.Point
	bestArea := math.MaxInt
	// Shorter edges and the flow direction win ties in occupied area. Larger
	// gaps can place the leaf beyond a nearby obstacle while its wire stays clear.
	for _, gap := range []int{48, layoutgraph.ConnectedNodeGap, 80, 100} {
		for _, face := range faces {
			if face == preferred.GetOpposite() {
				continue
			}
			if ctx.Err() != nil {
				return nil, nil
			}
			body, wire := boardLeafAt(hostRect, int(leaf.Width), int(leaf.Height), face, gap)
			if body.Empty() {
				continue
			}
			bounds := envelope.Union(body)
			if bounds.Dx() > limits.MaxGraphSize || bounds.Dy() > limits.MaxGraphSize ||
				body.Min.X < -limits.MaxGraphSize || body.Min.Y < -limits.MaxGraphSize ||
				body.Max.X > limits.MaxGraphSize || body.Max.Y > limits.MaxGraphSize {
				continue
			}
			area := bounds.Dx() * bounds.Dy()
			if area >= bestArea || !boardLeafFits(body, wire, others, segments) {
				continue
			}
			bestArea = area
			best = geo.NewBox(geo.NewPoint(float64(body.Min.X), float64(body.Min.Y)), leaf.Width, leaf.Height)
			route = []*geo.Point{wire.Start, wire.End}
		}
	}
	if best != nil && leafIsSource {
		route[0], route[1] = route[1], route[0]
	}
	return best, route
}

func boardLeafDimension(n float64) bool { return n >= 1 && n <= limits.MaxGraphSize }

func boardLeafPoint(p *geo.Point) bool {
	return p != nil && math.Abs(p.X) <= limits.MaxGraphSize && math.Abs(p.Y) <= limits.MaxGraphSize
}

func boardLeafAt(host image.Rectangle, w, h int, face geo.Orientation, gap int) (image.Rectangle, geo.Segment) {
	x, y := float64(host.Min.X+host.Max.X)/2, float64(host.Min.Y+host.Max.Y)/2
	left, top := int(math.Round(x-float64(w)/2)), int(math.Round(y-float64(h)/2))
	a, b := geo.NewPoint(x, y), geo.NewPoint(x, y)
	// With differing odd/even face lengths the port on the new shape can be
	// half a pixel off center. Keep it strictly between the rendered corners.
	if face.IsHorizontal() && min(h, host.Dy()) < 2 || face.IsVertical() && min(w, host.Dx()) < 2 {
		return image.Rectangle{}, geo.Segment{}
	}
	switch face {
	case geo.Bottom:
		top = host.Max.Y + gap
		a.Y, b.Y = float64(host.Max.Y), float64(top)
	case geo.Right:
		left = host.Max.X + gap
		a.X, b.X = float64(host.Max.X), float64(left)
	case geo.Top:
		top = host.Min.Y - gap - h
		a.Y, b.Y = float64(host.Min.Y), float64(top+h)
	case geo.Left:
		left = host.Min.X - gap - w
		a.X, b.X = float64(host.Min.X), float64(left+w)
	}
	return image.Rect(left, top, left+w, top+h), geo.Segment{Start: a, End: b}
}

func boardLeafFits(body image.Rectangle, wire geo.Segment, others []image.Rectangle, segments []geo.Segment) bool {
	// The new wire is axis-aligned. Reserve clearance around its whole length,
	// including both endpoint markers, and around the leaf's painted bounds.
	const clearance = layoutgraph.MinArrowheadClearance
	a, b := wire.Start, wire.End
	corridor := image.Rect(int(math.Floor(min(a.X, b.X))), int(math.Floor(min(a.Y, b.Y))),
		int(math.Ceil(max(a.X, b.X))), int(math.Ceil(max(a.Y, b.Y)))).Inset(-clearance)
	room := body.Inset(-layoutgraph.NodeGap)
	for _, old := range others {
		if room.Overlaps(old) || corridor.Overlaps(old) {
			return false
		}
	}
	toBox := func(r image.Rectangle) *geo.Box {
		return geo.NewBox(geo.NewPoint(float64(r.Min.X), float64(r.Min.Y)), float64(r.Dx()), float64(r.Dy()))
	}
	for _, obstacle := range []*geo.Box{toBox(room), toBox(corridor)} {
		for _, old := range segments {
			// Intersects checks the rectangle's boundary. The endpoint checks
			// also catch short segments lying wholly inside it.
			if obstacle.Contains(old.Start) || obstacle.Contains(old.End) || obstacle.Intersects(old, 0) {
				return false
			}
		}
	}
	return true
}
