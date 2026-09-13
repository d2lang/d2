//go:build !nodagre

package d2cli

import (
	"context"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2dagrelayout"
	"github.com/d2lang/util-go/xmain"
)

const dagreEnabled = true
const dagreShortHelp = "The directed graph layout library Dagre"
const dagreLongHelp = `dagre is a directed graph layout algorithm implemented natively in Go by Dagro.
See https://d2lang.com/tour/dagre for more.

Dagro implements the Dagre 3.1.1 layout surface used by D2: https://github.com/d2lang/dagro.

Flags correspond to ones found at https://github.com/dagrejs/dagre/wiki.
`

func registerDagreFlags(opts *xmain.Opts) {
	opts.Int64("", "dagre-nodesep", "", int64(d2dagrelayout.DefaultOpts.NodeSep), "number of pixels that separate nodes horizontally.")
	opts.Int64("", "dagre-edgesep", "", int64(d2dagrelayout.DefaultOpts.EdgeSep), "number of pixels that separate edges horizontally.")
}

func dagreLayout(ms *xmain.State) (d2graph.LayoutGraph, error) {
	opts := d2dagrelayout.DefaultOpts
	var err error
	opts.NodeSep, err = layoutIntFlag(ms, "dagre-nodesep", opts.NodeSep)
	if err != nil {
		return nil, err
	}
	opts.EdgeSep, err = layoutIntFlag(ms, "dagre-edgesep", opts.EdgeSep)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, g *d2graph.Graph) error {
		return d2dagrelayout.Layout(ctx, g, &opts)
	}, nil
}
