//go:build !noelk

package d2cli

import (
	"context"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
)

func TestELKLayoutOptions(t *testing.T) {
	wantOpts := d2elklayout.ConfigurableOpts{
		Algorithm:       "layered",
		NodeSpacing:     123,
		Padding:         "[top=11,left=22,bottom=33,right=44]",
		EdgeNodeSpacing: 67,
		EdgeEdgeSpacing: go2.Pointer(78),
		SelfLoopSpacing: 89,
		LayeredOptions:  d2elklayout.LayeredOptions{NodeNode: go2.Pointer(35.5), EdgeNode: go2.Pointer(16.), EdgeEdge: go2.Pointer(22.), EdgeRouting: "POLYLINE", WrappingStrategy: "OFF", ForceNodeModelOrder: true, FeedbackEdges: true, MergeEdges: true},
	}
	want := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraphJSON(t, d2elklayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured ELK spacing")
	}
	state := layoutOptionsState(t,
		"--elk-algorithm=layered", "--elk-nodeNodeBetweenLayers=123",
		"--elk-padding=[top=11,left=22,bottom=33,right=44]",
		"--elk-edgeNodeBetweenLayers=67", "--elk-edgeEdgeBetweenLayers=78",
		"--elk-nodeSelfLoop=89", "--elk-nodeNode=35.5", "--elk-edgeNode=16", "--elk-edgeEdge=22",
		"--elk-edgeRouting=polyline", "--elk-wrappingStrategy=off", "--elk-forceNodeModelOrder", "--elk-feedbackEdges", "--elk-mergeEdges")
	resolve := LayoutResolver(t.Context(), state)
	if err := state.Opts.Flags.Set("elk-nodeNodeBetweenLayers", "234"); err != nil {
		t.Fatal(err)
	}
	layout, err := resolve("elk")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("ELK command-line options differ from native options")
	}
	cached, err := resolve("ELK")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, cached); got != want {
		t.Fatal("changing CLI flags changed an already-resolved ELK layout")
	}
	other, err := LayoutResolver(t.Context(), state)("elk")
	if err != nil {
		t.Fatal(err)
	}
	wantOpts.NodeSpacing = 234
	otherWant := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &wantOpts)
	})
	if got := layoutGraphJSON(t, other); got != otherWant {
		t.Fatal("new ELK resolver did not capture changed options")
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("creating another ELK resolver changed the first")
	}
	defaultLayout, err := LayoutResolver(t.Context(), nil)("elk")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, defaultLayout); got != defaults {
		t.Fatal("nil CLI state did not select ELK defaults")
	}
}

// Exercise the public CLI, D2 configuration, and SVG renderer together.
func TestELKMergeEdgesDiagramPrecedence(t *testing.T) {
	dir := t.TempDir()
	const fan = "direction: right\nGateway -> Orders\nGateway -> Billing\nGateway -> Events\n"
	render := func(config string, flags ...string) []string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "diagram.d2"), []byte(config+fan), 0600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"--layout=elk"}, flags...)
		svg, err := runLayoutCLI(t, dir, nil, append(args, "diagram.d2", "-")...)
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		decoder := xml.NewDecoder(strings.NewReader(svg))
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			start, ok := token.(xml.StartElement)
			if !ok || start.Name.Local != "path" {
				continue
			}
			var class, d string
			for _, attr := range start.Attr {
				switch attr.Name.Local {
				case "class":
					class = attr.Value
				case "d":
					d = attr.Value
				}
			}
			if slices.Contains(strings.Fields(class), "connection") {
				paths = append(paths, d)
			}
		}
		if len(paths) != 3 {
			t.Fatalf("got %d connection paths, want 3", len(paths))
		}
		return paths
	}
	const enable = "vars: {d2-config: {data: {elk-mergeEdges: true}}}\n"
	const disable = "vars: {d2-config: {data: {elk-mergeEdges: false}}}\n"
	separate := render("")
	shared := render("", "--elk-mergeEdges")
	if slices.Equal(shared, separate) {
		t.Fatal("merge flag did not change SVG paths")
	}
	if !slices.Equal(render(enable, "--elk-mergeEdges=false"), shared) || !slices.Equal(render(disable, "--elk-mergeEdges"), separate) {
		t.Fatal("diagram configuration did not override the CLI flag")
	}
	if _, err := runLayoutCLI(t, dir, nil, "--layout=elk", "--elk-edgeRouting=unsupported", "diagram.d2", "-"); err == nil || !strings.Contains(err.Error(), "elk-edgeRouting") {
		t.Fatalf("invalid routing diagnostic: %v", err)
	}
}
