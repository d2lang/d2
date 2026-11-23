// Package d2elklayout adapts D2 graphs to the native Go port of ELK.
//
// Coordinates are relative to parents.
// See https://www.eclipse.org/elk/documentation/tooldevelopers/graphdatastructure/coordinatesystem.html
package d2elklayout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/util-go/xdefer"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/internal/layoutguard"
	"github.com/d2lang/d2/d2layouts/internal/ranklabel"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/shape"
)

type ELKNode struct {
	ID            string      `json:"id"`
	X             float64     `json:"x"`
	Y             float64     `json:"y"`
	Width         float64     `json:"width"`
	Height        float64     `json:"height"`
	Children      []*ELKNode  `json:"children,omitempty"`
	Ports         []*ELKPort  `json:"ports,omitempty"`
	Labels        []*ELKLabel `json:"labels,omitempty"`
	LayoutOptions *elkOpts    `json:"layoutOptions,omitempty"`
}

type PortSide string

const (
	South PortSide = "SOUTH"
	North PortSide = "NORTH"
	East  PortSide = "EAST"
	West  PortSide = "WEST"
)

type Direction string

const (
	Down  Direction = "DOWN"
	Up    Direction = "UP"
	Right Direction = "RIGHT"
	Left  Direction = "LEFT"
)

type ELKPort struct {
	ID            string   `json:"id"`
	X             float64  `json:"x"`
	Y             float64  `json:"y"`
	Width         float64  `json:"width"`
	Height        float64  `json:"height"`
	LayoutOptions *elkOpts `json:"layoutOptions,omitempty"`
}

type ELKLabel struct {
	Text          string   `json:"text"`
	X             float64  `json:"x"`
	Y             float64  `json:"y"`
	Width         float64  `json:"width"`
	Height        float64  `json:"height"`
	LayoutOptions *elkOpts `json:"layoutOptions,omitempty"`
}

type ELKPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type ELKEdgeSection struct {
	Start      ELKPoint   `json:"startPoint"`
	End        ELKPoint   `json:"endPoint"`
	BendPoints []ELKPoint `json:"bendPoints,omitempty"`
}

type ELKEdge struct {
	ID        string           `json:"id"`
	Sources   []string         `json:"sources"`
	Targets   []string         `json:"targets"`
	Sections  []ELKEdgeSection `json:"sections,omitempty"`
	Labels    []*ELKLabel      `json:"labels,omitempty"`
	Container string           `json:"container"`
}

type ELKGraph struct {
	ID            string     `json:"id"`
	LayoutOptions *elkOpts   `json:"layoutOptions"`
	Children      []*ELKNode `json:"children,omitempty"`
	Edges         []*ELKEdge `json:"edges,omitempty"`
}

type ConfigurableOpts struct {
	LayeredOptions
	AlgorithmOptions
	Algorithm       string `json:"elk.algorithm,omitempty"`
	NodeSpacing     int    `json:"spacing.nodeNodeBetweenLayers,omitempty"`
	Padding         string `json:"elk.padding,omitempty"`
	EdgeNodeSpacing int    `json:"spacing.edgeNodeBetweenLayers,omitempty"`
	EdgeEdgeSpacing *int   `json:"spacing.edgeEdgeBetweenLayers,omitempty"`
	SelfLoopSpacing int    `json:"elk.spacing.nodeSelfLoop"`
}

const default_edge_edge_spacing = 50

var DefaultOpts = ConfigurableOpts{
	Algorithm:       "layered",
	NodeSpacing:     70.0,
	Padding:         "[top=50,left=50,bottom=50,right=50]",
	EdgeNodeSpacing: 40.0,
	EdgeEdgeSpacing: go2.Pointer(default_edge_edge_spacing),
	SelfLoopSpacing: 50.0,
}

var port_spacing = 40.

const edge_node_spacing = 40

type objectDegree struct {
	incoming float64
	outgoing float64
}

type selfLoopLabelSize struct {
	width  int
	height int
}

// elkGraphStats contains the D2-specific graph-wide measurements used while
// adapting a graph to ELK. Keeping them here avoids rescanning every edge for
// every object and container.
type elkGraphStats struct {
	degrees          map[*d2graph.Object]objectDegree
	selfLoopLabelMax map[*d2graph.Object]selfLoopLabelSize
}

func collectELKGraphStats(g *d2graph.Graph) elkGraphStats {
	return collectELKGraphStatsInto(g, elkGraphStats{
		degrees:          make(map[*d2graph.Object]objectDegree, len(g.Objects)),
		selfLoopLabelMax: make(map[*d2graph.Object]selfLoopLabelSize),
	})
}

// collectELKGraphStatsInto is collectELKGraphStats filling the given empty
// maps.
func collectELKGraphStatsInto(g *d2graph.Graph, stats elkGraphStats) elkGraphStats {
	for _, edge := range g.Edges {
		srcDegree := stats.degrees[edge.Src]
		srcDegree.outgoing++
		stats.degrees[edge.Src] = srcDegree

		dstDegree := stats.degrees[edge.Dst]
		dstDegree.incoming++
		stats.degrees[edge.Dst] = dstDegree

		if edge.Src != edge.Dst || edge.Label.Value == "" || edge.Src.Parent == nil {
			continue
		}
		parent := edge.Src.Parent
		labelSize := stats.selfLoopLabelMax[parent]
		labelSize.width = go2.Max(labelSize.width, edge.LabelDimensions.Width)
		labelSize.height = go2.Max(labelSize.height, edge.LabelDimensions.Height)
		stats.selfLoopLabelMax[parent] = labelSize
	}
	return stats
}

func (s elkGraphStats) maxSelfLoopLabel(parent *d2graph.Object, isWidth bool) int {
	size := s.selfLoopLabelMax[parent]
	if isWidth {
		return size.width
	}
	return size.height
}

type elkOpts struct {
	FixedAlignment        string    `json:"elk.layered.nodePlacement.bk.fixedAlignment,omitempty"`
	Thoroughness          int       `json:"elk.layered.thoroughness,omitempty"`
	Direction             Direction `json:"elk.direction"`
	HierarchyHandling     string    `json:"elk.hierarchyHandling,omitempty"`
	InlineEdgeLabels      bool      `json:"elk.edgeLabels.inline,omitempty"`
	ConsiderModelOrder    string    `json:"elk.layered.considerModelOrder.strategy,omitempty"`
	CycleBreakingStrategy string    `json:"elk.layered.cycleBreaking.strategy,omitempty"`

	SelfLoopDistribution string `json:"elk.layered.edgeRouting.selfLoopDistribution,omitempty"`
	LayerConstraint      string `json:"elk.layered.layering.layerConstraint,omitempty"`

	NodeSizeConstraints string `json:"elk.nodeSize.constraints,omitempty"`
	ContentAlignment    string `json:"elk.contentAlignment,omitempty"`
	NodeSizeMinimum     string `json:"elk.nodeSize.minimum,omitempty"`

	PortSide        PortSide `json:"elk.port.side,omitempty"`
	PortConstraints string   `json:"elk.portConstraints,omitempty"`

	ConfigurableOpts
	RankGroups string `json:"d2.rankGroups,omitempty"`
}

// edgeEdgeSpacingInto stores the edge-edge spacing to send in value and
// returns value.
func edgeEdgeSpacingInto(opts *ConfigurableOpts, value *int) *int {
	// Callers predating this option leave the field nil, and D2 sent 50 before
	// it existed. Nil keeps that rather than dropping to ELK's default.
	if opts.EdgeEdgeSpacing == nil {
		*value = default_edge_edge_spacing
	} else {
		*value = *opts.EdgeEdgeSpacing
	}
	return value
}

func newRootLayoutOptions(opts *ConfigurableOpts) *elkOpts {
	o := rootLayoutOptions(opts, new(float64), new(int))
	return &o
}

// rootLayoutOptions is *newRootLayoutOptions(opts) with the values its
// pointers would point to stored in edgeNode and edgeEdge.
func rootLayoutOptions(opts *ConfigurableOpts, edgeNode *float64, edgeEdge *int) elkOpts {
	return elkOpts{
		Thoroughness:          8,
		HierarchyHandling:     "INCLUDE_CHILDREN",
		FixedAlignment:        "BALANCED",
		ConsiderModelOrder:    "NODES_AND_EDGES",
		CycleBreakingStrategy: "GREEDY_MODEL_ORDER",
		NodeSizeConstraints:   "MINIMUM_SIZE",
		ContentAlignment:      "H_CENTER V_CENTER",
		ConfigurableOpts: ConfigurableOpts{
			LayeredOptions:   opts.parentOptionsInto(true, edgeNode),
			AlgorithmOptions: opts.AlgorithmOptions,
			Algorithm:        opts.Algorithm,
			NodeSpacing:      opts.NodeSpacing,
			EdgeNodeSpacing:  opts.EdgeNodeSpacing,
			EdgeEdgeSpacing:  edgeEdgeSpacingInto(opts, edgeEdge),
			SelfLoopSpacing:  opts.SelfLoopSpacing,
		},
	}
}

func newContainerLayoutOptions(opts *ConfigurableOpts) *elkOpts {
	o := containerLayoutOptions(opts, new(float64), new(int))
	return &o
}

// containerLayoutOptions is *newContainerLayoutOptions(opts) with the values
// its pointers would point to stored in edgeNode and edgeEdge.
func containerLayoutOptions(opts *ConfigurableOpts, edgeNode *float64, edgeEdge *int) elkOpts {
	return elkOpts{
		Thoroughness:      8,
		HierarchyHandling: "INCLUDE_CHILDREN",
		FixedAlignment:    "BALANCED",
		// ELK 0.12 crashes on compound subgraphs when model-order
		// processing is enabled below the root. Keep it disabled there.
		ConsiderModelOrder:    "NONE",
		CycleBreakingStrategy: "GREEDY",
		NodeSizeConstraints:   "MINIMUM_SIZE",
		ContentAlignment:      "H_CENTER V_CENTER",
		ConfigurableOpts: ConfigurableOpts{
			LayeredOptions:  opts.parentOptionsInto(false, edgeNode),
			NodeSpacing:     opts.NodeSpacing,
			EdgeNodeSpacing: opts.EdgeNodeSpacing,
			EdgeEdgeSpacing: edgeEdgeSpacingInto(opts, edgeEdge),
			SelfLoopSpacing: opts.SelfLoopSpacing,
			Padding:         opts.Padding,
		},
	}
}

func DefaultLayout(ctx context.Context, g *d2graph.Graph) (err error) {
	return Layout(ctx, g, nil)
}

func Layout(ctx context.Context, g *d2graph.Graph, opts *ConfigurableOpts) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer xdefer.Errorf(&err, "failed to ELK layout")
	if err := layoutguard.CheckGraph(ctx, "ELK", g); err != nil {
		return err
	}
	// The request is built in scratch storage that later calls reuse. It is
	// released when the layout returns, not deferred: a layout that panics
	// drops it rather than offering it for reuse while the panic unwinds.
	scratch := acquireScratch(g)
	err = layoutWith(ctx, g, opts, scratch)
	releaseScratch(scratch)
	return err
}

// layoutWith is Layout after the graph check, building the request in
// scratch.
func layoutWith(ctx context.Context, g *d2graph.Graph, opts *ConfigurableOpts, scratch *elkScratch) (err error) {
	opts, err = scratch.graphOptions.of(g, opts)
	if err != nil {
		return err
	}
	padding, err := readPaddingCached(opts.Padding)
	if err != nil {
		return err
	}
	// One state per object the walk reaches (see walkObjects), holding the
	// edge statistics the walk reads.
	objects := scratch.objectStates
	visits, rootRange := walkObjects(g, objects, &scratch.objects, scratch.visits[:0])
	scratch.visits = visits
	scratch.collectEdgeStats(g)

	// The root's option struct and the values its pointers point to are
	// kept in the scratch, as is the graph.
	values := &scratch.optionValues
	scratch.rootOpts = rootLayoutOptions(opts, &values.rootEdgeNode, &values.rootEdgeEdge)
	// ELK includes outside labels and icons in the layout box. Symmetric
	// margins on the flow axis keep the visible ranked shapes centered when
	// those margins are stripped on return, including inside compounds.
	var rankBoxes map[*d2graph.Object]bool
	groups := g.SameRankGroups()
	if len(groups) > 0 {
		if !opts.usesAlgorithm("layered") {
			return fmt.Errorf("rank groups require ELK Layered, got %q", opts.Algorithm)
		}
		rankBoxes = make(map[*d2graph.Object]bool)
		ids := make([][]string, len(groups))
		for i, group := range groups {
			for _, obj := range group {
				ids[i] = append(ids[i], obj.AbsID())
				for p := obj; p != nil && p != g.Root && !rankBoxes[p]; p = p.Parent {
					rankBoxes[p] = true
				}
			}
		}
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		scratch.rootOpts.RankGroups = string(encoded)
	}
	elkGraph := &scratch.graph
	*elkGraph = ELKGraph{
		ID:            "",
		LayoutOptions: &scratch.rootOpts,
	}
	if elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing == DefaultOpts.SelfLoopSpacing {
		// +5 for a tiny bit of padding
		elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing = go2.Max(elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing, scratch.selfLoop(g, g.Root).of(g.Root.Direction.Value == "down" || g.Root.Direction.Value == "" || g.Root.Direction.Value == "up")/2+5)
	}
	switch g.Root.Direction.Value {
	case "down":
		elkGraph.LayoutOptions.Direction = Down
	case "up":
		elkGraph.LayoutOptions.Direction = Up
	case "right":
		elkGraph.LayoutOptions.Direction = Right
	case "left":
		elkGraph.LayoutOptions.Direction = Left
	default:
		elkGraph.LayoutOptions.Direction = Down
	}
	// The root's option struct is now complete: a function of the graph
	// options (when they are the memo's result, of its generation), the
	// self-loop spacing, the direction and the edge-edge spacing value (see
	// rootOptionsKey).
	scratch.request.rootKey = rootOptionsKey{
		generation: scratch.graphOptions.resultGeneration(opts),
		selfLoop:   elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing,
		direction:  elkGraph.LayoutOptions.Direction,
		edgeEdge:   values.rootEdgeEdge,
		rankGroups: scratch.rootOpts.RankGroups,
	}

	// set label and icon positions for ELK
	var store elkGraphStore
	store.reserve(g)
	for _, obj := range g.Objects {
		positionLabelsIconsWith(obj, store.stringPointer)
	}

	// Option structs: leaves, ports and edge labels share the request's
	// structs of the static option sets (never written); containers start
	// from one copy of the container options of this call.
	shared := &scratch.request.shared
	containerBase := containerLayoutOptions(opts, &values.containerEdgeNode, &values.containerEdgeEdge)
	vertical := elkGraph.LayoutOptions.Direction == Down || elkGraph.LayoutOptions.Direction == Up

	// Per object: its ELK node, the margin taken off after layout and its
	// absolute ID (computed once; nothing changes IDs or parents here), in
	// its state; per edge, its ELK edge.
	edgeStates := scratch.edgeStates
	if len(g.Root.ChildrenArray) > 0 {
		elkGraph.Children = scratch.nodeLists.makeCap(0, len(g.Root.ChildrenArray))
	}
	if len(g.Edges) > 0 {
		elkGraph.Edges = scratch.edgeLists.makeCap(0, len(g.Edges))
	}

	// The walk's visits (depth first through ChildrenArray, objects with a
	// Parent). An object visited twice shares its state, whose fields hold
	// the latest visit's values: what Layout's map by object, written at the
	// end of each visit, held.
	for _, visit := range visits {
		obj, parent, state := visit.obj, visit.parent, visit.state
		degree := state.degree
		incoming := degree.incoming
		outgoing := degree.outgoing
		if incoming >= 2 || outgoing >= 2 {
			switch g.Root.Direction.Value {
			case "right", "left":
				if obj.Attributes.HeightAttr == nil {
					obj.Height = math.Max(obj.Height, math.Max(incoming, outgoing)*port_spacing)
				}
			default:
				if obj.Attributes.WidthAttr == nil {
					obj.Width = math.Max(obj.Width, math.Max(incoming, outgoing)*port_spacing)
				}
			}
		}

		if obj.HasLabel() && obj.HasIcon() {
			// this gives shapes extra height for their label if they also have an icon
			obj.Height += float64(obj.LabelDimensions.Height + label.PADDING)
		}

		margin, _ := obj.SpacingOpt(label.PADDING, label.PADDING, false)
		if rankBoxes[obj] {
			if vertical {
				margin.Top = math.Max(margin.Top, margin.Bottom)
				margin.Bottom = margin.Top
			} else {
				margin.Left = math.Max(margin.Left, margin.Right)
				margin.Right = margin.Left
			}
		}
		width := margin.Left + obj.Width + margin.Right
		height := margin.Top + obj.Height + margin.Bottom
		state.margin = margin
		// obj.AbsID() is its parent's AbsID, "." and its ID when the parent
		// has an ID; the parent's was computed when the walk visited it.
		if parentState := visitedState(objects, parent); parentState != nil && obj.Parent == parent && parent.ID != "" {
			state.absID = parentState.absID + "." + obj.ID
		} else {
			state.absID = obj.AbsID()
		}

		n := scratch.nodes.new()
		n.ID, n.Width, n.Height = state.absID, width, height
		state.node = n

		// The node's options: a container's copy of containerBase, or the
		// leaf options (SelfLoopDistribution EQUALLY), plus the layer
		// constraint, the padding and, for SQL tables, fixed port positions.
		// The padding is set for objects with children in their Children
		// map (IsContainer); a node with ChildrenArray entries but an empty
		// Children map keeps containerBase's padding, as the original's
		// container options did.
		layerConstraint := strings.ToUpper(obj.ELKLayerConstraint)
		isContainer := obj.IsContainer()
		paddingOption := ""
		if isContainer {
			paddingOption = scratch.paddingTexts.text(adjustPadding(obj, width, height, padding))
		}
		switch {
		case len(obj.ChildrenArray) > 0:
			o := scratch.opts.new()
			*o = containerBase
			if o.ConfigurableOpts.SelfLoopSpacing == DefaultOpts.SelfLoopSpacing {
				o.ConfigurableOpts.SelfLoopSpacing = go2.Max(o.ConfigurableOpts.SelfLoopSpacing, scratch.selfLoop(g, obj).of(vertical)/2+5)
			}

			switch elkGraph.LayoutOptions.Direction {
			case Down, Up:
				o.NodeSizeMinimum = nodeSizeMinimum(int(math.Ceil(height)), int(math.Ceil(width)))
			case Right, Left:
				o.NodeSizeMinimum = nodeSizeMinimum(int(math.Ceil(width)), int(math.Ceil(height)))
			}
			o.LayerConstraint = layerConstraint
			if isContainer {
				o.Padding = paddingOption
			}
			if obj.SQLTable != nil {
				o.PortConstraints = "FIXED_POS"
			}
			n.LayoutOptions = o
			n.Children = scratch.nodeLists.makeCap(0, len(obj.ChildrenArray))
		case layerConstraint == "" && paddingOption == "" && obj.SQLTable == nil:
			n.LayoutOptions = &shared[leafOptionSet]
		case layerConstraint == "" && paddingOption == "":
			n.LayoutOptions = &shared[sqlLeafOptionSet]
		default:
			o := scratch.opts.new()
			o.SelfLoopDistribution = "EQUALLY"
			o.LayerConstraint = layerConstraint
			o.Padding = paddingOption
			if obj.SQLTable != nil {
				o.PortConstraints = "FIXED_POS"
			}
			n.LayoutOptions = o
		}

		if obj.HasLabel() {
			l := scratch.labels.new()
			l.Text = obj.Label.Value
			l.Width = float64(obj.LabelDimensions.Width)
			l.Height = float64(obj.LabelDimensions.Height)
			n.Labels = scratch.labelList(l)
		}

		if parent == g.Root {
			elkGraph.Children = append(elkGraph.Children, n)
		} else {
			parentState := visitedState(objects, parent)
			parentState.node.Children = append(parentState.node.Children, n)
		}

		if obj.SQLTable != nil {
			columns := obj.SQLTable.Columns
			colHeight := n.Height / float64(len(columns)+1)
			n.Ports = scratch.portLists.makeCap(0, len(columns)*2)
			srcSide, dstSide := &shared[eastPortOptionSet], &shared[westPortOptionSet]
			if elkGraph.LayoutOptions.Direction == Left {
				srcSide, dstSide = dstSide, srcSide
			}
			for i, col := range columns {
				p := scratch.ports.new()
				p.ID = portID(state.absID, col.Name.Label, ".src")
				p.Y = float64(i+1)*colHeight + colHeight/2
				p.LayoutOptions = srcSide
				n.Ports = append(n.Ports, p)
				p = scratch.ports.new()
				p.ID = portID(state.absID, col.Name.Label, ".dst")
				p.Y = float64(i+1)*colHeight + colHeight/2
				p.LayoutOptions = dstSide
				n.Ports = append(n.Ports, p)
			}
		}

		state.visited = true
	}

	var srcSide, dstSide PortSide
	srcOptions, dstOptions := &shared[southPortOptionSet], &shared[northPortOptionSet]
	switch elkGraph.LayoutOptions.Direction {
	case Up:
		srcSide, dstSide = North, South
		srcOptions, dstOptions = dstOptions, srcOptions
	default:
		srcSide, dstSide = South, North
	}

	ports := map[struct {
		obj  *d2graph.Object
		side PortSide
	}][]*ELKPort{}

	absID := func(obj *d2graph.Object) string {
		if state := visitedState(objects, obj); state != nil {
			return state.absID
		}
		return obj.AbsID()
	}
	for ei, edge := range g.Edges {
		var src, dst string

		switch {
		case edge.SrcTableColumnIndex != nil:
			src = portID(absID(edge.Src), edge.Src.SQLTable.Columns[*edge.SrcTableColumnIndex].Name.Label, ".src")
		case edge.Src.SQLTable != nil:
			p := scratch.ports.new()
			p.ID = portID(absID(edge.Src), "__root__", ".src") + "." + strconv.Itoa(ei)
			p.LayoutOptions = srcOptions
			src = p.ID
			srcState := visitedState(objects, edge.Src)
			srcState.node.Ports = append(srcState.node.Ports, p)
			k := struct {
				obj  *d2graph.Object
				side PortSide
			}{edge.Src, srcSide}
			ports[k] = append(ports[k], p)
		default:
			src = absID(edge.Src)
		}

		switch {
		case edge.DstTableColumnIndex != nil:
			dst = portID(absID(edge.Dst), edge.Dst.SQLTable.Columns[*edge.DstTableColumnIndex].Name.Label, ".dst")
		case edge.Dst.SQLTable != nil:
			p := scratch.ports.new()
			p.ID = portID(absID(edge.Dst), "__root__", ".dst") + "." + strconv.Itoa(ei)
			p.LayoutOptions = dstOptions
			dst = p.ID
			dstState := visitedState(objects, edge.Dst)
			dstState.node.Ports = append(dstState.node.Ports, p)
			k := struct {
				obj  *d2graph.Object
				side PortSide
			}{edge.Dst, dstSide}
			ports[k] = append(ports[k], p)
		default:
			dst = absID(edge.Dst)
		}

		e := scratch.edges.new()
		e.ID = edge.AbsID()
		e.Sources, e.Targets = scratch.idList(src), scratch.idList(dst)
		if edge.Label.Value != "" {
			l := scratch.labels.new()
			l.Text = edge.Label.Value
			l.Width = float64(edge.LabelDimensions.Width)
			l.Height = float64(edge.LabelDimensions.Height)
			l.LayoutOptions = &shared[edgeLabelOptionSet]
			e.Labels = scratch.labelList(l)
		}
		elkGraph.Edges = append(elkGraph.Edges, e)
		// An edge listed twice has one state, holding its last ELK edge (as
		// a map by edge did).
		edgeState := edgeStates[edge]
		if edgeState == nil {
			edgeState = scratch.edgeStateValues.new()
			edgeState.first = ei
			edgeStates[edge] = edgeState
		}
		edgeState.elk, edgeState.index = e, ei
		scratch.edgeAt = append(scratch.edgeAt, edgeState)
	}

	for k, ports := range ports {
		width := objects[k.obj].node.Width
		spacing := width / float64(len(ports)+1)
		for i, p := range ports {
			p.X = float64(i+1) * spacing
		}
	}

	if err := runELK(ctx, elkGraph, &scratch.request); err != nil {
		return err
	}

	// The request names edge containers by node: nodeObjects holds every
	// node's object by elk.NodeRef (none for the root).
	nodeObjects := append(scratch.nodeObjects[:0], nil)
	for _, visit := range visits {
		obj, parent := visit.obj, visit.parent
		n := visit.state.node

		parentX := 0.0
		parentY := 0.0
		if parent != nil && parent != g.Root {
			parentX = parent.TopLeft.X
			parentY = parent.TopLeft.Y
		}
		obj.TopLeft = store.point(parentX+n.X, parentY+n.Y)
		obj.Width = math.Ceil(n.Width)
		obj.Height = math.Ceil(n.Height)

		nodeObjects = append(nodeObjects, obj)
	}
	scratch.nodeObjects = nodeObjects

	// The routes are cut from one block, each capped at its length: every
	// route still owns its elements, and later appends to a route copy it
	// (only routes' lengths and elements are ever read, and no other slice
	// shares a route's elements, so capacity is not observable).
	routePoints := 0
	for ei := range g.Edges {
		for _, s := range scratch.edgeAt[ei].elk.Sections {
			routePoints += 2 + len(s.BendPoints)
		}
	}
	routeBlock := make([]*geo.Point, routePoints)
	for ei, edge := range g.Edges {
		edgeState := scratch.edgeAt[ei]
		e := edgeState.elk

		parentX := 0.0
		parentY := 0.0
		if e.Container != "" {
			container := nodeObjects[scratch.request.containerRefs[edgeState.index]]
			parentX = container.TopLeft.X
			parentY = container.TopLeft.Y
		}

		if len(groups) > 0 {
			for _, l := range e.Labels {
				l.X += parentX
				l.Y += parentY
			}
		}

		var points []*geo.Point
		if n := routeLength(e); n > 0 {
			points = routeBlock[:0:n]
			routeBlock = routeBlock[n:]
		}
		for _, s := range e.Sections {
			points = append(points, store.point(parentX+s.Start.X, parentY+s.Start.Y))
			for _, bp := range s.BendPoints {
				points = append(points, store.point(parentX+bp.X, parentY+bp.Y))
			}
			points = append(points, store.point(parentX+s.End.X, parentY+s.End.Y))
		}
		edge.Route = points
	}

	// Per object, the edges that start or end at it, in order and once each
	// (lists cut from one block: counted first, then filled), in its state.
	collectIncidentEdges(g, objects, &scratch.edgeRefs)
	descendantShifter := newDescendantShifterWith(g, rootRange, objects, edgeStates)

	for _, obj := range g.Objects {
		if state := visitedState(objects, obj); state != nil {
			margin := state.margin
			edges := state.incident
			// also move edges with the shrinking sides
			if margin.Left > 0 {
				for _, e := range edges {
					l := len(e.Route)
					if e.Src == obj && e.Route[0].X == obj.TopLeft.X {
						e.Route[0].X += margin.Left
					}
					if e.Dst == obj && e.Route[l-1].X == obj.TopLeft.X {
						e.Route[l-1].X += margin.Left
					}
				}
				obj.TopLeft.X += margin.Left
				descendantShifter.shift(obj, margin.Left/2, 0)
				obj.Width -= margin.Left
			}
			if margin.Right > 0 {
				for _, e := range edges {
					l := len(e.Route)
					if e.Src == obj && e.Route[0].X == obj.TopLeft.X+obj.Width {
						e.Route[0].X -= margin.Right
					}
					if e.Dst == obj && e.Route[l-1].X == obj.TopLeft.X+obj.Width {
						e.Route[l-1].X -= margin.Right
					}
				}
				descendantShifter.shift(obj, -margin.Right/2, 0)
				obj.Width -= margin.Right
			}
			if margin.Top > 0 {
				for _, e := range edges {
					l := len(e.Route)
					if e.Src == obj && e.Route[0].Y == obj.TopLeft.Y {
						e.Route[0].Y += margin.Top
					}
					if e.Dst == obj && e.Route[l-1].Y == obj.TopLeft.Y {
						e.Route[l-1].Y += margin.Top
					}
				}
				obj.TopLeft.Y += margin.Top
				descendantShifter.shift(obj, 0, margin.Top/2)
				obj.Height -= margin.Top
			}
			if margin.Bottom > 0 {
				for _, e := range edges {
					l := len(e.Route)
					if e.Src == obj && e.Route[0].Y == obj.TopLeft.Y+obj.Height {
						e.Route[0].Y -= margin.Bottom
					}
					if e.Dst == obj && e.Route[l-1].Y == obj.TopLeft.Y+obj.Height {
						e.Route[l-1].Y -= margin.Bottom
					}
				}
				descendantShifter.shift(obj, 0, -margin.Bottom/2)
				obj.Height -= margin.Bottom
			}
		}
	}

	var keepRoutes map[*d2graph.Edge]bool
	mrTree := opts.usesAlgorithm("mrtree")
	if mrTree {
		keepRoutes = make(map[*d2graph.Edge]bool)
	}
	for ei, edge := range g.Edges {
		points := edge.Route
		if mrTree && scratch.edgeAt[ei].elk.Container == "" {
			// Child graphs can still use layered routing. Preserve the tree's
			// own detours when simplifying the other routes later.
			keepRoutes[edge] = true
			if edge.Src.Parent == edge.Dst.Parent {
				points = trimMrTreeBends(edge)
			}
		}

		startIndex, endIndex := 0, len(points)-1
		start := points[startIndex]
		end := points[endIndex]

		var originalSrcTL, originalDstTL *geo.Point
		// if the edge passes through 3d/multiple, use the offset box for tracing to border
		if srcDx, srcDy := edge.Src.GetModifierElementAdjustments(); srcDx != 0 || srcDy != 0 {
			if start.X > edge.Src.TopLeft.X+srcDx &&
				start.Y < edge.Src.TopLeft.Y+edge.Src.Height-srcDy {
				originalSrcTL = edge.Src.TopLeft.Copy()
				edge.Src.TopLeft.X += srcDx
				edge.Src.TopLeft.Y -= srcDy
			}
		}
		if dstDx, dstDy := edge.Dst.GetModifierElementAdjustments(); dstDx != 0 || dstDy != 0 {
			if end.X > edge.Dst.TopLeft.X+dstDx &&
				end.Y < edge.Dst.TopLeft.Y+edge.Dst.Height-dstDy {
				originalDstTL = edge.Dst.TopLeft.Copy()
				edge.Dst.TopLeft.X += dstDx
				edge.Dst.TopLeft.Y -= dstDy
			}
		}

		if len(groups) > 0 {
			startIndex, endIndex = edge.TraceOrthogonalToShape(points, startIndex, endIndex)
		} else {
			startIndex, endIndex = edge.TraceToShape(points, startIndex, endIndex)
		}
		points = points[startIndex : endIndex+1]

		if edge.Label.Value != "" {
			if len(groups) > 0 && len(scratch.edgeAt[ei].elk.Labels) > 0 {
				pos, pct := rankLabelPosition(points, scratch.edgeAt[ei].elk.Labels[0])
				edge.LabelPosition = store.stringPointer(pos.String())
				edge.LabelPercentage = &pct
			} else {
				edge.LabelPosition = store.stringPointer(label.InsideMiddleCenter.String())
			}
		}

		edge.Route = points

		// undo 3d/multiple offset
		if originalSrcTL != nil {
			edge.Src.TopLeft.X = originalSrcTL.X
			edge.Src.TopLeft.Y = originalSrcTL.Y
		}
		if originalDstTL != nil {
			edge.Dst.TopLeft.X = originalDstTL.X
			edge.Dst.TopLeft.Y = originalDstTL.Y
		}
	}

	// Ranked routes already account for compound boxes, parallel tracks and labels.
	if opts.EdgeRouting != "POLYLINE" && len(groups) == 0 {
		// The bend index finds duplicate objects and edges by their states.
		scratch.bendGrids.states, scratch.bendGrids.edgeAt = objects, scratch.edgeAt
		deleteBendsWith(g, opts.LayeredOptions, keepRoutes, nil, &scratch.bendGrids)
	}
	deconflictSelfLoopLabels(g)
	if len(groups) > 0 {
		ranklabel.ClearOverlaps(g)
	}

	return nil
}

// routeLength is the number of points of the route of e's sections.
func routeLength(e *ELKEdge) int {
	n := 0
	for _, s := range e.Sections {
		n += 2 + len(s.BendPoints)
	}
	return n
}

// Mr. Tree can repeat a bend on a return edge or run its final segment along
// the target's border. Keep the attachment points and the bends in open space
// so tracing and rounding the route do not send the arrow back into a shape.
func trimMrTreeBends(edge *d2graph.Edge) []*geo.Point {
	points := edge.Route
	if len(points) <= 2 || edge.Src == edge.Dst {
		return points
	}
	end := points[len(points)-1]
	bends := points[1 : len(points)-1]
	first := 0
	for i, bend := range bends {
		if edge.Dst.Box.Contains(bend) {
			bends = bends[:i]
			break
		}
		if edge.Src.Box.Contains(bend) {
			first = i + 1
		}
	}
	bends = slices.CompactFunc(bends[first:], (*geo.Point).Equals)
	points = append(points[:1], bends...)
	return append(points, end)
}

func srcPortID(obj *d2graph.Object, column string) string {
	return portID(obj.AbsID(), column, ".src")
}

func dstPortID(obj *d2graph.Object, column string) string {
	return portID(obj.AbsID(), column, ".dst")
}

// deconflictSelfLoopLabels keeps the usual centered label position unless two
// labels on self loops around the same object materially overlap. Newer ELK
// versions can place multiple fixed-port loops on the same side of an object,
// making both route midpoints coincide. Moving only the later label preserves
// the routed geometry and existing output for non-overlapping loops.
func deconflictSelfLoopLabels(g *d2graph.Graph) {
	const materialOverlap = float64(label.PADDING)
	candidatePercentages := [...]float64{0.4, 0.6, 0.3, 0.7, 0.2, 0.8, 0.1, 0.9}

	placedByObject := make(map[*d2graph.Object][]*geo.Box)
	for _, edge := range g.Edges {
		if edge.Src != edge.Dst || edge.Label.Value == "" || edge.LabelPosition == nil || len(edge.Route) < 2 {
			continue
		}

		position := label.FromString(*edge.LabelPosition)
		percentage := 0.0
		if edge.LabelPercentage != nil {
			percentage = *edge.LabelPercentage
		}
		box := edgeLabelBox(edge, position, percentage)
		if box == nil {
			continue
		}

		placed := placedByObject[edge.Src]
		if materiallyOverlapsAny(box, placed, materialOverlap) {
			for _, candidate := range candidatePercentages {
				candidateBox := edgeLabelBox(edge, label.UnlockedMiddle, candidate)
				if candidateBox == nil || overlapsAny(candidateBox, placed) {
					continue
				}
				edge.LabelPosition = go2.Pointer(label.UnlockedMiddle.String())
				edge.LabelPercentage = go2.Pointer(candidate)
				box = candidateBox
				break
			}
		}
		placedByObject[edge.Src] = append(placed, box)
	}
}

func edgeLabelBox(edge *d2graph.Edge, position label.Position, percentage float64) *geo.Box {
	topLeft, _ := position.GetPointOnRoute(
		geo.Route(edge.Route),
		0,
		percentage,
		float64(edge.LabelDimensions.Width),
		float64(edge.LabelDimensions.Height),
	)
	if topLeft == nil {
		return nil
	}
	return geo.NewBox(topLeft, float64(edge.LabelDimensions.Width), float64(edge.LabelDimensions.Height))
}

func materiallyOverlapsAny(box *geo.Box, others []*geo.Box, minimum float64) bool {
	for _, other := range others {
		overlapWidth := math.Min(box.TopLeft.X+box.Width, other.TopLeft.X+other.Width) - math.Max(box.TopLeft.X, other.TopLeft.X)
		overlapHeight := math.Min(box.TopLeft.Y+box.Height, other.TopLeft.Y+other.Height) - math.Max(box.TopLeft.Y, other.TopLeft.Y)
		if overlapWidth > minimum && overlapHeight > minimum {
			return true
		}
	}
	return false
}

func overlapsAny(box *geo.Box, others []*geo.Box) bool {
	for _, other := range others {
		if box.Overlaps(*other) {
			return true
		}
	}
	return false
}

// deleteBendsWith is a shim for ELK to delete unnecessary bends (see
// https://github.com/terrastruct/d2/issues/1030), with the bend index form
// chosen by scan when it is not nil (for tests; both forms give the same
// result), and the storage of its grids taken from grids when that is not
// nil.
func deleteBendsWith(g *d2graph.Graph, opts LayeredOptions, keepRoute map[*d2graph.Edge]bool, scan *bool, grids *bendGrids) {
	hasCandidate := false
	for _, edge := range g.Edges {
		if edge.Src != edge.Dst && len(edge.Route) >= 4 && !keepRoute[edge] {
			hasCandidate = true
			break
		}
	}
	if !hasCandidate {
		return
	}
	var collisionIndex *bendCollisionIndex
	if scan != nil {
		collisionIndex = newBendCollisionIndexWith(g, opts, *scan, grids)
	} else {
		items := len(g.Objects)
		for _, edge := range g.Edges {
			items += max(0, len(edge.Route)-1)
		}
		collisionIndex = newBendCollisionIndexWith(g, opts, items <= linearIndexLimit, grids)
	}

	// Straightening a bend can slide the attachment along the shape border.
	// Keep ELK's shared input and output ports at their original positions.
	type endpointKey struct {
		object *d2graph.Object
		point  geo.Point
		source bool
	}
	var attachments map[endpointKey]int
	if opts.MergeEdges {
		attachments = make(map[endpointKey]int)
		for _, edge := range g.Edges {
			attachments[endpointKey{edge.Src, *edge.Route[0], true}]++
			attachments[endpointKey{edge.Dst, *edge.Route[len(edge.Route)-1], false}]++
		}
	}

	// Get rid of S-shapes at the source and the target
	// TODO there might be value in repeating this. removal of an S shape introducing another S shape that can still be removed
	for _, isSource := range []bool{true, false} {
		for ei, e := range g.Edges {
			if len(e.Route) < 4 || keepRoute[e] {
				continue
			}
			if e.Src == e.Dst {
				continue
			}
			var endpoint *d2graph.Object
			var start *geo.Point
			var corner *geo.Point
			var end *geo.Point

			var columnIndex *int
			if isSource {
				start = e.Route[0]
				corner = e.Route[1]
				end = e.Route[2]
				endpoint = e.Src
				columnIndex = e.SrcTableColumnIndex
			} else {
				start = e.Route[len(e.Route)-1]
				corner = e.Route[len(e.Route)-2]
				end = e.Route[len(e.Route)-3]
				endpoint = e.Dst
				columnIndex = e.DstTableColumnIndex
			}

			if opts.MergeEdges && attachments[endpointKey{endpoint, *start, isSource}] > 1 {
				continue
			}
			isHorizontal := math.Ceil(start.Y) == math.Ceil(corner.Y)
			dx, dy := endpoint.GetModifierElementAdjustments()

			// Make sure it's still attached
			switch {
			case columnIndex != nil:
				rowHeight := endpoint.Height / float64(len(endpoint.SQLTable.Columns)+1)
				rowCenter := endpoint.TopLeft.Y + rowHeight*float64(*columnIndex+1) + rowHeight/2

				// for row connections new Y coordinate should be within 1/3 row height from the row center
				if math.Abs(end.Y-rowCenter) > rowHeight/3 {
					continue
				}
			case isHorizontal:
				if end.Y <= endpoint.TopLeft.Y+10-dy {
					continue
				}
				if end.Y >= endpoint.TopLeft.Y+endpoint.Height-10 {
					continue
				}
			default:
				if end.X <= endpoint.TopLeft.X+10 {
					continue
				}
				if end.X >= endpoint.TopLeft.X+endpoint.Width-10+dx {
					continue
				}
			}

			var newStart *geo.Point
			if isHorizontal {
				newStart = geo.NewPoint(start.X, end.Y)
			} else {
				newStart = geo.NewPoint(end.X, start.Y)
			}

			endpointShape := shape.NewShape(d2target.DSL_SHAPE_TO_SHAPE_TYPE[strings.ToLower(endpoint.Shape.Value)], endpoint.Box)
			newStart = shape.TraceToShapeBorder(endpointShape, newStart, end)

			// Check that the new segment doesn't collide with anything new

			oldSegment := geo.NewSegment(start, corner)
			newSegment := geo.NewSegment(newStart, end)

			oldIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldSegment)
			newIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *newSegment)

			if newIntersects > oldIntersects {
				continue
			}

			oldCrossingsCount, oldOverlapsCount, oldCloseOverlapsCount, oldTouchingCount := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldSegment)
			newCrossingsCount, newOverlapsCount, newCloseOverlapsCount, newTouchingCount := collisionIndex.countEdgeIntersects(g.Edges[ei], *newSegment)

			if newCrossingsCount > oldCrossingsCount {
				continue
			}
			if newOverlapsCount > oldOverlapsCount {
				continue
			}

			if newCloseOverlapsCount > oldCloseOverlapsCount {
				continue
			}
			if newTouchingCount > oldTouchingCount {
				continue
			}

			// commit
			if isSource {
				g.Edges[ei].Route = append(
					[]*geo.Point{newStart},
					e.Route[3:]...,
				)
			} else {
				g.Edges[ei].Route = append(
					e.Route[:len(e.Route)-3],
					newStart,
				)
			}
			collisionIndex.updateEdgeAt(ei, g.Edges[ei])
		}
	}

	// Get rid of ladders
	// ELK likes to do these for some reason
	// .   ┌─
	// . ┌─┘
	// . │
	// We want to transform these into L-shapes

	// How many route points (as they are now) equal a point. Small graphs
	// count in a flat copy instead of a map: the same equality (==, as map
	// keys compare), the same snapshot.
	var points map[geo.Point]int
	var pointList []geo.Point
	if collisionIndex.grids == nil {
		total := 0
		for _, e := range g.Edges {
			total += len(e.Route)
		}
		pointList = make([]geo.Point, 0, total)
		for _, e := range g.Edges {
			for _, p := range e.Route {
				pointList = append(pointList, *p)
			}
		}
	} else {
		points = map[geo.Point]int{}
		for _, e := range g.Edges {
			for _, p := range e.Route {
				points[*p]++
			}
		}
	}
	pointCount := func(p geo.Point) int {
		if points != nil {
			return points[p]
		}
		count := 0
		for _, q := range pointList {
			if q == p {
				count++
			}
		}
		return count
	}

	for ei, e := range g.Edges {
		if len(e.Route) < 6 || keepRoute[e] {
			continue
		}
		if e.Src == e.Dst {
			continue
		}

		for i := 1; i < len(e.Route)-3; i++ {
			before := e.Route[i-1]
			start := e.Route[i]
			corner := e.Route[i+1]
			end := e.Route[i+2]
			after := e.Route[i+3]

			if c := pointCount(*corner); c > 1 {
				// If corner is shared with another edge, they merge
				continue
			}

			// S-shape on sources only concerned one segment, since the other was just along the bound of endpoint
			// These concern two segments

			var newCorner *geo.Point
			if math.Ceil(start.X) == math.Ceil(corner.X) {
				newCorner = geo.NewPoint(end.X, start.Y)
				// not ladder
				if (end.X > start.X) != (start.X > before.X) {
					continue
				}
				if (end.Y > start.Y) != (after.Y > end.Y) {
					continue
				}
			} else {
				newCorner = geo.NewPoint(start.X, end.Y)
				if (end.Y > start.Y) != (start.Y > before.Y) {
					continue
				}
				if (end.X > start.X) != (after.X > end.X) {
					continue
				}
			}

			oldS1 := geo.NewSegment(start, corner)
			oldS2 := geo.NewSegment(corner, end)

			newS1 := geo.NewSegment(start, newCorner)
			newS2 := geo.NewSegment(newCorner, end)

			// Check that the new segments doesn't collide with anything new
			oldIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldS1) + collisionIndex.countObjectIntersects(e.Src, e.Dst, *oldS2)
			newIntersects := collisionIndex.countObjectIntersects(e.Src, e.Dst, *newS1) + collisionIndex.countObjectIntersects(e.Src, e.Dst, *newS2)

			if newIntersects > oldIntersects {
				continue
			}

			oldCrossingsCount1, oldOverlapsCount1, oldCloseOverlapsCount1, oldTouchingCount1 := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldS1)
			oldCrossingsCount2, oldOverlapsCount2, oldCloseOverlapsCount2, oldTouchingCount2 := collisionIndex.countEdgeIntersects(g.Edges[ei], *oldS2)
			oldCrossingsCount := oldCrossingsCount1 + oldCrossingsCount2
			oldOverlapsCount := oldOverlapsCount1 + oldOverlapsCount2
			oldCloseOverlapsCount := oldCloseOverlapsCount1 + oldCloseOverlapsCount2
			oldTouchingCount := oldTouchingCount1 + oldTouchingCount2

			newCrossingsCount1, newOverlapsCount1, newCloseOverlapsCount1, newTouchingCount1 := collisionIndex.countEdgeIntersects(g.Edges[ei], *newS1)
			newCrossingsCount2, newOverlapsCount2, newCloseOverlapsCount2, newTouchingCount2 := collisionIndex.countEdgeIntersects(g.Edges[ei], *newS2)
			newCrossingsCount := newCrossingsCount1 + newCrossingsCount2
			newOverlapsCount := newOverlapsCount1 + newOverlapsCount2
			newCloseOverlapsCount := newCloseOverlapsCount1 + newCloseOverlapsCount2
			newTouchingCount := newTouchingCount1 + newTouchingCount2

			if newCrossingsCount > oldCrossingsCount {
				continue
			}
			if newOverlapsCount > oldOverlapsCount {
				continue
			}

			if newCloseOverlapsCount > oldCloseOverlapsCount {
				continue
			}
			if newTouchingCount > oldTouchingCount {
				continue
			}

			// commit
			g.Edges[ei].Route = append(append(
				e.Route[:i],
				newCorner,
			),
				e.Route[i+3:]...,
			)
			collisionIndex.updateEdgeAt(ei, g.Edges[ei])
			break
		}
	}
}

type shapePadding struct {
	top, left, bottom, right float64
}

// ELK accepts named padding on any subset of the four sides.
func readPadding(value string) (shapePadding, error) {
	var p shapePadding
	// Match the delimiters accepted by the ELK spacing parser.
	fields := strings.Trim(value, "()[]{}\"' \t\r\n")
	for _, part := range strings.FieldsFunc(fields, func(r rune) bool { return r == ',' || r == ';' }) {
		name, raw, ok := strings.Cut(part, "=")
		n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if !ok || err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
			return p, fmt.Errorf("invalid elk-padding field %q: expected side=nonnegative-number", part)
		}
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "top":
			p.top = n
		case "left":
			p.left = n
		case "bottom":
			p.bottom = n
		case "right":
			p.right = n
		default:
			return p, fmt.Errorf("unknown elk-padding side %q", name)
		}
	}
	return p, nil
}

// String is fmt.Sprintf("[top=%g,left=%g,bottom=%g,right=%g]", ...): %g of a
// float64 is strconv's shortest 'g' format, NaN and infinities included.
func (padding shapePadding) String() string {
	return "[top=" + formatG(padding.top) + ",left=" + formatG(padding.left) +
		",bottom=" + formatG(padding.bottom) + ",right=" + formatG(padding.right) + "]"
}

func formatG(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func adjustPadding(obj *d2graph.Object, width, height float64, padding shapePadding) shapePadding {
	if !obj.IsContainer() {
		return padding
	}

	// compute extra space padding for label/icon
	var extraTop, extraBottom, extraLeft, extraRight int
	if obj.HasLabel() && obj.LabelPosition != nil {
		labelHeight := obj.LabelDimensions.Height + 2*label.PADDING
		labelWidth := obj.LabelDimensions.Width + 2*label.PADDING
		switch label.FromString(*obj.LabelPosition) {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
			// Note: for corners we only add height
			extraTop = labelHeight
		case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
			extraBottom = labelHeight
		case label.InsideMiddleLeft:
			extraLeft = labelWidth
		case label.InsideMiddleRight:
			extraRight = labelWidth
		}
	}
	if obj.HasIcon() && obj.IconPosition != nil {
		iconSize := d2target.MAX_ICON_SIZE + 2*label.PADDING
		switch label.FromString(*obj.IconPosition) {
		case label.InsideTopLeft, label.InsideTopCenter, label.InsideTopRight:
			extraTop = go2.Max(extraTop, iconSize)
		case label.InsideBottomLeft, label.InsideBottomCenter, label.InsideBottomRight:
			extraBottom = go2.Max(extraBottom, iconSize)
		case label.InsideMiddleLeft:
			extraLeft = go2.Max(extraLeft, iconSize)
		case label.InsideMiddleRight:
			extraRight = go2.Max(extraRight, iconSize)
		}
	}

	maxChildWidth, maxChildHeight := math.Inf(-1), math.Inf(-1)
	for _, c := range obj.ChildrenArray {
		if c.Width > maxChildWidth {
			maxChildWidth = c.Width
		}
		if c.Height > maxChildHeight {
			maxChildHeight = c.Height
		}
	}
	// We don't know exactly what the shape dimensions will be after layout, but for more accurate innerBox dimensions,
	// we add the maxChildWidth and maxChildHeight with computed additions for the innerBox calculation
	width += maxChildWidth + float64(extraLeft+extraRight)
	height += maxChildHeight + float64(extraTop+extraBottom)
	contentBox := geo.NewBox(geo.NewPoint(0, 0), width, height)
	shapeType := d2target.DSL_SHAPE_TO_SHAPE_TYPE[obj.Shape.Value]
	s := shape.NewShape(shapeType, contentBox)
	innerBox := s.GetInnerBox()

	// If the shape inner box + label/icon height becomes greater than the default padding, we want to use that
	//
	// ┌OUTER───────────────────────────┬────────────────────────────────────────────┐
	// │                                │                                            │
	// │  ┌INNER──────── ┬ ─────────────│───────────────────────────────────────┐    │
	// │  │              │Label Padding │                                       │    │
	// │  │      ┌LABEL─ ┴ ─────────────│───────┐┬             ┌ICON── ┬ ────┐  │    │
	// │  │      │                      │       ││             │       │     │  │    │
	// │  │      │                      │       ││Label Height │   Icon│     │  │    │
	// │  │      │                      │       ││             │ Height│     │  │    │
	// │  │      └──────────────────────│───────┘┴             │       │     │  │    │
	// │  │                             │                      └────── ┴ ────┘  │    │
	// │  │                             │                                       │    │
	// │  │                             ┴Default ELK Padding                    │    │
	// │  │   ┌CHILD────────────────────────────────────────────────────────┐   │    │
	// │  │   │                                                             │   │    │
	// │  │   │                                                             │   │    │
	// │  │   │                                                             │   │    │
	// │  │   └─────────────────────────────────────────────────────────────┘   │    │
	// │  │                                                                     │    │
	// │  └─────────────────────────────────────────────────────────────────────┘    │
	// │                                                                             │
	// └─────────────────────────────────────────────────────────────────────────────┘

	// estimated shape innerBox padding
	innerTop := int(math.Ceil(innerBox.TopLeft.Y))
	innerBottom := int(math.Ceil(height - (innerBox.TopLeft.Y + innerBox.Height)))
	innerLeft := int(math.Ceil(innerBox.TopLeft.X))
	innerRight := int(math.Ceil(width - (innerBox.TopLeft.X + innerBox.Width)))

	padding.top = math.Max(padding.top, float64(innerTop+extraTop))
	padding.bottom = math.Max(padding.bottom, float64(innerBottom+extraBottom))
	padding.left = math.Max(padding.left, float64(innerLeft+extraLeft))
	padding.right = math.Max(padding.right, float64(innerRight+extraRight))

	return padding
}

func positionLabelsIcons(obj *d2graph.Object) {
	positionLabelsIconsWith(obj, go2.Pointer[string])
}

// positionLabelsIconsWith is positionLabelsIcons taking the string pointers it
// stores from newString (each call a new variable).
func positionLabelsIconsWith(obj *d2graph.Object, newString func(string) *string) {
	if obj.Icon != nil && obj.IconPosition == nil {
		if len(obj.ChildrenArray) > 0 {
			obj.IconPosition = newString(label.InsideTopLeft.String())
			if obj.LabelPosition == nil {
				obj.LabelPosition = newString(label.InsideTopRight.String())
				return
			}
		} else if obj.SQLTable != nil || obj.Class != nil || obj.Language != "" {
			obj.IconPosition = newString(label.OutsideTopLeft.String())
		} else {
			obj.IconPosition = newString(label.InsideMiddleCenter.String())
		}
	}
	if obj.HasLabel() && obj.LabelPosition == nil {
		if len(obj.ChildrenArray) > 0 {
			obj.LabelPosition = newString(label.InsideTopCenter.String())
		} else if obj.HasOutsideBottomLabel() {
			obj.LabelPosition = newString(label.OutsideBottomCenter.String())
		} else if obj.Icon != nil {
			obj.LabelPosition = newString(label.InsideTopCenter.String())
		} else {
			obj.LabelPosition = newString(label.InsideMiddleCenter.String())
		}
		if float64(obj.LabelDimensions.Width) > obj.Width || float64(obj.LabelDimensions.Height) > obj.Height {
			if len(obj.ChildrenArray) > 0 {
				obj.LabelPosition = newString(label.OutsideTopCenter.String())
			} else {
				obj.LabelPosition = newString(label.OutsideBottomCenter.String())
			}
		}
	}
}
