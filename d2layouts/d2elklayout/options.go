package d2elklayout

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/util-go/go2"
)

// LayeredOptions are applied to parent graphs. Nil spacing values keep the
// layout defaults, and an explicit zero requests zero clearance.
type LayeredOptions struct {
	NodeNode            *float64 `json:"elk.spacing.nodeNode,omitempty"`
	EdgeNode            *float64 `json:"elk.spacing.edgeNode,omitempty"`
	EdgeEdge            *float64 `json:"elk.spacing.edgeEdge,omitempty"`
	EdgeRouting         string   `json:"elk.edgeRouting,omitempty"`
	WrappingStrategy    string   `json:"elk.layered.wrapping.strategy,omitempty"`
	ForceNodeModelOrder bool     `json:"elk.layered.crossingMinimization.forceNodeModelOrder,omitempty"`
	FeedbackEdges       bool     `json:"elk.layered.feedbackEdges,omitempty"`
	MergeEdges          bool     `json:"elk.layered.mergeEdges,omitempty"`
}

func (opts *ConfigurableOpts) parentOptions(root bool) LayeredOptions {
	o := opts.LayeredOptions
	if o.EdgeNode == nil {
		o.EdgeNode = go2.Pointer(float64(edge_node_spacing))
	}
	// D2 disables model-order processing below the root for compound graphs.
	o.ForceNodeModelOrder = root && o.ForceNodeModelOrder
	return o
}

var diagramOptions = []string{
	"elk-nodeNode", "elk-edgeNode", "elk-edgeEdge", "elk-edgeRouting",
	"elk-wrappingStrategy", "elk-forceNodeModelOrder", "elk-feedbackEdges", "elk-mergeEdges",
	"elk-nodeNodeBetweenLayers", "elk-edgeNodeBetweenLayers", "elk-edgeEdgeBetweenLayers",
	"elk-nodeSelfLoop", "elk-padding",
}

// SetOption sets one ELK diagram option using its CLI name and textual value.
func (options *ConfigurableOpts) SetOption(name, value string) error {
	opts := *options
	var err error
	switch name {
	case "elk-nodeNode":
		opts.NodeNode, err = parseSpacing(value)
	case "elk-edgeNode":
		opts.EdgeNode, err = parseSpacing(value)
	case "elk-edgeEdge":
		opts.EdgeEdge, err = parseSpacing(value)
	case "elk-edgeRouting":
		opts.EdgeRouting, err = optionEnum(value, "ORTHOGONAL", "POLYLINE")
	case "elk-wrappingStrategy":
		opts.WrappingStrategy, err = optionEnum(value, "OFF", "SINGLE_EDGE", "MULTI_EDGE")
	case "elk-forceNodeModelOrder":
		opts.ForceNodeModelOrder, err = strconv.ParseBool(value)
	case "elk-feedbackEdges":
		opts.FeedbackEdges, err = strconv.ParseBool(value)
	case "elk-mergeEdges":
		opts.MergeEdges, err = strconv.ParseBool(value)
	case "elk-nodeNodeBetweenLayers", "elk-edgeNodeBetweenLayers", "elk-edgeEdgeBetweenLayers", "elk-nodeSelfLoop":
		n, parseErr := strconv.Atoi(value)
		if parseErr != nil || n < 0 {
			err = fmt.Errorf("expected a nonnegative integer")
			break
		}
		switch name {
		case "elk-nodeNodeBetweenLayers":
			opts.NodeSpacing = n
		case "elk-edgeNodeBetweenLayers":
			opts.EdgeNodeSpacing = n
		case "elk-edgeEdgeBetweenLayers":
			opts.EdgeEdgeSpacing = &n
		case "elk-nodeSelfLoop":
			opts.SelfLoopSpacing = n
		}
	case "elk-padding":
		padding, parseErr := readPadding(value)
		if parseErr != nil {
			err = parseErr
		} else {
			opts.Padding = padding.String()
		}
	default:
		return fmt.Errorf("unknown ELK option %q", name)
	}
	if err != nil {
		return fmt.Errorf("%s=%q: %w", name, value, err)
	}
	*options = opts
	return nil
}

func parseSpacing(value string) (*float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil, fmt.Errorf("expected a finite, nonnegative spacing")
	}
	return &n, nil
}

func optionEnum(value string, allowed ...string) (string, error) {
	v := strings.ToUpper(value)
	if slices.Contains(allowed, v) {
		return v, nil
	}
	return "", fmt.Errorf("expected %s", strings.Join(allowed, " or "))
}

func graphOptions(g *d2graph.Graph, defaults *ConfigurableOpts) (*ConfigurableOpts, error) {
	if defaults == nil {
		defaults = &DefaultOpts
	}
	opts := *defaults
	for _, name := range diagramOptions {
		if raw, ok := g.Data[name]; ok {
			switch raw.(type) {
			case string, bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			default:
				return nil, fmt.Errorf("%s expects a scalar value", name)
			}
			if err := opts.SetOption(name, fmt.Sprint(raw)); err != nil {
				return nil, err
			}
		}
	}
	// Go callers can also set the typed fields directly.
	for _, v := range []*float64{opts.NodeNode, opts.EdgeNode, opts.EdgeEdge} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return nil, fmt.Errorf("ELK spacing must be finite and nonnegative")
		}
	}
	for _, option := range []struct{ name, value string }{
		{"elk-edgeRouting", opts.EdgeRouting}, {"elk-wrappingStrategy", opts.WrappingStrategy},
	} {
		if option.value != "" {
			if err := opts.SetOption(option.name, option.value); err != nil {
				return nil, err
			}
		}
	}
	return &opts, nil
}
