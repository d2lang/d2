package d2elklayout

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/label"
	"github.com/d2lang/d2/lib/textmeasure"
)

// The original request construction (d2 e22f4d566, Layout up to
// json.Marshal(elkGraph)), copied so that the request the adapter sends can be
// compared with the original's for graphs the corpus does not have. Helpers
// whose code did not change (graphOptions, readPadding, adjustPadding,
// positionLabelsIcons, srcPortID, dstPortID) are called directly.

func origEdgeEdgeSpacingOrDefault(opts *ConfigurableOpts) *int {
	if opts.EdgeEdgeSpacing == nil {
		return go2.Pointer(default_edge_edge_spacing)
	}
	return go2.Pointer(*opts.EdgeEdgeSpacing)
}

func origParentOptions(opts *ConfigurableOpts, root bool) LayeredOptions {
	o := opts.LayeredOptions
	if o.EdgeNode == nil && (!root || opts.usesAlgorithm("layered")) {
		o.EdgeNode = go2.Pointer(float64(edge_node_spacing))
	}
	o.ForceNodeModelOrder = root && o.ForceNodeModelOrder
	return o
}

func origNewRootLayoutOptions(opts *ConfigurableOpts) *elkOpts {
	return &elkOpts{
		Thoroughness:          8,
		HierarchyHandling:     "INCLUDE_CHILDREN",
		FixedAlignment:        "BALANCED",
		ConsiderModelOrder:    "NODES_AND_EDGES",
		CycleBreakingStrategy: "GREEDY_MODEL_ORDER",
		NodeSizeConstraints:   "MINIMUM_SIZE",
		ContentAlignment:      "H_CENTER V_CENTER",
		ConfigurableOpts: ConfigurableOpts{
			LayeredOptions:   origParentOptions(opts, true),
			AlgorithmOptions: opts.AlgorithmOptions,
			Algorithm:        opts.Algorithm,
			NodeSpacing:      opts.NodeSpacing,
			EdgeNodeSpacing:  opts.EdgeNodeSpacing,
			EdgeEdgeSpacing:  origEdgeEdgeSpacingOrDefault(opts),
			SelfLoopSpacing:  opts.SelfLoopSpacing,
		},
	}
}

func origNewContainerLayoutOptions(opts *ConfigurableOpts) *elkOpts {
	return &elkOpts{
		Thoroughness:          8,
		HierarchyHandling:     "INCLUDE_CHILDREN",
		FixedAlignment:        "BALANCED",
		ConsiderModelOrder:    "NONE",
		CycleBreakingStrategy: "GREEDY",
		NodeSizeConstraints:   "MINIMUM_SIZE",
		ContentAlignment:      "H_CENTER V_CENTER",
		ConfigurableOpts: ConfigurableOpts{
			LayeredOptions:  origParentOptions(opts, false),
			NodeSpacing:     opts.NodeSpacing,
			EdgeNodeSpacing: opts.EdgeNodeSpacing,
			EdgeEdgeSpacing: origEdgeEdgeSpacingOrDefault(opts),
			SelfLoopSpacing: opts.SelfLoopSpacing,
			Padding:         opts.Padding,
		},
	}
}

func origPaddingString(p shapePadding) string {
	return fmt.Sprintf("[top=%g,left=%g,bottom=%g,right=%g]", p.top, p.left, p.bottom, p.right)
}

// origELKRequest is the original Layout's request for g: json.Marshal of
// the ELKGraph it built (it changes g as the original did on the way).
func origELKRequest(g *d2graph.Graph, opts *ConfigurableOpts) ([]byte, error) {
	opts, err := graphOptions(g, opts)
	if err != nil {
		return nil, err
	}
	padding, err := readPadding(opts.Padding)
	if err != nil {
		return nil, err
	}
	degrees := make(map[*d2graph.Object]objectDegree, len(g.Objects))
	selfLoopLabelMax := make(map[*d2graph.Object]selfLoopLabelSize)
	for _, edge := range g.Edges {
		srcDegree := degrees[edge.Src]
		srcDegree.outgoing++
		degrees[edge.Src] = srcDegree
		dstDegree := degrees[edge.Dst]
		dstDegree.incoming++
		degrees[edge.Dst] = dstDegree
		if edge.Src != edge.Dst || edge.Label.Value == "" || edge.Src.Parent == nil {
			continue
		}
		parent := edge.Src.Parent
		labelSize := selfLoopLabelMax[parent]
		labelSize.width = go2.Max(labelSize.width, edge.LabelDimensions.Width)
		labelSize.height = go2.Max(labelSize.height, edge.LabelDimensions.Height)
		selfLoopLabelMax[parent] = labelSize
	}
	maxSelfLoopLabel := func(parent *d2graph.Object, isWidth bool) int {
		size := selfLoopLabelMax[parent]
		if isWidth {
			return size.width
		}
		return size.height
	}
	vertical := g.Root.Direction.Value == "down" || g.Root.Direction.Value == "" || g.Root.Direction.Value == "up"

	elkGraph := &ELKGraph{ID: "", LayoutOptions: origNewRootLayoutOptions(opts)}
	if elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing == DefaultOpts.SelfLoopSpacing {
		elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing = go2.Max(elkGraph.LayoutOptions.ConfigurableOpts.SelfLoopSpacing, maxSelfLoopLabel(g.Root, vertical)/2+5)
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
	for _, obj := range g.Objects {
		positionLabelsIcons(obj)
	}
	elkNodes := make(map[*d2graph.Object]*ELKNode)
	var walk func(*d2graph.Object, *d2graph.Object, func(*d2graph.Object, *d2graph.Object))
	walk = func(obj, parent *d2graph.Object, fn func(*d2graph.Object, *d2graph.Object)) {
		if obj.Parent != nil {
			fn(obj, parent)
		}
		for _, ch := range obj.ChildrenArray {
			walk(ch, obj, fn)
		}
	}
	walk(g.Root, nil, func(obj, parent *d2graph.Object) {
		degree := degrees[obj]
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
			obj.Height += float64(obj.LabelDimensions.Height + label.PADDING)
		}
		margin, _ := obj.SpacingOpt(label.PADDING, label.PADDING, false)
		width := margin.Left + obj.Width + margin.Right
		height := margin.Top + obj.Height + margin.Bottom
		n := &ELKNode{ID: obj.AbsID(), Width: width, Height: height}
		if len(obj.ChildrenArray) > 0 {
			n.LayoutOptions = origNewContainerLayoutOptions(opts)
			if n.LayoutOptions.ConfigurableOpts.SelfLoopSpacing == DefaultOpts.SelfLoopSpacing {
				n.LayoutOptions.ConfigurableOpts.SelfLoopSpacing = go2.Max(n.LayoutOptions.ConfigurableOpts.SelfLoopSpacing, maxSelfLoopLabel(obj, vertical)/2+5)
			}
			switch elkGraph.LayoutOptions.Direction {
			case Down, Up:
				n.LayoutOptions.NodeSizeMinimum = fmt.Sprintf("(%d, %d)", int(math.Ceil(height)), int(math.Ceil(width)))
			case Right, Left:
				n.LayoutOptions.NodeSizeMinimum = fmt.Sprintf("(%d, %d)", int(math.Ceil(width)), int(math.Ceil(height)))
			}
		} else {
			n.LayoutOptions = &elkOpts{SelfLoopDistribution: "EQUALLY"}
		}
		n.LayoutOptions.LayerConstraint = strings.ToUpper(obj.ELKLayerConstraint)
		if obj.IsContainer() {
			n.LayoutOptions.Padding = origPaddingString(adjustPadding(obj, width, height, padding))
		}
		if obj.HasLabel() {
			n.Labels = append(n.Labels, &ELKLabel{Text: obj.Label.Value, Width: float64(obj.LabelDimensions.Width), Height: float64(obj.LabelDimensions.Height)})
		}
		if parent == g.Root {
			elkGraph.Children = append(elkGraph.Children, n)
		} else {
			elkNodes[parent].Children = append(elkNodes[parent].Children, n)
		}
		if obj.SQLTable != nil {
			n.LayoutOptions.PortConstraints = "FIXED_POS"
			columns := obj.SQLTable.Columns
			colHeight := n.Height / float64(len(columns)+1)
			n.Ports = make([]*ELKPort, 0, len(columns)*2)
			var srcSide, dstSide PortSide
			switch elkGraph.LayoutOptions.Direction {
			case Left:
				srcSide, dstSide = West, East
			default:
				srcSide, dstSide = East, West
			}
			for i, col := range columns {
				n.Ports = append(n.Ports, &ELKPort{ID: srcPortID(obj, col.Name.Label), Y: float64(i+1)*colHeight + colHeight/2, LayoutOptions: &elkOpts{PortSide: srcSide}})
				n.Ports = append(n.Ports, &ELKPort{ID: dstPortID(obj, col.Name.Label), Y: float64(i+1)*colHeight + colHeight/2, LayoutOptions: &elkOpts{PortSide: dstSide}})
			}
		}
		elkNodes[obj] = n
	})
	var srcSide, dstSide PortSide
	switch elkGraph.LayoutOptions.Direction {
	case Up:
		srcSide, dstSide = North, South
	default:
		srcSide, dstSide = South, North
	}
	type portKey struct {
		obj  *d2graph.Object
		side PortSide
	}
	ports := map[portKey][]*ELKPort{}
	var portOrder []portKey
	for ei, edge := range g.Edges {
		var src, dst string
		switch {
		case edge.SrcTableColumnIndex != nil:
			src = srcPortID(edge.Src, edge.Src.SQLTable.Columns[*edge.SrcTableColumnIndex].Name.Label)
		case edge.Src.SQLTable != nil:
			p := &ELKPort{ID: fmt.Sprintf("%s.%d", srcPortID(edge.Src, "__root__"), ei), LayoutOptions: &elkOpts{PortSide: srcSide}}
			src = p.ID
			elkNodes[edge.Src].Ports = append(elkNodes[edge.Src].Ports, p)
			k := portKey{edge.Src, srcSide}
			if ports[k] == nil {
				portOrder = append(portOrder, k)
			}
			ports[k] = append(ports[k], p)
		default:
			src = edge.Src.AbsID()
		}
		switch {
		case edge.DstTableColumnIndex != nil:
			dst = dstPortID(edge.Dst, edge.Dst.SQLTable.Columns[*edge.DstTableColumnIndex].Name.Label)
		case edge.Dst.SQLTable != nil:
			p := &ELKPort{ID: fmt.Sprintf("%s.%d", dstPortID(edge.Dst, "__root__"), ei), LayoutOptions: &elkOpts{PortSide: dstSide}}
			dst = p.ID
			elkNodes[edge.Dst].Ports = append(elkNodes[edge.Dst].Ports, p)
			k := portKey{edge.Dst, dstSide}
			if ports[k] == nil {
				portOrder = append(portOrder, k)
			}
			ports[k] = append(ports[k], p)
		default:
			dst = edge.Dst.AbsID()
		}
		e := &ELKEdge{ID: edge.AbsID(), Sources: []string{src}, Targets: []string{dst}}
		if edge.Label.Value != "" {
			e.Labels = append(e.Labels, &ELKLabel{Text: edge.Label.Value, Width: float64(edge.LabelDimensions.Width), Height: float64(edge.LabelDimensions.Height),
				LayoutOptions: &elkOpts{InlineEdgeLabels: true}})
		}
		elkGraph.Edges = append(elkGraph.Edges, e)
	}
	// (The original iterated the map; each key's ports get positions from
	// that key alone, so the order does not matter.)
	for _, k := range portOrder {
		width := elkNodes[k.obj].Width
		spacing := width / float64(len(ports[k])+1)
		for i, p := range ports[k] {
			p.X = float64(i+1) * spacing
		}
	}
	return json.Marshal(elkGraph)
}

// layoutRequest returns json.Marshal of the request Layout sends for g
// (through beforeELKHook), or the error Layout ends with before it. The hook
// cancels the layout's context, so Layout returns right after building the
// request, before the engine runs (Mr. Tree and radial overflow the stack on
// some graphs, in the original too), and without a panic (TinyGo's wasm
// target, which runs this package's tests, cannot recover from one).
func layoutRequest(g *d2graph.Graph, opts *ConfigurableOpts) (request []byte, outcome string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() { beforeELKHook = nil }()
	hooked := false
	beforeELKHook = func(elkGraph *ELKGraph) {
		hooked = true
		raw, err := json.Marshal(elkGraph)
		if err != nil {
			request = []byte("marshal error: " + err.Error())
		} else {
			request = raw
		}
		cancel()
	}
	err := Layout(ctx, g, opts)
	switch {
	case hooked:
		return request, "ok"
	case err != nil:
		return request, "error: " + err.Error()
	}
	return request, "laid out without the hook"
}

// childrenMutations make graphs whose Children maps and ChildrenArray
// disagree, as graphs built or edited through d2's Go API can.
var childrenMutations = []struct {
	name   string
	mutate func(g *d2graph.Graph)
}{
	{"as compiled", func(g *d2graph.Graph) {}},
	{"containers without Children map", func(g *d2graph.Graph) {
		for _, obj := range g.Objects {
			if len(obj.ChildrenArray) > 0 {
				for key := range obj.Children {
					delete(obj.Children, key)
				}
			}
		}
	}},
	{"leaves with a Children map entry", func(g *d2graph.Graph) {
		for i, obj := range g.Objects {
			if len(obj.ChildrenArray) == 0 {
				if obj.Children == nil {
					obj.Children = make(map[string]*d2graph.Object)
				}
				obj.Children[fmt.Sprint("extra", i)] = obj
			}
		}
	}},
	{"every other container without Children map", func(g *d2graph.Graph) {
		n := 0
		for _, obj := range g.Objects {
			if len(obj.ChildrenArray) > 0 {
				if n%2 == 0 {
					for key := range obj.Children {
						delete(obj.Children, key)
					}
				}
				n++
			}
		}
	}},
}

var requestDiagrams = append([]string{
	"a.b -> c",
	"a: {b; d}\na.b -> a.d\nx -> a",
	"direction: right\nouter: {inner: {leaf -> leaf2}; side}\nouter.inner.leaf -> outer.side: label\nouter -> z",
	"t: {shape: sql_table; id: int; name: string}\nc: {t2: {shape: sql_table; k: int}}\nt.id -> c.t2.k\nt -> c\nc.t2 -> t",
	"direction: left\nbox: {label: a container with a long label; x -> y; x -> x: self}\nbox.y -> out",
	"direction: up\ng: {icon: https://icons.terrastruct.com/essentials/087-menu.svg; m; n}\ng.m -> g.n\nk.elk-layer-constraint: first\nk -> g",
}, elkGoDiagrams...)

// TestRequestMatchesOriginal compares the request Layout sends
// with the original's (option building included) for graphs
// whose Children maps and ChildrenArray agree or not, with every option
// variant.
func TestRequestMatchesOriginal(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	variants := append([]*ConfigurableOpts{nil}, func() (out []*ConfigurableOpts) {
		for _, v := range elkGoOptionVariants() {
			v := v
			out = append(out, &v)
		}
		return out
	}()...)
	compared := 0
	for _, input := range requestDiagrams {
		for _, mutation := range childrenMutations {
			for vi, opts := range variants {
				want := compileELKGoDiagram(t, ruler, input)
				mutation.mutate(want)
				wantRequest, wantErr := origELKRequest(want, opts)
				got := compileELKGoDiagram(t, ruler, input)
				mutation.mutate(got)
				gotRequest, outcome := layoutRequest(got, opts)
				if wantErr != nil {
					if !strings.Contains(outcome, wantErr.Error()) {
						t.Fatalf("%q, %s, options %d: %s, want error %v", input, mutation.name, vi, outcome, wantErr)
					}
					continue
				}
				if string(gotRequest) != string(wantRequest) {
					t.Fatalf("%q, %s, options %d (%s): request differs from the original's\n got %s\nwant %s",
						input, mutation.name, vi, outcome, gotRequest, wantRequest)
				}
				compared++
			}
		}
	}
	if compared < 200 {
		t.Fatalf("only %d requests compared", compared)
	}
}

// TestEdgeEdgePointeeSequences changes the int behind
// EdgeEdgeSpacing (of a caller's options and of DefaultOpts) between layouts,
// with repeats so that the root's option set is found by its key, and
// compares every typed layout with the JSON path's.
func TestEdgeEdgePointeeSequences(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	savedDefault := *DefaultOpts.EdgeEdgeSpacing
	defer func() { *DefaultOpts.EdgeEdgeSpacing = savedDefault }()
	input := "a -> c\na -> d\nb -> c\nb -> d\na -> e\nb -> e\nc -> f\nd -> f\ne -> f\na -> f\nb -> f"
	spacing := 5
	opts := DefaultOpts
	opts.EdgeEdgeSpacing = &spacing
	values := []int{5, 5, 400, 400, 5, 77, 77, 77, 50, 0, 0, 400}
	distinct := map[string]bool{}
	for _, useDefault := range []bool{false, true} {
		for step, value := range values {
			options := &opts
			if useDefault {
				options = nil
				*DefaultOpts.EdgeEdgeSpacing = value
			} else {
				spacing = value
			}
			want := layoutOutcome(t, ruler, input, options, true)
			if got := layoutOutcome(t, ruler, input, options, false); got != want {
				t.Fatalf("default %v, step %d (spacing %d): typed layout differs from JSON", useDefault, step, value)
			}
			distinct[want] = true
		}
	}
	if len(distinct) < 3 {
		t.Fatalf("the spacing changes only %d layouts; pick another diagram", len(distinct))
	}
}

// TestScratchByteBounds checks that the per-element byte bounds
// retainedBytes uses are at least the elements' sizes.
func TestScratchByteBounds(t *testing.T) {
	for _, c := range []struct {
		value any
		bound int
	}{
		{elkObject{}, elkObjectBytes}, {ELKNode{}, elkNodeBytes}, {elkOpts{}, elkOptsBytes}, {ELKLabel{}, elkLabelBytes},
		{ELKEdge{}, elkEdgeBytes}, {ELKPort{}, elkPortBytes}, {elkEdgeState{}, elkEdgeStateBytes}, {elkVisit{}, elkVisitBytes},
		{ELKEdgeSection{}, edgeSectionBytes}, {ELKPoint{}, elkPointBytes}, {elkPreparedSet{}, preparedSetBytes},
		{elkOptionPair{}, optionPairBytes}, {linearObject{}, linearObjectBytes}, {linearSegment{}, linearSegmentBytes},
		{segmentItem{}, segmentItemBytes}, {spatialBounds{}, boundsBytes}, {(*ELKNode)(nil), pointerBytes},
		{"", stringBytes}, {[]int32(nil), sliceBytes}, {any(nil), anyBytes},
	} {
		if size := int(reflect.TypeOf(&c.value).Elem().Size()); c.value != nil {
			size = int(reflect.TypeOf(c.value).Size())
			if size > c.bound {
				t.Errorf("%T takes %d bytes, bound %d", c.value, size, c.bound)
			}
		} else if size > c.bound {
			t.Errorf("any takes %d bytes, bound %d", size, c.bound)
		}
	}
}

// TestStringFieldsComplete checks that stringFields lists every
// string field of the option structs (the caches copy exactly those).
func TestStringFieldsComplete(t *testing.T) {
	var o elkOpts
	value := reflect.ValueOf(&o).Elem()
	var count func(v reflect.Value) int
	count = func(v reflect.Value) int {
		n := 0
		for i := 0; i < v.NumField(); i++ {
			switch field := v.Field(i); field.Kind() {
			case reflect.String:
				field.SetString(fmt.Sprint("s", n, v.Type().Field(i).Name))
				n++
			case reflect.Struct:
				n += count(field)
			}
		}
		return n
	}
	total := count(value)
	fields := o.stringFields()
	if len(fields) != total {
		t.Fatalf("stringFields lists %d fields, elkOpts has %d", len(fields), total)
	}
	seen := map[string]bool{}
	for _, field := range fields {
		seen[*field] = true
	}
	if len(seen) != total {
		t.Fatalf("stringFields lists %d distinct fields of %d", len(seen), total)
	}
	c := o.ConfigurableOpts
	if got, want := len(c.stringFields()), count(reflect.ValueOf(&c).Elem()); got != want {
		t.Fatalf("ConfigurableOpts.stringFields lists %d fields of %d", got, want)
	}
}

// TestMemoPointers checks the graph-options memo against
// graphOptions when the defaults' pointers change but not what they point
// to: the result must carry the current defaults' pointers, as graphOptions'
// does, and release must leave none of them in the memo.
func TestMemoPointers(t *testing.T) {
	var memo graphOptionsMemo
	plain := &d2graph.Graph{}
	defaults := func() *ConfigurableOpts {
		o := DefaultOpts
		o.NodeNode, o.EdgeNode, o.EdgeEdge = go2.Pointer(12.5), nil, go2.Pointer(0.0)
		o.ForceRepulsion, o.StressDesiredEdgeLength, o.RadialRadius = go2.Pointer(2.0), go2.Pointer(3.0), go2.Pointer(4.0)
		o.EdgeEdgeSpacing = go2.Pointer(7)
		o.Algorithm, o.EdgeRouting = "MrTree", "polyline"
		return &o
	}
	for step := 0; step < 4; step++ {
		in := defaults()
		want, err := graphOptions(plain, in)
		if err != nil {
			t.Fatal(err)
		}
		got, err := memo.of(plain, in)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(*got, *want) {
			t.Fatalf("step %d: %+v, want %+v", step, *got, *want)
		}
		if got.NodeNode != in.NodeNode || got.EdgeNode != in.EdgeNode || got.EdgeEdge != in.EdgeEdge ||
			got.ForceRepulsion != in.ForceRepulsion || got.StressDesiredEdgeLength != in.StressDesiredEdgeLength ||
			got.RadialRadius != in.RadialRadius || got.EdgeEdgeSpacing != in.EdgeEdgeSpacing {
			t.Fatalf("step %d: the result's pointers are not the defaults'", step)
		}
		if step > 0 && memo.resultGeneration(got) != 1 {
			t.Fatalf("step %d: equal defaults with other pointers missed the memo", step)
		}
		memo.release()
		for _, p := range []*float64{memo.result.NodeNode, memo.result.EdgeEdge, memo.result.ForceRepulsion,
			memo.result.StressDesiredEdgeLength, memo.result.RadialRadius, memo.input.NodeNode} {
			if p != &presentFloat {
				t.Fatalf("step %d: the memo keeps a pointer of the caller's", step)
			}
		}
		if memo.result.EdgeNode != nil || memo.result.EdgeEdgeSpacing != &presentInt || memo.input.EdgeEdgeSpacing != &presentInt {
			t.Fatalf("step %d: the memo's pointers are not markers", step)
		}
	}
}

// liveHeap is the live heap after collections.
func liveHeap() uint64 {
	var stats runtime.MemStats
	for i := 0; i < 3; i++ {
		runtime.GC()
	}
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// TestLargeLayoutNotRetained lays out a SQL table of 20000
// columns (40000 ports; layoutguard does not count columns) and then small
// diagrams: what the scratch and elk-go keep for reuse must stay within
// their bounds (an earlier version kept 4.8 MB more, for good).
func TestLargeLayoutNotRetained(t *testing.T) {
	if runtime.Compiler == "tinygo" {
		t.Skip("heap statistics differ under TinyGo")
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	if testing.Short() {
		t.Skip("lays out a table of 20000 columns")
	}
	small := func() {
		if err := Layout(context.Background(), compileELKGoDiagram(t, ruler, "a -> b\nb -> c"), nil); err != nil {
			t.Fatal(err)
		}
	}
	small()
	before := liveHeap()
	func() {
		var table strings.Builder
		table.WriteString("t: {shape: sql_table\n")
		for i := 0; i < 20000; i++ {
			fmt.Fprintf(&table, "c%d: int\n", i)
		}
		table.WriteString("}\nx -> t\nt -> y\n")
		if err := Layout(context.Background(), compileELKGoDiagram(t, ruler, table.String()), nil); err != nil {
			t.Fatal(err)
		}
	}()
	for i := 0; i < 5; i++ {
		small()
	}
	after := liveHeap()
	t.Logf("live heap %d -> %d bytes after one large layout", before, after)
	if after > before+1<<20 {
		t.Fatalf("live heap %d -> %d bytes after one large layout", before, after)
	}
	releasedScratch.Lock()
	kept := releasedScratch.slot
	releasedScratch.Unlock()
	if kept != nil && kept.retainedBytes() > maxScratchBytes {
		t.Fatalf("the slot keeps a scratch of %d bytes", kept.retainedBytes())
	}
}

// TestCallerOptionsNotRetained lays out with options whose
// strings are cut from one large string and whose pointers point into one
// large array: nothing kept for reuse may refer to either afterwards
// (an earlier version's graph-options memo and root option cache kept them).
func TestCallerOptionsNotRetained(t *testing.T) {
	if runtime.Compiler == "tinygo" {
		t.Skip("heap statistics differ under TinyGo")
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	const size = 32 << 20
	run := func() {
		var builder strings.Builder
		builder.Grow(size)
		builder.WriteString("[top=60,left=60,bottom=60,right=60]layeredPOLYLINE")
		for builder.Len() < size {
			builder.WriteString("..........")
		}
		big := builder.String()
		values := make([]float64, size/8)
		values[1], values[2] = 15, 25
		spacing := make([]int, size/8)
		spacing[3] = 33
		opts := DefaultOpts
		opts.Padding, opts.Algorithm = big[0:35], big[35:42]
		opts.NodeNode, opts.EdgeEdge, opts.EdgeEdgeSpacing = &values[1], &values[2], &spacing[3]
		for _, routing := range []string{"", big[42:50]} {
			opts.EdgeRouting = routing
			for _, input := range []string{"a -> b\nb -> c", "x: {y -> z}\nx -> w"} {
				if err := Layout(context.Background(), compileELKGoDiagram(t, ruler, input), &opts); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	before := liveHeap()
	run()
	if after := liveHeap(); after > before+size/2 {
		t.Fatalf("live heap %d -> %d bytes: the caller's options are still referenced", before, after)
	}
}

// TestPanicDropsScratch lays out a diagram on which Layout
// panics (Radial leaves the container's self loop without a route, which
// the route trimming then indexes; the original panics the same way) and
// checks that the panic value reaches the caller unchanged and that the
// scratch the layout took was not offered for reuse while the panic unwound;
// later layouts are unchanged.
func TestPanicDropsScratch(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	layout := func(input string) string {
		g := compileELKGoDiagram(t, ruler, input)
		if err := Layout(context.Background(), g, nil); err != nil {
			t.Fatal(err)
		}
		serialized, err := d2graph.SerializeGraph(g)
		if err != nil {
			t.Fatal(err)
		}
		return string(serialized)
	}
	input := "a -> b: label\nc: {d -> e}\nb -> c.d"
	want := layout(input)
	// The runtime error of indexing an empty slice at 0, as this runtime
	// words it (TinyGo leaves out the index and length).
	wantPanic := func() (value any) {
		defer func() { value = recover() }()
		var empty []int
		index := 0
		return empty[index]
	}()
	for round := 0; round < 3; round++ {
		releasedScratch.Lock()
		taken := releasedScratch.slot
		releasedScratch.Unlock()
		g := compileELKGoDiagram(t, ruler, "src.z.k\nB -> src.z.k\nsrc.z -> src.z")
		g.Data = map[string]any{"elk-algorithm": "radial"}
		recovered := func() (value any) {
			defer func() { value = recover() }()
			_ = Layout(context.Background(), g, nil)
			return nil
		}()
		if _, ok := recovered.(runtime.Error); !ok || fmt.Sprint(recovered) != fmt.Sprint(wantPanic) {
			t.Fatalf("round %d: recovered %v, want %v (the original's runtime error)", round, recovered, wantPanic)
		}
		releasedScratch.Lock()
		slot := releasedScratch.slot
		releasedScratch.Unlock()
		if taken != nil && slot == taken {
			t.Fatalf("round %d: the panicking layout's scratch was offered for reuse", round)
		}
		if got := layout(input); got != want {
			t.Fatalf("round %d: layout after a panic differs", round)
		}
	}
}

// TestGridListBytes checks the measure of the bend grids' lists
// that release keeps (they grow only in calls that build the grids): after a
// layout that builds them and after one that does not, it equals the lists'
// storage.
func TestGridListBytes(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	measure := func(grids *bendGrids) int {
		bytes := grids.objectGrid.cellListBytes() + grids.segmentGrid.cellListBytes()
		for _, ids := range grids.segmentIDs[:cap(grids.segmentIDs)] {
			bytes += 4 * cap(ids)
		}
		return bytes
	}
	for step, input := range []string{denseBenchmarkInput(60, 120), "a -> b", denseBenchmarkInput(80, 160), "x: {y -> z}"} {
		if err := Layout(context.Background(), compileELKGoDiagram(t, ruler, input), nil); err != nil {
			t.Fatal(err)
		}
		releasedScratch.Lock()
		kept := releasedScratch.slot
		releasedScratch.Unlock()
		if kept == nil {
			t.Fatalf("step %d: no scratch kept", step)
		}
		if step == 0 && cap(kept.bendGrids.objectGrid.cells) == 0 {
			t.Fatalf("step %d: the grids were not built; pick a larger diagram", step)
		}
		if got, want := kept.bendGrids.listBytes, measure(&kept.bendGrids); got != want {
			t.Fatalf("step %d: listBytes %d, the lists take %d", step, got, want)
		}
	}
}
