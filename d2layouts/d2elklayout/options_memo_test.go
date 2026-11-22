package d2elklayout

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/d2lang/util-go/go2"

	"github.com/d2lang/d2/d2graph"
)

// TestGraphOptionsMemoMatches runs graphOptionsMemo.of and graphOptions on
// the same inputs, the defaults changing between calls (fields, pointer
// targets, invalid values) and graphs with and without diagram options.
func TestGraphOptionsMemoMatches(t *testing.T) {
	var memo graphOptionsMemo
	spacing := 12.0
	custom := DefaultOpts
	custom.NodeNode = &spacing
	custom.Algorithm = "mrtree"
	plain := &d2graph.Graph{}
	withData := &d2graph.Graph{Data: map[string]any{"elk-nodeNode": 3.5}}
	steps := []struct {
		g        *d2graph.Graph
		defaults *ConfigurableOpts
		mutate   func()
	}{
		{plain, nil, nil},
		{plain, nil, nil},
		{plain, &custom, nil},
		{plain, &custom, nil},
		{plain, &custom, func() { spacing = 7 }},
		{plain, &custom, nil},
		{plain, &custom, func() { spacing = math.NaN() }},
		{plain, &custom, func() { spacing = -1 }},
		{plain, &custom, func() { spacing = 4 }},
		{withData, &custom, nil},
		{plain, &custom, func() { custom.EdgeRouting = "polyline" }},
		{plain, &custom, func() { custom.EdgeEdgeSpacing = go2.Pointer(3) }},
		{plain, &custom, func() { *custom.EdgeEdgeSpacing = 9 }},
		{plain, nil, nil},
		{withData, nil, nil},
		{plain, nil, nil},
	}
	for i, step := range steps {
		if step.mutate != nil {
			step.mutate()
		}
		want, wantErr := graphOptions(step.g, step.defaults)
		got, gotErr := memo.of(step.g, step.defaults)
		if fmt.Sprint(wantErr) != fmt.Sprint(gotErr) {
			t.Fatalf("step %d: error %v, want %v", i, gotErr, wantErr)
		}
		if wantErr == nil && !reflect.DeepEqual(*got, *want) {
			t.Fatalf("step %d: %+v, want %+v", i, *got, *want)
		}
	}
}
