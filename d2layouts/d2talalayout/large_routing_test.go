package d2talalayout_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/routing"
	"github.com/d2lang/d2/d2lib"
	"github.com/d2lang/d2/d2renderers/d2svg"
	"github.com/d2lang/d2/d2target"
	"github.com/d2lang/d2/internal/testlog"
	"github.com/d2lang/d2/lib/geo"
	"github.com/d2lang/d2/lib/log"
	"github.com/d2lang/d2/lib/textmeasure"
)

func TestLargeContainerRouting(t *testing.T) {
	for _, seed := range d2talalayout.DefaultOptions().Seeds {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) { checkLargeContainerRouting(t, 100, 160, false, seed) })
	}
}

func checkLargeContainerRouting(t *testing.T, links, shapes int, nested bool, seed int64) {
	t.Helper()
	source := largeContainerDiagram(links, nested)
	var renderOpts *d2svg.RenderOpts
	if nested {
		renderOpts = &d2svg.RenderOpts{Sketch: new(true), DarkThemeID: new(int64(104))}
	}
	telemetry := new(routing.SearchTelemetry)
	ctx := routing.WithSearchTelemetry(log.With(t.Context(), testlog.New(t)), telemetry)
	diagram := compileRoutingDiagram(t, ctx, source, seed, renderOpts)
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
			for _, shape := range diagram.Shapes {
				if !strings.Contains(shape.ID, ".") || shape.ID == connection.Src || shape.ID == connection.Dst {
					continue
				}
				left, top := float64(shape.Pos.X), float64(shape.Pos.Y)
				right, bottom := left+float64(shape.Width), top+float64(shape.Height)
				horizontalHit := from.Y == to.Y && top < from.Y && from.Y < bottom && max(left, min(from.X, to.X)) < min(right, max(from.X, to.X))
				verticalHit := from.X == to.X && left < from.X && from.X < right && max(top, min(from.Y, to.Y)) < min(bottom, max(from.Y, to.Y))
				if horizontalHit || verticalHit {
					t.Fatalf("%s crosses the interior of %s between %v and %v", connection.ID, shape.ID, from, to)
				}
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

func compileRoutingDiagram(t *testing.T, ctx context.Context, source string, seed int64, renderOpts *d2svg.RenderOpts) *d2target.Diagram {
	t.Helper()
	layout := func(ctx context.Context, g *d2graph.Graph) error {
		return d2talalayout.Layout(ctx, g, &d2talalayout.Options{Seeds: []int64{seed}})
	}
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		t.Fatal(err)
	}
	diagram, _, err := d2lib.Compile(ctx, source, &d2lib.CompileOptions{
		Ruler:          ruler,
		Layout:         new("tala"),
		LayoutResolver: func(string) (d2graph.LayoutGraph, error) { return layout, nil },
		RouterResolver: func(string) (d2graph.RouteEdges, error) { return d2talalayout.RouteEdges, nil },
	}, renderOpts)
	if err != nil {
		t.Fatal(err)
	}
	return diagram
}

func TestCompactCompoundRoutes(t *testing.T) {
	// These four routes acquired detours or extra crossings when the router
	// discarded cross-container hierarchy lanes on small diagrams.
	for _, tc := range []struct {
		name string
		want map[string]geo.Points
	}{
		{"shared-source", map[string]geo.Points{
			"g2.n0 -> g4.n1": {{X: 184, Y: 645}, {X: 298, Y: 645}, {X: 298, Y: 1164}},
			"g2.n0 -> g4.n2": {{X: 122, Y: 678}, {X: 122, Y: 1164}},
		}},
		{"shared-target", map[string]geo.Points{
			"g0.n1 -> g1.n2": {{X: 902, Y: 126}, {X: 902, Y: 231}, {X: 422, Y: 231}, {X: 422, Y: 336}},
			"g0.n0 -> g1.n2": {{X: 705, Y: 93}, {X: 422, Y: 93}, {X: 422, Y: 336}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile("testdata/" + tc.name + ".d2")
			if err != nil {
				t.Fatal(err)
			}
			ctx := log.With(t.Context(), testlog.New(t))
			diagram := compileRoutingDiagram(t, ctx, string(source), 1, nil)
			found := 0
			for _, edge := range diagram.Connections {
				id := edge.Src + " -> " + edge.Dst
				want, ok := tc.want[id]
				if ok {
					found++
					if !slices.EqualFunc(edge.Route, want, (*geo.Point).Equals) {
						t.Errorf("%s route = %v, want %v", id, geo.Points(edge.Route).ToString(), want.ToString())
					}
				}
			}
			if found != len(tc.want) {
				t.Fatalf("found %d of %d expected connections", found, len(tc.want))
			}
		})
	}
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
