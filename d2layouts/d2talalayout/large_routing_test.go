package d2talalayout_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestLargeContainerRouting(t *testing.T) {
	checkLargeContainerRouting(t, 100, 160, false)
}

func checkLargeContainerRouting(t *testing.T, links, shapes int, nested bool) {
	t.Helper()
	source := largeContainerDiagram(links, nested)
	layout := d2talalayout.DefaultLayout
	var renderOpts *d2svg.RenderOpts
	if nested {
		// One seed exercises the larger routing and balancing workload.
		layout = func(ctx context.Context, g *d2graph.Graph) error {
			return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{1}})
		}
		renderOpts = &d2svg.RenderOpts{Sketch: new(true), DarkThemeID: new(int64(104))}
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	telemetry := new(routing.SearchTelemetry)
	ctx := routing.WithSearchTelemetry(log.With(t.Context(), testlog.New(t)), telemetry)
	diagram, _, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         new("tala"),
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return layout, nil },
		RouterResolver: func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil },
	}, renderOpts)
	if err != nil {
		t.Fatal(err)
	}
	if len(diagram.Shapes) != shapes || len(diagram.Connections) != links {
		t.Fatalf("got %d shapes and %d connections", len(diagram.Shapes), len(diagram.Connections))
	}
	for _, connection := range diagram.Connections {
		if len(connection.Route) < 2 {
			t.Fatalf("%s has an incomplete route", connection.ID)
		}
		// The public reproducer uses rectangular shapes and orthogonal routes.
		// Sketch layouts can also select direct connections between shapes.
		if nested {
			continue
		}
		for i := 1; i < len(connection.Route); i++ {
			from, to := connection.Route[i-1], connection.Route[i]
			if from.X != to.X && from.Y != to.Y {
				t.Fatalf("%s has a diagonal segment from %v to %v", connection.ID, from, to)
			}
		}
	}
	samples := telemetry.WorkSamples()
	if len(samples) == 0 {
		t.Fatal("no route search recorded")
	}
	peak := slices.Max(samples)
	if peak >= routing.MaxSearchWorkUnits {
		t.Fatalf("route search exhausted its %d-unit budget", routing.MaxSearchWorkUnits)
	}
	t.Logf("peak route-search work: %d of %d units", peak, routing.MaxSearchWorkUnits)
}

// Reproduce the pair ordering and stride in #2891. The nested variant groups
// every five containers inside another container.
func largeContainerDiagram(links int, nested bool) string {
	const containers, children = 40, 3
	var source strings.Builder
	var names []string
	for c := range containers {
		prefix := ""
		if nested {
			if c%5 == 0 {
				fmt.Fprintf(&source, "domain%d: Domain %d {\n", c/5, c/5)
			}
			prefix = fmt.Sprintf("domain%d.", c/5)
		}
		fmt.Fprintf(&source, "c%d: Container %d {\n", c, c)
		for n := range children {
			fmt.Fprintf(&source, "  n%d: Node %d\n", n, n)
			names = append(names, fmt.Sprintf("%sc%d.n%d", prefix, c, n))
		}
		source.WriteString("}\n")
		if nested && c%5 == 4 {
			source.WriteString("}\n")
		}
	}
	pairCount := containers * (containers - 1) / 2 * children * children
	stride := pairCount / links
	pair, written := 0, 0
	for i, from := range names {
		for j := i + 1; j < len(names); j++ {
			if i/children == j/children {
				continue
			}
			if pair%stride == 0 && written < links {
				fmt.Fprintf(&source, "%s -> %s\n", from, names[j])
				written++
			}
			pair++
		}
	}
	return source.String()
}
