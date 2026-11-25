// Package sequencediagram implements the opt-in sequence-diagram layout.
// It deliberately keeps participant placement, the source-ordered timeline,
// activation extents, and frame geometry as separate passes.
package sequencediagram

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2exporter"
	"github.com/d2lang/d2/d2format"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2sequence"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
)

const (
	padding              = 24.
	framePadding         = 16.
	defaultHorizontalGap = 60.
	defaultVerticalGap   = 48.
	spanWidth            = 12.
	spanPadding          = 12.
)

type item struct {
	object       *d2graph.Object
	edge         *d2graph.Edge
	afterGap     *d2graph.Scalar
	pos          sourcePosition
	above, below float64
	loopHeight   float64
	spanMinimums []spanMinimum
	y            float64
}

// A minimum activation height constrains the bottom of its last row relative
// to the first row, whose absolute position is known only during placement.
type spanMinimum struct {
	first  *item
	height float64
}

type sourcePosition []uint64

func (a sourcePosition) less(b sourcePosition) bool {
	return slices.Compare(a, b) < 0
}

type diagram struct {
	graph                                             *d2graph.Graph
	actors, lanes, spans, events, groups, actorGroups []*d2graph.Object
	owner                                             map[*d2graph.Object]*d2graph.Object
	rank                                              map[*d2graph.Object]int
	depth                                             map[*d2graph.Object]int
	timeline                                          []*item
	emptySpans                                        map[*d2graph.Object]float64
	frameTop, frameBottom                             map[*d2graph.Object]float64
	hgap, vgap                                        float64
}

// Layout places a single sequence diagram after graph text dimensions have been
// measured. The caller is responsible for extracting nested sequence diagrams.
func Layout(ctx context.Context, g *d2graph.Graph) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d := &diagram{graph: g, owner: make(map[*d2graph.Object]*d2graph.Object), rank: make(map[*d2graph.Object]int), depth: make(map[*d2graph.Object]int), emptySpans: make(map[*d2graph.Object]float64), frameTop: make(map[*d2graph.Object]float64), frameBottom: make(map[*d2graph.Object]float64)}
	d.hgap = gap(g.Root.HorizontalGap, gap(g.Root.GridGap, defaultHorizontalGap))
	d.vgap = gap(g.Root.VerticalGap, gap(g.Root.GridGap, defaultVerticalGap))
	if err := d.collect(g.Root, nil, 0); err != nil {
		return err
	}
	for i, a := range d.lanes {
		d.rank[a] = i
	}
	if len(d.lanes) == 0 {
		return fmt.Errorf("no actors declared in sequence diagram")
	}
	if err := d.buildTimeline(); err != nil {
		return err
	}
	d.placeActors()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := d.placeTimeline(); err != nil {
		return err
	}
	d.placeSpans()
	d.routeEndpoints()
	d.placeMessageGroups()
	d.finishActors()
	d.placeActorGroups()
	d.fitRoot()
	return nil
}

func gap(s *d2graph.Scalar, fallback float64) float64 {
	if s == nil {
		return fallback
	}
	n, err := strconv.ParseFloat(s.Value, 64)
	if err != nil || n < 0 || math.IsInf(n, 0) || math.IsNaN(n) {
		return fallback
	}
	return n
}
func setPosition(p **string, v label.Position) {
	if *p == nil {
		s := v.String()
		*p = &s
	}
}

func (d *diagram) collect(parent, actor *d2graph.Object, depth int) error {
	children := append([]*d2graph.Object(nil), parent.ChildrenArray...)
	sort.SliceStable(children, func(i, j int) bool { return sourcePosition(children[i].SequenceOrder).less(children[j].SequenceOrder) })
	for _, o := range children {
		if o.Box == nil {
			o.Box = geo.NewBox(geo.NewPoint(0, 0), 100, 60)
		}
		if o.TopLeft == nil {
			o.TopLeft = geo.NewPoint(0, 0)
		}
		if o.IsSequenceDiagram() {
			return fmt.Errorf("actors in sequence diagrams cannot themselves be sequence diagrams: %s", o.AbsID())
		}
		if o.Shape.Value == d2target.ShapeSequenceDiagramEdgeGroup || o.IsSequenceDiagramBranch() {
			d.groups = append(d.groups, o)
			setPosition(&o.LabelPosition, label.InsideTopLeft)
			if err := d.collect(o, actor, depth); err != nil {
				return err
			}
			continue
		}
		switch o.Shape.Value {
		case d2target.ShapeSequenceDiagramActorGroup:
			d.actorGroups = append(d.actorGroups, o)
			if len(o.ChildrenArray) == 0 {
				// A group with no actors still occupies its own header column.
				d.lanes = append(d.lanes, o)
			}
			setPosition(&o.LabelPosition, label.InsideTopCenter)
			if err := d.collect(o, nil, 0); err != nil {
				return err
			}
		default:
			if actor == nil {
				d.actors = append(d.actors, o)
				d.lanes = append(d.lanes, o)
				d.owner[o] = o
				if o.WidthAttr == nil && o.Width < 100 {
					switch o.Shape.Value {
					case d2target.ShapeCircle, d2target.ShapeOval, d2target.ShapePerson, d2target.ShapeSquare:
						if o.Width > 0 {
							o.Height *= 100 / o.Width
						}
					}
					o.Width = 100
				}
				d.actorAppearance(o)
				if err := d.collect(o, o, 0); err != nil {
					return err
				}
			} else {
				d.owner[o] = actor
				if o.Shape.Value == "" {
					d.spans = append(d.spans, o)
					d.depth[o] = depth
					if o.HasLabel() {
						setPosition(&o.LabelPosition, label.InsideTopLeft)
					}
					if err := d.collect(o, actor, depth+1); err != nil {
						return err
					}
				} else {
					if o.Shape.Value == d2target.ShapeSequenceDiagramActor {
						// A repeat has its own identity and source position, but the actor's
						// appearance. Its children and references still belong to this object.
						order, declaration, afterGap := o.SequenceOrder, o.SequenceDeclarationIndex, o.VerticalGap
						o.Attributes = actor.Attributes
						o.SequenceOrder, o.SequenceDeclarationIndex = order, declaration
						o.VerticalGap = afterGap
						o.Shape.Value = d2target.ShapeSequenceDiagramActor
						o.Box = geo.NewBox(geo.NewPoint(0, 0), actor.Width, actor.Height)
						o.LabelPosition = actor.LabelPosition
						o.IconPosition = actor.IconPosition
						o.Class = actor.Class
						o.SQLTable = actor.SQLTable
						o.ContentAspectRatio = actor.ContentAspectRatio
					} else {
						setPosition(&o.LabelPosition, label.InsideMiddleCenter)
					}
					d.events = append(d.events, o)
					if err := d.collect(o, actor, depth); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (d *diagram) actorAppearance(o *d2graph.Object) {
	if o.Icon != nil && o.Shape.Value != d2target.ShapeImage {
		setPosition(&o.LabelPosition, label.OutsideTopCenter)
		setPosition(&o.IconPosition, label.InsideMiddleCenter)
	} else if o.HasOutsideBottomLabel() {
		setPosition(&o.LabelPosition, label.OutsideBottomCenter)
	} else {
		setPosition(&o.LabelPosition, label.InsideMiddleCenter)
	}
}

func (d *diagram) buildTimeline() error {
	used := make(map[*d2graph.Object]bool)
	for _, e := range d.graph.Edges {
		if d2sequence.IsLifelineEnd(e.Dst) {
			continue
		}
		if d.owner[e.Src] == nil || d.owner[e.Dst] == nil {
			return fmt.Errorf("sequence message %s must connect actors or spans", e.AbsID())
		}
		for _, o := range []*d2graph.Object{e.Src, e.Dst} {
			if d.owner[o] != o {
				if _, ok := d.depth[o]; !ok {
					return fmt.Errorf("sequence messages cannot connect notes or events: %s", o.AbsID())
				}
			}
			for p := o; p != nil && p != d.owner[o]; p = p.Parent {
				used[p] = true
			}
		}
		h := float64(e.LabelDimensions.Height) / 2
		it := &item{edge: e, pos: e.SequenceOrder, afterGap: e.VerticalGap, above: h, below: h}
		if d.owner[e.Src] == d.owner[e.Dst] {
			it.loopHeight = math.Max(32, float64(e.LabelDimensions.Height)+12)
			it.below = it.loopHeight + h
		}
		if near := e.Attributes.LabelPosition; near != nil && e.Label.Value != "" {
			position := d2ast.LabelPositionsMapping[near.Value]
			if position.IsOutside() {
				extent := float64(e.LabelDimensions.Height) + label.PADDING + messageStrokeWidth(e)/2
				if !bottomMessageLabel(position) {
					it.above, it.below = extent, it.loopHeight
				} else {
					it.above, it.below = 0, it.loopHeight+extent
				}
			}
		}
		if hasEndpointLabels(e) {
			// Endpoint captions follow route direction. Measure their vertical
			// extents against the same horizontal legs we will route below.
			reversed := d.rank[d.owner[e.Src]] > d.rank[d.owner[e.Dst]]
			run := messageLabelRun(e, reversed)
			if reversed {
				run = -run
			}
			c := d2exporter.ToConnection(e, nil)
			c.Route = []*geo.Point{geo.NewPoint(0, 0), geo.NewPoint(run, 0)}
			if it.loopHeight > 0 {
				c.Route = append(c.Route, geo.NewPoint(run, it.loopHeight), geo.NewPoint(0, it.loopHeight))
			}
			b := endpointLabelBounds(&c)
			it.above, it.below = math.Max(it.above, -b.minY), math.Max(it.below, b.maxY)
		}
		d.timeline = append(d.timeline, it)
	}
	for _, o := range d.events {
		margin, _ := o.Spacing()
		d.timeline = append(d.timeline, &item{object: o, pos: o.SequenceOrder, afterGap: o.VerticalGap, above: o.Height/2 + margin.Top, below: o.Height/2 + margin.Bottom})
		for p := o.Parent; p != nil && p != d.owner[o]; p = p.Parent {
			used[p] = true
		}
	}
	for _, s := range d.spans {
		if !used[s] {
			d.timeline = append(d.timeline, &item{object: s, pos: s.SequenceOrder, above: spanPadding, below: spanPadding})
		}
	}
	// Empty message frames are timeline events too; they reserve space at
	// their declaration rather than floating over a neighboring note or message.
	for i := len(d.groups) - 1; i >= 0; i-- {
		// Schedule empty children before deciding whether their parent needs a row.
		g := d.groups[i]
		hasContent := false
		for _, it := range d.timeline {
			if d.inGroup(it, g) {
				hasContent = true
				break
			}
		}
		if !hasContent {
			d.timeline = append(d.timeline, &item{object: g, pos: g.SequenceOrder, below: math.Max(40, float64(g.LabelDimensions.Height)+framePadding*2)})
		}
	}
	// Stable sorting uses the graph's deterministic encounter order for generated
	// or imported references that have no distinct source position.
	sort.SliceStable(d.timeline, func(i, j int) bool { return d.timeline[i].pos.less(d.timeline[j].pos) })
	d.reserveSpanExtents()
	return nil
}

// Account for activation borders and labels before scheduling. Growing a
// nested activation after routing must not move it into a participant header.
func (d *diagram) reserveSpanExtents() {
	first, last := make(map[*d2graph.Object]int), make(map[*d2graph.Object]int)
	for _, s := range d.spans {
		for i, it := range d.timeline {
			inside := it.object != nil && it.object.IsDescendantOf(s)
			if it.edge != nil {
				inside = it.edge.Src.IsDescendantOf(s) || it.edge.Dst.IsDescendantOf(s)
			}
			if inside {
				if _, ok := first[s]; !ok {
					first[s] = i
				}
				last[s] = i
			}
		}
	}
	above, below := make([]float64, len(d.timeline)), make([]float64, len(d.timeline))
	for _, s := range d.spans {
		start, ok := first[s]
		if !ok {
			continue
		}
		end := last[s]
		top, bottom := 0., 0.
		for p := s; p != nil && p != d.owner[s]; p = p.Parent {
			if _, span := d.depth[p]; !span {
				continue
			}
			t, b := spanPadding, spanPadding
			if p.HasLabel() && p.LabelPosition != nil {
				switch label.FromString(*p.LabelPosition) {
				case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight, label.OutsideTopLeft, label.OutsideTopCenter, label.OutsideTopRight:
					t += float64(p.LabelDimensions.Height) + label.PADDING*2
				case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight, label.OutsideBottomLeft, label.OutsideBottomCenter, label.OutsideBottomRight:
					b += float64(p.LabelDimensions.Height) + label.PADDING*2
				}
			}
			if first[p] == start {
				top += t
			}
			if last[p] == end {
				bottom += b
			}
		}
		if s.HeightAttr != nil {
			it := d.timeline[start]
			// A self-message can enter the span on its return leg. Leave
			// space below a fixed-height span for any enclosing borders.
			height := gap(s.HeightAttr, 0) + it.loopHeight - spanPadding + bottom
			d.timeline[end].spanMinimums = append(d.timeline[end].spanMinimums, spanMinimum{it, height})
		}
		above[start] = math.Max(above[start], top)
		below[end] = math.Max(below[end], bottom)
	}
	for i, it := range d.timeline {
		it.above += above[i]
		it.below += below[i]
	}
}

func (d *diagram) actorGroupDepth(a *d2graph.Object) float64 {
	v := 0.
	for p := a.Parent; p != nil && p != d.graph.Root; p = p.Parent {
		if p.Shape.Value == d2target.ShapeSequenceDiagramActorGroup {
			v += framePadding + float64(p.LabelDimensions.Height) + label.PADDING*2
		}
	}
	return v
}

func (d *diagram) placeActors() {
	n := len(d.lanes)
	left, right := make([]float64, n), make([]float64, n)
	headerHeight := 0.
	for i, a := range d.lanes {
		margin, _ := a.Spacing()
		left[i] = a.Width/2 + margin.Left
		right[i] = a.Width/2 + margin.Right
		if a.LabelPosition != nil && (label.FromString(*a.LabelPosition) == label.OutsideTopCenter || label.FromString(*a.LabelPosition) == label.OutsideBottomCenter) {
			left[i] = math.Max(left[i], float64(a.LabelDimensions.Width)/2)
			right[i] = math.Max(right[i], float64(a.LabelDimensions.Width)/2)
		}
		headerHeight = math.Max(headerHeight, a.Height+margin.Top+margin.Bottom+d.actorGroupDepth(a))
	}
	for _, o := range d.events {
		i := d.rank[d.owner[o]]
		m, _ := o.Spacing()
		left[i] = math.Max(left[i], o.Width/2+m.Left)
		right[i] = math.Max(right[i], o.Width/2+m.Right)
	}
	for _, s := range d.spans {
		i := d.rank[d.owner[s]]
		offset := float64(d.depth[s]) * spanWidth / 2
		halfWidth := activationWidth(s) / 2
		left[i] = math.Max(left[i], halfWidth-offset)
		right[i] = math.Max(right[i], halfWidth+offset)
		w := spanWidth + offset
		left[i] = math.Max(left[i], w)
		right[i] = math.Max(right[i], w)
		if s.HasLabel() {
			left[i] = math.Max(left[i], float64(s.LabelDimensions.Width)+w)
			right[i] = math.Max(right[i], float64(s.LabelDimensions.Width)+w)
		}
	}
	for _, it := range d.timeline {
		if it.edge == nil {
			continue
		}
		e := it.edge
		if d.owner[e.Src] == d.owner[e.Dst] {
			i := d.rank[d.owner[e.Src]]
			offset := 0.
			for _, o := range []*d2graph.Object{e.Src, e.Dst} {
				if depth, ok := d.depth[o]; ok {
					offset = math.Max(offset, float64(depth)*spanWidth/2+activationWidth(o)/2)
				}
			}
			right[i] = math.Max(right[i], offset+selfTravel(e)+float64(e.LabelDimensions.Width)/2+spanWidth)
		}
	}
	// Reserve the sides of all enclosing participant frames. This also
	// handles a single participant whose group's label is wider than its lane.
	for i, a := range d.lanes {
		for p := a.Parent; p != nil && p != d.graph.Root; p = p.Parent {
			if !containsObject(d.actorGroups, p) {
				continue
			}
			left[i] += framePadding
			right[i] += framePadding
			left[i] = math.Max(left[i], float64(p.LabelDimensions.Width)/2+framePadding)
			right[i] = math.Max(right[i], float64(p.LabelDimensions.Width)/2+framePadding)
		}
	}
	steps := make([]float64, n-1)
	for i := range steps {
		steps[i] = right[i] + left[i+1] + d.hgap
	}
	for _, it := range d.timeline {
		if it.edge == nil {
			continue
		}
		e := it.edge
		lo, hi := d.rank[d.owner[e.Src]], d.rank[d.owner[e.Dst]]
		if lo > hi {
			lo, hi = hi, lo
		}
		if lo == hi {
			continue
		}
		want := messageLabelRun(e, d.rank[d.owner[e.Src]] == hi)
		// Messages attach to activation boundaries, not to the actor center.
		for _, endpoint := range []*d2graph.Object{e.Src, e.Dst} {
			if depth, ok := d.depth[endpoint]; ok {
				offset := float64(depth) * spanWidth / 2
				if d.rank[d.owner[endpoint]] == hi {
					offset = -offset
				}
				want += activationWidth(endpoint)/2 + offset
			}
		}
		have := 0.
		for i := lo; i < hi; i++ {
			have += steps[i]
		}
		if want > have {
			for i := lo; i < hi; i++ {
				steps[i] += (want - have) / float64(hi-lo)
			}
		}
	}
	x := left[0]
	titleHeight := 0.
	if d.graph.Root.HasLabel() {
		titleHeight = float64(d.graph.Root.LabelDimensions.Height) + padding
	}
	for i, a := range d.lanes {
		margin, _ := a.Spacing()
		a.TopLeft = geo.NewPoint(x-a.Width/2, titleHeight+headerHeight-a.Height-margin.Bottom)
		a.ZIndex = 3
		if i < len(steps) {
			x += steps[i]
		}
	}
}

// An off-center label sits a quarter of the way along the message. Reserve
// enough horizontal space for its near half to clear the participant.
func messageLabelRun(e *d2graph.Edge, reversed bool) float64 {
	mainWidth := float64(e.LabelDimensions.Width)
	fraction := label.CENTER_LABEL_POSITION
	if near := e.Attributes.LabelPosition; near != nil {
		switch d2ast.LabelPositionsMapping[near.Value] {
		case label.InsideMiddleLeft, label.OutsideTopLeft, label.OutsideBottomLeft:
			fraction = label.LEFT_LABEL_POSITION
		case label.InsideMiddleRight, label.OutsideTopRight, label.OutsideBottomRight:
			fraction = label.RIGHT_LABEL_POSITION
		}
	}
	// On each side of the main label, allow room for the inward-facing
	// endpoint caption and the portion of the main label on that side.
	left, right := 0., 0.
	if e.SrcArrowhead != nil && e.SrcArrowhead.Label.Value != "" {
		left = float64(e.SrcArrowhead.LabelDimensions.Width)
	}
	if e.DstArrowhead != nil && e.DstArrowhead.Label.Value != "" {
		right = float64(e.DstArrowhead.LabelDimensions.Width)
	}
	if reversed {
		left, right = right, left
	}
	if e.Label.Value == "" {
		return left + right + 2*padding
	}
	return math.Max((left+mainWidth/2+padding)/fraction, (right+mainWidth/2+padding)/(1-fraction))
}

func selfTravel(e *d2graph.Edge) float64 {
	if near := e.Attributes.LabelPosition; hasEndpointLabels(e) || near != nil && d2ast.LabelPositionsMapping[near.Value].IsOutside() {
		return math.Max(60, messageLabelRun(e, false))
	}
	return math.Max(60, float64(e.LabelDimensions.Width)/2+padding)
}
func (d *diagram) inGroup(it *item, g *d2graph.Object) bool {
	// A style override in a later branch refers to the original item. Its
	// declaring scope determines which compartment owns it.
	var scope *d2graph.Object
	if e := it.edge; e != nil {
		if i := e.SequenceDeclarationIndex; i >= 0 && i < len(e.References) {
			scope = e.References[i].ScopeObj
		}
	} else {
		o := it.object
		if o == g {
			return false
		}
		if o.IsDescendantOf(g) {
			return true
		}
		if i := o.SequenceDeclarationIndex; i >= 0 && i < len(o.References) {
			scope = o.References[i].ScopeObj
		}
	}
	return scope != nil && scope.IsDescendantOf(g)
}

func (d *diagram) placeTimeline() error {
	first, last := make(map[*d2graph.Object]int), make(map[*d2graph.Object]int)
	for _, g := range d.groups {
		first[g] = -1
		for i, it := range d.timeline {
			if d.inGroup(it, g) {
				if first[g] < 0 {
					first[g] = i
				} else if g.IsSequenceDiagramBranch() && last[g]+1 != i {
					return fmt.Errorf("events in sequence diagram branch %s must be consecutive", g.AbsID())
				}
				last[g] = i
			}
		}
	}
	// A frame boundary ends a shared row and leaves room for its border.
	boundary := make([]bool, len(d.timeline)+1)
	opening, closing := make([][]*d2graph.Object, len(d.timeline)), make([][]*d2graph.Object, len(d.timeline))
	for _, g := range d.groups {
		if first[g] >= 0 {
			start, end := first[g], last[g]
			boundary[start], boundary[end+1] = true, true
			// Collection visits every parent before its descendants.
			opening[start] = append(opening[start], g)
			closing[end] = append(closing[end], g)
		}
	}
	for i, it := range d.timeline {
		if it.object != nil && containsObject(d.groups, it.object) {
			boundary[i], boundary[i+1] = true, true
		}
	}
	// The first item in a shared row must clear the previous row even if a
	// later member is taller. A self-message joins the next item at its return.
	for i := len(d.timeline) - 2; i >= 0; i-- {
		it := d.timeline[i]
		if !boundary[i+1] && it.afterGap != nil && gap(it.afterGap, -1) == 0 {
			it.above = math.Max(it.above, d.timeline[i+1].above-it.loopHeight)
		}
	}
	headerBottom := 0.
	for _, a := range d.lanes {
		b := objectBounds(a)
		headerBottom = math.Max(headerBottom, b.maxY)
	}
	end, bottom := headerBottom, headerBottom
	nextGap := d.vgap
	explicit := false
	for i, it := range d.timeline {
		y := end + nextGap
		if !explicit {
			y = math.Max(y, bottom+d.vgap+it.above)
		} else if boundary[i] {
			// Rows may share time within a frame. Items outside it must
			// clear its closing border, including when its last gap is zero.
			y = math.Max(y, bottom+it.above+framePadding)
		}
		for _, g := range opening[i] {
			top := y - it.above
			d.frameTop[g] = top
			if g.IsSequenceDiagramBranch() && first[g.Parent] == i && inlineGuard(g.Parent, g) {
				y += math.Max(0, float64(g.LabelDimensions.Height-g.Parent.LabelDimensions.Height))
				continue
			}
			y += framePadding + float64(g.LabelDimensions.Height) + label.PADDING*2
		}
		it.y = y
		if it.edge != nil {
			e := it.edge
			sx, dx := d.owner[e.Src].Center().X, d.owner[e.Dst].Center().X
			if d.owner[e.Src] == d.owner[e.Dst] {
				by := y + it.loopHeight
				e.Route = []*geo.Point{geo.NewPoint(sx, y), geo.NewPoint(sx+selfTravel(e), y), geo.NewPoint(sx+selfTravel(e), by), geo.NewPoint(dx, by)}
				end = by
			} else {
				e.Route = []*geo.Point{geo.NewPoint(sx, y), geo.NewPoint(dx, y)}
				end = y
			}
			e.IsCurve = false
			e.ZIndex = 5
		} else {
			o := it.object
			if containsObject(d.groups, o) {
				d.frameTop[o] = y
				d.frameBottom[o] = y + it.below
			} else if _, isSpan := d.depth[o]; isSpan {
				d.emptySpans[o] = y
			} else {
				o.TopLeft = geo.NewPoint(d.owner[o].Center().X-o.Width/2, y-o.Height/2)
				o.ZIndex = 6
			}
			end = y
			if containsObject(d.groups, o) {
				end += it.below
			}
		}
		bottom = math.Max(bottom, y+it.below)
		for _, span := range it.spanMinimums {
			bottom = math.Max(bottom, span.first.y+span.height)
		}
		for j := len(closing[i]) - 1; j >= 0; j-- {
			g := closing[i][j]
			bottom += framePadding
			d.frameBottom[g] = bottom
		}
		explicit = it.afterGap != nil
		nextGap = gap(it.afterGap, d.vgap)
	}
	// An empty group still has a finite, visible frame at its textual position.
	for _, g := range d.groups {
		if _, placed := d.frameTop[g]; placed {
			continue
		}
		if first[g] >= 0 {
			continue
		}
		y := headerBottom + d.vgap
		p := g.SequenceOrder
		for _, it := range d.timeline {
			if it.pos.less(p) {
				y = math.Max(y, it.y+it.below+framePadding)
			}
		}
		d.frameTop[g] = y
		d.frameBottom[g] = y + math.Max(40, float64(g.LabelDimensions.Height)+framePadding*2)
	}
	return nil
}

func (d *diagram) placeSpans() {
	spans := append([]*d2graph.Object(nil), d.spans...)
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Level() > spans[j].Level() })
	for _, s := range spans {
		minY, maxY := math.Inf(1), math.Inf(-1)
		for _, it := range d.timeline {
			if it.edge == nil {
				continue
			}
			e := it.edge
			if e.Src.IsDescendantOf(s) {
				minY = math.Min(minY, e.Route[0].Y)
				maxY = math.Max(maxY, e.Route[0].Y)
			}
			if e.Dst.IsDescendantOf(s) {
				y := e.Route[len(e.Route)-1].Y
				minY = math.Min(minY, y)
				maxY = math.Max(maxY, y)
			}
		}
		for _, child := range s.ChildrenArray {
			if child.Box != nil && !containsObject(d.groups, child) {
				b := objectBounds(child)
				minY = math.Min(minY, b.minY)
				maxY = math.Max(maxY, b.maxY)
			}
		}
		if y, ok := d.emptySpans[s]; ok {
			minY = math.Min(minY, y)
			maxY = math.Max(maxY, y)
		}
		if math.IsInf(minY, 1) {
			minY = d.owner[s].TopLeft.Y + d.owner[s].Height + d.vgap
			maxY = minY
		}
		minY -= spanPadding
		maxY += spanPadding
		if s.HasLabel() && s.LabelPosition != nil {
			p := label.FromString(*s.LabelPosition)
			switch p {
			case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
				minY -= float64(s.LabelDimensions.Height) + label.PADDING*2
			case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
				maxY += float64(s.LabelDimensions.Height) + label.PADDING*2
			}
		}
		h := math.Max(32, maxY-minY)
		w := activationWidth(s)
		if s.HeightAttr != nil {
			h = math.Max(h, gap(s.HeightAttr, h))
		}
		s.Box = geo.NewBox(geo.NewPoint(d.owner[s].Center().X+float64(d.depth[s])*spanWidth/2-w/2, minY), w, h)
		s.ZIndex = 4
	}
}

func activationWidth(s *d2graph.Object) float64 {
	w := spanWidth
	if s.HasLabel() && s.LabelPosition != nil && !label.FromString(*s.LabelPosition).IsOutside() {
		w = math.Max(w, float64(s.LabelDimensions.Width)+label.PADDING*2)
	}
	if s.WidthAttr != nil {
		w = gap(s.WidthAttr, w)
	}
	return w
}

func (d *diagram) routeEndpoints() {
	for _, it := range d.timeline {
		if it.edge == nil {
			continue
		}
		e := it.edge
		for i, o := range []*d2graph.Object{e.Src, e.Dst} {
			if _, span := d.depth[o]; !span {
				continue
			}
			other := e.Dst
			point := e.Route[0]
			if i == 1 {
				other = e.Src
				point = e.Route[len(e.Route)-1]
			}
			right := d.rank[d.owner[other]] >= d.rank[d.owner[o]]
			point.X = o.TopLeft.X
			if right {
				point.X += o.Width
			}
		}
		if d.owner[e.Src] == d.owner[e.Dst] {
			x := math.Max(e.Route[0].X, e.Route[len(e.Route)-1].X) + selfTravel(e)
			e.Route[1].X = x
			e.Route[2].X = x
		}
		positionMessageLabel(e)
	}
}

// D2 target positions follow the route direction. Sequence positions follow
// the page: a return message's "top" must still be above its arrow.
func positionMessageLabel(e *d2graph.Edge) {
	near := e.Attributes.LabelPosition
	if near == nil {
		setPosition(&e.LabelPosition, label.InsideMiddleCenter)
		return
	}
	position := d2ast.LabelPositionsMapping[near.Value]
	if len(e.Route) == 4 && position.IsOutside() {
		// The midpoint of a self-message is on its vertical leg. Anchor an
		// outside caption along its top or bottom horizontal leg instead.
		fraction := label.CENTER_LABEL_POSITION
		switch position {
		case label.OutsideTopLeft, label.OutsideBottomLeft:
			fraction = label.LEFT_LABEL_POSITION
		case label.OutsideTopRight, label.OutsideBottomRight:
			fraction = label.RIGHT_LABEL_POSITION
		}
		length := geo.Route(e.Route).Length()
		distance := fraction * (e.Route[1].X - e.Route[0].X)
		if bottomMessageLabel(position) {
			distance = length - fraction*(e.Route[2].X-e.Route[3].X)
		}
		percentage := distance / length
		e.LabelPercentage = &percentage
		// The top leg points right and the bottom leg points left. The
		// route-relative top side is outside the loop on both legs.
		position = label.UnlockedTop
	} else if len(e.Route) == 2 && e.Route[0].X > e.Route[1].X {
		position = position.Mirrored()
	}
	resolved := position.String()
	e.LabelPosition = &resolved
}

func bottomMessageLabel(position label.Position) bool {
	return position == label.OutsideBottomLeft || position == label.OutsideBottomCenter || position == label.OutsideBottomRight
}

func messageStrokeWidth(e *d2graph.Edge) float64 {
	if width := e.Style.StrokeWidth; width != nil {
		value, _ := strconv.Atoi(width.Value)
		return float64(value) + width.TweenOffset
	}
	return 2
}

type bounds struct{ minX, minY, maxX, maxY float64 }

func emptyBounds() bounds { return bounds{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)} }
func (b *bounds) point(x, y float64) {
	b.minX = math.Min(b.minX, x)
	b.minY = math.Min(b.minY, y)
	b.maxX = math.Max(b.maxX, x)
	b.maxY = math.Max(b.maxY, y)
}
func (b *bounds) include(other bounds) {
	b.point(other.minX, other.minY)
	b.point(other.maxX, other.maxY)
}
func (b bounds) box() *geo.Box {
	return geo.NewBox(geo.NewPoint(b.minX, b.minY), b.maxX-b.minX, b.maxY-b.minY)
}
func objectBounds(o *d2graph.Object) bounds {
	b := bounds{o.TopLeft.X, o.TopLeft.Y, o.TopLeft.X + o.Width, o.TopLeft.Y + o.Height}
	margin, _ := o.Spacing()
	b.minX -= margin.Left
	b.minY -= margin.Top
	b.maxX += margin.Right
	b.maxY += margin.Bottom
	if o.HasLabel() && o.LabelPosition != nil {
		p := label.FromString(*o.LabelPosition).GetPointOnBox(o.Box, label.PADDING, float64(o.LabelDimensions.Width), float64(o.LabelDimensions.Height))
		b.point(p.X, p.Y)
		b.point(p.X+float64(o.LabelDimensions.Width), p.Y+float64(o.LabelDimensions.Height))
	}
	return b
}
func hasEndpointLabels(e *d2graph.Edge) bool {
	return e.SrcArrowhead != nil && e.SrcArrowhead.Label.Value != "" || e.DstArrowhead != nil && e.DstArrowhead.Label.Value != ""
}

func endpointLabelBounds(c *d2target.Connection) bounds {
	b := emptyBounds()
	for i, t := range []*d2target.Text{c.SrcLabel, c.DstLabel} {
		if t == nil {
			continue
		}
		p := c.GetArrowheadLabelPosition(i == 1)
		b.point(p.X, p.Y)
		b.point(p.X+float64(t.LabelWidth), p.Y+float64(t.LabelHeight))
	}
	return b
}

func edgeBounds(e *d2graph.Edge) bounds {
	b := emptyBounds()
	for _, p := range e.Route {
		b.point(p.X, p.Y)
	}
	if e.Label.Value != "" && e.LabelPosition != nil {
		percentage := .5
		if e.LabelPercentage != nil {
			percentage = *e.LabelPercentage
		}
		p, _ := label.FromString(*e.LabelPosition).GetPointOnRoute(e.Route, messageStrokeWidth(e), percentage, float64(e.LabelDimensions.Width), float64(e.LabelDimensions.Height))
		b.point(p.X, p.Y)
		b.point(p.X+float64(e.LabelDimensions.Width), p.Y+float64(e.LabelDimensions.Height))
	}
	if hasEndpointLabels(e) {
		c := d2exporter.ToConnection(e, nil)
		b.include(endpointLabelBounds(&c))
	}
	return b
}

func (d *diagram) placeMessageGroups() {
	groups := append([]*d2graph.Object(nil), d.groups...)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Level() > groups[j].Level() })
	for _, g := range groups {
		b := emptyBounds()
		for _, it := range d.timeline {
			if d.inGroup(it, g) {
				if it.edge != nil {
					b.include(edgeBounds(it.edge))
				} else {
					b.include(objectBounds(it.object))
				}
			}
		}
		for _, child := range g.ChildrenArray {
			if child.Shape.Value == d2target.ShapeSequenceDiagramEdgeGroup || containsObject(d.groups, child) {
				b.include(objectBounds(child))
			}
		}
		if math.IsInf(b.minX, 1) {
			b.minX = d.lanes[0].Center().X - framePadding
			b.maxX = d.lanes[len(d.lanes)-1].Center().X + framePadding
		}
		b.minX -= framePadding
		b.maxX += framePadding
		b.minY = d.frameTop[g]
		b.maxY = math.Max(b.maxY+framePadding, d.frameBottom[g])
		if b.maxX-b.minX < float64(g.LabelDimensions.Width)+framePadding*2 {
			b.maxX = b.minX + float64(g.LabelDimensions.Width) + framePadding*2
		}
		g.Box = b.box()
		g.ZIndex = 2
		if g.Sequence != nil && g.Sequence.Branched {
			d.placeBranches(g)
		}
	}
}

func inlineGuard(frame, branch *d2graph.Object) bool {
	return frame.HasLabel() && frame.LabelPosition != nil && *frame.LabelPosition == label.InsideTopLeft.String() &&
		branch.LabelPosition != nil && *branch.LabelPosition == label.InsideTopLeft.String()
}

// Child frame extents have already contributed to the enclosing frame. Keep
// just the guard captions and draw separators across that frame's full width.
func (d *diagram) placeBranches(frame *d2graph.Object) {
	branches := append([]*d2graph.Object(nil), frame.ChildrenArray...)
	sort.SliceStable(branches, func(i, j int) bool {
		return d.frameTop[branches[i]] < d.frameTop[branches[j]]
	})
	if len(branches) == 0 {
		return
	}
	first := branches[0]
	inline := inlineGuard(frame, first)
	if inline {
		frame.Width = math.Max(frame.Width, float64(frame.LabelDimensions.Width+first.LabelDimensions.Width)+2*framePadding)
	}
	for i, branch := range branches {
		x, y := frame.TopLeft.X, d.frameTop[branch]
		if i == 0 && inline {
			x += float64(frame.LabelDimensions.Width) + framePadding
			y = frame.TopLeft.Y
		} else if i > 0 {
			d.graph.Edges = append(d.graph.Edges, &d2graph.Edge{
				Src: frame, Dst: branch, ZIndex: 3,
				Route: []*geo.Point{geo.NewPoint(x, y), geo.NewPoint(x+frame.Width, y)},
				Attributes: d2graph.Attributes{Style: d2graph.Style{
					Stroke: frame.Style.Stroke, StrokeWidth: &d2graph.Scalar{Value: "1"}, StrokeDash: &d2graph.Scalar{Value: "5"},
				}},
			})
		}
		branch.Box = geo.NewBox(geo.NewPoint(x, y), float64(branch.LabelDimensions.Width)+2*label.PADDING, float64(branch.LabelDimensions.Height)+2*label.PADDING)
		branch.ZIndex = 3
	}
}
func containsObject(list []*d2graph.Object, o *d2graph.Object) bool {
	for _, v := range list {
		if o == v {
			return true
		}
	}
	return false
}

func (d *diagram) finishActors() {
	end := 0.
	for _, o := range d.graph.Objects {
		if o == d.graph.Root || containsObject(d.actorGroups, o) {
			continue
		}
		end = math.Max(end, objectBounds(o).maxY)
	}
	for _, it := range d.timeline {
		if it.edge != nil {
			end = math.Max(end, edgeBounds(it.edge).maxY)
		}
	}
	end += math.Max(d.vgap, padding)
	for _, a := range d.actors {
		b := objectBounds(a)
		x := a.Center().X
		lineEnd := end
		if d.graph.Root.Sequence != nil && d.graph.Root.Sequence.Mirrored {
			// Structured shapes consume their field children during compilation.
			// Repetitions are graph objects and need a new child map in that case.
			if a.Children == nil {
				a.Children = make(map[string]*d2graph.Object)
			}
			clone := *a
			clone.Parent = a
			clone.Shape.Value = d2target.ShapeSequenceDiagramActor
			clone.Children = make(map[string]*d2graph.Object)
			clone.ChildrenArray = nil
			clone.References = nil
			for i := 0; ; i++ {
				clone.IDVal = "mirror"
				if i > 0 {
					clone.IDVal += strconv.Itoa(i)
				}
				clone.ID = d2format.Format(d2ast.MakeKeyPath([]string{clone.IDVal}))
				if _, exists := a.Children[strings.ToLower(clone.ID)]; !exists {
					break
				}
			}
			clone.Box = geo.NewBox(geo.NewPoint(a.TopLeft.X, end), a.Width, a.Height)
			margin, _ := clone.Spacing()
			clone.TopLeft.Y += margin.Top
			lineEnd = clone.TopLeft.Y - margin.Top
			a.Children[strings.ToLower(clone.ID)] = &clone
			a.ChildrenArray = append(a.ChildrenArray, &clone)
			d.graph.Objects = append(d.graph.Objects, &clone)
		}
		style := d2graph.Style{StrokeDash: &d2graph.Scalar{Value: "6"}, StrokeWidth: &d2graph.Scalar{Value: "2"}}
		if a.Style.Stroke != nil {
			style.Stroke = a.Style.Stroke
		}
		if a.Style.StrokeDash != nil {
			style.StrokeDash = a.Style.StrokeDash
		}
		// Break the lifeline around event bodies and outside labels. Index is
		// part of edge identity, so each visible segment is independently addressable.
		var interruptions []bounds
		for _, o := range d.events {
			if d.owner[o] == a {
				interruptions = append(interruptions, objectBounds(o))
			}
		}
		sort.SliceStable(interruptions, func(i, j int) bool { return interruptions[i].minY < interruptions[j].minY })
		start := b.maxY
		index := 0
		segment := func(stop float64) {
			if stop <= start {
				return
			}
			d.graph.Edges = append(d.graph.Edges, &d2graph.Edge{Index: index, Src: a, Dst: &d2graph.Object{ID: d2sequence.LifelineEndID(a.ID)}, Attributes: d2graph.Attributes{Style: style}, Route: []*geo.Point{geo.NewPoint(x, start), geo.NewPoint(x, stop)}, ZIndex: 1})
			index++
		}
		for _, gap := range interruptions {
			segment(math.Min(lineEnd, gap.minY-label.PADDING))
			start = math.Max(start, gap.maxY+label.PADDING)
		}
		segment(lineEnd)
	}
}

func (d *diagram) placeActorGroups() {
	groups := append([]*d2graph.Object(nil), d.actorGroups...)
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].Level() > groups[j].Level() })
	for _, g := range groups {
		if len(g.ChildrenArray) == 0 {
			g.ZIndex = 0
			continue
		}
		b := emptyBounds()
		for _, o := range d.graph.Objects {
			if o != g && o.IsDescendantOf(g) {
				b.include(objectBounds(o))
			}
		}
		for _, e := range d.graph.Edges {
			if d2sequence.IsLifelineEnd(e.Dst) && e.Src.IsDescendantOf(g) {
				b.include(edgeBounds(e))
			}
		}
		if math.IsInf(b.minX, 1) {
			b = bounds{0, 0, 100, 60}
		}
		b.minX -= framePadding
		b.maxX += framePadding
		b.minY -= framePadding + float64(g.LabelDimensions.Height) + label.PADDING*2
		b.maxY += framePadding
		if b.maxX-b.minX < float64(g.LabelDimensions.Width)+framePadding*2 {
			extra := (float64(g.LabelDimensions.Width) + framePadding*2 - b.maxX + b.minX) / 2
			b.minX -= extra
			b.maxX += extra
		}
		g.Box = b.box()
		g.ZIndex = 0
	}
}

func (d *diagram) fitRoot() {
	b := emptyBounds()
	for _, o := range d.graph.Objects {
		if o != d.graph.Root {
			b.include(objectBounds(o))
		}
	}
	for _, e := range d.graph.Edges {
		b.include(edgeBounds(e))
	}
	title := 0.
	if d.graph.Root.HasLabel() {
		title = float64(d.graph.Root.LabelDimensions.Height) + padding
	}
	dx, dy := padding-b.minX, padding+title-b.minY
	for _, o := range d.graph.Objects {
		if o != d.graph.Root {
			o.TopLeft.X += dx
			o.TopLeft.Y += dy
		}
	}
	for _, e := range d.graph.Edges {
		e.Move(dx, dy)
	}
	w := math.Max(b.maxX-b.minX+padding*2, float64(d.graph.Root.LabelDimensions.Width)+padding*2)
	h := b.maxY - b.minY + padding*2 + title
	d.graph.Root.Box = geo.NewBox(geo.NewPoint(0, 0), w, h)
	setPosition(&d.graph.Root.LabelPosition, label.InsideTopCenter)
}
