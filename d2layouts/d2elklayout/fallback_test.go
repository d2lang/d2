package d2elklayout

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2ast"
	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/textmeasure"
)

// layoutOutcome lays input out with opts, through the typed path or the
// original's JSON round trip (runELKViaJSON), and returns the serialized
// graph, the error or the panic.
func layoutOutcome(t *testing.T, ruler *textmeasure.Ruler, input string, opts *ConfigurableOpts, viaJSON bool) (outcome string) {
	t.Helper()
	saved := jsonOracle
	setJSONOracle(viaJSON)
	defer func() { jsonOracle = saved }()
	g := compileELKGoDiagram(t, ruler, input)
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome = fmt.Sprint("panic: ", recovered)
		}
	}()
	if err := Layout(context.Background(), g, opts); err != nil {
		return "error: " + err.Error()
	}
	serialized, err := d2graph.SerializeGraph(g)
	if err != nil {
		t.Fatal(err)
	}
	return string(serialized)
}

// TestEdgeEdgePointee changes the value that
// ConfigurableOpts.EdgeEdgeSpacing points to between two layouts with the same
// options struct. The original sent the new value; the typed path must too.
func TestEdgeEdgePointee(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	input := "a -> c\na -> d\nb -> c\nb -> d\na -> e\nb -> e\nc -> f\nd -> f\ne -> f\na -> f\nb -> f"
	spacing := 5
	opts := DefaultOpts
	opts.EdgeEdgeSpacing = &spacing

	small := layoutOutcome(t, ruler, input, &opts, true)
	spacing = 400
	large := layoutOutcome(t, ruler, input, &opts, true)
	if small == large {
		t.Fatalf("the spacing does not change this layout; pick another diagram")
	}

	// Typed: lay out with 5 (the root's option set is cached), then with 400.
	spacing = 5
	if got := layoutOutcome(t, ruler, input, &opts, false); got != small {
		t.Fatalf("typed layout with spacing 5 differs from JSON")
	}
	spacing = 400
	got := layoutOutcome(t, ruler, input, &opts, false)
	if got != large {
		if got == small {
			t.Fatalf("typed layout after *EdgeEdgeSpacing changed 5 -> 400 still uses 5 (stale root option set)")
		}
		t.Fatalf("typed layout with spacing 400 differs from JSON")
	}
}

// raceDetector is set when the race detector is on (race_test.go).
var raceDetector bool

// TestFallbackCoverage lays out, through the typed path and through the
// original's JSON round trip (runELKViaJSON), the requests the typed path
// once left to JSON, and expects the same graph, error or panic from both:
// strings that are not valid UTF-8 (JSON replaces their bytes), non-finite
// numbers (json.Marshal rejects them), requests elk-go rejects, and deep
// nesting. The d2 compiler replaces invalid UTF-8 in its input, so invalid
// strings are put into compiled graphs directly here.
func TestFallbackCoverage(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	deep := func(depth int) string {
		path := "n"
		for i := 1; i < depth; i++ {
			path += ".n"
		}
		return path + "\nx -> " + path + "\n"
	}
	sqlColumns := func(names ...string) func(g *d2graph.Graph) {
		return func(g *d2graph.Graph) {
			for i, name := range names {
				objectByID(t, g, "t").SQLTable.Columns[i].Name.Label = name
			}
		}
	}
	// nestBelow nests depth objects below the object id, each the only child
	// of the one before, and leaves them out of g.Objects: the graph guard
	// counts g.Objects, the layout walks ChildrenArray.
	nestBelow := func(id string, depth int) func(g *d2graph.Graph) {
		return func(g *d2graph.Graph) {
			objects := len(g.Objects)
			parent := objectByID(t, g, id)
			for i := 0; i < depth; i++ {
				parent = parent.EnsureChild([]d2ast.String{d2ast.FlatUnquotedString("n")})
				parent.Box = geo.NewBox(nil, 40, 30)
			}
			g.Objects = g.Objects[:objects]
		}
	}
	type testCase struct {
		name, input string
		mutate      func(g *d2graph.Graph)
		// panics marks the cases that end in a panic (on both paths), which
		// TinyGo's wasm target cannot recover from: they run under gc only.
		panics bool
	}
	// The deep cases compare what Layout wrote (layoutDigest) instead of
	// the serialized graph, whose size grows with the square of the nesting
	// depth (70 MB, and 1.4 GB of heap to make it, at depth 600).
	digest := map[string]bool{}
	cases := []testCase{
		{"plain", "a -> b", nil, false},
		{"compiled invalid UTF-8 label", "a: \"bad \xff label\"\na -> b", nil, false},
		{"invalid UTF-8 label", "a -> b", func(g *d2graph.Graph) { g.Objects[0].Label.Value = "bad \xff" }, false},
		{"invalid UTF-8 id", "a -> b", func(g *d2graph.Graph) { g.Objects[1].ID = "b\xfe" }, false},
		{"invalid UTF-8 ids colliding", "a -> b", func(g *d2graph.Graph) { g.Objects[0].ID, g.Objects[1].ID = "x\xfe", "x\xff" }, false},
		// The original found no object with the container's ID JSON gave
		// back and dereferenced nil.
		{"invalid UTF-8 container id", "a.b -> a.c", func(g *d2graph.Graph) { objectByID(t, g, "a").ID = "a\xfe" }, true},
		{"invalid UTF-8 edge label", "a -> b: e", func(g *d2graph.Graph) { g.Edges[0].Label.Value = "e\xc3" }, false},
		// Layout upper-cases the constraint, which already writes the
		// invalid byte as U+FFFD.
		{"invalid UTF-8 layer constraint", "a -> b", func(g *d2graph.Graph) { g.Objects[0].ELKLayerConstraint = "fi\xffrst" }, false},
		{"invalid UTF-8 SQL column", "t: {shape: sql_table; a: int; b: int}\nt.a -> x", sqlColumns("a\xfe"), false},
		{"invalid UTF-8 SQL columns colliding", "t: {shape: sql_table; a: int; b: int}\nt.a -> x", sqlColumns("c\xfe", "c\xff"), false},
		{"NaN width", "a -> b", func(g *d2graph.Graph) { g.Objects[0].Width = math.NaN() }, false},
		{"infinite height", "a -> b", func(g *d2graph.Graph) { g.Objects[1].Height = math.Inf(1) }, false},
		{"negative zero width", "a -> b", func(g *d2graph.Graph) { g.Objects[1].Width = math.Copysign(0, -1) }, false},
		{"duplicate ids", "a -> b", func(g *d2graph.Graph) { g.Objects[1].ID = g.Objects[0].ID }, false},
		{"duplicate container ids", "a.x -> b.y", func(g *d2graph.Graph) { objectByID(t, g, "b").ID = "a" }, false},
		// json.Marshal's error comes first, as in the original.
		{"NaN and duplicate ids", "a -> b", func(g *d2graph.Graph) {
			g.Objects[1].ID = g.Objects[0].ID
			g.Objects[1].Height = math.NaN()
		}, false},
		// An edge end outside the walk (not in a ChildrenArray) is no node.
		{"edge end not laid out", "a -> b", func(g *d2graph.Graph) { g.Root.ChildrenArray = g.Root.ChildrenArray[1:] }, false},
	}
	// Nesting json.Marshal rejects (encoding/json's depth limit) fails with
	// its error on both paths. The graph guard stops compiled diagrams this
	// deep, but a caller can nest objects through ChildrenArray. TinyGo's
	// wasm stack is too small for it.
	failing := map[string]bool{}
	if runtime.Compiler != "tinygo" {
		name := "nesting json.Marshal rejects"
		cases = append(cases, testCase{name, "a -> b", nestBelow("b", 6000), false})
		failing[name] = true
	}
	// The two deep diagrams take almost all of the test's time: the JSON
	// path lays a 1000-level nesting out the original's way (about 3 s each
	// time, three times; 13 minutes for the test with -race) and minutes
	// under TinyGo's wasm target, which d2's ci/test-wasm.sh runs this
	// package's tests on. They run with D2_SLOW=1, never under TinyGo or
	// -short, and not with -race: there one such layout takes 97 s and
	// 3.8 GB (the original's too), and the race detector's memory for the
	// three of them in one process exceeds 6 GB.
	slow := os.Getenv("D2_SLOW") == "1" && !testing.Short() && runtime.Compiler != "tinygo"
	if slow && raceDetector {
		t.Log("D2_SLOW: the deep cases do not run with -race (memory); run them without -race")
		slow = false
	}
	if slow {
		// Beyond jsonSafeDepth the typed path asks json.Marshal, which
		// succeeds at these depths.
		for _, depth := range []int{jsonSafeDepth, jsonSafeDepth + 1, 1023} {
			name := fmt.Sprint("depth ", depth)
			cases = append(cases, testCase{name, deep(depth), nil, false})
			digest[name] = true
		}
	}
	run := func(c testCase, viaJSON bool) (outcome string) {
		saved := jsonOracle
		setJSONOracle(viaJSON)
		defer func() { jsonOracle = saved }()
		g := compileELKGoDiagram(t, ruler, c.input)
		if c.mutate != nil {
			c.mutate(g)
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome = fmt.Sprint("panic: ", recovered)
			}
		}()
		if err := Layout(context.Background(), g, nil); err != nil {
			return "error: " + err.Error()
		}
		if digest[c.name] {
			return layoutDigest(g)
		}
		serialized, err := d2graph.SerializeGraph(g)
		if err != nil {
			return "serialize error: " + err.Error()
		}
		return string(serialized)
	}
	// Each deep layout through JSON leaves gigabytes of garbage: with
	// D2_SLOW, it is returned to the system after every layout, so that the
	// test needs the memory of one layout, not of several.
	freeAfter := func() {
		if slow {
			debug.FreeOSMemory()
		}
	}
	for _, c := range cases {
		if c.panics && runtime.Compiler == "tinygo" {
			continue
		}
		got := run(c, false)
		freeAfter()
		want := run(c, true)
		freeAfter()
		if got != want {
			t.Errorf("%s: result differs from the JSON path:\n%.200s\n%.200s", c.name, got, want)
		}
		if strings.HasPrefix(got, "panic: ") != c.panics {
			t.Errorf("%s: panics %v, want %v (%.80s)", c.name, !c.panics, c.panics, got)
		}
		if failing[c.name] && !strings.HasPrefix(got, "error: ") {
			t.Errorf("%s: %.80s, want an error", c.name, got)
		}
		if !strings.HasPrefix(got, "panic: ") && !strings.HasPrefix(got, "error: ") {
			got = "laid out"
		}
		t.Logf("%s: %s", c.name, got)
	}
}

// layoutDigest is a SHA-256 of what Layout writes into g: every object's
// position, size, label and icon position, and every edge's route (bits of
// every coordinate), label position and label percentage, in g's order.
func layoutDigest(g *d2graph.Graph) string {
	h := sha256.New()
	str := func(s *string) string {
		if s == nil {
			return "<nil>"
		}
		return *s
	}
	num := func(f *float64) string {
		if f == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%x", math.Float64bits(*f))
	}
	for _, o := range g.Objects {
		var x, y uint64
		if o.TopLeft != nil {
			x, y = math.Float64bits(o.TopLeft.X), math.Float64bits(o.TopLeft.Y)
		}
		fmt.Fprintf(h, "%q %x %x %x %x %q %q;", o.AbsID(), x, y, math.Float64bits(o.Width), math.Float64bits(o.Height), str(o.LabelPosition), str(o.IconPosition))
	}
	for _, e := range g.Edges {
		fmt.Fprintf(h, "%q", e.AbsID())
		for _, p := range e.Route {
			fmt.Fprintf(h, " %x %x", math.Float64bits(p.X), math.Float64bits(p.Y))
		}
		fmt.Fprintf(h, " %q %s;", str(e.LabelPosition), num(e.LabelPercentage))
	}
	return fmt.Sprintf("layout digest %x", h.Sum(nil))
}

// TestContainerPaddingWithoutChildrenMap lays out a graph whose
// container lists its children in ChildrenArray while its Children map is
// empty (IsContainer is false). The original sent the container options'
// padding (the graph options' Padding) for it; it must still be sent.
func TestContainerPaddingWithoutChildrenMap(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { beforeELKHook = nil }()
	g := compileELKGoDiagram(t, ruler, "a.b -> c")
	for _, obj := range g.Objects {
		if len(obj.ChildrenArray) > 0 {
			for key := range obj.Children {
				delete(obj.Children, key)
			}
		}
	}
	padding := "unset"
	beforeELKHook = func(elkGraph *ELKGraph) {
		for _, n := range elkGraph.Children {
			if n.ID == "a" {
				padding = n.LayoutOptions.Padding
			}
		}
	}
	if err := Layout(context.Background(), g, nil); err != nil {
		t.Fatal(err)
	}
	if padding != DefaultOpts.Padding {
		t.Fatalf("container padding option %q, want %q (newContainerLayoutOptions' value, as the original sent)", padding, DefaultOpts.Padding)
	}
}
