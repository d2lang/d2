package d2elklayout

import (
	elk "github.com/d2lang/elk-go"
)

// elkGoGraph is an ELKGraph converted to elk-go's graph values, with the
// pairs of corresponding elements the result is copied back through. Layout
// gives elk-go its request through the typed Request (elkGoRequest) instead;
// the tests check with this conversion that the values both take from the
// ELKGraph (appendOptionPairs, jsonText) are what elk-go decodes from its
// JSON, and that elk-go lays out the converted graph as the request.
type elkGoGraph struct {
	graph  *elk.Graph
	nodes  []elkNodePair
	ports  []elkPortPair
	labels []elkLabelPair
	edges  []elkEdgePair

	floats      slab[float64]
	nodeValues  slab[elk.Node]
	portValues  slab[elk.Port]
	labelValues slab[elk.Label]
	edgeValues  slab[elk.Edge]
	idValues    slab[elk.ID]
	textValues  slab[string]
	nodeLists   lists[*elk.Node]
	portLists   lists[*elk.Port]
	labelLists  lists[*elk.Label]
	edgeLists   lists[*elk.Edge]
	idLists     lists[elk.ID]
	keyLists    lists[string]
	maps        []elk.LayoutOptions // option maps, emptied; the first mapsUsed are in use
	mapsUsed    int
	optionSets  []elkOptionSet
	keyOrders   []elkKeyOrder
	pairs       []elkOptionPair

	// shared are option structs that elements of the ELKGraph share by
	// pointer, one per static option set, holding its source.
	shared [staticOptionSetCount]elkOpts
	// sharedIntact is true while every shared struct still holds its
	// source: then a pointer to one selects its static set without
	// comparing the struct.
	sharedIntact bool

	// sections and bends hold the result copied back into the ELKGraph.
	sections lists[ELKEdgeSection]
	bends    lists[ELKPoint]
}

// reserve sizes the storage for a request of the given element counts and
// sets the shared option structs; the request must be empty.
func (r *elkGoGraph) reserve(nodes, ports, labels, edges int) {
	r.nodeValues.reserve(nodes + 1)
	r.portValues.reserve(ports)
	r.labelValues.reserve(labels)
	r.edgeValues.reserve(edges)
	r.idValues.reserve(edges)
	r.textValues.reserve(labels)
	r.floats.reserve(4 * (nodes + ports + labels))
	r.nodeLists.reserve(nodes)
	r.portLists.reserve(ports)
	r.labelLists.reserve(labels)
	r.edgeLists.reserve(edges)
	r.idLists.reserve(2 * edges)
	r.keyLists.reserve(0)
	r.sections.reserve(0)
	r.bends.reserve(0)
	for i := range r.shared {
		r.shared[i] = staticOptionSets()[i].source
	}
}

type elkNodePair struct {
	d2  *ELKNode
	elk *elk.Node
}

type elkPortPair struct {
	d2  *ELKPort
	elk *elk.Port
}

type elkLabelPair struct {
	d2  *ELKLabel
	elk *elk.Label
}

type elkEdgePair struct {
	d2  *ELKEdge
	elk *elk.Edge
}

// newELKGoGraph converts g into a new request.
func newELKGoGraph(g *ELKGraph) *elkGoGraph {
	r := &elkGoGraph{}
	nodes, ports, labels := countELKGraph(g)
	r.reserve(nodes, ports, labels, len(g.Edges))
	r.convert(g)
	return r
}

// convert fills the request (reserved and empty) with g's values.
func (r *elkGoGraph) convert(g *ELKGraph) {
	r.sharedIntact = true
	for i := range r.shared {
		if r.shared[i] != staticOptionSets()[i].source {
			r.sharedIntact = false
		}
	}
	root := r.nodeValues.new()
	root.ID = r.id(g.ID)
	if g.LayoutOptions != nil {
		r.setNodeOptions(root, g.LayoutOptions)
	}
	if len(g.Children) > 0 {
		root.Children = r.nodeSlice(g.Children)
	}
	if len(g.Edges) > 0 {
		root.Edges = r.edgeLists.make(len(g.Edges))
		for i, edge := range g.Edges {
			if edge != nil {
				root.Edges[i] = r.edge(edge)
			}
		}
	}
	r.graph = root
}

// countELKGraph counts the nodes, ports and labels of g.
func countELKGraph(g *ELKGraph) (nodes, ports, labels int) {
	var count func([]*ELKNode)
	count = func(children []*ELKNode) {
		for _, n := range children {
			if n != nil {
				nodes++
				ports += len(n.Ports)
				labels += len(n.Labels)
				count(n.Children)
			}
		}
	}
	count(g.Children)
	for _, edge := range g.Edges {
		if edge != nil {
			labels += len(edge.Labels)
		}
	}
	return nodes, ports, labels
}

func (r *elkGoGraph) id(value string) elk.ID {
	return elk.StringID(jsonText(value))
}

func (r *elkGoGraph) number(value float64) *float64 {
	pointer := r.floats.new()
	*pointer = value
	return pointer
}

func (r *elkGoGraph) nodeSlice(children []*ELKNode) []*elk.Node {
	out := r.nodeLists.make(len(children))
	for i, child := range children {
		if child != nil {
			out[i] = r.node(child)
		}
	}
	return out
}

func (r *elkGoGraph) node(n *ELKNode) *elk.Node {
	out := r.nodeValues.new()
	out.ID = r.id(n.ID)
	out.X, out.Y = r.number(n.X), r.number(n.Y)
	out.Width, out.Height = r.number(n.Width), r.number(n.Height)
	if len(n.Children) > 0 {
		out.Children = r.nodeSlice(n.Children)
	}
	if len(n.Ports) > 0 {
		out.Ports = r.portLists.make(len(n.Ports))
		for i, port := range n.Ports {
			if port != nil {
				out.Ports[i] = r.port(port)
			}
		}
	}
	if len(n.Labels) > 0 {
		out.Labels = r.labelSlice(n.Labels)
	}
	if n.LayoutOptions != nil {
		r.setNodeOptions(out, n.LayoutOptions)
	}
	r.nodes = append(r.nodes, elkNodePair{n, out})
	return out
}

func (r *elkGoGraph) setNodeOptions(out *elk.Node, opts *elkOpts) {
	options, keys := r.options(opts)
	out.LayoutOptions = options
	out.SetLayoutOptionKeys(keys)
}

func (r *elkGoGraph) port(p *ELKPort) *elk.Port {
	out := r.portValues.new()
	out.ID = r.id(p.ID)
	out.X, out.Y = r.number(p.X), r.number(p.Y)
	out.Width, out.Height = r.number(p.Width), r.number(p.Height)
	if p.LayoutOptions != nil {
		options, keys := r.options(p.LayoutOptions)
		out.LayoutOptions = options
		out.SetLayoutOptionKeys(keys)
	}
	r.ports = append(r.ports, elkPortPair{p, out})
	return out
}

func (r *elkGoGraph) labelSlice(labels []*ELKLabel) []*elk.Label {
	out := r.labelLists.make(len(labels))
	for i, l := range labels {
		if l != nil {
			out[i] = r.label(l)
		}
	}
	return out
}

func (r *elkGoGraph) label(l *ELKLabel) *elk.Label {
	out := r.labelValues.new()
	text := r.textValues.new()
	*text = jsonText(l.Text)
	out.Text = text
	out.X, out.Y = r.number(l.X), r.number(l.Y)
	out.Width, out.Height = r.number(l.Width), r.number(l.Height)
	if l.LayoutOptions != nil {
		options, keys := r.options(l.LayoutOptions)
		out.LayoutOptions = options
		out.SetLayoutOptionKeys(keys)
	}
	r.labels = append(r.labels, elkLabelPair{l, out})
	return out
}

func (r *elkGoGraph) edge(e *ELKEdge) *elk.Edge {
	out := r.edgeValues.new()
	out.ID = r.id(e.ID)
	out.Sources = r.ids(e.Sources)
	out.Targets = r.ids(e.Targets)
	if len(e.Labels) > 0 {
		out.Labels = r.labelSlice(e.Labels)
	}
	container := r.idValues.new()
	*container = r.id(e.Container)
	out.Container = container
	r.edges = append(r.edges, elkEdgePair{e, out})
	return out
}

// ids converts sources or targets: nil (JSON null) stays nil, any other
// slice, even an empty one, becomes a non-nil slice.
func (r *elkGoGraph) ids(values []string) []elk.ID {
	if values == nil {
		return nil
	}
	out := r.idLists.make(len(values))
	for i, value := range values {
		out[i] = r.id(value)
	}
	return out
}

func (r *elkGoGraph) options(opts *elkOpts) (elk.LayoutOptions, []string) {
	if r.sharedIntact {
		for i := range r.shared {
			if opts == &r.shared[i] {
				return staticOptionSets()[i].options, staticOptionSets()[i].keys
			}
		}
	}
	for i := range staticOptionSets() {
		if staticOptionSets()[i].source == *opts {
			return staticOptionSets()[i].options, staticOptionSets()[i].keys
		}
	}
	for i := len(r.optionSets) - 1; i >= 0; i-- {
		if r.optionSets[i].source == *opts {
			return r.optionSets[i].options, r.optionSets[i].keys
		}
	}
	var mask uint64
	r.pairs, mask, _ = appendOptionPairs(r.pairs[:0], opts)
	var keys []string
	for _, order := range r.keyOrders {
		if order.mask == mask {
			keys = order.keys
			break
		}
	}
	if keys == nil {
		keys = r.keyLists.make(len(r.pairs))
		for i, pair := range r.pairs {
			keys[i] = pair.key
		}
		r.keyOrders = append(r.keyOrders, elkKeyOrder{mask: mask, keys: keys})
	}
	var options elk.LayoutOptions
	if r.mapsUsed < len(r.maps) {
		options = r.maps[r.mapsUsed]
	} else {
		options = make(elk.LayoutOptions, len(r.pairs))
		r.maps = append(r.maps, options)
	}
	r.mapsUsed++
	for _, pair := range r.pairs {
		options[pair.key] = pair.value
	}
	if len(r.optionSets) == optionSetsKept {
		copy(r.optionSets, r.optionSets[1:])
		r.optionSets = r.optionSets[:optionSetsKept-1]
	}
	r.optionSets = append(r.optionSets, elkOptionSet{source: *opts, options: options, keys: keys})
	return options, keys
}

// apply copies the layout into the ELKGraph the request was made from.
func (r *elkGoGraph) apply() {
	for _, pair := range r.nodes {
		setFromJSONNumber(&pair.d2.X, pair.elk.X)
		setFromJSONNumber(&pair.d2.Y, pair.elk.Y)
		setFromJSONNumber(&pair.d2.Width, pair.elk.Width)
		setFromJSONNumber(&pair.d2.Height, pair.elk.Height)
	}
	for _, pair := range r.ports {
		setFromJSONNumber(&pair.d2.X, pair.elk.X)
		setFromJSONNumber(&pair.d2.Y, pair.elk.Y)
		setFromJSONNumber(&pair.d2.Width, pair.elk.Width)
		setFromJSONNumber(&pair.d2.Height, pair.elk.Height)
	}
	for _, pair := range r.labels {
		setFromJSONNumber(&pair.d2.X, pair.elk.X)
		setFromJSONNumber(&pair.d2.Y, pair.elk.Y)
		setFromJSONNumber(&pair.d2.Width, pair.elk.Width)
		setFromJSONNumber(&pair.d2.Height, pair.elk.Height)
	}
	for _, pair := range r.edges {
		out := pair.elk
		if out.Sections != nil {
			sections := r.sections.make(len(out.Sections))
			for i, section := range out.Sections {
				if section == nil {
					continue
				}
				if section.StartPoint != nil {
					sections[i].Start = pointFromJSON(*section.StartPoint)
				}
				if section.EndPoint != nil {
					sections[i].End = pointFromJSON(*section.EndPoint)
				}
				if section.BendPoints != nil {
					bends := r.bends.make(len(section.BendPoints))
					for j, bend := range section.BendPoints {
						bends[j] = pointFromJSON(bend)
					}
					sections[i].BendPoints = bends
				}
			}
			pair.d2.Sections = sections
		}
		// Container IDs are node IDs, which are all strings here.
		if out.Container != nil && out.Container.IsValid() {
			pair.d2.Container = out.Container.String()
		}
	}
}

// elkKeyOrder is a member order shared by the option sets of one request
// that have the same members (mask bit i: member i of elkOpts is present).
type elkKeyOrder struct {
	mask uint64
	keys []string
}
