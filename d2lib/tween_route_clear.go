package d2lib

import (
	"image"
	"math"
	"slices"
	"strings"

	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

func validTweenRoute(r []*geo.Point) bool {
	if len(r) < 2 || len(r) > maxTweenRoutePoints {
		return false
	}
	for _, p := range r {
		if !tweenFinitePoint(p) {
			return false
		}
	}
	return true
}

// Check the actual sample: a third shape can move through two stationary endpoints.
// TALA can already cross an overlapping key card or label. Only charge additional
// occlusion, comparing each obstacle individually against this sample's native wire.
func tweenRouteClear(frame *d2target.Diagram, edge *d2target.Connection, route []*geo.Point, endpoints tweenRouteShapePair) bool {
	if frame == nil || edge == nil || endpoints.src == nil || endpoints.dst == nil || edge.IsCurve || !validTweenRoute(route) || !validTweenRoute(edge.Route) {
		return false
	}
	// The router's own geometry is always a legal fallback, even where the
	// author has pinned overlapping objects or included a dense frame.
	if slices.EqualFunc(route, edge.Route, (*geo.Point).Equals) {
		return true
	}
	if len(frame.Shapes)+len(frame.Connections) > 512 {
		return false
	}
	candidate := *edge
	candidate.Route = route
	candidate.BorderRadius = tweenRouteCornerRadius(edge, route, endpoints)
	now := tweenRouteInkFor(&candidate, endpoints)
	var old *tweenRouteInk
	clearWithPort := func(r image.Rectangle, port int) bool {
		if r.Empty() {
			return true
		}
		stroke, decoration := now.overlap(r, port)
		if stroke == 0 && decoration == 0 {
			return true
		}
		if old == nil {
			original := tweenRouteInkFor(edge, endpoints)
			old = &original
		}
		wasStroke, wasDecoration := old.overlap(r, port)
		// Permit subpixel rounding only when there was already an intersection.
		return (stroke == 0 || wasStroke > 0 && stroke <= wasStroke+0.5) && decoration <= wasDecoration
	}
	clearBox := func(r image.Rectangle) bool { return clearWithPort(r, -1) }
	for i := range frame.Shapes {
		s := &frame.Shapes[i]
		if s.Opacity <= 0 {
			continue
		}
		self := s.ID == edge.Src || s.ID == edge.Dst
		ancestor := strings.HasPrefix(edge.Src, s.ID+".") || strings.HasPrefix(edge.Dst, s.ID+".")
		if !ancestor {
			// Boundary contact is required at each port. Interior crossings after
			// departure or before arrival still obscure the endpoint.
			pad := int(math.Ceil(s.RenderedStrokeWidth() / 2))
			port := -1
			if self {
				pad = -2 - now.pad
				// A marker rotates about this very border. Its axis-aligned
				// viewport can straddle the body even when its ink stays outside.
				port = 0
				if s.ID == edge.Dst {
					port = 1
				}
			}
			body := tweenShapeBody(frame, i, pad, !self)
			if !clearWithPort(tweenRouteClip(body, s.MotionClip), port) {
				return false
			}
		}
		if s.Label != "" {
			pos := label.FromString(s.LabelPosition)
			if self || ancestor || pos.IsOutside() || pos.IsBorder() || s.MotionLabelTopLeft != nil {
				if !clearBox(tweenRouteClip(tweenCaptionBox(s, pos), s.MotionClip)) {
					return false
				}
			}
		}
		if !clearBox(tweenRouteClip(tweenShapeIconBox(s), s.MotionClip)) {
			return false
		}
	}
	// A reroute can move its caption or icon onto an otherwise clear wire.
	// Compare only decorations which actually move; ordinary wire crossings
	// and multiple links sharing a port remain legal.
	var moved [][2]image.Rectangle
	if edge.Label != "" || edge.Icon != nil || edge.SrcLabel != nil || edge.DstLabel != nil {
		before, after := tweenRouteDecorations(edge), tweenRouteDecorations(&candidate)
		for i, box := range after {
			if box != before[i] && !box.Empty() {
				moved = append(moved, [2]image.Rectangle{before[i], box})
			}
		}
	}
	var shapes map[string]*d2target.Shape
	for i := range frame.Connections {
		other := &frame.Connections[i]
		if other.ID == edge.ID || other.Opacity <= 0 {
			continue
		}
		if len(moved) == 0 && other.Label == "" && other.Icon == nil && other.SrcLabel == nil && other.DstLabel == nil {
			continue
		}
		if !validTweenRoute(other.Route) || other.IsCurve && (len(other.Route)-1)%3 != 0 {
			return false
		}
		for _, r := range tweenRouteDecorations(other) {
			if !clearBox(tweenRouteClip(r, other.MotionClip)) {
				return false
			}
		}
		if len(moved) != 0 {
			if shapes == nil {
				shapes = gridEdgeShapeMap(frame.Shapes)
			}
			src, dst := shapes[other.Src], shapes[other.Dst]
			if src == nil || dst == nil {
				return false
			}
			ink := tweenRouteInkFor(other, tweenRouteShapePair{src, dst})
			for _, boxes := range moved {
				newLine, newArea := ink.overlap(tweenRouteClip(boxes[1], other.MotionClip), -1)
				if newLine == 0 && newArea == 0 {
					continue
				}
				oldLine, oldArea := ink.overlap(tweenRouteClip(boxes[0], other.MotionClip), -1)
				if newLine > 0 && (oldLine == 0 || newLine > oldLine+.5) || newArea > oldArea {
					return false
				}
			}
		}
	}
	return true
}

func tweenShapeBody(frame *d2target.Diagram, i, pad int, decorations bool) image.Rectangle {
	s := &frame.Shapes[i]
	body := image.Rect(s.Pos.X, s.Pos.Y, s.Pos.X+s.Width, s.Pos.Y+s.Height).Inset(-pad)
	if decorations && (s.ThreeDee || s.Multiple || s.Shadow || s.Type == d2target.ShapeC4Person || s.Tooltip != "" || s.Link != "") {
		// Stacked sheets, tilted faces and decorations extend beyond the layout rectangle.
		one := d2target.Diagram{Shapes: frame.Shapes[i : i+1], FontFamily: frame.FontFamily, MonoFontFamily: frame.MonoFontFamily}
		lo, hi := one.InkBoundingBox()
		body = body.Union(image.Rect(lo.X, lo.Y, hi.X, hi.Y))
	}
	return body
}

func tweenShapeIconBox(s *d2target.Shape) image.Rectangle {
	if s.Icon == nil || s.Type == d2target.ShapeImage {
		return image.Rectangle{}
	}
	icon := s.IconBounds(s.Geometry())
	return tweenRouteTextBox(icon.TopLeft, int(math.Ceil(icon.Width)), int(math.Ceil(icon.Height)))
}

func tweenRouteClip(r image.Rectangle, clip *d2target.MotionClip) image.Rectangle {
	if clip != nil {
		return r.Intersect(image.Rect(clip.X, clip.Y, clip.X+clip.Width, clip.Y+clip.Height))
	}
	return r
}

func tweenRouteTextBox(p *geo.Point, w, h int) image.Rectangle {
	if p == nil || w <= 0 || h <= 0 {
		return image.Rectangle{}
	}
	return image.Rect(int(math.Floor(p.X)), int(math.Floor(p.Y)), int(math.Ceil(p.X+float64(w))), int(math.Ceil(p.Y+float64(h))))
}

func tweenRouteDecorations(c *d2target.Connection) []image.Rectangle {
	var boxes []image.Rectangle
	if c.Label != "" {
		boxes = append(boxes, tweenRouteTextBox(c.GetLabelTopLeft(), c.LabelWidth, c.LabelHeight))
	}
	if c.Icon != nil {
		boxes = append(boxes, tweenRouteTextBox(c.GetIconPosition(), d2target.DEFAULT_ICON_SIZE, d2target.DEFAULT_ICON_SIZE))
	}
	if c.SrcLabel != nil && c.SrcLabel.Label != "" {
		boxes = append(boxes, tweenRouteTextBox(c.GetArrowheadLabelPosition(false), c.SrcLabel.LabelWidth, c.SrcLabel.LabelHeight))
	}
	if c.DstLabel != nil && c.DstLabel.Label != "" {
		boxes = append(boxes, tweenRouteTextBox(c.GetArrowheadLabelPosition(true), c.DstLabel.LabelWidth, c.DstLabel.LabelHeight))
	}
	return boxes
}

type tweenRouteLine struct {
	from, to geo.Point
}

type tweenRouteInk struct {
	lines       []tweenRouteLine
	decorations []image.Rectangle
	markers     [2]image.Rectangle
	pad         int
}

func tweenRouteInkFor(c *d2target.Connection, endpoints tweenRouteShapePair) tweenRouteInk {
	ink := tweenRouteInk{decorations: tweenRouteDecorations(c), pad: int(math.Ceil(c.RenderedStrokeWidth() / 2))}
	src, dst := c.ArrowheadAdjustmentsForWidths(endpoints.src.RenderedStrokeWidth(), endpoints.dst.RenderedStrokeWidth())
	// Include the marker viewport at each path end. A thick arrow can brush
	// a neighboring label even when its centerline clears the label.
	marker := func(arrow d2target.Arrowhead, port, next, inset *geo.Point, target bool) {
		if arrow == "" || arrow == d2target.NoArrowhead {
			return
		}
		dx, dy := next.X-port.X, next.Y-port.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			return
		}
		dx, dy = dx/l, dy/l
		w, h := arrow.Dimensions(c.RenderedStrokeWidth())
		overhang := 1.5 * c.RenderedStrokeWidth()
		if arrow == d2target.DiamondArrowhead {
			overhang = w/8 + .6*c.RenderedStrokeWidth()
			if target {
				overhang = .1*w + .6*c.RenderedStrokeWidth()
			}
			w *= 1.1
		}
		cx := port.X + inset.X + (w/2-overhang)*dx
		cy := port.Y + inset.Y + (w/2-overhang)*dy
		rx, ry := (math.Abs(dx)*w+math.Abs(dy)*h)/2, (math.Abs(dy)*w+math.Abs(dx)*h)/2
		portIndex := 0
		if target {
			portIndex = 1
		}
		ink.markers[portIndex] = image.Rect(int(math.Floor(cx-rx)), int(math.Floor(cy-ry)), int(math.Ceil(cx+rx)), int(math.Ceil(cy+ry)))
	}
	n := len(c.Route) - 1
	marker(c.SrcArrow, c.Route[0], c.Route[1], src, false)
	marker(c.DstArrow, c.Route[n], c.Route[n-1], dst, true)
	var last geo.Point
	for cmd := range c.PathCommands(src, dst) {
		switch cmd.Kind {
		case 'L':
			ink.lines = append(ink.lines, tweenRouteLine{last, cmd.End})
		case 'C', 'S':
			// Rounded orthogonal bends are cubic. Subdivide their control hulls
			// so the inside of a turn remains available to a nearby label.
			ink.addCurve(last, cmd.Control1, cmd.Control2, cmd.End, 0)
		}
		last = cmd.End
	}
	return ink
}

// The convex hull lies within half a unit of its chord when both control
// points do. Beyond the depth limit keep the whole hull as a conservative box.
func (ink *tweenRouteInk) addCurve(a, b, c, d geo.Point, depth int) {
	nearChord := func(p geo.Point) bool {
		x, y := d.X-a.X, d.Y-a.Y
		t := 0.
		if l := x*x + y*y; l != 0 {
			t = max(0., min(1., ((p.X-a.X)*x+(p.Y-a.Y)*y)/l))
		}
		return math.Hypot(p.X-a.X-t*x, p.Y-a.Y-t*y) <= .5
	}
	if nearChord(b) && nearChord(c) {
		ink.lines = append(ink.lines, tweenRouteLine{a, d})
		return
	}
	if depth == 3 {
		ink.decorations = append(ink.decorations, image.Rect(int(math.Floor(min(a.X, b.X, c.X, d.X))), int(math.Floor(min(a.Y, b.Y, c.Y, d.Y))), int(math.Ceil(max(a.X, b.X, c.X, d.X)))+1, int(math.Ceil(max(a.Y, b.Y, c.Y, d.Y)))+1).Inset(-ink.pad))
		return
	}
	mid := func(p, q geo.Point) geo.Point { return geo.Point{X: (p.X + q.X) / 2, Y: (p.Y + q.Y) / 2} }
	p, q, r := mid(a, b), mid(b, c), mid(c, d)
	u, v := mid(p, q), mid(q, r)
	m := mid(u, v)
	ink.addCurve(a, p, u, m, depth+1)
	ink.addCurve(m, v, r, d, depth+1)
}

func (ink *tweenRouteInk) decorationOverlap(r image.Rectangle, port int) float64 {
	area := 0.
	for _, box := range ink.decorations {
		v := box.Intersect(r)
		area += float64(v.Dx()) * float64(v.Dy())
	}
	for i, marker := range ink.markers {
		if i != port {
			v := marker.Intersect(r)
			area += float64(v.Dx()) * float64(v.Dy())
		}
	}
	return area
}

func (ink *tweenRouteInk) overlap(r image.Rectangle, port int) (float64, float64) {
	if r.Empty() {
		return 0, 0
	}
	var length float64
	// A half-unit curve approximation and integer stroke padding are included.
	minX, minY := float64(r.Min.X-ink.pad)-.5, float64(r.Min.Y-ink.pad)-.5
	maxX, maxY := float64(r.Max.X+ink.pad)+.5, float64(r.Max.Y+ink.pad)+.5
	for _, line := range ink.lines {
		a, b := line.from, line.to
		if max(a.X, b.X) < minX || min(a.X, b.X) > maxX || max(a.Y, b.Y) < minY || min(a.Y, b.Y) > maxY {
			continue
		}
		enter, leave := 0., 1.
		clip := func(x, y, lo, hi float64) {
			if x == y {
				return
			}
			p, q := (lo-x)/(y-x), (hi-x)/(y-x)
			enter = max(enter, min(p, q))
			leave = min(leave, max(p, q))
		}
		clip(a.X, b.X, minX, maxX)
		clip(a.Y, b.Y, minY, maxY)
		if enter < leave {
			length += (leave - enter) * math.Hypot(b.X-a.X, b.Y-a.Y)
		}
	}
	return length, ink.decorationOverlap(r, port)
}
