package d2dagrelayout

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2compiler"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2themes/d2themescatalog"
	"github.com/d2lang/d2/lib/textmeasure"
	"github.com/d2lang/dagro"
)

// randomDagreSource writes a random d2 diagram: nested containers, leaves of
// every shape (tables and classes included), labeled and unlabeled edges with
// every arrow, self-loops, parallel edges in both directions, edges to
// containers and a random direction.
func randomDagreSource(r *rand.Rand) string {
	n := 1 + r.Intn(24)
	parent := make([]int, n)
	children := make([][]int, n)
	for i := range parent {
		parent[i] = -1
		if i > 0 && r.Intn(3) == 0 {
			parent[i] = r.Intn(i)
			children[parent[i]] = append(children[parent[i]], i)
		}
	}
	path := func(i int) string {
		p := fmt.Sprintf("n%d", i)
		for j := parent[i]; j >= 0; j = parent[j] {
			p = fmt.Sprintf("n%d.%s", j, p)
		}
		return p
	}
	shapes := []string{
		"", "", "", "oval", "circle", "cylinder", "diamond", "hexagon", "cloud", "person", "queue",
		"page", "package", "step", "callout", "stored_data", "document", "parallelogram", "c4-person",
		"sql_table", "class", "text", "square",
	}
	var b strings.Builder
	if r.Intn(2) == 0 {
		fmt.Fprintf(&b, "direction: %s\n", []string{"up", "down", "left", "right"}[r.Intn(4)])
	}
	var write func(i int, indent string)
	write = func(i int, indent string) {
		fmt.Fprintf(&b, "%sn%d", indent, i)
		if r.Intn(2) == 0 {
			fmt.Fprintf(&b, ": label %d", i)
		}
		if len(children[i]) > 0 {
			b.WriteString(" {\n")
			for _, c := range children[i] {
				write(c, indent+"  ")
			}
			fmt.Fprintf(&b, "%s}\n", indent)
			return
		}
		if s := shapes[r.Intn(len(shapes))]; s != "" {
			fmt.Fprintf(&b, " {shape: %s}", s)
		}
		b.WriteString("\n")
	}
	for i := range parent {
		if parent[i] < 0 {
			write(i, "")
		}
	}
	arrows := []string{"->", "<-", "<->", "--"}
	for e := r.Intn(2*n + 2); e > 0; e-- {
		line := fmt.Sprintf("%s %s %s", path(r.Intn(n)), arrows[r.Intn(len(arrows))], path(r.Intn(n)))
		if r.Intn(2) == 0 {
			line += fmt.Sprintf(": e%d", e)
		}
		for k := 1 + r.Intn(3)/2*r.Intn(3); k > 0; k-- {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

func compileForDagre(t *testing.T, ruler *textmeasure.Ruler, source string) (*d2graph.Graph, bool) {
	t.Helper()
	g, _, err := d2compiler.Compile("index.d2", strings.NewReader(source), nil)
	if err != nil {
		return nil, false
	}
	if err := g.ApplyTheme(d2themescatalog.NeutralDefault.ID); err != nil {
		t.Fatal(err)
	}
	if err := g.SetDimensions(nil, ruler, nil, nil); err != nil {
		return nil, false
	}
	return g, true
}

// dagroCallInputs computes what Layout passes to dagroInput.
func dagroCallInputs(g *d2graph.Graph) (ix *objectIndex, resolvedSrc, resolvedDst []*d2graph.Object, sameWay, bothWays []int) {
	ix = newObjectIndex(g)
	resolver := newEndpointResolver(g)
	resolvedSrc = make([]*d2graph.Object, len(g.Edges))
	resolvedDst = make([]*d2graph.Object, len(g.Edges))
	for i, edge := range g.Edges {
		resolvedSrc[i], resolvedDst[i] = resolver.resolve(edge)
	}
	sameWay, bothWays = countParallelEdges(resolvedSrc, resolvedDst)
	return ix, resolvedSrc, resolvedDst, sameWay, bothWays
}

// originalDagroGraph makes the dagro Graph calls of the original adapter
// (e22f4d566) for g and returns the Graph, or the error of the SetParent call
// that failed, which the original returned before anything else. It also
// returns the error the original returned after the layout, when it read the
// result: a node without a label (the node "" that SetParent or SetEdge
// created for an object outside g.Objects) or fewer edges than g has.
func originalDagroGraph(g *d2graph.Graph, rootAttrs dagreOpts) (*dagro.Graph, string, error) {
	mapper := NewObjectMapper()
	for _, obj := range g.Objects {
		mapper.Register(obj)
	}
	resolver := newEndpointResolver(g)
	type endpointPair struct {
		src, dst *d2graph.Object
	}
	resolved := make([]endpointPair, len(g.Edges))
	counts := make(map[endpointPair]int, len(g.Edges))
	for i, edge := range g.Edges {
		src, dst := resolver.resolve(edge)
		resolved[i] = endpointPair{src: src, dst: dst}
		counts[resolved[i]]++
	}
	graph := dagro.NewGraph(dagro.GraphOptions{Compound: true, Multigraph: true}).SetGraph(dagro.Attrs{
		"ranksep": float64(rootAttrs.ranksep),
		"edgesep": float64(rootAttrs.EdgeSep),
		"nodesep": float64(rootAttrs.NodeSep),
		"rankdir": rootAttrs.rankdir,
	})
	for _, obj := range g.Objects {
		id := mapper.ToID(obj)
		graph.SetNode(id, dagro.Attrs{"id": id, "width": float64(int(obj.Width)), "height": float64(int(obj.Height))})
		if obj.Parent != g.Root {
			if err := graph.SetParent(id, mapper.ToID(obj.Parent)); err != nil {
				return nil, "", err
			}
		}
	}
	for i, edge := range g.Edges {
		pair := resolved[i]
		width, height := edge.LabelDimensions.Width, edge.LabelDimensions.Height
		numEdges := counts[pair]
		if pair.src != pair.dst {
			numEdges += counts[endpointPair{src: pair.dst, dst: pair.src}]
		}
		if numEdges > 1 {
			switch g.Root.Direction.Value {
			case "down", "up", "":
				width += EDGE_LABEL_GAP
			case "left", "right":
				height += EDGE_LABEL_GAP
			}
		}
		graph.SetEdge(mapper.ToID(pair.src), mapper.ToID(pair.dst), dagro.Attrs{
			"width": float64(width), "height": float64(height), "labelpos": "c",
		}, edge.AbsID())
	}
	readErr := ""
	switch {
	case graph.HasNode(""):
		readErr = fmt.Sprintf("dagro returned invalid node label for %q", "")
	case graph.EdgeCount() != len(g.Edges):
		readErr = fmt.Sprintf("dagro returned %d edges, expected %d", graph.EdgeCount(), len(g.Edges))
	}
	return graph, readErr, nil
}

// sameDagroValue compares two attribute values, floats by their bits.
func sameDagroValue(a, b any) bool {
	fa, okA := a.(float64)
	fb, okB := b.(float64)
	if okA || okB {
		return okA && okB && math.Float64bits(fa) == math.Float64bits(fb)
	}
	return a == b
}

// sameDagroGraph fails unless the two graphs lay out alike: the same graph
// attributes, nodes, parents and children (in order), node sizes (a node
// without a label has size 0 by dagro's defaults), and edges between the same
// nodes in the same order with, after dagro's edge defaults, the same
// attribute values. An edge's name only makes its id: the edges' names must
// be equal or empty in got, and the edge counts equal, so that the same calls
// replaced an earlier edge's label.
func sameDagroGraph(t *testing.T, what string, want, got *dagro.Graph) {
	t.Helper()
	wantGraph, _ := want.Graph().(dagro.Attrs)
	gotGraph, _ := got.Graph().(dagro.Attrs)
	if len(wantGraph) != len(gotGraph) {
		t.Fatalf("%s: graph label %v, typed input gives %v", what, wantGraph, gotGraph)
	}
	for k, v := range wantGraph {
		if !sameDagroValue(v, gotGraph[k]) {
			t.Fatalf("%s: graph %s = %v, typed input gives %v", what, k, v, gotGraph[k])
		}
	}
	wantNodes, gotNodes := want.Nodes(), got.Nodes()
	if fmt.Sprint(wantNodes) != fmt.Sprint(gotNodes) {
		t.Fatalf("%s: nodes %v, typed input gives %v", what, wantNodes, gotNodes)
	}
	for _, v := range wantNodes {
		wl, _ := want.Node(v).(dagro.Attrs)
		gl, _ := got.Node(v).(dagro.Attrs)
		if wl == nil {
			wl = dagro.Attrs{"width": 0.0, "height": 0.0}
		}
		if len(gl) != 2 || !sameDagroValue(wl["width"], gl["width"]) || !sameDagroValue(wl["height"], gl["height"]) {
			t.Fatalf("%s: node %q label %v, typed input gives %v", what, v, wl, gl)
		}
		wp, wok := want.Parent(v)
		gp, gok := got.Parent(v)
		if wp != gp || wok != gok {
			t.Fatalf("%s: node %q parent %q %v, typed input gives %q %v", what, v, wp, wok, gp, gok)
		}
		if fmt.Sprint(want.Children(v)) != fmt.Sprint(got.Children(v)) {
			t.Fatalf("%s: node %q children %v, typed input gives %v", what, v, want.Children(v), got.Children(v))
		}
	}
	wantEdges, gotEdges := want.Edges(), got.Edges()
	if len(wantEdges) != len(gotEdges) {
		t.Fatalf("%s: %d edges, typed input gives %d", what, len(wantEdges), len(gotEdges))
	}
	defaults := dagro.Attrs{"minlen": 1.0, "weight": 1.0, "width": 0.0, "height": 0.0, "labeloffset": 10.0, "labelpos": "r"}
	for i, we := range wantEdges {
		ge := gotEdges[i]
		if we.V != ge.V || we.W != ge.W || ge.Name != "" && ge.Name != we.Name {
			t.Fatalf("%s: edge %d is %+v, typed input gives %+v", what, i, we, ge)
		}
		wl, _ := want.Edge(we).(dagro.Attrs)
		gl, _ := got.Edge(ge).(dagro.Attrs)
		for k, d := range defaults {
			wv, ok := wl[k]
			if !ok {
				wv = d
			}
			if !sameDagroValue(wv, gl[k]) {
				t.Fatalf("%s: edge %d %s = %v, typed input gives %v", what, i, k, wv, gl[k])
			}
		}
		for k := range wl {
			if _, ok := defaults[k]; !ok {
				t.Fatalf("%s: edge %d has unexpected attribute %s", what, i, k)
			}
		}
	}
}

// breakGraph changes g as only a Go caller can: it repeats an object in
// g.Objects, leaves one out (its children's parent and the endpoints of its
// edges are then outside g.Objects), repeats an edge, or swaps the parents of
// two objects (a parent cycle; it then drops the edges, whose endpoints'
// resolution would walk the cycle forever, in the original too). It returns
// what it did, or "" when it left g as it was (no edge to repeat).
func breakGraph(r *rand.Rand, g *d2graph.Graph) string {
	switch r.Intn(4) {
	case 0:
		o := g.Objects[r.Intn(len(g.Objects))]
		g.Objects = slices.Insert(g.Objects, r.Intn(len(g.Objects)+1), o)
		return "repeated object"
	case 1:
		i := r.Intn(len(g.Objects))
		g.Objects = slices.Delete(g.Objects, i, i+1)
		return "object outside g.Objects"
	case 2:
		if len(g.Edges) == 0 {
			return ""
		}
		g.Edges = append(g.Edges, g.Edges[r.Intn(len(g.Edges))])
		return "repeated edge"
	default:
		a, b := g.Objects[r.Intn(len(g.Objects))], g.Objects[r.Intn(len(g.Objects))]
		a.Parent, b.Parent = b, a
		g.Edges = nil
		return "parent cycle"
	}
}

// TestDagroInputMatchesGraphCalls checks, on random diagrams and on random
// diagrams broken as only Go callers can break them, that the typed input
// describes the Graph the original adapter built, and that dagroInput
// returns the errors the original returned besides the layout's own:
// dagro.LayoutTyped(in) is defined as Layout(in.Graph()), so equal graphs
// give equal layouts.
func TestDagroInputMatchesGraphCalls(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(7))
	checked, named := 0, 0
	broken := map[string]int{}
	for c := 0; c < 800; c++ {
		source := randomDagreSource(r)
		g, ok := compileForDagre(t, ruler, source)
		if !ok {
			continue
		}
		what := fmt.Sprintf("case %d:\n%s\n", c, source)
		if c%2 == 1 {
			if how := breakGraph(r, g); how != "" {
				broken[how]++
				what += how + "\n"
			}
		}
		rootAttrs := dagreOpts{
			ranksep:          100 + r.Intn(60),
			rankdir:          []string{"TB", "LR", "RL", "BT"}[r.Intn(4)],
			ConfigurableOpts: ConfigurableOpts{NodeSep: 60, EdgeSep: 20},
		}
		ix, resolvedSrc, resolvedDst, sameWay, bothWays := dagroCallInputs(g)
		in, buildErr, readErr := dagroInput(g, ix, rootAttrs, resolvedSrc, resolvedDst, sameWay, bothWays)
		want, wantReadErr, wantBuildErr := originalDagroGraph(g, rootAttrs)
		if fmt.Sprint(buildErr) != fmt.Sprint(wantBuildErr) {
			t.Fatalf("%s: error %v, original's %v", what, buildErr, wantBuildErr)
		}
		if wantBuildErr != nil {
			continue
		}
		if readErr != nil && readErr.Error() != wantReadErr || readErr == nil && wantReadErr != "" {
			t.Fatalf("%s: result error %v, original's %q", what, readErr, wantReadErr)
		}
		got, err := in.Graph()
		if err != nil {
			t.Fatal(err)
		}
		sameDagroGraph(t, what, want, got)
		checked++
		for _, n := range sameWay {
			if n > 1 {
				named++
			}
		}
	}
	t.Logf("%d graphs compared, %d named edges; broken: %v", checked, named, broken)
	if checked < 600 || named == 0 || len(broken) < 4 {
		t.Fatalf("only %d graphs compiled (%d named edges, broken: %v)", checked, named, broken)
	}
}

// TestLayoutIrregularGraphs checks that Layout lays out a graph with a
// repeated object and returns the original's errors for graphs with an edge
// endpoint outside g.Objects, a parent cycle or a repeated edge, which only a
// Go caller can make.
func TestLayoutIrregularGraphs(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	compileSource := func(source string) *d2graph.Graph {
		g, ok := compileForDagre(t, ruler, source)
		if !ok {
			t.Fatal("does not compile")
		}
		return g
	}
	compile := func() *d2graph.Graph { return compileSource("a: {b; c}\nd\na.b -> d\nd -> a.c\nd -> a.c") }
	layout := func(what string, g *d2graph.Graph, want string) {
		t.Helper()
		err := DefaultLayout(context.Background(), g)
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%s: Layout error %v, want %q", what, err, want)
		}
	}

	g := compile()
	g.Objects = append(g.Objects, g.Objects[0])
	layout("repeated object", g, "")
	for _, obj := range g.Objects {
		if obj.TopLeft == nil {
			t.Errorf("repeated object: %s has no position", obj.ID)
		}
	}

	g = compileSource("a -> b")
	g.Objects = slices.DeleteFunc(g.Objects, func(obj *d2graph.Object) bool { return obj.ID == "b" })
	layout("edge endpoint outside g.Objects", g, `dagro returned invalid node label for ""`)

	g = compile()
	g.Objects[0].Parent, g.Objects[1].Parent = g.Objects[1], g.Objects[0]
	layout("parent cycle", g, "would create a cycle")

	g = compile()
	g.Edges = append(g.Edges, g.Edges[len(g.Edges)-1])
	layout("repeated edge", g, "dagro returned 3 edges, expected 4")
}
