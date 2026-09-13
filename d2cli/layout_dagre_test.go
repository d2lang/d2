//go:build !nodagre

package d2cli

import (
	"context"
	"testing"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
)

func TestDagreLayoutOptions(t *testing.T) {
	wantOpts := d2dagrelayout.ConfigurableOpts{NodeSep: 123, EdgeSep: 67}
	want := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &wantOpts)
	})
	defaults := layoutGraphJSON(t, d2dagrelayout.DefaultLayout)
	if defaults == want {
		t.Fatal("test graph does not exercise configured Dagre spacing")
	}
	state := layoutOptionsState(t, "--dagre-nodesep=123", "--dagre-edgesep=67")
	resolve := LayoutResolver(t.Context(), state)
	if err := state.Opts.Flags.Set("dagre-nodesep", "234"); err != nil {
		t.Fatal(err)
	}
	layout, err := resolve("dagre")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("Dagre command-line spacing differs from native options")
	}
	cached, err := resolve("DAGRE")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, cached); got != want {
		t.Fatal("changing CLI flags changed an already-resolved Dagre layout")
	}
	other, err := LayoutResolver(t.Context(), state)("dagre")
	if err != nil {
		t.Fatal(err)
	}
	wantOpts.NodeSep = 234
	otherWant := layoutGraphJSON(t, func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &wantOpts)
	})
	if got := layoutGraphJSON(t, other); got != otherWant {
		t.Fatal("new Dagre resolver did not capture changed options")
	}
	if got := layoutGraphJSON(t, layout); got != want {
		t.Fatal("creating another Dagre resolver changed the first")
	}
	defaultLayout, err := LayoutResolver(t.Context(), nil)("dagre")
	if err != nil {
		t.Fatal(err)
	}
	if got := layoutGraphJSON(t, defaultLayout); got != defaults {
		t.Fatal("nil CLI state did not select Dagre defaults")
	}
}
