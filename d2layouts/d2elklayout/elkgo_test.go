package d2elklayout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"sync"
	"testing"

	elk "github.com/d2lang/elk-go"
	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

// elkGoDiagrams cover the shapes of request D2 sends: nesting, SQL table
// ports (column and table ends), labels on nodes and edges, self loops,
// icons, layer constraints and every direction.
var elkGoDiagrams = []string{
	"a -> b: hello\nb -> c\na -> c: long edge label\nc -> a",
	"direction: right\nx: {\n  y: {\n    z -> w: inner\n  }\n  y.z -> q\n}\nx.y.w -> tip\ntip -> tip: self",
	"direction: left\nt: {\n  shape: sql_table\n  id: int\n  name: string\n  email: string\n}\nu: {\n  shape: sql_table\n  id: int\n  t_id: int\n}\nu.t_id -> t.id\nu -> t\nx -> t\nt -> y\nt.email -> y",
	"direction: up\ncloud: {\n  icon: https://icons.terrastruct.com/essentials/087-menu.svg\n  a -> b\n}\nc: {icon: https://icons.terrastruct.com/essentials/087-menu.svg}\ncloud.a -> c\nc -> c: one\nc -> c: two",
	"a.b.c.d -> e.f\ne.f -> a.b\na.x -> a.y: within\na -> e: between containers\nn: {width: 300; height: 20}\nn -> a.b.c",
	"s: {shape: circle}\nd: {shape: diamond}\np: {shape: person}\ns -> d -> p\np -> s\nm: {shape: page; style.multiple: true}\nk: {style.3d: true}\nm -> k -> s",
}

func compileELKGoDiagram(t *testing.T, ruler *textmeasure.Ruler, input string) *d2graph.Graph {
	t.Helper()
	g, _, err := d2compiler.Compile("", strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("compile %q: %v", input, err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		t.Fatal(err)
	}
	return g
}

// elkGoOptionVariants exercise every option the request can carry, including
// non-integral, tiny and huge spacings that JSON writes in exponent form.
func elkGoOptionVariants() []ConfigurableOpts {
	custom := DefaultOpts
	custom.NodeNode = go2.Pointer(12.5)
	custom.EdgeNode = go2.Pointer(1e-7)
	custom.EdgeEdge = go2.Pointer(0.0)
	custom.EdgeRouting = "POLYLINE"
	custom.WrappingStrategy = "MULTI_EDGE"
	custom.ForceNodeModelOrder = true
	custom.FeedbackEdges = true
	custom.MergeEdges = true
	custom.NodeSpacing = 33
	custom.EdgeNodeSpacing = 0
	custom.EdgeEdgeSpacing = nil
	custom.SelfLoopSpacing = 7
	custom.Padding = "[top=11,left=22.5,bottom=0,right=44]"

	mrtree := DefaultOpts
	mrtree.Algorithm = "mrtree"
	mrtree.MrTreeSearchOrder = "BFS"
	mrtree.MrTreeWeighting = "FAN"
	mrtree.NodeNode = go2.Pointer(1e21)

	force := DefaultOpts
	force.Algorithm = "force"
	force.ForceModel = "EADES"
	force.ForceRepulsion = go2.Pointer(2.5e-3)
	force.StressDesiredEdgeLength = go2.Pointer(123456789.125)
	force.RadialRadius = go2.Pointer(5e-324)
	force.RadialWedgeCriteria = "LEAF_NUMBER"
	force.RadialCompactor = "WEDGE_COMPACTION"
	return []ConfigurableOpts{DefaultOpts, custom, mrtree, force}
}

func decodeLikeLayoutJSON(t *testing.T, raw []byte) *elk.Graph {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var graph elk.Graph
	if err := decoder.Decode(&graph); err != nil {
		t.Fatal(err)
	}
	return &graph
}

// TestELKGoRequestMatchesJSON checks that every request Layout builds converts
// to exactly the values elk.LayoutJSON decodes from its JSON, member orders
// included, and that the typed path and the original's JSON round trip
// (runELKViaJSON) leave identical graphs behind.
func TestELKGoRequestMatchesJSON(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		beforeELKHook = nil
		setJSONOracle(false)
	}()
	for _, input := range elkGoDiagrams {
		for vi, opts := range elkGoOptionVariants() {
			if opts.Algorithm == "mrtree" && strings.Contains(input, "sql_table") {
				// elk-go's Mr. Tree recurses without end on this graph, through
				// JSON as well (fatal stack overflow in placeNodes).
				continue
			}
			requests := 0
			beforeELKHook = func(elkGraph *ELKGraph) {
				requests++
				raw, err := json.Marshal(elkGraph)
				if err != nil {
					t.Fatal(err)
				}
				request := newELKGoGraph(elkGraph)
				if want := decodeLikeLayoutJSON(t, raw); !reflect.DeepEqual(request.graph, want) {
					got, _ := json.Marshal(request.graph)
					wantJSON, _ := json.Marshal(want)
					t.Fatalf("variant %d of %q: converted request differs\n got %s\nwant %s", vi, input, got, wantJSON)
				}
			}
			// Both paths must end the same way: the same graph, the same
			// error or the same panic.
			outputs := make([]string, 2)
			for path, viaJSON := range []bool{false, true} {
				setJSONOracle(viaJSON)
				g := compileELKGoDiagram(t, ruler, input)
				o := opts
				outputs[path] = func() (outcome string) {
					defer func() {
						if recovered := recover(); recovered != nil {
							outcome = fmt.Sprint("panic: ", recovered)
						}
					}()
					if err := Layout(context.Background(), g, &o); err != nil {
						return "error: " + err.Error()
					}
					serialized, err := d2graph.SerializeGraph(g)
					if err != nil {
						t.Fatal(err)
					}
					return string(serialized)
				}()
			}
			if requests != 2 {
				t.Fatalf("saw %d requests, want 2", requests)
			}
			if outputs[0] != outputs[1] {
				t.Fatalf("variant %d of %q: typed and JSON layouts differ:\n%.300s\n%.300s", vi, input, outputs[0], outputs[1])
			}
			if strings.HasPrefix(outputs[0], "panic: ") || strings.HasPrefix(outputs[0], "error: ") {
				t.Logf("variant %d of %q: both paths: %.200s", vi, input, outputs[0])
			}
		}
	}
}

// TestELKGoOptionsMatchJSON compares the option conversion with JSON for
// random option structs, every field set or empty in turn.
func TestELKGoOptionsMatchJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	strs := []string{"", "DOWN", "a<b>&c", "x y", " ", "é", "0"}
	// 1<<40 and -(1<<62) where int has 64 bits (written so that the test
	// also compiles where it has 32, as for TinyGo's wasm).
	ints := []int{0, 1, -1, 8, 255, 256, math.MaxInt>>23 + 1, -(math.MaxInt>>1 + 1)}
	floats := []float64{0, math.Copysign(0, -1), 1, -1, 0.1, 1e-6, 9.99e-7, 1e-7, 1e20, 1e21, 1.5e300, 5e-324,
		math.MaxFloat64, 123456789.125, 1.0 / 3}
	pick := func() string { return strs[rng.Intn(len(strs))] }
	pickInt := func() int { return ints[rng.Intn(len(ints))] }
	pickFloat := func() *float64 {
		if rng.Intn(3) == 0 {
			return nil
		}
		return go2.Pointer(floats[rng.Intn(len(floats))])
	}
	for i := 0; i < 2000; i++ {
		o := &elkOpts{
			FixedAlignment: pick(), Thoroughness: pickInt(), Direction: Direction(pick()),
			HierarchyHandling: pick(), InlineEdgeLabels: rng.Intn(2) == 0, ConsiderModelOrder: pick(),
			CycleBreakingStrategy: pick(), SelfLoopDistribution: pick(), LayerConstraint: pick(),
			NodeSizeConstraints: pick(), ContentAlignment: pick(), NodeSizeMinimum: pick(),
			PortSide: PortSide(pick()), PortConstraints: pick(),
			ConfigurableOpts: ConfigurableOpts{
				LayeredOptions: LayeredOptions{
					NodeNode: pickFloat(), EdgeNode: pickFloat(), EdgeEdge: pickFloat(), EdgeRouting: pick(),
					WrappingStrategy: pick(), ForceNodeModelOrder: rng.Intn(2) == 0,
					FeedbackEdges: rng.Intn(2) == 0, MergeEdges: rng.Intn(2) == 0,
				},
				AlgorithmOptions: AlgorithmOptions{
					ForceModel: pick(), ForceRepulsion: pickFloat(), StressDesiredEdgeLength: pickFloat(),
					MrTreeSearchOrder: pick(), MrTreeWeighting: pick(), RadialRadius: pickFloat(),
					RadialWedgeCriteria: pick(), RadialCompactor: pick(),
				},
				Algorithm: pick(), NodeSpacing: pickInt(), Padding: pick(), EdgeNodeSpacing: pickInt(),
				SelfLoopSpacing: pickInt(),
			},
		}
		if rng.Intn(2) == 0 {
			o.EdgeEdgeSpacing = go2.Pointer(pickInt())
		}
		raw, err := json.Marshal(&ELKNode{LayoutOptions: o})
		if err != nil {
			t.Fatal(err)
		}
		want := decodeLikeLayoutJSON(t, raw)
		r := &elkGoGraph{}
		options, keys := r.options(o)
		if !reflect.DeepEqual(options, want.LayoutOptions) || !reflect.DeepEqual(keys, want.LayoutOptionKeys()) {
			t.Fatalf("options %d differ:\n got %v %v\nwant %v %v", i, keys, options, want.LayoutOptionKeys(), want.LayoutOptions)
		}
	}
}

// TestELKGoRequestFormerFallbacks checks what the typed path takes from the
// requests it once left to JSON. Strings that are not valid UTF-8 (IDs,
// texts, edge ends, option values) convert to what elk-go decodes from their
// JSON; a non-finite number, which json.Marshal rejects, or nesting deeper
// than jsonSafeDepth makes build flag the request for json.Marshal. Negative
// zero keeps its bits.
func TestELKGoRequestFormerFallbacks(t *testing.T) {
	base := func() *ELKGraph {
		return &ELKGraph{
			LayoutOptions: &elkOpts{Direction: Down},
			Children: []*ELKNode{
				{ID: "a", X: math.Copysign(0, -1), Width: 10, Height: 10, Labels: []*ELKLabel{{Text: "l", Width: 3, Height: 2}}},
				nil,
				{ID: "b", Width: 10, Height: 10, Ports: []*ELKPort{{ID: "b.p", LayoutOptions: &elkOpts{PortSide: North}}}},
			},
			Edges: []*ELKEdge{{ID: "e", Sources: []string{"a"}, Targets: []string{"b.p"}}, nil, {ID: "f", Sources: []string{}}},
		}
	}
	matchesJSON := func(name string, g *ELKGraph) {
		t.Helper()
		raw, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if got := newELKGoGraph(g).graph; !reflect.DeepEqual(got, decodeLikeLayoutJSON(t, raw)) {
			gotJSON, _ := json.Marshal(got)
			t.Errorf("%s: converted request differs\n got %s\nwant %s", name, gotJSON, raw)
		}
	}
	// flagged builds the typed request for g and reports whether build
	// flagged it for json.Marshal.
	flagged := func(g *ELKGraph) bool {
		var request elkGoRequest
		request.reserve(0, 0, 0, 0)
		request.build(g).Release()
		defer request.release()
		return request.marshalMayFail
	}
	matchesJSON("plain", base())
	if flagged(base()) {
		t.Error("plain request flagged for json.Marshal")
	}
	if x := *newELKGoGraph(base()).graph.Children[0].X; x != 0 || !math.Signbit(x) {
		t.Fatalf("x = %v, want -0", x)
	}
	for name, mutate := range map[string]func(*ELKGraph){
		"invalid UTF-8 id":  func(g *ELKGraph) { g.Children[2].ID = "b\xff" },
		"invalid UTF-8 txt": func(g *ELKGraph) { g.Children[0].Labels[0].Text = "\xc3" },
		"invalid option":    func(g *ELKGraph) { g.Children[2].Ports[0].LayoutOptions.LayerConstraint = "\xe2\x82" },
		"invalid target":    func(g *ELKGraph) { g.Edges[0].Targets[0] = "\xff" },
		"invalid container": func(g *ELKGraph) { g.Edges[0].Container = "\xff" },
	} {
		g := base()
		mutate(g)
		matchesJSON(name, g)
		if flagged(g) {
			t.Errorf("%s: flagged for json.Marshal", name)
		}
	}
	for name, mutate := range map[string]func(*ELKGraph){
		"NaN width":       func(g *ELKGraph) { g.Children[0].Width = math.NaN() },
		"infinite label":  func(g *ELKGraph) { g.Children[0].Labels[0].Y = math.Inf(-1) },
		"infinite option": func(g *ELKGraph) { g.LayoutOptions.NodeNode = go2.Pointer(math.Inf(1)) },
	} {
		g := base()
		mutate(g)
		if _, err := json.Marshal(g); err == nil {
			t.Errorf("%s: json.Marshal succeeded", name)
		}
		if !flagged(g) {
			t.Errorf("%s: not flagged for json.Marshal", name)
		}
	}
	nested := func(depth int) *ELKGraph {
		g := &ELKGraph{}
		children := &g.Children
		for i := 0; i < depth; i++ {
			n := &ELKNode{ID: "n" + string(rune('a'+i%26))}
			*children = []*ELKNode{n}
			children = &n.Children
		}
		return g
	}
	for _, depth := range []int{jsonSafeDepth, jsonSafeDepth + 1} {
		if got := flagged(nested(depth)); got != (depth > jsonSafeDepth) {
			t.Errorf("nesting of depth %d flagged %v", depth, got)
		}
	}
}

// TestELKGoApplyMatchesJSON checks that copying a result back matches
// decoding its JSON, for non-finite coordinates and negative zero too.
func TestELKGoApplyMatchesJSON(t *testing.T) {
	build := func() *ELKGraph {
		return &ELKGraph{
			LayoutOptions: &elkOpts{Direction: Down},
			Children: []*ELKNode{
				{ID: "a", X: 3, Y: 4, Width: 10, Height: 10, Labels: []*ELKLabel{{Text: "l", Width: 3, Height: 2}},
					Ports: []*ELKPort{{ID: "a.p", X: 1, Y: 2}}},
				{ID: "b", Width: 10, Height: 10, Children: []*ELKNode{{ID: "b.c", Width: 5, Height: 5}}},
			},
			Edges: []*ELKEdge{
				{ID: "e", Sources: []string{"a"}, Targets: []string{"b"}, Labels: []*ELKLabel{{Text: "m"}}},
				{ID: "f", Sources: []string{"a"}, Targets: []string{"b.c"}},
			},
		}
	}
	viaTyped, viaJSON := build(), build()
	request := newELKGoGraph(viaTyped)
	if err := elk.Layout(request.graph); err != nil {
		t.Fatal(err)
	}
	// Damage the result where JSON writes null or loses nothing.
	nodes := request.graph.Children
	*nodes[0].X = math.NaN()
	*nodes[0].Height = math.Inf(1)
	*nodes[1].Children[0].Y = math.Copysign(0, -1)
	*nodes[0].Labels[0].Width = math.Inf(-1)
	*nodes[0].Ports[0].Y = math.NaN()
	edges := request.graph.Edges
	edges[0].Sections[0].StartPoint.X = math.NaN()
	edges[0].Sections[0].BendPoints = append(edges[0].Sections[0].BendPoints, elk.Point{X: math.Inf(1), Y: math.Copysign(0, -1)})
	edges[1].Sections = append(edges[1].Sections, nil)
	edges[1].Container = nil

	response, err := json.Marshal(request.graph)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(response, &viaJSON); err != nil {
		t.Fatal(err)
	}
	request.apply()
	if !reflect.DeepEqual(viaTyped, viaJSON) {
		got, _ := json.Marshal(viaTyped)
		want, _ := json.Marshal(viaJSON)
		t.Fatalf("applied result differs\n got %s\nwant %s", got, want)
	}
	if y := viaTyped.Children[1].Children[0].Y; y != 0 || !math.Signbit(y) {
		t.Fatalf("y = %v, want -0", y)
	}
	if y := viaTyped.Edges[0].Sections[0].BendPoints[len(viaTyped.Edges[0].Sections[0].BendPoints)-1].Y; !math.Signbit(y) {
		t.Fatalf("bend y = %v, want -0", y)
	}
}

// TestDefaultPaddingParse checks that defaultPadding is what parsing its text
// gives.
func TestDefaultPaddingParse(t *testing.T) {
	padding, err := readPadding(defaultPaddingText)
	if err != nil || padding != defaultPadding {
		t.Fatalf("readPadding(%q) = %v, %v; want %v", defaultPaddingText, padding, err, defaultPadding)
	}
	if cached, err := readPaddingCached(defaultPaddingText); err != nil || cached != padding {
		t.Fatalf("readPaddingCached = %v, %v", cached, err)
	}
}

// TestBoxedValuesMatch checks that the shared boxes hold exactly the value
// a fresh conversion to an interface would.
func TestBoxedValuesMatch(t *testing.T) {
	for _, value := range []string{"", "BALANCED", "DOWN", "UP", "RIGHT", "LEFT", "INCLUDE_CHILDREN", "NODES_AND_EDGES",
		"NONE", "GREEDY_MODEL_ORDER", "GREEDY", "MINIMUM_SIZE", "H_CENTER V_CENTER", "EQUALLY", "FIXED_POS", "layered",
		"[top=50,left=50,bottom=50,right=50]", "other", "balanced"} {
		if boxed := boxedString(value); boxed != any(value) {
			t.Fatalf("boxedString(%q) = %#v", value, boxed)
		}
	}
	values := []float64{0, math.Copysign(0, -1), 1, 8, 40, 255, 255.5, 256, -1, 0.5, math.NaN(), math.Inf(1), 1e300}
	for i := 0; i < 300; i++ {
		values = append(values, float64(i), float64(i)+0.25)
	}
	for _, value := range values {
		boxed, ok := boxedFloat(value).(float64)
		if !ok || math.Float64bits(boxed) != math.Float64bits(value) {
			t.Fatalf("boxedFloat(%v) = %#v", value, boxedFloat(value))
		}
	}
}

// TestELKGoConcurrentLayouts lays out the same diagrams from several
// goroutines (shared option sets and registries) and expects the serial
// results; run with -race.
func TestELKGoConcurrentLayouts(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	var compileMu sync.Mutex
	layout := func(input string) string {
		compileMu.Lock() // the ruler is not safe for concurrent use
		g := compileELKGoDiagram(t, ruler, input)
		compileMu.Unlock()
		if err := Layout(context.Background(), g, nil); err != nil {
			return "error: " + err.Error()
		}
		serialized, err := d2graph.SerializeGraph(g)
		if err != nil {
			t.Error(err)
		}
		return string(serialized)
	}
	want := make([]string, len(elkGoDiagrams))
	for i, input := range elkGoDiagrams {
		want[i] = layout(input)
	}
	done := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for round := 0; round < 3; round++ {
				for i, input := range elkGoDiagrams {
					if got := layout(input); got != want[i] {
						t.Errorf("diagram %d differs under concurrency", i)
						return
					}
				}
			}
		}()
	}
	for worker := 0; worker < 4; worker++ {
		<-done
	}
}
