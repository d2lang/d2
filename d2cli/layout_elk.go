//go:build !noelk

package d2cli

import (
	"context"
	"strings"

	"github.com/spf13/pflag"

	"github.com/d2lang/util-go/go2"
	"github.com/d2lang/util-go/xmain"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2elklayout"
)

const elkEnabled = true
const elkShortHelp = "Eclipse Layout Kernel (ELK) with the Layered algorithm."
const elkLongHelp = `ELK is a layout engine offered by Eclipse.
Originally written in Java, D2's ELK.js 0.12.0 layout profile is bundled through the native Go elk-go port. Layered remains the default.
See https://d2lang.com/tour/elk for more.

Set spacing, routing, wrapping, ordering and edge merging in vars.d2-config.data
using the flag name without "--" (for example, elk-mergeEdges: true).
Diagram data takes precedence over the flag. These option values are case-insensitive.
Set elk-layer-constraint on an individual shape to first, last or none.

Flags correspond to ones found at https://eclipse.dev/elk/reference.html.
`

var elkLayeredFlags = []struct {
	name  string
	value any
	help  string
}{
	{"elk-nodeNode", 20., "minimum spacing between nodes in the same layer"},
	{"elk-edgeNode", 40., "minimum clearance between a passing edge and a node in the same layer"},
	{"elk-edgeEdge", 10., "minimum spacing between edges passing through the same layer"},
	{"elk-edgeRouting", "orthogonal", "edge routing style: orthogonal or polyline"},
	{"elk-wrappingStrategy", "off", "graph wrapping strategy: off, single_edge or multi_edge"},
	{"elk-forceNodeModelOrder", false, "keep declaration order within layers at the diagram root"},
	{"elk-feedbackEdges", false, "route backward edges around the main flow"},
	{"elk-mergeEdges", false, "share attachment points for ordinary incoming or outgoing edges"},
}

// pflag's bool parser accepts only some capitalizations of true and false.
type elkBoolValue struct{ pflag.Value }

func (v elkBoolValue) Set(s string) error { return v.Value.Set(strings.ToLower(s)) }
func (elkBoolValue) IsBoolFlag() bool     { return true }

func registerELKFlags(opts *xmain.Opts) {
	opts.String("", "elk-algorithm", "", d2elklayout.DefaultOpts.Algorithm, "layout algorithm")
	opts.Int64("", "elk-nodeNodeBetweenLayers", "", int64(d2elklayout.DefaultOpts.NodeSpacing), "the spacing to be preserved between any pair of nodes of two adjacent layers")
	opts.String("", "elk-padding", "", d2elklayout.DefaultOpts.Padding, "the padding to be left to a parent element’s border when placing child elements")
	opts.Int64("", "elk-edgeNodeBetweenLayers", "", int64(d2elklayout.DefaultOpts.EdgeNodeSpacing), "the spacing to be preserved between nodes and edges that are routed next to the node’s layer")
	opts.Int64("", "elk-edgeEdgeBetweenLayers", "", int64(*d2elklayout.DefaultOpts.EdgeEdgeSpacing), "the spacing to be preserved between pairs of edges routed between the same pair of layers")
	opts.Int64("", "elk-nodeSelfLoop", "", int64(d2elklayout.DefaultOpts.SelfLoopSpacing), "spacing to be preserved between a node and its self loops")
	for _, f := range elkLayeredFlags {
		switch v := f.value.(type) {
		case float64:
			opts.Flags.Float64(f.name, v, f.help)
		case bool:
			opts.Flags.Bool(f.name, v, f.help)
			flag := opts.Flags.Lookup(f.name)
			flag.Value = elkBoolValue{flag.Value}
		case string:
			opts.Flags.String(f.name, v, f.help)
		}
	}
}

func elkLayout(ms *xmain.State) (d2graph.LayoutGraph, error) {
	opts := d2elklayout.DefaultOpts
	var err error
	opts.Algorithm, err = layoutStringFlag(ms, "elk-algorithm", opts.Algorithm)
	if err != nil {
		return nil, err
	}
	opts.NodeSpacing, err = layoutIntFlag(ms, "elk-nodeNodeBetweenLayers", opts.NodeSpacing)
	if err != nil {
		return nil, err
	}
	opts.Padding, err = layoutStringFlag(ms, "elk-padding", opts.Padding)
	if err != nil {
		return nil, err
	}
	opts.EdgeNodeSpacing, err = layoutIntFlag(ms, "elk-edgeNodeBetweenLayers", opts.EdgeNodeSpacing)
	if err != nil {
		return nil, err
	}
	edgeEdgeSpacing, err := layoutIntFlag(ms, "elk-edgeEdgeBetweenLayers", *opts.EdgeEdgeSpacing)
	if err != nil {
		return nil, err
	}
	opts.EdgeEdgeSpacing = go2.Pointer(edgeEdgeSpacing)
	opts.SelfLoopSpacing, err = layoutIntFlag(ms, "elk-nodeSelfLoop", opts.SelfLoopSpacing)
	if err != nil {
		return nil, err
	}
	for _, f := range elkLayeredFlags {
		if hasLayoutFlag(ms, f.name) && ms.Opts.Flags.Changed(f.name) {
			if err := opts.SetOption(f.name, ms.Opts.Flags.Lookup(f.name).Value.String()); err != nil {
				return nil, xmain.UsageErrorf("%v", err)
			}
		}
	}
	return func(ctx context.Context, g *d2graph.Graph) error {
		return d2elklayout.Layout(ctx, g, &opts)
	}, nil
}
