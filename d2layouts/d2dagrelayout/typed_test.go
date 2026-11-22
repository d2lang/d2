package d2dagrelayout

import (
	"context"
	"fmt"
	"math"
	"math/rand"
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

// dagroCallInputs computes what Layout passes to dagroInput and
// buildDagroGraph.
func dagroCallInputs(g *d2graph.Graph, rootAttrs dagreOpts) (ix *objectIndex, resolvedSrc, resolvedDst []*d2graph.Object, sameWay, bothWays []int) {
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

// sameDagroValue compares two attribute values, floats by their bits.
func sameDagroValue(a, b any) bool {
	fa, okA := a.(float64)
	fb, okB := b.(float64)
	if okA || okB {
		return okA && okB && math.Float64bits(fa) == math.Float64bits(fb)
	}
	return a == b
}

// sameDagroGraph fails unless the two graphs have the same graph attributes,
// nodes, node labels, parents and children (in order), and edges with the
// same keys and, after dagro's edge defaults, the same attribute values.
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
		if len(wl) != 2 || len(gl) != 2 || !sameDagroValue(wl["width"], gl["width"]) || !sameDagroValue(wl["height"], gl["height"]) {
			t.Fatalf("%s: node %s label %v, typed input gives %v", what, v, wl, gl)
		}
		wp, wok := want.Parent(v)
		gp, gok := got.Parent(v)
		if wp != gp || wok != gok {
			t.Fatalf("%s: node %s parent %q %v, typed input gives %q %v", what, v, wp, wok, gp, gok)
		}
		if fmt.Sprint(want.Children(v)) != fmt.Sprint(got.Children(v)) {
			t.Fatalf("%s: node %s children %v, typed input gives %v", what, v, want.Children(v), got.Children(v))
		}
	}
	wantEdges, gotEdges := want.Edges(), got.Edges()
	if len(wantEdges) != len(gotEdges) {
		t.Fatalf("%s: %d edges, typed input gives %d", what, len(wantEdges), len(gotEdges))
	}
	defaults := dagro.Attrs{"minlen": 1.0, "weight": 1.0, "width": 0.0, "height": 0.0, "labeloffset": 10.0, "labelpos": "r"}
	for i, we := range wantEdges {
		ge := gotEdges[i]
		if we != ge {
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

// TestDagroInputMatchesGraphCalls checks that the typed input is the Graph
// the Attrs path builds, on random diagrams: dagro.LayoutTyped(in) is defined
// as Layout(in.Graph()), so equal graphs give equal layouts.
func TestDagroInputMatchesGraphCalls(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(7))
	checked, named := 0, 0
	for c := 0; c < 400; c++ {
		source := randomDagreSource(r)
		g, ok := compileForDagre(t, ruler, source)
		if !ok {
			continue
		}
		rootAttrs := dagreOpts{
			ranksep:          100 + r.Intn(60),
			rankdir:          []string{"TB", "LR", "RL", "BT"}[r.Intn(4)],
			ConfigurableOpts: ConfigurableOpts{NodeSep: 60, EdgeSep: 20},
		}
		ix, resolvedSrc, resolvedDst, sameWay, bothWays := dagroCallInputs(g, rootAttrs)
		in, ok := dagroInput(g, ix, rootAttrs, resolvedSrc, resolvedDst, sameWay, bothWays)
		if !ok {
			t.Fatalf("case %d: dagroInput refused a compiled graph:\n%s", c, source)
		}
		want, _, _, err := buildDagroGraph(g, ix, rootAttrs, resolvedSrc, resolvedDst, sameWay, bothWays)
		if err != nil {
			t.Fatal(err)
		}
		got, err := in.Graph()
		if err != nil {
			t.Fatal(err)
		}
		sameDagroGraph(t, fmt.Sprintf("case %d:\n%s\n", c, source), want, got)
		checked++
		for _, n := range sameWay {
			if n > 1 {
				named++
			}
		}
	}
	t.Logf("%d graphs compared, %d named edges", checked, named)
	if checked < 300 || named == 0 {
		t.Fatalf("only %d graphs compiled (%d named edges)", checked, named)
	}
}

// TestDagroInputRefusesIrregularGraphs checks the graphs that keep the Attrs
// path, and that Layout still reports their errors.
func TestDagroInputRefusesIrregularGraphs(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	compile := func() *d2graph.Graph {
		g, ok := compileForDagre(t, ruler, "a: {b; c}\nd\na.b -> d\nd -> a.c\nd -> a.c")
		if !ok {
			t.Fatal("does not compile")
		}
		return g
	}
	refused := func(what string, g *d2graph.Graph) {
		t.Helper()
		ix, resolvedSrc, resolvedDst, sameWay, bothWays := dagroCallInputs(g, dagreOpts{rankdir: "TB"})
		if _, ok := dagroInput(g, ix, dagreOpts{rankdir: "TB"}, resolvedSrc, resolvedDst, sameWay, bothWays); ok {
			t.Errorf("%s: dagroInput took the graph", what)
		}
	}

	if g := compile(); true {
		ix, resolvedSrc, resolvedDst, sameWay, bothWays := dagroCallInputs(g, dagreOpts{rankdir: "TB"})
		if _, ok := dagroInput(g, ix, dagreOpts{rankdir: "TB"}, resolvedSrc, resolvedDst, sameWay, bothWays); !ok {
			t.Fatal("regular graph refused")
		}
	}

	g := compile()
	g.Objects = append(g.Objects, g.Objects[0])
	refused("repeated object", g)

	g = compile()
	var kept []*d2graph.Object
	for _, obj := range g.Objects {
		if obj.ID != "a" {
			kept = append(kept, obj)
		}
	}
	g.Objects = kept
	refused("parent outside g.Objects", g)

	g = compile()
	g.Objects[0].Parent, g.Objects[1].Parent = g.Objects[1], g.Objects[0]
	refused("parent cycle", g)

	g = compile()
	g.Edges = append(g.Edges, g.Edges[len(g.Edges)-1])
	refused("duplicate edge key", g)
	err = DefaultLayout(context.Background(), g)
	if err == nil || !strings.Contains(err.Error(), "dagro returned 3 edges, expected 4") {
		t.Errorf("duplicate edge key: Layout error %v", err)
	}
}
