package d2talalayout

import (
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/color"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/xsort"
)

type translation struct {
	objectIDs        map[*d2graph.Object]layoutgraph.EntityID
	edgeIDs          map[*d2graph.Edge]layoutgraph.EntityID
	edgeDestinations map[*d2graph.Edge]*d2graph.Object
}

const firstD2SpillEntityID layoutgraph.EntityID = 1 << 32

func d2FNV32(id string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(id))
	return hash.Sum32()
}

type d2EntityIdentity[T comparable] struct {
	entity T
	absID  string
}

type hashedD2EntityIdentity[T comparable] struct {
	d2EntityIdentity[T]
	hash uint32
}

// allocateD2EntityIDs preserves the unsigned FNV-1a ID for every nonzero
// singleton bucket. Only colliding or root-reserved zero hashes receive
// deterministic spill IDs above the uint32 range. Consequently, every bounded
// set of distinct D2 identities has a total, architecture-independent mapping
// without perturbing ordinary graph ordering.
func allocateD2EntityIDs[T comparable](
	ctx context.Context,
	kind string,
	identities []d2EntityIdentity[T],
) (map[T]layoutgraph.EntityID, error) {
	if ctx == nil {
		return nil, fmt.Errorf("allocate D2 %s IDs requires a context", kind)
	}
	hashed := make([]hashedD2EntityIdentity[T], len(identities))
	bucketSizes := make(map[uint32]int, len(identities))
	seenAbsIDs := make(map[string]struct{}, len(identities))
	for i, identity := range identities {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if _, duplicate := seenAbsIDs[identity.absID]; duplicate {
			return nil, fmt.Errorf("D2 %s ID %q is repeated", kind, identity.absID)
		}
		seenAbsIDs[identity.absID] = struct{}{}
		hash := d2FNV32(identity.absID)
		hashed[i] = hashedD2EntityIdentity[T]{d2EntityIdentity: identity, hash: hash}
		bucketSizes[hash]++
	}

	allocated := make(map[T]layoutgraph.EntityID, len(identities))
	ambiguous := make([]hashedD2EntityIdentity[T], 0)
	for i, identity := range hashed {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if identity.hash != 0 && bucketSizes[identity.hash] == 1 {
			allocated[identity.entity] = layoutgraph.EntityID(identity.hash)
			continue
		}
		ambiguous = append(ambiguous, identity)
	}
	xsort.Func(ambiguous, func(a, b hashedD2EntityIdentity[T]) int {
		if order := cmp.Compare(a.hash, b.hash); order != 0 {
			return order
		}
		return cmp.Compare(a.absID, b.absID)
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i, identity := range ambiguous {
		allocated[identity.entity] = firstD2SpillEntityID + layoutgraph.EntityID(i)
	}
	return allocated, nil
}

func d2TALAEdgeAbsID(edge *d2graph.Edge) string {
	if !isSupportedLifelineEdge(edge) {
		return edge.AbsID()
	}
	effectiveEdge := *edge
	effectiveEdge.Dst = edge.Src
	return effectiveEdge.AbsID()
}

func translateGraph(ctx context.Context, g *d2graph.Graph, talaGraph *layoutgraph.Graph, loadExistingLayout bool) (translation, error) {
	bindings := translation{
		objectIDs:        make(map[*d2graph.Object]layoutgraph.EntityID),
		edgeIDs:          make(map[*d2graph.Edge]layoutgraph.EntityID),
		edgeDestinations: make(map[*d2graph.Edge]*d2graph.Object),
	}
	if err := ctx.Err(); err != nil {
		return bindings, err
	}
	if g == nil || g.Root == nil {
		return bindings, fmt.Errorf("tala requires a D2 graph with a root object")
	}
	if talaGraph == nil {
		return bindings, fmt.Errorf("tala requires a destination graph")
	}
	visits, objects, err := collectD2ObjectVisits(ctx, g.Root)
	if err != nil {
		return bindings, err
	}
	if err := validateD2GraphStructure(g, visits, objects); err != nil {
		return bindings, err
	}
	if err := validateD2Objects(ctx, visits, loadExistingLayout); err != nil {
		return bindings, err
	}
	if err := validateD2Edges(ctx, g.Edges, objects); err != nil {
		return bindings, err
	}
	objectIdentities := make([]d2EntityIdentity[*d2graph.Object], len(visits))
	for i, visit := range visits {
		objectIdentities[i] = d2EntityIdentity[*d2graph.Object]{
			entity: visit.object,
			absID:  visit.object.AbsID(),
		}
	}
	objectIDs, err := allocateD2EntityIDs(ctx, "object", objectIdentities)
	if err != nil {
		return bindings, err
	}
	edgeIdentities := make([]d2EntityIdentity[*d2graph.Edge], len(g.Edges))
	edgeAbsIDs := make(map[*d2graph.Edge]string, len(g.Edges))
	for i, edge := range g.Edges {
		absID := d2TALAEdgeAbsID(edge)
		edgeAbsIDs[edge] = absID
		edgeIdentities[i] = d2EntityIdentity[*d2graph.Edge]{entity: edge, absID: absID}
	}
	edgeIDs, err := allocateD2EntityIDs(ctx, "edge", edgeIdentities)
	if err != nil {
		return bindings, err
	}

	toNode := make(map[*d2graph.Object]*layoutgraph.Node)
	if g.Root.Direction.Value != "" {
		talaGraph.Directions[nil] = parseDirection(g.Root.Direction.Value)
	}
	if g.Root.Shape.Value == d2target.ShapeHierarchy {
		talaGraph.IsRootHierarchy = true
	}

	for _, visit := range visits {
		if err := ctx.Err(); err != nil {
			return bindings, err
		}
		obj, parent := visit.object, visit.parent
		intID := objectIDs[obj]
		bindings.objectIDs[obj] = intID
		n := layoutgraph.NewNode(intID, obj.Width, obj.Height)
		if obj.Top != nil && obj.Left != nil {
			top, _ := parseD2IntegerAttribute(obj.ID, "top", obj.Top, -maxResultCoordinate, maxResultCoordinate)
			left, _ := parseD2IntegerAttribute(obj.ID, "left", obj.Left, -maxResultCoordinate, maxResultCoordinate)
			n.FixedTopLeft = geo.NewPoint(float64(left), float64(top))
		}
		toNode[obj] = n
		if obj.WidthAttr != nil {
			desiredWidth, _ := parseD2IntegerAttribute(obj.ID, "width", obj.WidthAttr, 1, maxResultCoordinate)
			n.DesiredWidth = new(float64(desiredWidth))
		}
		if obj.HeightAttr != nil {
			desiredHeight, _ := parseD2IntegerAttribute(obj.ID, "height", obj.HeightAttr, 1, maxResultCoordinate)
			n.DesiredHeight = new(float64(desiredHeight))
		}

		if loadExistingLayout {
			if obj.TopLeft != nil {
				n.TopLeft = obj.TopLeft.Copy()
			}
		}

		if d2target.IsShape(obj.Shape.Value) {
			dslShape := strings.ToLower(obj.Shape.Value)
			if dslShape == d2target.ShapeHierarchy {
				n.ForceHierarchy = true
			}
			if shapeType, ok := d2target.DSL_SHAPE_TO_SHAPE_TYPE[dslShape]; ok {
				n.SetShape(shapeType)
			}
			if obj.SQLTable != nil {
				n.SetNumColumns(len(obj.SQLTable.Columns))
			}
		}
		if obj.Icon != nil {
			n.InitIcon()
			if obj.IconPosition != nil {
				n.Icon.Position = label.FromString(*obj.IconPosition)
			}
		}
		if obj.Direction.Value != "" {
			talaGraph.Directions[n] = parseDirection(obj.Direction.Value)
		}

		// Only automatic font sizes participate in TALA's density scaling.
		// D2 has already measured the label using any authored font size.
		if obj.Style.FontSize == nil || obj.HasLayoutFontSize() {
			n.FontSize = new(obj.Text().FontSize)
			if obj.Class != nil || obj.SQLTable != nil {
				*n.FontSize -= d2target.HeaderFontAdd
			}
		}

		if obj.HasLabel() {
			n.Label = &layoutgraph.Label{
				Text:       obj.Label.Value,
				Width:      float64(obj.LabelDimensions.Width),
				Height:     float64(obj.LabelDimensions.Height),
				KeepInBody: obj.Language != "" && obj.Language != "latex" && obj.Language != "markdown",
			}
			if obj.LabelPosition != nil {
				n.Label.Position = label.FromString(*obj.LabelPosition)
			}
		}

		if obj.Is3D() {
			n.Is3D = true
		}
		if obj.IsMultiple() {
			n.IsMultiple = true
		}

		if obj.Style.Opacity != nil {
			f, err := strconv.ParseFloat(obj.Style.Opacity.Value, 64)
			if err == nil && f == 0 {
				n.IsInvisible = true
			}
		}
		if obj.Label.Value == "" && obj.Icon == nil && unpaintedBody(obj, g) {
			n.IsInvisible = true
		}

		n.D2ID = new(obj.AbsID())

		talaGraph.AddNewNodeToContainer(toNode[parent], n)
	}

	unpaintedSteps := map[*d2graph.Object]bool{}
	// The exporter retains g.Objects order for sibling shapes of equal ZIndex.
	for i, obj := range g.Objects {
		if err := ctx.Err(); err != nil {
			return bindings, err
		}
		if n := toNode[obj]; n.IsSequenceStep() {
			low, high := paintAlphaRange(obj.Style.Fill)
			opacity := 1.
			if obj.Style.Opacity != nil {
				opacity, _ = strconv.ParseFloat(obj.Style.Opacity.Value, 64)
			}
			// Even black and white underneath this fill can differ by at most
			// three sRGB channel levels, including a 99% opaque face.
			covers := 255-float64(low)*opacity <= 3
			n.StepPaint = &layoutgraph.StepPaint{Order: i + 1, ZIndex: obj.ZIndex, Opaque: covers}
			unpaintedSteps[obj] = high == 0 && invisibleOutline(obj) && !hasFillPattern(obj, g)
		}
		if obj.NearKey != nil {
			if len(obj.NearKey.Path) > maxInputTreeDepth {
				return bindings, fmt.Errorf("D2 object %q near key depth exceeds limit %d", obj.AbsID(), maxInputTreeDepth)
			}
			for partIndex, part := range obj.NearKey.Path {
				if part == nil || part.Unbox() == nil {
					return bindings, fmt.Errorf("D2 object %q has an invalid near key part at index %d", obj.AbsID(), partIndex)
				}
			}
			key := d2graph.Key(obj.NearKey)
			if len(key) == 0 {
				return bindings, fmt.Errorf("D2 object %q has an empty near key", obj.AbsID())
			}
			near, found, err := findD2Object(ctx, g.Root, key)
			if err != nil {
				return bindings, err
			}
			if found {
				nearNode, translated := toNode[near]
				if !translated {
					return bindings, fmt.Errorf("D2 object %q has a near key outside the object tree", obj.AbsID())
				}
				toNode[obj].AddNear(nearNode)
				continue
			}
			if len(key) != 1 {
				return bindings, fmt.Errorf("D2 object %q has a near key outside the object tree", obj.AbsID())
			}
			if _, isConst := d2ast.NearConstants[key[0]]; !isConst {
				return bindings, fmt.Errorf("D2 object %q has a near key outside the object tree", obj.AbsID())
			}
		}
	}

	for _, e := range g.Edges {
		if err := ctx.Err(); err != nil {
			return bindings, err
		}
		if e == nil || e.Src == nil || e.Dst == nil {
			return bindings, fmt.Errorf("tala cannot translate an edge with a nil endpoint")
		}

		edgeID := edgeAbsIDs[e]
		edgeSrc, edgeDst := e.Src, e.Dst
		srcColumnIndex, dstColumnIndex := e.SrcTableColumnIndex, e.DstTableColumnIndex
		srcArrow, dstArrow := e.SrcArrow, e.DstArrow
		srcAttrs, dstAttrs := e.SrcArrowhead, e.DstArrowhead
		reversed := srcArrow && !dstArrow
		if reversed {
			edgeSrc, edgeDst = edgeDst, edgeSrc
			srcColumnIndex, dstColumnIndex = dstColumnIndex, srcColumnIndex
			srcArrow, dstArrow = dstArrow, srcArrow
			srcAttrs, dstAttrs = dstAttrs, srcAttrs
		} else if isSupportedLifelineEdge(e) {
			// Sequence diagram lifelines aren't connected on both ends.
			edgeDst = edgeSrc
			bindings.edgeDestinations[e] = edgeDst
		}
		talaEdge := talaGraph.Connect(toNode[edgeSrc], toNode[edgeDst])
		talaEdge.ReversedForLayout = reversed
		talaEdge.KeepExplicit = e.Tooltip != nil || e.Link != nil || e.Icon != nil || len(e.Classes) > 0 ||
			e.SrcArrowhead != nil || e.DstArrowhead != nil || e.ZIndex != 0
		if talaEdge.From.IsSequenceStep() && talaEdge.To.IsSequenceStep() && (unpaintedSteps[edgeSrc] || unpaintedSteps[edgeDst]) {
			// The text or icon may be visible even when the connecting wedge is not.
			talaEdge.KeepExplicit = true
		}
		talaEdge.SourceArrowhead, talaEdge.SourceArrowheadLabel = translateArrowhead(srcArrow, srcAttrs)
		talaEdge.TargetArrowhead, talaEdge.TargetArrowheadLabel = translateArrowhead(dstArrow, dstAttrs)
		if loadExistingLayout {
			for _, point := range e.Route {
				talaEdge.Points = append(talaEdge.Points, point.Copy())
			}
			if reversed {
				slices.Reverse(talaEdge.Points)
			}
		}

		talaEdge.ID = edgeIDs[e]
		bindings.edgeIDs[e] = talaEdge.ID
		talaEdge.D2ID = new(edgeID)
		talaEdge.Style = cloneStyle(e.Style)
		minWidth, minHeight, _ := talaEdge.LabelMinDimensions(e.LabelDimensions.Width, e.LabelDimensions.Height)
		if a, b := talaEdge.From, talaEdge.To; a.IsSequenceStep() && b.IsSequenceStep() {
			// Explicit steps can be placed in either order on a row. Protect both
			// facing overhangs of each fixed caption and icon above or below it.
			ae := a.StepDecorations(geo.NewBox(&geo.Point{}, a.Width, a.Height))
			be := b.StepDecorations(geo.NewBox(&geo.Point{}, b.Width, b.Height))
			for _, left := range ae {
				if left.Side != geo.Top && left.Side != geo.Bottom {
					continue
				}
				for _, right := range be {
					if right.Side != left.Side {
						continue
					}
					l, r := left.Box, right.Box
					gap := max(l.TopLeft.X+l.Width-a.Width-r.TopLeft.X, r.TopLeft.X+r.Width-b.Width-l.TopLeft.X) + label.PADDING
					minWidth = max(minWidth, int64(math.Ceil(gap)))
				}
			}
		}
		if minWidth > maxResultCoordinate || minHeight > maxResultCoordinate {
			return bindings, fmt.Errorf("D2 edge %q minimum dimensions exceed supported limit %d", e.AbsID(), maxResultCoordinate)
		}
		talaEdge.MinWidth = int(minWidth)
		talaEdge.MinHeight = int(minHeight)

		if e.Label.Value != "" {
			talaEdge.Label = &layoutgraph.Label{
				Text:   e.Label.Value,
				Width:  float64(e.LabelDimensions.Width),
				Height: float64(e.LabelDimensions.Height),
			}
			if loadExistingLayout && e.LabelPosition != nil {
				talaEdge.Label.Position = label.FromString(*e.LabelPosition)
				if e.LabelPercentage != nil {
					talaEdge.LabelPercentage = *e.LabelPercentage
				}
				if e.SrcArrow && !e.DstArrow {
					// The route was reversed above. Preserve the label's
					// physical position so rerouting reserves its actual box.
					talaEdge.Label.Position = talaEdge.Label.Position.Mirrored()
					if e.LabelPercentage != nil || talaEdge.Label.Position.IsUnlocked() {
						talaEdge.LabelPercentage = 1 - talaEdge.LabelPercentage
					}
				}
			}
		}

		if edgeSrc.SQLTable != nil && srcColumnIndex != nil {
			talaEdge.FromTableColumnIndex = new(*srcColumnIndex)
		}
		if edgeDst.SQLTable != nil && dstColumnIndex != nil {
			talaEdge.ToTableColumnIndex = new(*dstColumnIndex)
		}
		if e.Style.Opacity != nil {
			f, err := strconv.ParseFloat(e.Style.Opacity.Value, 64)
			if err == nil && f == 0 {
				talaEdge.IsInvisible = true
				talaEdge.MinHeight = 0
				talaEdge.MinWidth = 0
			}
		}
		if e.Style.Stroke != nil && strings.EqualFold(e.Style.Stroke.Value, "transparent") {
			talaEdge.IsInvisible = true
		}
	}

	return bindings, nil
}

// Unspecified and theme paints take their opaque defaults. A gradient with
// clear stops throughout paints no body; the least opaque stop limits how
// much of the whole face can cover for a neighboring annotation.
func paintAlphaRange(paint *d2graph.Scalar) (uint8, uint8) {
	if paint == nil {
		return 255, 255
	}
	alpha := func(value string) uint8 {
		if c, ok := color.ParseRGBA(value); ok {
			return c.A
		}
		return 255
	}
	if color.IsGradient(paint.Value) {
		g, err := color.ParseGradient(paint.Value)
		if err != nil {
			return 0, 255
		}
		low, high := uint8(255), uint8(0)
		for _, stop := range g.ColorStops {
			a := alpha(stop.Color)
			low, high = min(low, a), max(high, a)
		}
		return low, high
	}
	a := alpha(paint.Value)
	return a, a
}

func invisibleOutline(obj *d2graph.Object) bool {
	if obj.Style.StrokeWidth != nil {
		width, err := strconv.Atoi(obj.Style.StrokeWidth.Value)
		if err == nil && float64(width)+obj.Style.StrokeWidth.TweenOffset <= 0 {
			return true
		}
	}
	_, high := paintAlphaRange(obj.Style.Stroke)
	return high == 0
}

func hasFillPattern(obj *d2graph.Object, g *d2graph.Graph) bool {
	if pattern := obj.Style.FillPattern; pattern != nil {
		return pattern.Value != "" && !strings.EqualFold(pattern.Value, "none")
	}
	if theme := g.Theme; theme != nil {
		if theme.SpecialRules.ContainerDots {
			return len(obj.ChildrenArray) != 0
		}
		return theme.SpecialRules.AllPaper
	}
	return false
}

func unpaintedBody(obj *d2graph.Object, g *d2graph.Graph) bool {
	_, high := paintAlphaRange(obj.Style.Fill)
	return high == 0 && invisibleOutline(obj) && !hasFillPattern(obj, g)
}

func cloneStyle(style d2graph.Style) layoutgraph.EdgeStyle {
	cloneScalar := func(scalar *d2graph.Scalar) *layoutgraph.StyleScalar {
		if scalar == nil {
			return nil
		}
		// MapKey points back into the mutable D2 AST. TALA only consumes the
		// scalar value, so do not retain that provenance in the isolated graph.
		return &layoutgraph.StyleScalar{Value: scalar.Value}
	}
	return layoutgraph.EdgeStyle{
		Opacity:       cloneScalar(style.Opacity),
		Stroke:        cloneScalar(style.Stroke),
		Fill:          cloneScalar(style.Fill),
		FillPattern:   cloneScalar(style.FillPattern),
		StrokeWidth:   cloneScalar(style.StrokeWidth),
		StrokeDash:    cloneScalar(style.StrokeDash),
		BorderRadius:  cloneScalar(style.BorderRadius),
		Shadow:        cloneScalar(style.Shadow),
		ThreeDee:      cloneScalar(style.ThreeDee),
		Multiple:      cloneScalar(style.Multiple),
		Font:          cloneScalar(style.Font),
		FontSize:      cloneScalar(style.FontSize),
		FontColor:     cloneScalar(style.FontColor),
		Animated:      cloneScalar(style.Animated),
		Bold:          cloneScalar(style.Bold),
		Italic:        cloneScalar(style.Italic),
		Underline:     cloneScalar(style.Underline),
		Filled:        cloneScalar(style.Filled),
		DoubleBorder:  cloneScalar(style.DoubleBorder),
		TextTransform: cloneScalar(style.TextTransform),
	}
}

func parseDirection(direction string) geo.Orientation {
	switch direction {
	case "up":
		return geo.Top
	case "down":
		return geo.Bottom
	case "left":
		return geo.Left
	case "right":
		return geo.Right
	default:
		return geo.NONE
	}
}

// RouteEdges reroutes edges in g. It mutates g and must not run concurrently
// against the same graph.
func RouteEdges(ctx context.Context, g *d2graph.Graph, edges []*d2graph.Edge) (err error) {
	defer recoverAsError("edge routing", &err)
	if ctx == nil {
		return fmt.Errorf("TALA edge routing requires a context")
	}
	if err := validateRequestedD2Edges(g, edges); err != nil {
		return err
	}

	talaGraph := layoutgraph.NewGraph()
	bindings, err := translateGraph(ctx, g, talaGraph, true)
	if err != nil {
		return err
	}

	talaEdgeByID := make(map[layoutgraph.EntityID]*layoutgraph.Edge, len(talaGraph.Edges))
	for _, edge := range talaGraph.Edges {
		talaEdgeByID[edge.ID] = edge
	}
	talaEdges := make([]*layoutgraph.Edge, 0, len(edges))
	for _, e := range edges {
		if e == nil {
			return fmt.Errorf("cannot route a nil D2 edge")
		}
		talaEdgeID, bound := bindings.edgeIDs[e]
		talaEdge, found := talaEdgeByID[talaEdgeID]
		if !bound || !found {
			return fmt.Errorf("could not find edge %#v in graph", e.AbsID())
		}
		talaEdges = append(talaEdges, talaEdge)
	}

	workCtx := ctx
	cancelWork := func() {}
	if ctx.Done() == nil {
		workCtx, cancelWork = context.WithCancel(ctx)
	}
	defer cancelWork()
	err = routing.RouteEdges(workCtx, talaGraph, talaEdges)
	if err != nil {
		return preferContextError(ctx, "edge routing", err)
	}

	if supportsRouteRefinements(g) {
		if err := routing.RefineOrdinaryFanLanes(workCtx, talaGraph, talaEdges); err != nil {
			return preferContextError(ctx, "edge routing", err)
		}
	}

	patch, err := buildRoutePatch(workCtx, g, bindings, talaEdges)
	if err != nil {
		return preferContextError(ctx, "edge routing", err)
	}
	return commitRoutePatch(workCtx, patch)
}

func recoverAsError(operation string, err *error) {
	if recover() != nil {
		*err = fmt.Errorf("TALA %s failed due to an internal invariant", operation)
	}
}

// D2 can label an endpoint even when the connection has no arrow there.
func translateArrowhead(enabled bool, attrs *d2graph.Attributes) (layoutgraph.Arrowhead, *layoutgraph.Label) {
	arrow := d2target.NoArrowhead
	if enabled {
		arrow = d2target.DefaultArrowhead
		if attrs != nil {
			arrow = attrs.ToArrowhead()
		}
	}
	if attrs == nil || attrs.Label.Value == "" {
		return layoutgraph.Arrowhead(arrow), nil
	}
	return layoutgraph.Arrowhead(arrow), &layoutgraph.Label{
		Text: attrs.Label.Value, Width: float64(attrs.LabelDimensions.Width), Height: float64(attrs.LabelDimensions.Height),
	}
}
