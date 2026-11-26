package d2dagrelayout

import (
	"context"
	"errors"
	"fmt"
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

// dagroCallInput returns the dagro.LayoutTyped input Layout builds for g.
func dagroCallInput(g *d2graph.Graph) *dagro.LayoutInput {
	ix := newObjectIndex(g)
	resolver := newEndpointResolver(g)
	resolvedSrc := make([]*d2graph.Object, len(g.Edges))
	resolvedDst := make([]*d2graph.Object, len(g.Edges))
	for i, edge := range g.Edges {
		resolvedSrc[i], resolvedDst[i] = resolver.resolve(edge)
	}
	sameWay, bothWays := countParallelEdges(resolvedSrc, resolvedDst)
	rootAttrs := dagreOpts{ranksep: 100, rankdir: "TB", ConfigurableOpts: ConfigurableOpts{NodeSep: 60, EdgeSep: 20}}
	in := dagroInput(g, ix, rootAttrs, resolvedSrc, resolvedDst, sameWay, bothWays)
	return &in
}

// TestRandomDiagramsLayOut checks, on random diagrams, that dagro lays out
// every input the adapter builds from a compiled diagram (dagre itself may
// still fail on a diagram, which dagro reports as a *dagro.PanicError), and
// that it rejects the inputs of diagrams broken as only Go callers can break
// them (see breakGraph) with an error wrapping dagro.ErrUnsupportedInput.
func TestRandomDiagramsLayOut(t *testing.T) {
	t.Parallel()
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(7))
	laidOut, dagreFailed := 0, 0
	broken := map[string]int{}
	for c := 0; c < 800; c++ {
		source := randomDagreSource(r)
		g, ok := compileForDagre(t, ruler, source)
		if !ok {
			continue
		}
		what := fmt.Sprintf("case %d:\n%s\n", c, source)
		how := ""
		if c%2 == 1 {
			how = breakGraph(r, g)
		}
		// A broken graph goes to dagro directly: D2's code after the layout
		// assumes an intact graph.
		var err error
		if how != "" {
			err = dagro.LayoutTyped(dagroCallInput(g), &dagro.LayoutOutput{})
		} else {
			err = DefaultLayout(context.Background(), g)
		}
		var perr *dagro.PanicError
		switch {
		case how != "":
			broken[how]++
			// An object left out of g.Objects breaks the input only when
			// it has children or edges.
			if err == nil && how == "object outside g.Objects" {
				break
			}
			if !errors.Is(err, dagro.ErrUnsupportedInput) {
				t.Fatalf("%s%s: error %v, want one wrapping dagro.ErrUnsupportedInput", what, how, err)
			}
		case err == nil:
			laidOut++
		case errors.As(err, &perr):
			dagreFailed++
		default:
			t.Fatalf("%s: %v", what, err)
		}
	}
	t.Logf("%d diagrams laid out, %d on which dagre fails; broken: %v", laidOut, dagreFailed, broken)
	if laidOut < 300 || len(broken) < 4 {
		t.Fatalf("only %d diagrams laid out (broken: %v)", laidOut, broken)
	}
}

// TestLayoutIrregularGraphs checks the errors Layout returns for graphs that
// only a Go caller can make: a repeated object, an edge endpoint outside
// g.Objects, a parent cycle and a repeated edge. dagro rejects their inputs
// before laying anything out. (Before dagro v0.4.0, its Graph engine laid out
// the first graph, and Layout returned "dagro returned invalid node label for
// """ and "dagro returned 3 edges, expected 4" for the second and the last.)
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
		if !errors.Is(err, dagro.ErrUnsupportedInput) || err.Error() != "failed to dagre layout: dagro: unsupported input: "+want {
			t.Errorf("%s: Layout error %v, want %q", what, err, want)
		}
		for _, obj := range g.Objects {
			if obj.TopLeft != nil {
				t.Errorf("%s: %s has a position", what, obj.ID)
			}
		}
	}

	g := compile()
	g.Objects = append(g.Objects, g.Objects[0])
	layout("repeated object", g, `nodes 0 and 4 have the same id "4"`)

	g = compileSource("a -> b")
	g.Objects = slices.DeleteFunc(g.Objects, func(obj *d2graph.Object) bool { return obj.ID == "b" })
	layout("edge endpoint outside g.Objects", g, `node 1 has the id "" (empty or containing "\x01")`)

	g = compile()
	g.Objects[0].Parent, g.Objects[1].Parent = g.Objects[1], g.Objects[0]
	layout("parent cycle", g, "making node 0 the parent of node 1 would create a cycle")

	g = compile()
	g.Edges = append(g.Edges, g.Edges[len(g.Edges)-1])
	layout("repeated edge", g, "edge 3 repeats the endpoints and name of an earlier edge")
}
