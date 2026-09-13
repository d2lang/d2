package d2cli

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/util-go/xmain"
)

const talaShortHelp = "TALA is D2's native layout and edge-routing engine."
const talaLongHelp = `TALA is D2's native layout and edge-routing engine for software architecture diagrams.

Diagram data under tala-seeds takes precedence over the command-line flag.
`

func registerTALAFlags(opts *xmain.Opts) {
	opts.Int64Slice("", "tala-seeds", "", d2talalayout.DefaultOptions().Seeds, "random seeds for deterministic TALA layout attempts; the best complete result is selected.")
}

func talaLayout(ms *xmain.State) (d2graph.LayoutGraph, error) {
	opts := d2talalayout.DefaultOptions()
	if hasLayoutFlag(ms, "tala-seeds") {
		var err error
		// GetInt64Slice returns an owned slice, independent of the flag value.
		opts.Seeds, err = ms.Opts.Flags.GetInt64Slice("tala-seeds")
		if err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context, g *d2graph.Graph) error {
		return d2talalayout.Layout(ctx, g, &opts)
	}, nil
}
