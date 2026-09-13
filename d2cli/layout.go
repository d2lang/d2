package d2cli

import (
	"context"
	"strings"

	"github.com/spf13/pflag"

	"github.com/d2lang/d2/d2graph"
	"github.com/d2lang/d2/d2layouts/d2talalayout"
	"github.com/d2lang/util-go/xmain"
)

func builtinLayoutNames() []string {
	names := make([]string, 0, 3)
	if dagreEnabled {
		names = append(names, "dagre")
	}
	if elkEnabled {
		names = append(names, "elk")
	}
	return append(names, "tala")
}

func isBuiltinLayout(name string) bool {
	switch strings.ToLower(name) {
	case "dagre":
		return dagreEnabled
	case "elk":
		return elkEnabled
	case "tala":
		return true
	default:
		return false
	}
}

func getBuiltinLayout(name string, ms *xmain.State) (d2graph.LayoutGraph, error) {
	switch strings.ToLower(name) {
	case "dagre":
		return dagreLayout(ms)
	case "elk":
		return elkLayout(ms)
	case "tala":
		return talaLayout(ms)
	default:
		return nil, layoutNotFound(name)
	}
}

// LayoutResolver captures the built-in engines' typed options for one compilation.
// Custom Go layouts can be supplied directly to d2lib.CompileOptions.LayoutResolver.
func LayoutResolver(_ context.Context, ms *xmain.State) func(string) (d2graph.LayoutGraph, error) {
	type result struct {
		layout d2graph.LayoutGraph
		err    error
	}
	layouts := make(map[string]result, 3)
	for _, name := range builtinLayoutNames() {
		layout, err := getBuiltinLayout(name, ms)
		layouts[name] = result{layout, err}
	}
	// The map and bound options are read-only after construction.
	return func(name string) (d2graph.LayoutGraph, error) {
		r, ok := layouts[strings.ToLower(name)]
		if !ok {
			return nil, layoutNotFound(name)
		}
		return r.layout, r.err
	}
}

// RouterResolver selects the built-in engine's optional edge router.
func RouterResolver(_ context.Context, _ *xmain.State) func(string) (d2graph.RouteEdges, error) {
	return func(name string) (d2graph.RouteEdges, error) {
		if !isBuiltinLayout(name) {
			return nil, layoutNotFound(name)
		}
		if strings.EqualFold(name, "tala") {
			return d2talalayout.RouteEdges, nil
		}
		return nil, nil
	}
}

func populateLayoutOpts(ms *xmain.State) {
	opts := xmain.NewOpts(nil, nil)
	registerDagreFlags(opts)
	registerELKFlags(opts)
	registerTALAFlags(opts)
	opts.Flags.VisitAll(func(flag *pflag.Flag) {
		// Engine options are documented by "d2 layout <name>".
		flag.Hidden = true
		ms.Opts.Flags.AddFlag(flag)
	})
}

func hasLayoutFlag(ms *xmain.State, name string) bool {
	return ms != nil && ms.Opts != nil && ms.Opts.Flags != nil && ms.Opts.Flags.Lookup(name) != nil
}

func layoutIntFlag(ms *xmain.State, name string, fallback int) (int, error) {
	if !hasLayoutFlag(ms, name) {
		return fallback, nil
	}
	value, err := ms.Opts.Flags.GetInt64(name)
	if err != nil {
		return 0, err
	}
	if int64(int(value)) != value {
		return 0, xmain.UsageErrorf("--%s is out of range for this platform", name)
	}
	return int(value), nil
}

func layoutStringFlag(ms *xmain.State, name, fallback string) (string, error) {
	if !hasLayoutFlag(ms, name) {
		return fallback, nil
	}
	return ms.Opts.Flags.GetString(name)
}
