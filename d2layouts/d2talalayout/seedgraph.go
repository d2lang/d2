package d2talalayout

import (
	"context"
	"fmt"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2target"

	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/engine"
	"github.com/d2lang/d2/d2layouts/d2talalayout/internal/layoutgraph"
)

// seedInput is an isolated TALA layout input with the D2 bindings needed to
// apply the selected result.
type seedInput struct {
	graph    *layoutgraph.Graph
	bindings translation
}

// newSeedInput translates a D2 graph into an isolated TALA input. It reads but
// never mutates graph.
func newSeedInput(ctx context.Context, graph *d2graph.Graph) (seedInput, error) {
	if err := ctx.Err(); err != nil {
		return seedInput{}, err
	}
	internal := layoutgraph.NewGraph()
	bindings, err := translateGraph(ctx, graph, internal, false)
	if err != nil {
		return seedInput{}, err
	}
	if hasPlainTextLoop(graph) {
		// A lone text label with one loop needs no competing ports. Use
		// its measured dimensions as the desired box on every seed.
		n := internal.Nodes[0]
		n.DesiredWidth, n.DesiredHeight = new(n.Width), new(n.Height)
	}
	// Every seed reads (clones and validates) the input at the same time.
	internal.MarkSharedInput()
	return seedInput{graph: internal, bindings: bindings}, nil
}

func hasPlainTextLoop(graph *d2graph.Graph) bool {
	if !graph.IsWholeLayout() || len(graph.Objects) != 1 || len(graph.Edges) != 1 || graph.Root.Shape.Value == d2target.ShapeHierarchy {
		return false
	}
	obj, e := graph.Objects[0], graph.Edges[0]
	if obj.Shape.Value != d2target.ShapeText || obj.Label.Value == "" || !plainCaptionSpacing(obj.Label.Value) || obj.Language != "" || obj.LabelPosition != nil ||
		len(obj.Classes) != 0 || obj.Left != nil || obj.Top != nil ||
		obj.Icon != nil || obj.Tooltip != nil || obj.Link != nil || obj.WidthAttr != nil || obj.HeightAttr != nil || obj.Style != (d2graph.Style{FontSize: obj.Style.FontSize}) {
		return false
	}
	return e.Src == obj && e.Dst == obj && e.SrcArrow != e.DstArrow && e.SrcArrowhead == nil && e.DstArrowhead == nil &&
		len(e.Classes) == 0 && e.Label.Value == "" && e.Tooltip == nil && e.Link == nil && !e.HasIcon() && e.Style == (d2graph.Style{})
}

// runSeed runs one TALA variation in a new workspace. Input remains unchanged
// and may be reused concurrently by other attempts.
func runSeed(ctx context.Context, input seedInput, seed int64) (*layoutgraph.Graph, error) {
	if input.graph == nil {
		return nil, fmt.Errorf("TALA seed graph is empty")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return engine.Layout(ctx, input.graph, engine.LayoutOptions{
		Seed: seed,
	})
}
