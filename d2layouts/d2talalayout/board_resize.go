package d2talalayout

import (
	"cmp"
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// TweenResize replaces the geometry of interstitial samples on an automatic
// resize. Both held boards have already gone through ordinary layout and board
// continuation. Their geometry is input only, even when another step follows.
func TweenResize(ctx context.Context, start, end *d2graph.Graph) (bool, error) {
	if ctx == nil {
		return false, errors.New("TALA tween layout requires a context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if start == nil || end == nil || start.Root == end.Root || len(end.TweenFrames) == 0 || len(end.TweenFrames) > d2target.MaxTweenFrames ||
		!validBoardGeometry(start, true) || !validBoardGeometry(end, true) ||
		start.Root.Direction.Value != end.Root.Direction.Value ||
		!reflect.DeepEqual(start.Data["tala-seeds"], end.Data["tala-seeds"]) ||
		len(start.Objects) != len(end.Objects) || len(start.Edges) != len(end.Edges) {
		return false, nil
	}

	first, last := resizeObjectIndex(start), resizeObjectIndex(end)
	wires := continuityEdges(start)
	resize := false
	for _, b := range end.Objects {
		a := first[b.AbsID()]
		if !sameResizeContent(a, b) {
			return false, nil
		}
		if boardSizeChanged(a, b) {
			if b.IsContainer() {
				return false, nil
			}
			resize = true
		}
	}
	if !resize {
		return false, nil
	}
	for _, e := range end.Edges {
		if !sameResizeWire(wires[e.AbsID()], e) {
			return false, nil
		}
	}

	roots := map[*d2graph.Object]bool{start.Root: true, end.Root: true}
	for _, f := range end.TweenFrames {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if f == nil || roots[f.Root] || !validBoardGeometry(f, true) ||
			len(f.Objects) != len(first) || len(f.Edges) != len(wires) ||
			f.Root.Direction.Value != start.Root.Direction.Value || !reflect.DeepEqual(f.Data["tala-seeds"], start.Data["tala-seeds"]) {
			return false, nil
		}
		roots[f.Root] = true
		for _, n := range f.Objects {
			if !sameResizeContent(first[n.AbsID()], n) {
				return false, nil
			}
		}
		for _, e := range f.Edges {
			if !sameResizeWire(wires[e.AbsID()], e) {
				return false, nil
			}
		}
	}

	// If any sample cannot be certified by the router, restore the entire
	// ordinary track. A single isolated fallback can look like a layout flash.
	switching := resizeCrossings(start, last, len(end.TweenFrames))
	plans := make([]map[*d2graph.Object]*geo.Box, len(end.TweenFrames))
	var saved []resizeFrame
	for i, f := range end.TweenFrames {
		saved = append(saved, saveResizeFrame(f))
		if ctx.Err() != nil {
			break
		}
		if i == 0 {
			for _, n := range f.Objects {
				o := first[n.AbsID()]
				n.Box = o.Box.Copy()
				n.LabelPosition = copyResizeValue(o.LabelPosition)
				n.IconPosition = copyResizeValue(o.IconPosition)
			}
			for _, e := range f.Edges {
				o := wires[e.AbsID()]
				e.Route = translatedBoardRoute(o.Route, 0, 0)
				e.LabelPosition = copyResizeValue(o.LabelPosition)
				e.LabelPercentage = copyResizeValue(o.LabelPercentage)
			}
		} else {
			t := float64(i) / float64(len(end.TweenFrames))
			plans[i] = resizeSamplePlan(f, first, last, switching, t, i, len(end.TweenFrames))
		}
		if i+1 == len(end.TweenFrames) && ctx.Err() == nil && routeResizeSequence(ctx, start, end, plans) {
			end.TweenFrames[0].TweenReorder = len(switching) != 0
			return true, nil
		}
	}
	for _, f := range saved {
		f.restore()
	}
	return false, ctx.Err()
}

// Siblings whose bodies would pass through one another switch placement while
// faded. Work in parent-local coordinates so a moving container neither
// schedules a second switch for its contents nor disturbs stationary siblings.
type resizeSwitch struct{ from, to float64 }

func resizeCrossings(start *d2graph.Graph, last map[string]*d2graph.Object, count int) map[string]resizeSwitch {
	staged := make(map[string]resizeSwitch)
	cut := float64((count+1)/2) / float64(count)
	margin := resizeSettleTime(count)
	poseCut := resizeMotionTime(cut, margin)
	// Keep stationary context visible, then prefer showing the objects whose
	// authored dimensions actually change. Even a wholesale permutation has
	// a moving visual anchor rather than a blank frame.
	resizes := make(map[*d2graph.Object]bool)
	for _, o := range start.Objects {
		if boardSizeChanged(o, last[o.AbsID()]) {
			for n := o; n != nil && !resizes[n]; n = n.Parent {
				resizes[n] = true
			}
		}
	}
	var visit func(*d2graph.Object)
	visit = func(parent *d2graph.Object) {
		type track struct {
			id       string
			a, b     *geo.Box
			motion   float64
			priority int
		}
		var children []track
		for _, o := range parent.ChildrenArray {
			if o.NearKey != nil {
				continue
			}
			b := last[o.AbsID()]
			if !o.IsContainer() && continuityHidden(o.Style) && continuityHidden(b.Style) {
				continue
			}
			aBox, bBox := o.Box.Copy(), b.Box.Copy()
			if parent.Parent != nil {
				aBox.TopLeft.X -= parent.TopLeft.X
				aBox.TopLeft.Y -= parent.TopLeft.Y
				bBox.TopLeft.X -= b.Parent.TopLeft.X
				bBox.TopLeft.Y -= b.Parent.TopLeft.Y
			}
			motion := math.Hypot(bBox.TopLeft.X-aBox.TopLeft.X, bBox.TopLeft.Y-aBox.TopLeft.Y)
			priority := 2
			if motion < 1 {
				priority = 0
			} else if resizes[o] {
				priority = 1
			}
			children = append(children, track{o.AbsID(), aBox, bBox, motion, priority})
		}
		xsort.StableFunc(children, func(a, b track) int {
			if x := cmp.Compare(a.priority, b.priority); x != 0 {
				return x
			}
			return cmp.Compare(a.motion, b.motion)
		})
		var visible []track
		for _, c := range children {
			switches := false
			for _, v := range visible {
				if resizeOverlap(c.a, c.a, v.a, v.a) || resizeOverlap(c.b, c.b, v.b, v.b) {
					continue
				}
				if resizeOverlap(c.a, c.b, v.a, v.b) {
					switches = true
					break
				}
			}
			if switches {
				staged[c.id] = resizeSwitch{cut, cut}
			} else {
				visible = append(visible, c)
			}
		}
		for _, s := range children {
			window, ok := staged[s.id]
			if !ok {
				continue
			}
			// The switching subtree holds its endpoint position. Hide it
			// before an unchanged/moving neighbor reaches its old position,
			// and reveal it only after the destination position is clear.
			for _, v := range visible {
				if resizeOverlap(s.a, s.a, v.a, v.a) || resizeOverlap(s.b, s.b, v.b, v.b) {
					continue
				}
				mid := geo.NewBox(geo.NewPoint(resizeLerp(v.a.TopLeft.X, v.b.TopLeft.X, poseCut), resizeLerp(v.a.TopLeft.Y, v.b.TopLeft.Y, poseCut)), resizeLerp(v.a.Width, v.b.Width, poseCut), resizeLerp(v.a.Height, v.b.Height, poseCut))
				before, after := s.a.Copy(), s.b.Copy()
				// A leaf's numeric width/height stays on its authored sample
				// even while its placement switches under the fade.
				width, height := resizeLerp(s.a.Width, s.b.Width, cut), resizeLerp(s.a.Height, s.b.Height, cut)
				before.Width, before.Height = max(before.Width, width), max(before.Height, height)
				after.Width, after.Height = max(after.Width, width), max(after.Height, height)
				if lo, _, hits := resizeOverlapInterval(s.a, before, v.a, mid); hits {
					window.from = min(window.from, margin+lo*poseCut*(1-2*margin))
				}
				if _, hi, hits := resizeOverlapInterval(after, s.b, mid, v.b); hits {
					window.to = max(window.to, cut+hi*(1-poseCut)*(1-2*margin))
				}
			}
			staged[s.id] = window
		}
		for _, child := range parent.ChildrenArray {
			if _, ok := staged[child.AbsID()]; !ok {
				visit(child)
			}
		}
	}
	visit(start.Root)
	return staged
}

func resizeOverlap(a, b, c, d *geo.Box) bool {
	_, _, overlap := resizeOverlapInterval(a, b, c, d)
	return overlap
}

// Intersect the time intervals where the four separating inequalities hold.
// Positive-area overlap must persist for some time; touching sides can pass.
func resizeOverlapInterval(a, b, c, d *geo.Box) (float64, float64, bool) {
	lo, hi := 0., 1.
	for _, gap := range [4][2]float64{
		{c.TopLeft.X + c.Width - a.TopLeft.X, d.TopLeft.X + d.Width - b.TopLeft.X},
		{a.TopLeft.X + a.Width - c.TopLeft.X, b.TopLeft.X + b.Width - d.TopLeft.X},
		{c.TopLeft.Y + c.Height - a.TopLeft.Y, d.TopLeft.Y + d.Height - b.TopLeft.Y},
		{a.TopLeft.Y + a.Height - c.TopLeft.Y, b.TopLeft.Y + b.Height - d.TopLeft.Y},
	} {
		x, y := gap[0], gap[1]
		if x <= 0 && y <= 0 {
			return 0, 0, false
		}
		if x <= 0 {
			lo = max(lo, x/(x-y))
		}
		if y <= 0 {
			hi = min(hi, x/(x-y))
		}
	}
	return lo, hi, hi-lo > 1e-9
}

func resizeSamplePlan(f *d2graph.Graph, first, last map[string]*d2graph.Object, switching map[string]resizeSwitch, t float64, index, count int) map[*d2graph.Object]*geo.Box {
	plan := make(map[*d2graph.Object]*geo.Box, len(first))
	positionTime := t
	if len(switching) > 0 {
		positionTime = resizeMotionTime(t, resizeSettleTime(count))
	}
	fading := make(map[*d2graph.Object]float64)
	cut := (count + 1) / 2
	var place func(*d2graph.Object, *resizeSwitch)
	place = func(n *d2graph.Object, window *resizeSwitch) {
		id := n.AbsID()
		a, b := first[id], last[id]
		if w, ok := switching[id]; ok {
			window = &w
		}
		u := positionTime
		if window != nil {
			u = 0
			if index >= cut {
				u = 1
			}
			alpha := 1.
			if count >= 4 {
				duration := min(2/float64(count), 0.16)
				switch {
				case t < window.from:
					alpha = min(1, (window.from-t)/min(duration, window.from))
				case t > window.to:
					alpha = min(1, (t-window.to)/min(duration, 1-window.to))
				default:
					alpha = 0
				}
				alpha = alpha * alpha * (3 - 2*alpha)
			}
			fading[n] = alpha
			n.Style.Opacity = resizeOpacity(n.Style.Opacity, alpha)
		}
		x, y := resizeLerp(a.TopLeft.X, b.TopLeft.X, u), resizeLerp(a.TopLeft.Y, b.TopLeft.Y, u)
		if parent := plan[n.Parent]; parent != nil {
			x += parent.TopLeft.X - resizeLerp(a.Parent.TopLeft.X, b.Parent.TopLeft.X, u)
			y += parent.TopLeft.Y - resizeLerp(a.Parent.TopLeft.Y, b.Parent.TopLeft.Y, u)
		}
		box := geo.NewBox(geo.NewPoint(x, y), resizeLerp(a.Width, b.Width, u), resizeLerp(a.Height, b.Height, u))
		if !n.IsContainer() {
			box.Width, box.Height = n.Width, n.Height
		}
		plan[n] = box
		for _, child := range n.ChildrenArray {
			place(child, window)
			// Different local motion can temporarily require more space than
			// a simple interpolation of the parent’s two outer dimensions.
			c := plan[child]
			ac, bc := first[child.AbsID()], last[child.AbsID()]
			right := min(20., a.TopLeft.X+a.Width-ac.TopLeft.X-ac.Width, b.TopLeft.X+b.Width-bc.TopLeft.X-bc.Width)
			bottom := min(20., a.TopLeft.Y+a.Height-ac.TopLeft.Y-ac.Height, b.TopLeft.Y+b.Height-bc.TopLeft.Y-bc.Height)
			box.Width = max(box.Width, c.TopLeft.X+c.Width+right-x)
			box.Height = max(box.Height, c.TopLeft.Y+c.Height+bottom-y)
		}
	}
	for _, root := range f.Root.ChildrenArray {
		place(root, nil)
	}
	for _, e := range f.Edges {
		alpha := 1.
		if x, ok := fading[e.Src]; ok {
			alpha = x
		}
		if x, ok := fading[e.Dst]; ok {
			alpha = min(alpha, x)
		}
		e.Style.Opacity = resizeOpacity(e.Style.Opacity, alpha)
	}
	return plan
}

// Allow crossing objects to fade before an anchor starts moving or after it
// arrives. This prevents a crowded endpoint from appearing in a single frame.
func resizeSettleTime(count int) float64 { return min(4/float64(count), 0.16) }

func resizeMotionTime(t, margin float64) float64 {
	return max(0, min(1, (t-margin)/(1-2*margin)))
}

func resizeOpacity(value *d2graph.Scalar, alpha float64) *d2graph.Scalar {
	if alpha == 1 {
		return value
	}
	opacity := 1.
	if value != nil {
		opacity, _ = strconv.ParseFloat(value.Value, 64)
	}
	return &d2graph.Scalar{Value: strconv.FormatFloat(opacity*alpha, 'f', -1, 64)}
}
func resizeLerp(a, b, t float64) float64 { return a + (b-a)*t }

func resizeObjectIndex(graph *d2graph.Graph) map[string]*d2graph.Object {
	result := make(map[string]*d2graph.Object, len(graph.Objects))
	for _, obj := range graph.Objects {
		result[obj.AbsID()] = obj
	}
	return result
}

func sameResizeContent(a, b *d2graph.Object) bool {
	return sameResizeObject(a, b) && a.LabelDimensions == b.LabelDimensions &&
		reflect.DeepEqual(a.SQLTable, b.SQLTable) && reflect.DeepEqual(a.Class, b.Class)
}

func sameResizeWire(a, b *d2graph.Edge) bool {
	return sameResizeEdge(a, b) && !a.IsCurve && !b.IsCurve && a.LabelDimensions == b.LabelDimensions
}

type resizeObjectState struct {
	box         *geo.Box
	label, icon *string
	opacity     *d2graph.Scalar
}
type resizeEdgeState struct {
	route       []*geo.Point
	destination *d2graph.Object
	label       *string
	percentage  *float64
	dimensions  d2target.TextDimensions
	isCurve     bool
	opacity     *d2graph.Scalar
}
type resizeFrame struct {
	graph *d2graph.Graph
	edges []*d2graph.Edge
	boxes []resizeObjectState
	wires []resizeEdgeState
}

func saveResizeFrame(g *d2graph.Graph) resizeFrame {
	s := resizeFrame{graph: g, edges: g.Edges, boxes: make([]resizeObjectState, len(g.Objects)), wires: make([]resizeEdgeState, len(g.Edges))}
	for i, o := range g.Objects {
		s.boxes[i] = resizeObjectState{o.Box, o.LabelPosition, o.IconPosition, o.Style.Opacity}
	}
	for i, e := range g.Edges {
		s.wires[i] = resizeEdgeState{e.Route, e.Dst, e.LabelPosition, e.LabelPercentage, e.LabelDimensions, e.IsCurve, e.Style.Opacity}
	}
	return s
}
func (s resizeFrame) restore() {
	for i, o := range s.graph.Objects {
		v := s.boxes[i]
		o.Box, o.LabelPosition, o.IconPosition, o.Style.Opacity = v.box, v.label, v.icon, v.opacity
	}
	s.graph.Edges = s.edges
	for i, e := range s.edges {
		v := s.wires[i]
		e.Route, e.Dst, e.LabelPosition, e.LabelPercentage, e.LabelDimensions, e.IsCurve, e.Style.Opacity = v.route, v.destination, v.label, v.percentage, v.dimensions, v.isCurve, v.opacity
	}
}
func sameResizeObject(a, b *d2graph.Object) bool {
	if a == nil || b == nil || a.Shape.Value != b.Shape.Value || a.Language != b.Language ||
		a.Label.Value != b.Label.Value || a.Direction.Value != b.Direction.Value ||
		a.Is3D() || b.Is3D() || a.IsMultiple() || b.IsMultiple() ||
		!sameBoardScalar(a.Attributes.LabelPosition, b.Attributes.LabelPosition) ||
		!sameBoardScalar(a.Attributes.IconPosition, b.Attributes.IconPosition) ||
		!sameBoardScalar(a.Style.StrokeWidth, b.Style.StrokeWidth) ||
		!reflect.DeepEqual(a.Icon, b.Icon) || a.IsContainer() != b.IsContainer() {
		return false
	}
	if a.NearKey == nil || b.NearKey == nil {
		return a.NearKey == b.NearKey
	}
	return slices.Equal(d2graph.Key(a.NearKey), d2graph.Key(b.NearKey))
}

func sameResizeEdge(a, b *d2graph.Edge) bool {
	return a != nil && b != nil && a.Label.Value == b.Label.Value &&
		a.SrcArrow == b.SrcArrow && a.DstArrow == b.DstArrow &&
		sameResizeArrow(a.SrcArrowhead, b.SrcArrowhead) && sameResizeArrow(a.DstArrowhead, b.DstArrowhead) &&
		sameBoardScalar(a.Attributes.LabelPosition, b.Attributes.LabelPosition) &&
		sameBoardScalar(a.Style.StrokeWidth, b.Style.StrokeWidth) &&
		reflect.DeepEqual(a.SrcTableColumnIndex, b.SrcTableColumnIndex) &&
		reflect.DeepEqual(a.DstTableColumnIndex, b.DstTableColumnIndex)
}

func sameResizeArrow(a, b *d2graph.Attributes) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ToArrowhead() == b.ToArrowhead() && a.Label.Value == b.Label.Value
}

func copyResizeValue[T any](v *T) *T {
	if v == nil {
		return nil
	}
	return new(*v)
}

func sameBoardScalar(a, b *d2graph.Scalar) bool {
	return a == b || a != nil && b != nil && a.Value == b.Value
}

func boardSizeChanged(a, b *d2graph.Object) bool {
	return !sameBoardScalar(a.WidthAttr, b.WidthAttr) || !sameBoardScalar(a.HeightAttr, b.HeightAttr)
}
