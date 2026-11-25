package d2elklayout

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/d2lang/d2/d2graph"
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

// AlgorithmOptions tune the algorithm selected at the diagram root.
// An omitted value lets the selected ELK provider use its own default.
type AlgorithmOptions struct {
	ForceModel              string   `json:"elk.force.model,omitempty"`
	ForceRepulsion          *float64 `json:"elk.force.repulsion,omitempty"`
	StressDesiredEdgeLength *float64 `json:"elk.stress.desiredEdgeLength,omitempty"`
	MrTreeSearchOrder       string   `json:"elk.mrtree.searchOrder,omitempty"`
	MrTreeWeighting         string   `json:"elk.mrtree.weighting,omitempty"`
	RadialRadius            *float64 `json:"elk.radial.radius,omitempty"`
	RadialWedgeCriteria     string   `json:"elk.radial.wedgeCriteria,omitempty"`
	RadialCompactor         string   `json:"elk.radial.compactor,omitempty"`
}

func (opts *ConfigurableOpts) usesAlgorithm(name string) bool {
	if opts.Algorithm == "" {
		return name == "layered"
	}
	return opts.Algorithm == name || strings.HasSuffix(opts.Algorithm, "."+name)
}

// parentOptionsInto returns the layered options of the root (root set) or of
// a container, storing the default edge-node spacing, when it sets one, in
// edgeNode.
func (opts *ConfigurableOpts) parentOptionsInto(root bool, edgeNode *float64) LayeredOptions {
	o := opts.LayeredOptions
	// The 40px clearance belongs to D2's layered profile. Mr. Tree spaces
	// levels 20px apart and needs its own default for routing between them.
	if o.EdgeNode == nil && (!root || opts.usesAlgorithm("layered")) {
		*edgeNode = float64(edge_node_spacing)
		o.EdgeNode = edgeNode
	}
	// D2 disables model-order processing below the root for compound graphs.
	o.ForceNodeModelOrder = root && o.ForceNodeModelOrder
	return o
}

var diagramOptions = []string{
	"elk-algorithm", "elk-forceModel", "elk-forceRepulsion", "elk-stressDesiredEdgeLength",
	"elk-mrtreeSearchOrder", "elk-mrtreeWeighting", "elk-radialRadius", "elk-radialWedgeCriteria", "elk-radialCompactor",
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
	case "elk-algorithm":
		opts.Algorithm = algorithmName(value)
	case "elk-forceModel":
		opts.ForceModel, err = optionEnum(value, "fruchterman_reingold", "eades")
	case "elk-forceRepulsion":
		opts.ForceRepulsion, err = parseNumber(name, value)
	case "elk-stressDesiredEdgeLength":
		opts.StressDesiredEdgeLength, err = parseNumber(name, value)
	case "elk-mrtreeSearchOrder":
		opts.MrTreeSearchOrder, err = optionEnum(value, "dfs", "bfs")
	case "elk-mrtreeWeighting":
		opts.MrTreeWeighting, err = optionEnum(value, "model_order", "descendants", "fan")
	case "elk-radialRadius":
		opts.RadialRadius, err = parseNumber(name, value)
	case "elk-radialWedgeCriteria":
		opts.RadialWedgeCriteria, err = optionEnum(value, "node_size", "leaf_number")
	case "elk-radialCompactor":
		opts.RadialCompactor, err = optionEnum(value, "none", "radial_compaction", "wedge_compaction")
	case "elk-nodeNode":
		opts.NodeNode, err = parseNumber(name, value)
	case "elk-edgeNode":
		opts.EdgeNode, err = parseNumber(name, value)
	case "elk-edgeEdge":
		opts.EdgeEdge, err = parseNumber(name, value)
	case "elk-edgeRouting":
		opts.EdgeRouting, err = optionEnum(value, "orthogonal", "polyline")
	case "elk-wrappingStrategy":
		opts.WrappingStrategy, err = optionEnum(value, "off", "single_edge", "multi_edge")
	case "elk-forceNodeModelOrder":
		opts.ForceNodeModelOrder, err = strconv.ParseBool(strings.ToLower(value))
	case "elk-feedbackEdges":
		opts.FeedbackEdges, err = strconv.ParseBool(strings.ToLower(value))
	case "elk-mergeEdges":
		opts.MergeEdges, err = strconv.ParseBool(strings.ToLower(value))
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

func parseNumber(name, value string) (*float64, error) {
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, err
	}
	if err := validateNumber(name, n); err != nil {
		return nil, err
	}
	return &n, nil
}

func validateNumber(name string, n float64) error {
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return fmt.Errorf("expected a finite number")
	}
	if name == "elk-stressDesiredEdgeLength" && n <= 0 {
		return fmt.Errorf("desired edge length must be positive")
	}
	if n < 0 {
		return fmt.Errorf("expected a nonnegative number")
	}
	return nil
}

func algorithmName(value string) string {
	id := strings.ToLower(value)
	// ELK accepts short names and qualified IDs. Its two SPOrE IDs use camel case.
	start := strings.LastIndexByte(id, '.') + 1
	switch id[start:] {
	case "sporeoverlap":
		return id[:start] + "sporeOverlap"
	case "sporecompaction":
		return id[:start] + "sporeCompaction"
	}
	return id
}

func optionEnum(value string, allowed ...string) (string, error) {
	v := strings.ToLower(value)
	if slices.Contains(allowed, v) {
		return strings.ToUpper(v), nil // ELK's layout API uses uppercase enum names.
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
	for _, opt := range []struct {
		name  string
		value *float64
	}{
		{"elk-nodeNode", opts.NodeNode},
		{"elk-edgeNode", opts.EdgeNode},
		{"elk-edgeEdge", opts.EdgeEdge},
		{"elk-forceRepulsion", opts.ForceRepulsion},
		{"elk-stressDesiredEdgeLength", opts.StressDesiredEdgeLength},
		{"elk-radialRadius", opts.RadialRadius},
	} {
		if opt.value != nil {
			if err := validateNumber(opt.name, *opt.value); err != nil {
				return nil, fmt.Errorf("%s: %w", opt.name, err)
			}
		}
	}
	for _, option := range []struct{ name, value string }{
		{"elk-algorithm", opts.Algorithm}, {"elk-forceModel", opts.ForceModel},
		{"elk-mrtreeSearchOrder", opts.MrTreeSearchOrder}, {"elk-mrtreeWeighting", opts.MrTreeWeighting},
		{"elk-radialWedgeCriteria", opts.RadialWedgeCriteria}, {"elk-radialCompactor", opts.RadialCompactor},
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

// graphOptionsMemo remembers graphOptions' result for the last defaults it
// was given with a graph without diagram options. graphOptions copies the
// defaults, validates the values their float pointers point to and
// normalizes string fields, keeping the defaults' pointers: its result is a
// function of the defaults' fields, of whether each pointer is set and of
// the values they point to, except for the pointers themselves, which are
// the defaults'. So for defaults equal in those, the result is the memo's
// with the defaults' pointers.
//
// The memo outlives Layout calls in the scratch (a slot that garbage
// collection never empties), so it keeps no pointer and no string of the
// caller's: input and result hold copies of the strings (see owned), and
// their pointer fields are markers (see markPointers), except that result's
// are the caller's during the call that took it (release marks them again).
type graphOptionsMemo struct {
	valid    bool
	input    ConfigurableOpts
	pointees [6]uint64
	result   ConfigurableOpts
	// generation numbers the results stored in result (from 1): equal
	// generations of one memo mean equal results.
	generation uint64
}

// hasDiagramOptions reports whether g.Data holds any diagram option.
func hasDiagramOptions(g *d2graph.Graph) bool {
	if len(g.Data) == 0 {
		return false
	}
	for _, name := range diagramOptions {
		if _, ok := g.Data[name]; ok {
			return true
		}
	}
	return false
}

func optionPointees(opts *ConfigurableOpts) (bits [6]uint64) {
	for i, value := range [...]*float64{opts.NodeNode, opts.EdgeNode, opts.EdgeEdge, opts.ForceRepulsion,
		opts.StressDesiredEdgeLength, opts.RadialRadius} {
		if value != nil {
			bits[i] = math.Float64bits(*value)
		}
	}
	return bits
}

// of is graphOptions(g, defaults); the result is only read.
func (memo *graphOptionsMemo) of(g *d2graph.Graph, defaults *ConfigurableOpts) (*ConfigurableOpts, error) {
	if defaults == nil {
		defaults = &DefaultOpts
	}
	plain := !hasDiagramOptions(g)
	if plain && memo.valid {
		input := *defaults
		input.markPointers()
		if input == memo.input && optionPointees(defaults) == memo.pointees {
			memo.result.setPointers(defaults)
			return &memo.result, nil
		}
	}
	opts, err := graphOptions(g, defaults)
	if err == nil && plain && optionTextBytes(defaults) <= maxOwnedOptionText && optionTextBytes(opts) <= maxOwnedOptionText {
		memo.valid, memo.input, memo.pointees, memo.result = true, owned(*defaults), optionPointees(defaults), owned(*opts)
		memo.generation++
	}
	return opts, err
}

// release drops the caller's pointers from the memo's result.
func (memo *graphOptionsMemo) release() {
	memo.result.markPointers()
}

// maxOwnedOptionText bounds the text of the option structs the scratch's
// caches copy and keep (longer ones are not remembered).
const maxOwnedOptionText = 1 << 10

// presentFloat and presentInt are what the pointer fields of option structs
// that caches keep point to when set (see markPointers).
var (
	presentFloat float64
	presentInt   int
)

// markPointers replaces every pointer field that is set by a pointer to
// presentFloat or presentInt: what is left of a pointer is whether it is
// set (its value is recorded separately).
func (c *ConfigurableOpts) markPointers() {
	for _, field := range [...]**float64{&c.NodeNode, &c.EdgeNode, &c.EdgeEdge, &c.ForceRepulsion,
		&c.StressDesiredEdgeLength, &c.RadialRadius} {
		if *field != nil {
			*field = &presentFloat
		}
	}
	if c.EdgeEdgeSpacing != nil {
		c.EdgeEdgeSpacing = &presentInt
	}
}

// setPointers sets c's pointer fields to those of other.
func (c *ConfigurableOpts) setPointers(other *ConfigurableOpts) {
	c.NodeNode, c.EdgeNode, c.EdgeEdge = other.NodeNode, other.EdgeNode, other.EdgeEdge
	c.ForceRepulsion, c.StressDesiredEdgeLength, c.RadialRadius = other.ForceRepulsion, other.StressDesiredEdgeLength, other.RadialRadius
	c.EdgeEdgeSpacing = other.EdgeEdgeSpacing
}

// stringFields returns pointers to every string field of c.
func (c *ConfigurableOpts) stringFields() [9]*string {
	return [...]*string{&c.EdgeRouting, &c.WrappingStrategy, &c.ForceModel, &c.MrTreeSearchOrder,
		&c.MrTreeWeighting, &c.RadialWedgeCriteria, &c.RadialCompactor, &c.Algorithm, &c.Padding}
}

// optionTextBytes is the length of c's strings together.
func optionTextBytes(c *ConfigurableOpts) int {
	total := 0
	for _, field := range c.stringFields() {
		total += len(*field)
	}
	return total
}

// owned is c with marked pointers and strings of its own (see ownString).
func owned(c ConfigurableOpts) ConfigurableOpts {
	c.markPointers()
	for _, field := range c.stringFields() {
		ownString(field)
	}
	return c
}

// ownString replaces *s by a string that refers to nothing of the caller's:
// the constant of the same text for the texts D2 sends most, else a copy.
func ownString(s *string) {
	if known, ok := knownString(*s); ok {
		*s = known
	} else {
		*s = strings.Clone(*s)
	}
}

// resultGeneration is the generation of opts when it is the memo's result
// (as of returns on a hit), else 0.
func (memo *graphOptionsMemo) resultGeneration(opts *ConfigurableOpts) uint64 {
	if opts == &memo.result {
		return memo.generation
	}
	return 0
}
