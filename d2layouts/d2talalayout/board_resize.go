package d2talalayout

import (
	"cmp"
	"context"
	"math"
	"reflect"
	"slices"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2near"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/xsort"
)

// Independent static layouts can pick entirely different sibling orders one
// pixel apart. Derive every sample (including the held endpoint) from the same
// authored layout, so temporary growth neither swaps branches nor accumulates.
func continueBoardResize(ctx context.Context, previous, current *d2graph.Graph) bool {
	step := current
	if len(step.TweenFrames) == 0 {
		step = current.Parent
		if step == nil || !slices.Contains(step.TweenFrames, current) {
			return false
		}
	}
	if step.Parent == nil {
		return false
	}
	index := slices.Index(step.Parent.Steps, step)
	if index < 0 {
		return false
	}
	start := step.Parent
	if index > 0 {
		start = step.Parent.Steps[index-1]
	}
	if previous != start && !slices.Contains(step.TweenFrames, previous) ||
		start.Root == nil || step.Root == nil || start.Root.Direction.Value != current.Root.Direction.Value ||
		start.Root.Direction.Value != step.Root.Direction.Value || step.Root.IsGridDiagram() || step.Root.IsSequenceDiagram() ||
		!reflect.DeepEqual(start.Data["tala-seeds"], step.Data["tala-seeds"]) ||
		!reflect.DeepEqual(start.Data["tala-seeds"], current.Data["tala-seeds"]) ||
		len(start.Objects) != len(current.Objects) || len(step.Objects) != len(current.Objects) ||
		len(start.Edges) != len(step.Edges) || len(start.Edges) != len(current.Edges) || !validBoardGeometry(start, true) || !validBoardGeometry(current, true) {
		return false
	}
	old := make(map[string]*d2graph.Object, len(start.Objects))
	for _, obj := range start.Objects {
		old[obj.AbsID()] = obj
	}

	resize := false
	for _, dst := range step.Objects {
		if dst == nil {
			return false
		}
		src := old[dst.AbsID()]
		if !sameResizeObject(src, dst) || dst.Top != nil || dst.Left != nil ||
			dst.IsGridDiagram() || dst.IsSequenceDiagram() {
			return false
		}
		if boardSizeChanged(src, dst) {
			if dst.IsContainer() {
				return false
			}
			resize = true
		}
	}
	if !resize {
		return false
	}
	edges := continuityEdges(start)
	for _, e := range step.Edges {
		// Constant-near groups move as units after the core is routed. A
		// wire crossing that boundary needs the ordinary near-aware router.
		if e == nil || e.Src == nil || e.Dst == nil || e.Src.OuterNearContainer() != e.Dst.OuterNearContainer() ||
			!sameResizeEdge(edges[e.AbsID()], e) {
			return false
		}
	}
	if origin := start.ResizeLayoutOrigin; origin != nil {
		start = origin
		edges = continuityEdges(start)
		clear(old)
		for _, obj := range start.Objects {
			old[obj.AbsID()] = obj
		}
	}
	resized, hasNear := false, false
	p := resizePlacement{old: make(map[*d2graph.Object]*d2graph.Object, len(old)), boxes: make(map[*d2graph.Object]*geo.Box, len(old))}
	for _, n := range current.Objects {
		o := old[n.AbsID()]
		if !sameResizeObject(o, n) || n.LabelDimensions != o.LabelDimensions ||
			!reflect.DeepEqual(n.SQLTable, o.SQLTable) || !reflect.DeepEqual(n.Class, o.Class) {
			return false
		}
		hasNear = hasNear || n.NearKey != nil
		box := o.Box.Copy()
		if boardSizeChanged(o, n) {
			box.Width, box.Height = n.Width, n.Height
		} else if !n.IsContainer() && (n.Width != o.Width || n.Height != o.Height) {
			// Another change, such as font size, affected intrinsic sizing.
			return false
		}
		resized = resized || box.Width != o.Width || box.Height != o.Height
		p.old[n], p.boxes[n] = o, box
	}
	p.gaps = make(map[[2]*d2graph.Object]d2target.TextDimensions)
	for _, e := range current.Edges {
		o := edges[e.AbsID()]
		if !sameResizeEdge(o, e) || e.IsCurve || o.IsCurve || e.LabelDimensions != o.LabelDimensions {
			return false
		}
		if e.Label.Value != "" {
			p.reserveCaption(e)
		}
	}
	if ctx.Err() != nil {
		return false
	}
	if resized {
		if !p.fit(current.Root) || !p.route(ctx, start, current, hasNear) {
			return false
		}
	} else {
		// At the original dimensions the original geometry is already valid,
		// including ports, intermediate collinear points, and caption anchors.
		for _, n := range current.Objects {
			n.Box = p.boxes[n]
			n.LabelPosition = copyResizeValue(p.old[n].LabelPosition)
			n.IconPosition = copyResizeValue(p.old[n].IconPosition)
		}
		for _, e := range current.Edges {
			o := edges[e.AbsID()]
			e.Route = translatedBoardRoute(o.Route, 0, 0)
			e.LabelPosition = copyResizeValue(o.LabelPosition)
			e.LabelPercentage = copyResizeValue(o.LabelPercentage)
		}
	}
	if current == step {
		current.ResizeLayoutOrigin = start
	}
	return true
}

// These are authored inputs that are available even before the held board's
// layout. An incoming transition with a simultaneous structural change must
// retain its ordinary layout throughout, including its first numeric samples.
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

type resizePlacement struct {
	old   map[*d2graph.Object]*d2graph.Object
	boxes map[*d2graph.Object]*geo.Box
	gaps  map[[2]*d2graph.Object]d2target.TextDimensions
}

func (p *resizePlacement) route(ctx context.Context, start, current *d2graph.Graph, hasNear bool) bool {
	if !hasNear {
		return routeResizeBoard(ctx, start, current, p.boxes)
	}
	// Near positions depend on the final wire extents. The router installs
	// fresh boxes and routes, so retaining the old values also preserves the
	// original geometry if a decoration would leave the coordinate range.
	objects := make([]d2graph.Object, len(current.Objects))
	for i, obj := range current.Objects {
		objects[i] = *obj
	}
	originalEdges := current.Edges
	edges := make([]d2graph.Edge, len(originalEdges))
	for i, edge := range originalEdges {
		edges[i] = *edge
	}
	if !routeResizeBoard(ctx, start, current, p.boxes) {
		return false
	}
	d2near.Reposition(current)
	if ctx.Err() == nil && canShiftBoardObjects(current.Objects, current.Edges, 0, 0) {
		return true
	}
	for i, obj := range current.Objects {
		*obj = objects[i]
	}
	current.Edges = originalEdges
	for i, edge := range originalEdges {
		*edge = edges[i]
	}
	return false
}

// Retain room for edge captions in the gap between the branches they join.
func (p *resizePlacement) reserveCaption(e *d2graph.Edge) {
	a, b := e.Src, e.Dst
	for da, db := a.Level(), b.Level(); da != db; {
		if da > db {
			a, da = a.Parent, da-1
		} else {
			b, db = b.Parent, db-1
		}
	}
	if a == b {
		return
	}
	for a.Parent != b.Parent {
		a, b = a.Parent, b.Parent
	}
	key := [2]*d2graph.Object{a, b}
	size := p.gaps[key]
	size.Width = max(size.Width, e.LabelDimensions.Width)
	size.Height = max(size.Height, e.LabelDimensions.Height)
	p.gaps[key], p.gaps[[2]*d2graph.Object{b, a}] = size, size
}

func (p *resizePlacement) fit(parent *d2graph.Object) bool {
	var children []*d2graph.Object
	for _, n := range parent.ChildrenArray {
		if !p.fit(n) {
			return false
		}
		if n.NearKey == nil {
			children = append(children, n)
		}
	}
	if len(children) == 0 {
		return true
	}
	// The original separating axis defines sibling order. Consume empty space
	// first, retaining up to 20 pixels of the original clearance between boxes.
	for _, horizontal := range []bool{true, false} {
		xsort.StableFunc(children, func(a, b *d2graph.Object) int {
			return cmp.Compare(axisStart(p.old[a].Box, horizontal), axisStart(p.old[b].Box, horizontal))
		})
		for i, n := range children {
			oldN := p.old[n].Box
			start := axisStart(p.boxes[n], horizontal)
			next := start
			for _, other := range children[:i] {
				oldOther := p.old[other].Box
				gap := axisStart(oldN, horizontal) - axisEnd(oldOther, horizontal)
				crossGap := max(axisStart(oldN, !horizontal)-axisEnd(oldOther, !horizontal), axisStart(oldOther, !horizontal)-axisEnd(oldN, !horizontal))
				if gap < 0 || gap < crossGap || gap == crossGap && !horizontal {
					continue
				}
				caption := p.gaps[[2]*d2graph.Object{n, other}]
				clearance := caption.Height
				if horizontal {
					clearance = caption.Width
				}
				next = max(next, axisEnd(p.boxes[other], horizontal)+min(gap, float64(clearance)+20))
			}
			if next > start {
				p.shift(n, horizontal, next-start)
			}
		}
	}
	if parent.Parent == nil {
		return true
	}
	b := p.boxes[parent]
	original := p.old[parent]
	for _, horizontal := range []bool{true, false} {
		before, after := math.Inf(-1), math.Inf(-1)
		for _, child := range children {
			before = max(before, axisEnd(p.old[child].Box, horizontal))
			after = max(after, axisEnd(p.boxes[child], horizontal))
		}
		if after == before {
			continue
		}
		if parent.Shape.Value != "" && parent.Shape.Value != d2target.ShapeRectangle {
			return false
		}
		// Preserve the original outer padding. Keep enough room for the
		// container's caption even when all its children shrink.
		if horizontal {
			b.Width = max(float64(parent.LabelDimensions.Width)+20, original.Width+after-before)
			if parent.WidthAttr != nil {
				b.Width = max(b.Width, original.Width)
			}
		} else {
			b.Height = max(float64(parent.LabelDimensions.Height)+20, original.Height+after-before)
			if parent.HeightAttr != nil {
				b.Height = max(b.Height, original.Height)
			}
		}
	}
	return true
}

func axisStart(b *geo.Box, horizontal bool) float64 {
	if horizontal {
		return b.TopLeft.X
	}
	return b.TopLeft.Y
}

func axisEnd(b *geo.Box, horizontal bool) float64 {
	if horizontal {
		return b.TopLeft.X + b.Width
	}
	return b.TopLeft.Y + b.Height
}

func (p *resizePlacement) shift(n *d2graph.Object, horizontal bool, delta float64) {
	b := p.boxes[n]
	if horizontal {
		b.TopLeft.X += delta
	} else {
		b.TopLeft.Y += delta
	}
	for _, child := range n.ChildrenArray {
		p.shift(child, horizontal, delta)
	}
}
