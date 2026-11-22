package d2elklayout

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/lib/textmeasure"
)

// TestRootOptionsKeyFollowsOptions lays out diagrams repeatedly (so that the
// root's option set is found by its rootKey) while the options change between
// runs: the graph options, DefaultOpts and the values its pointers point to,
// the direction and the self-loop spacing. Every typed layout must equal the
// JSON path's.
func TestRootOptionsKeyFollowsOptions(t *testing.T) {
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	saved, savedEdgeEdge := DefaultOpts, *DefaultOpts.EdgeEdgeSpacing
	defer func() {
		DefaultOpts, *DefaultOpts.EdgeEdgeSpacing = saved, savedEdgeEdge
		setJSONOracle(false)
	}()
	inputs := []string{
		"a -> b",
		"direction: right\na -> b -> c",
		"direction: up\na -> a: a long self-loop label to widen the spacing\na -> b",
		"direction: left\nx: {y -> z}\nx.y -> w",
	}
	mutations := []func(){
		func() {},
		func() { DefaultOpts.NodeSpacing = 33 },
		func() { *DefaultOpts.EdgeEdgeSpacing = 7 },
		func() { DefaultOpts.SelfLoopSpacing = 90 },
		func() { DefaultOpts, *DefaultOpts.EdgeEdgeSpacing = saved, savedEdgeEdge },
	}
	variants := append([]*ConfigurableOpts{nil}, func() (out []*ConfigurableOpts) {
		for _, v := range elkGoOptionVariants() {
			v := v
			out = append(out, &v)
		}
		return out
	}()...)
	// Both paths must end the same way: the same graph, error or panic.
	run := func(input string, opts *ConfigurableOpts, viaJSON bool) (outcome string) {
		setJSONOracle(viaJSON)
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
	laidOut, failed := 0, 0
	for mi, mutate := range mutations {
		mutate()
		for _, opts := range variants {
			if opts != nil && opts.Algorithm == "mrtree" {
				continue
			}
			for _, input := range inputs {
				want := run(input, opts, true)
				if strings.HasPrefix(want, "panic: ") || strings.HasPrefix(want, "error: ") {
					failed++
				} else {
					laidOut++
				}
				// Twice in a row: the second finds the root's set by rootKey.
				for repeat := 0; repeat < 2; repeat++ {
					if got := run(input, opts, false); got != want {
						t.Fatalf("mutation %d, options %+v, %q (run %d): typed layout differs from JSON", mi, opts, input, repeat)
					}
				}
			}
		}
	}
	if laidOut < 3*failed || laidOut < 50 {
		t.Fatalf("%d layouts, %d failures: too few layouts compared", laidOut, failed)
	}
}
